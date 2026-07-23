package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	loginOTPPurpose       = "login"
	emailIdentityIssuer   = "email"
	weChatLoginRatePrefix = "wechat-login:"
	otpCodeRange          = uint64(1_000_000)
	otpRandomRange        = uint64(1) << 32
	otpRandomLimit        = otpRandomRange / otpCodeRange * otpCodeRange
)

// RequestEmailLoginCode creates the short-lived Redis proof before asking the
// mail adapter to deliver it. A delivery failure removes only the matching
// challenge and cooldown; hourly abuse counters intentionally remain.
func (s *Service) RequestEmailLoginCode(
	ctx context.Context,
	rawEmail string,
	rawSourceIP string,
) error {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	ipKey, err := sourceIPDigest(rawSourceIP)
	if err != nil {
		return err
	}
	code, err := generateOTPCode(s.random)
	if err != nil {
		return fmt.Errorf("identity: generate login code: %w", err)
	}

	challenge := OTPChallenge{
		EmailKey: emailDigest(email),
		Digest:   otpDigest(s.policy.OTPPepper, code),
		Purpose:  loginOTPPurpose,
		Owner:    "",
		IP:       ipKey,
		TTL:      s.policy.OTPTTL,
		Cooldown: s.policy.OTPCooldown,
		Window:   time.Hour,
		MaxEmail: s.policy.OTPEmailPerHour,
		MaxIP:    s.policy.OTPIPPerHour,
		Attempts: s.policy.OTPAttempts,
	}
	if err := s.otpStore.Issue(ctx, challenge); err != nil {
		return err
	}
	if err := s.emailSender.SendCode(ctx, email, code, s.policy.OTPTTL); err != nil {
		// DeleteIfMatch protects a newer concurrently issued challenge. Cleanup
		// failure must not expose adapter details or change the delivery error.
		_ = s.otpStore.DeleteIfMatch(ctx, challenge)
		return fmt.Errorf("identity: deliver login code: %w", ErrUpstreamUnavailable)
	}
	return nil
}

// LoginEmail consumes a login proof before opening PostgreSQL, then resolves
// or creates the verified identity and its independently revocable session.
func (s *Service) LoginEmail(
	ctx context.Context,
	rawEmail string,
	code string,
	client ClientKind,
) (LoginResult, error) {
	if err := client.Validate(); err != nil {
		return LoginResult{}, err
	}
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.otpStore.Verify(ctx, OTPAttempt{
		EmailKey: emailDigest(email),
		Digest:   otpDigest(s.policy.OTPPepper, code),
		Purpose:  loginOTPPurpose,
		Owner:    "",
	}); err != nil {
		return LoginResult{}, err
	}
	return s.loginWithIdentity(
		ctx,
		IdentityEmail,
		emailIdentityIssuer,
		email,
		"",
		client,
	)
}

// LoginWeChat rate-limits and exchanges the provider's one-time proof before
// starting PostgreSQL work. AppID and OpenID are accepted only from the
// provider adapter.
func (s *Service) LoginWeChat(
	ctx context.Context,
	code string,
	rawSourceIP string,
	client ClientKind,
) (LoginResult, error) {
	if err := client.Validate(); err != nil {
		return LoginResult{}, err
	}
	ipKey, err := sourceIPDigest(rawSourceIP)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.otpStore.Allow(ctx, RateLimit{
		Key:    weChatLoginRatePrefix + ipKey,
		Max:    s.policy.WeChatIPPerHour,
		Window: time.Hour,
	}); err != nil {
		return LoginResult{}, err
	}

	provider, err := s.weChatExchanger.Exchange(ctx, code)
	if err != nil {
		return LoginResult{}, classifyWeChatLoginError(err)
	}
	if strings.TrimSpace(provider.AppID) == "" ||
		strings.TrimSpace(provider.OpenID) == "" {
		return LoginResult{}, fmt.Errorf(
			"identity: WeChat exchange returned no identity: %w",
			ErrUpstreamUnavailable,
		)
	}
	return s.loginWithIdentity(
		ctx,
		IdentityWeChatMini,
		provider.AppID,
		provider.OpenID,
		provider.UnionID,
		client,
	)
}

func (s *Service) loginWithIdentity(
	ctx context.Context,
	kind IdentityKind,
	issuer string,
	subject string,
	unionID string,
	client ClientKind,
) (LoginResult, error) {
	now := s.clock.Now().UTC()
	for attempt := 0; attempt < 2; attempt++ {
		var result LoginResult
		err := s.repository.WithinTx(ctx, func(tx Tx) error {
			var err error
			result, err = s.resolveLoginIdentity(
				ctx,
				tx,
				kind,
				issuer,
				subject,
				unionID,
				client,
				now,
			)
			return err
		})
		if err == nil {
			return result, nil
		}

		var uniqueConflict *identityInsertConflict
		if !errors.As(err, &uniqueConflict) {
			return LoginResult{}, err
		}
		if attempt == 1 {
			return LoginResult{}, ErrConflict
		}
	}
	return LoginResult{}, ErrConflict
}

func (s *Service) resolveLoginIdentity(
	ctx context.Context,
	tx Tx,
	kind IdentityKind,
	issuer string,
	subject string,
	unionID string,
	client ClientKind,
	now time.Time,
) (LoginResult, error) {
	external, err := tx.FindIdentity(ctx, kind, issuer, subject, false)
	switch {
	case err == nil:
		if external.ID == uuid.Nil || external.UserID == uuid.Nil {
			return LoginResult{}, fmt.Errorf(
				"identity: persisted external identity is invalid: %w",
				ErrStateUnavailable,
			)
		}
		user, err := tx.FindUser(ctx, external.UserID, true)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return LoginResult{}, fmt.Errorf(
					"identity: external identity has no account: %w",
					ErrStateUnavailable,
				)
			}
			return LoginResult{}, err
		}
		if err := validateLoginUser(user); err != nil {
			return LoginResult{}, err
		}
		if unionID != "" && external.UnionID == "" {
			if err := tx.UpdateIdentityUnionID(ctx, external.ID, unionID); err != nil &&
				!errors.Is(err, ErrConflict) {
				return LoginResult{}, err
			}
		}
		return s.createSession(ctx, tx, user, client, now)

	case !errors.Is(err, ErrNotFound):
		return LoginResult{}, err
	}

	userID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return LoginResult{}, fmt.Errorf("identity: generate user ID: %w", err)
	}
	identityID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return LoginResult{}, fmt.Errorf("identity: generate identity ID: %w", err)
	}
	user := User{
		ID:        userID,
		Status:    UserActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	external = ExternalIdentity{
		ID:         identityID,
		UserID:     userID,
		Kind:       kind,
		Issuer:     issuer,
		Subject:    subject,
		UnionID:    unionID,
		VerifiedAt: now,
		CreatedAt:  now,
	}
	if err := tx.InsertUser(ctx, user); err != nil {
		return LoginResult{}, err
	}
	if err := tx.InsertIdentity(ctx, external); err != nil {
		if errors.Is(err, ErrConflict) {
			return LoginResult{}, &identityInsertConflict{cause: err}
		}
		return LoginResult{}, err
	}
	return s.createSession(ctx, tx, user, client, now)
}

func validateLoginUser(user User) error {
	if user.ID == uuid.Nil {
		return fmt.Errorf("identity: persisted account is invalid: %w", ErrStateUnavailable)
	}
	switch user.Status {
	case UserActive:
		return nil
	case UserDisabled:
		return ErrAccountDisabled
	case UserMerged:
		return ErrConflict
	default:
		return fmt.Errorf("identity: persisted account status is invalid: %w", ErrStateUnavailable)
	}
}

type identityInsertConflict struct {
	cause error
}

func (e *identityInsertConflict) Error() string {
	return "identity: verified identity was claimed concurrently"
}

func (e *identityInsertConflict) Unwrap() error {
	return e.cause
}

func sourceIPDigest(raw string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: invalid source IP", ErrInvalidRequest)
	}
	return sha256Hex(address.Unmap().String()), nil
}

func emailDigest(normalized string) string {
	return sha256Hex(normalized)
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}

func otpDigest(pepper []byte, code string) string {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(code))
	return fmt.Sprintf("%x", mac.Sum(nil))
}

func generateOTPCode(random io.Reader) (string, error) {
	var bytes [4]byte
	for {
		if _, err := io.ReadFull(random, bytes[:]); err != nil {
			return "", err
		}
		value := uint64(binary.BigEndian.Uint32(bytes[:]))
		if value >= otpRandomLimit {
			continue
		}
		return fmt.Sprintf("%06d", value%otpCodeRange), nil
	}
}

func classifyWeChatLoginError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("identity: WeChat exchange canceled: %w", context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("identity: WeChat exchange timed out: %w", context.DeadlineExceeded)
	case errors.Is(err, ErrUpstreamUnavailable):
		return fmt.Errorf("identity: WeChat exchange unavailable: %w", ErrUpstreamUnavailable)
	case errors.Is(err, ErrCodeInvalid):
		return ErrCodeInvalid
	default:
		return ErrCodeInvalid
	}
}
