// Package redisotp stores short-lived verification challenges and rate limits
// in Redis.
package redisotp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/redis/go-redis/v9"
)

var issueScript = redis.NewScript(`
local email_count = tonumber(redis.call("GET", KEYS[3]) or "0")
local ip_count = tonumber(redis.call("GET", KEYS[4]) or "0")
local max_email = tonumber(ARGV[8])
local max_ip = tonumber(ARGV[9])

if email_count >= max_email or ip_count >= max_ip then
	return "rate"
end
if redis.call("EXISTS", KEYS[2]) == 1 then
	return "cooldown"
end

email_count = redis.call("INCR", KEYS[3])
if email_count == 1 then
	redis.call("PEXPIRE", KEYS[3], ARGV[7])
end
ip_count = redis.call("INCR", KEYS[4])
if ip_count == 1 then
	redis.call("PEXPIRE", KEYS[4], ARGV[7])
end

redis.call(
	"HSET",
	KEYS[1],
	"digest", ARGV[1],
	"attempts", ARGV[2],
	"purpose", ARGV[3],
	"owner", ARGV[4]
)
redis.call("PEXPIRE", KEYS[1], ARGV[5])
redis.call("SET", KEYS[2], "1", "PX", ARGV[6])
return "ok"
`)

var verifyScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 0 then
	return "expired"
end

local challenge = redis.call(
	"HMGET",
	KEYS[1],
	"digest",
	"attempts",
	"purpose",
	"owner"
)
local attempts = tonumber(challenge[2])
if not challenge[1] or not attempts or not challenge[3] or challenge[4] == false then
	return "corrupt"
end

if challenge[1] == ARGV[1] and challenge[3] == ARGV[2] and challenge[4] == ARGV[3] then
	redis.call("DEL", KEYS[1])
	return "ok"
end

attempts = attempts - 1
if attempts <= 0 then
	redis.call("DEL", KEYS[1])
else
	redis.call("HSET", KEYS[1], "attempts", attempts)
end
return "invalid"
`)

var deleteIfMatchScript = redis.NewScript(`
local digest = redis.call("HGET", KEYS[1], "digest")
if digest and digest == ARGV[1] then
	redis.call("DEL", KEYS[1], KEYS[2])
	return "deleted"
end
return "unchanged"
`)

var allowScript = redis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local maximum = tonumber(ARGV[1])
if current >= maximum then
	return "rate"
end

current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return "ok"
`)

type store struct {
	client redis.UniversalClient
	prefix string
}

var _ identity.OTPStore = (*store)(nil)

// New constructs an atomic Redis-backed OTP store. EmailKey, IP and generic
// rate-limit keys are expected to be opaque digests produced by the
// application service, not raw identity or network values.
func New(client redis.UniversalClient, prefix string) identity.OTPStore {
	if isNilClient(client) {
		client = nil
	}
	return &store{
		client: client,
		prefix: strings.TrimRight(prefix, ":"),
	}
}

func (s *store) Issue(ctx context.Context, challenge identity.OTPChallenge) error {
	if err := validateChallenge(challenge); err != nil {
		return err
	}
	result, err := s.run(
		ctx,
		issueScript,
		[]string{
			s.challengeKey(challenge.EmailKey, challenge.Purpose),
			s.cooldownKey(challenge.EmailKey, challenge.Purpose),
			s.emailRateKey(challenge.EmailKey),
			s.ipRateKey(challenge.IP),
		},
		challenge.Digest,
		challenge.Attempts,
		challenge.Purpose,
		challenge.Owner,
		challenge.TTL.Milliseconds(),
		challenge.Cooldown.Milliseconds(),
		challenge.Window.Milliseconds(),
		challenge.MaxEmail,
		challenge.MaxIP,
	)
	if err != nil {
		return stateUnavailable("issue verification challenge")
	}

	switch result {
	case "ok":
		return nil
	case "cooldown", "rate":
		return identity.ErrRateLimited
	default:
		return stateUnavailable("issue verification challenge")
	}
}

func (s *store) Verify(ctx context.Context, attempt identity.OTPAttempt) error {
	if err := validateAttempt(attempt); err != nil {
		return err
	}
	result, err := s.run(
		ctx,
		verifyScript,
		[]string{s.challengeKey(attempt.EmailKey, attempt.Purpose)},
		attempt.Digest,
		attempt.Purpose,
		attempt.Owner,
	)
	if err != nil {
		return stateUnavailable("verify challenge")
	}

	switch result {
	case "ok":
		return nil
	case "invalid":
		return identity.ErrCodeInvalid
	case "expired":
		return identity.ErrCodeExpired
	default:
		return stateUnavailable("verify challenge")
	}
}

func (s *store) DeleteIfMatch(ctx context.Context, challenge identity.OTPChallenge) error {
	if challenge.EmailKey == "" {
		return invalidRequest("OTPChallenge.EmailKey is required")
	}
	if challenge.Digest == "" {
		return invalidRequest("OTPChallenge.Digest is required")
	}
	if challenge.Purpose == "" {
		return invalidRequest("OTPChallenge.Purpose is required")
	}
	result, err := s.run(
		ctx,
		deleteIfMatchScript,
		[]string{
			s.challengeKey(challenge.EmailKey, challenge.Purpose),
			s.cooldownKey(challenge.EmailKey, challenge.Purpose),
		},
		challenge.Digest,
	)
	if err != nil {
		return stateUnavailable("delete verification challenge")
	}
	switch result {
	case "deleted", "unchanged":
		return nil
	default:
		return stateUnavailable("delete verification challenge")
	}
}

func (s *store) Allow(ctx context.Context, limit identity.RateLimit) error {
	if limit.Key == "" {
		return invalidRequest("RateLimit.Key is required")
	}
	if limit.Max <= 0 {
		return invalidRequest("RateLimit.Max must be positive")
	}
	if limit.Window < time.Millisecond {
		return invalidRequest("RateLimit.Window must be at least one millisecond")
	}
	result, err := s.run(
		ctx,
		allowScript,
		[]string{s.genericRateKey(limit.Key)},
		limit.Max,
		limit.Window.Milliseconds(),
	)
	if err != nil {
		return stateUnavailable("apply rate limit")
	}
	switch result {
	case "ok":
		return nil
	case "rate":
		return identity.ErrRateLimited
	default:
		return stateUnavailable("apply rate limit")
	}
}

func (s *store) run(
	ctx context.Context,
	script *redis.Script,
	keys []string,
	args ...any,
) (string, error) {
	if s == nil || s.client == nil {
		return "", errors.New("Redis client is unavailable")
	}
	return script.Run(ctx, s.client, keys, args...).Text()
}

func (s *store) challengeKey(emailKey, purpose string) string {
	return s.key("challenge", purpose, emailKey)
}

func (s *store) cooldownKey(emailKey, purpose string) string {
	return s.key("cooldown", purpose, emailKey)
}

func (s *store) emailRateKey(emailKey string) string {
	return s.key("rate", "email", emailKey)
}

func (s *store) ipRateKey(ip string) string {
	return s.key("rate", "ip", ip)
}

func (s *store) genericRateKey(key string) string {
	return s.key("rate", "generic", key)
}

func (s *store) key(parts ...string) string {
	if s.prefix == "" {
		return strings.Join(parts, ":")
	}
	return s.prefix + ":" + strings.Join(parts, ":")
}

func validateChallenge(challenge identity.OTPChallenge) error {
	switch {
	case challenge.EmailKey == "":
		return invalidRequest("OTPChallenge.EmailKey is required")
	case challenge.Digest == "":
		return invalidRequest("OTPChallenge.Digest is required")
	case challenge.Purpose == "":
		return invalidRequest("OTPChallenge.Purpose is required")
	case challenge.IP == "":
		return invalidRequest("OTPChallenge.IP is required")
	case challenge.TTL < time.Millisecond:
		return invalidRequest("OTPChallenge.TTL must be at least one millisecond")
	case challenge.Cooldown < time.Millisecond:
		return invalidRequest("OTPChallenge.Cooldown must be at least one millisecond")
	case challenge.Window < time.Millisecond:
		return invalidRequest("OTPChallenge.Window must be at least one millisecond")
	case challenge.MaxEmail <= 0:
		return invalidRequest("OTPChallenge.MaxEmail must be positive")
	case challenge.MaxIP <= 0:
		return invalidRequest("OTPChallenge.MaxIP must be positive")
	case challenge.Attempts <= 0:
		return invalidRequest("OTPChallenge.Attempts must be positive")
	default:
		return nil
	}
}

func validateAttempt(attempt identity.OTPAttempt) error {
	switch {
	case attempt.EmailKey == "":
		return invalidRequest("OTPAttempt.EmailKey is required")
	case attempt.Digest == "":
		return invalidRequest("OTPAttempt.Digest is required")
	case attempt.Purpose == "":
		return invalidRequest("OTPAttempt.Purpose is required")
	default:
		return nil
	}
}

func invalidRequest(message string) error {
	return fmt.Errorf("%w: %s", identity.ErrInvalidRequest, message)
}

func stateUnavailable(operation string) error {
	return fmt.Errorf("%s: %w", operation, identity.ErrStateUnavailable)
}

func isNilClient(client redis.UniversalClient) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
