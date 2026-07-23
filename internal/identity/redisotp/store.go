// Package redisotp stores short-lived verification challenges and rate limits
// in Redis.
package redisotp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/redis/go-redis/v9"
)

var issueScript = redis.NewScript(`
local challenge_type = redis.call("TYPE", KEYS[1])
if type(challenge_type) == "table" then
	challenge_type = challenge_type["ok"]
end
if challenge_type ~= "none" and challenge_type ~= "hash" then
	return "corrupt"
end

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
redis.call("SET", KEYS[2], ARGV[1], "PX", ARGV[6])
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
local challenge_type = redis.call("TYPE", KEYS[1])
if type(challenge_type) == "table" then
	challenge_type = challenge_type["ok"]
end
if challenge_type == "none" then
	return "unchanged"
end
if challenge_type ~= "hash" then
	return "corrupt"
end

local cooldown_type = redis.call("TYPE", KEYS[2])
if type(cooldown_type) == "table" then
	cooldown_type = cooldown_type["ok"]
end
if cooldown_type ~= "none" and cooldown_type ~= "string" then
	return "corrupt"
end

local digest = redis.call("HGET", KEYS[1], "digest")
if digest and digest == ARGV[1] then
	redis.call("DEL", KEYS[1])
	if cooldown_type == "string" and redis.call("GET", KEYS[2]) == ARGV[1] then
		redis.call("DEL", KEYS[2])
	end
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
	normalizedPrefix := strings.TrimRight(prefix, ":")
	prefixDigest := sha256.Sum256([]byte(normalizedPrefix))
	namespace := fmt.Sprintf("{redisotp-%x}", prefixDigest[:8])
	if normalizedPrefix != "" {
		namespace += ":" + normalizedPrefix
	}
	return &store{
		client: client,
		prefix: namespace,
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
			s.cooldownKey(challenge.EmailKey),
			s.emailRateKey(challenge.EmailKey),
			s.ipRateKey(challenge.IP),
		},
		challenge.Digest,
		challenge.Attempts,
		challenge.Purpose,
		challenge.Owner,
		durationMillisecondsCeil(challenge.TTL),
		durationMillisecondsCeil(challenge.Cooldown),
		durationMillisecondsCeil(challenge.Window),
		challenge.MaxEmail,
		challenge.MaxIP,
	)
	if err != nil {
		return mapStateError("issue verification challenge", err)
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
		return mapStateError("verify challenge", err)
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
			s.cooldownKey(challenge.EmailKey),
		},
		challenge.Digest,
	)
	if err != nil {
		return mapStateError("delete verification challenge", err)
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
		durationMillisecondsCeil(limit.Window),
	)
	if err != nil {
		return mapStateError("apply rate limit", err)
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
	if errors.Is(ctx.Err(), context.Canceled) {
		return "", context.Canceled
	}
	if s == nil || s.client == nil {
		return "", errors.New("Redis client is unavailable")
	}
	return script.Run(ctx, s.client, keys, args...).Text()
}

func (s *store) challengeKey(emailKey, purpose string) string {
	return s.key("challenge", purpose, emailKey)
}

func (s *store) cooldownKey(emailKey string) string {
	return s.key("cooldown", emailKey)
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

func mapStateError(operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", operation, context.Canceled)
	}
	return stateUnavailable(operation)
}

func durationMillisecondsCeil(duration time.Duration) int64 {
	milliseconds := int64(duration / time.Millisecond)
	if duration%time.Millisecond != 0 {
		milliseconds++
	}
	return milliseconds
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
