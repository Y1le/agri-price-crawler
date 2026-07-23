package identity

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/google/uuid"
)

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	got, err := NormalizeEmail("  Farmer@Example.COM ")
	if err != nil || got != "farmer@example.com" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestNormalizeEmailRejectsInvalidForms(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty":              "",
		"display name":       "Farmer <farmer@example.com>",
		"multiple addresses": "one@example.com, two@example.com",
		"over 254 bytes":     strings.Repeat("a", 243) + "@example.com",
	}
	for name, raw := range tests {
		raw := raw
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := NormalizeEmail(raw); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestNormalizeEmailTrimsUnicodeSpaceWithoutASCIIOnlyPolicy(t *testing.T) {
	t.Parallel()

	got, err := NormalizeEmail("\u00a0Farmer@Example.COM\u00a0")
	if err != nil || got != "farmer@example.com" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestNormalizeEmailAccepts254Bytes(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("a", 242) + "@example.com"
	got, err := NormalizeEmail(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw || len(got) != 254 {
		t.Fatalf("got length=%d", len(got))
	}
}

func TestClientKindValidate(t *testing.T) {
	t.Parallel()

	for _, client := range []ClientKind{ClientWeb, ClientWeChatMini} {
		if err := client.Validate(); err != nil {
			t.Fatalf("%q: %v", client, err)
		}
	}
	for _, client := range []ClientKind{ClientApp, "", "desktop"} {
		if err := client.Validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%q: error=%v", client, err)
		}
	}
}

func TestSentinelErrorsSupportErrorsIs(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		ErrNotFound,
		ErrConflict,
		ErrInvalidRequest,
		ErrRateLimited,
		ErrCodeInvalid,
		ErrCodeExpired,
		ErrAccountDisabled,
		ErrStateUnavailable,
		ErrUpstreamUnavailable,
		ErrTokenInvalid,
		ErrTokenReused,
	}
	for _, sentinel := range sentinels {
		if !errors.Is(errors.Join(errors.New("operation failed"), sentinel), sentinel) {
			t.Fatalf("sentinel %q does not support errors.Is", sentinel)
		}
	}
}

func TestNewServiceNamesEveryMissingMandatoryDependency(t *testing.T) {
	t.Parallel()

	_, err := NewService(Dependencies{}, Policy{})
	if err == nil {
		t.Fatal("expected dependency validation error")
	}
	for _, name := range []string{
		"Repository",
		"OTPStore",
		"EmailSender",
		"WeChatExchanger",
		"TokenManager",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestNewServiceRejectsTypedNilDependency(t *testing.T) {
	t.Parallel()

	deps := validDependencies()
	var repository *fakeRepository
	deps.Repository = repository

	_, err := NewService(deps, Policy{})
	if err == nil || !strings.Contains(err.Error(), "Repository") {
		t.Fatalf("error=%v", err)
	}
}

func TestNewServiceDefaultsClockAndRandom(t *testing.T) {
	t.Parallel()

	service, err := NewService(validDependencies(), validPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if service.random != cryptorand.Reader {
		t.Fatal("Random did not default to crypto/rand.Reader")
	}
	now := service.clock.Now()
	if now.Location() != time.UTC {
		t.Fatalf("clock location=%v", now.Location())
	}
	if delta := time.Since(now); delta < -time.Second || delta > time.Second {
		t.Fatalf("clock returned unexpected time delta %v", delta)
	}
}

func TestNewServicePreservesInjectedClockAndRandom(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	random := strings.NewReader("deterministic")
	deps := validDependencies()
	deps.Clock = clock
	deps.Random = random

	service, err := NewService(deps, validPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if got := service.clock.Now(); got != now {
		t.Fatalf("clock returned %v", got)
	}
	if service.random != random {
		t.Fatal("Random dependency was replaced")
	}
}

func TestNewServiceDefaultsTypedNilClockAndRandom(t *testing.T) {
	t.Parallel()

	deps := validDependencies()
	var clock *fixedClock
	var random *strings.Reader
	deps.Clock = clock
	deps.Random = random

	service, err := NewService(deps, validPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service.clock.(utcClock); !ok {
		t.Fatalf("clock type=%T", service.clock)
	}
	if service.random != cryptorand.Reader {
		t.Fatal("typed-nil Random did not default to crypto/rand.Reader")
	}
}

func TestNewServiceRejectsNilMergeParticipant(t *testing.T) {
	t.Parallel()

	deps := validDependencies()
	var participant *fakeMergeParticipant
	deps.MergeParticipants = append(deps.MergeParticipants, participant)

	_, err := NewService(deps, validPolicy())
	if err == nil || !strings.Contains(err.Error(), "MergeParticipants[1]") {
		t.Fatalf("error=%v", err)
	}
}

func TestNewServiceValidatesPolicy(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mutate func(*Policy)
		want   string
	}{
		"short pepper": {
			mutate: func(policy *Policy) { policy.OTPPepper = make([]byte, 31) },
			want:   "OTPPepper",
		},
		"zero OTP TTL": {
			mutate: func(policy *Policy) { policy.OTPTTL = 0 },
			want:   "OTPTTL",
		},
		"zero OTP attempts": {
			mutate: func(policy *Policy) { policy.OTPAttempts = 0 },
			want:   "OTPAttempts",
		},
		"zero OTP cooldown": {
			mutate: func(policy *Policy) { policy.OTPCooldown = 0 },
			want:   "OTPCooldown",
		},
		"zero email limit": {
			mutate: func(policy *Policy) { policy.OTPEmailPerHour = 0 },
			want:   "OTPEmailPerHour",
		},
		"zero IP limit": {
			mutate: func(policy *Policy) { policy.OTPIPPerHour = 0 },
			want:   "OTPIPPerHour",
		},
		"zero WeChat limit": {
			mutate: func(policy *Policy) { policy.WeChatIPPerHour = 0 },
			want:   "WeChatIPPerHour",
		},
		"zero refresh TTL": {
			mutate: func(policy *Policy) { policy.RefreshTTL = 0 },
			want:   "RefreshTTL",
		},
		"negative reuse grace": {
			mutate: func(policy *Policy) { policy.ReuseGrace = -time.Second },
			want:   "ReuseGrace",
		},
		"reuse grace equals refresh TTL": {
			mutate: func(policy *Policy) { policy.ReuseGrace = policy.RefreshTTL },
			want:   "ReuseGrace",
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			policy := validPolicy()
			test.mutate(&policy)
			_, err := NewService(validDependencies(), policy)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want field %s", err, test.want)
			}
		})
	}
}

func TestNewServiceDefensivelyCopiesSlices(t *testing.T) {
	t.Parallel()

	policy := validPolicy()
	participants := []MergeParticipant{fakeMergeParticipant{}}
	deps := validDependencies()
	deps.MergeParticipants = participants

	service, err := NewService(deps, policy)
	if err != nil {
		t.Fatal(err)
	}

	policy.OTPPepper[0] ^= 0xff
	participants[0] = secondFakeMergeParticipant{}
	if service.policy.OTPPepper[0] != 7 {
		t.Fatal("Policy.OTPPepper aliases caller memory")
	}
	if _, ok := service.mergeParticipants[0].(fakeMergeParticipant); !ok {
		t.Fatal("MergeParticipants aliases caller slice")
	}
}

func validDependencies() Dependencies {
	return Dependencies{
		Repository:        fakeRepository{},
		OTPStore:          fakeOTPStore{},
		EmailSender:       fakeEmailSender{},
		WeChatExchanger:   fakeWeChatExchanger{},
		TokenManager:      fakeTokenManager{},
		MergeParticipants: []MergeParticipant{fakeMergeParticipant{}},
	}
}

func validPolicy() Policy {
	return Policy{
		OTPPepper:       bytesOf(7, 32),
		OTPTTL:          10 * time.Minute,
		OTPAttempts:     5,
		OTPCooldown:     time.Minute,
		OTPEmailPerHour: 5,
		OTPIPPerHour:    30,
		WeChatIPPerHour: 60,
		RefreshTTL:      30 * 24 * time.Hour,
		ReuseGrace:      10 * time.Second,
	}
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

type fakeRepository struct{}

func (fakeRepository) WithinTx(context.Context, func(Tx) error) error { return nil }
func (fakeRepository) UserSummary(context.Context, uuid.UUID) (User, []ExternalIdentity, error) {
	return User{}, nil, nil
}

type fakeOTPStore struct{}

func (fakeOTPStore) Issue(context.Context, OTPChallenge) error { return nil }
func (fakeOTPStore) Verify(context.Context, OTPAttempt) error  { return nil }
func (fakeOTPStore) DeleteIfMatch(context.Context, OTPChallenge) error {
	return nil
}
func (fakeOTPStore) Allow(context.Context, RateLimit) error { return nil }

type fakeEmailSender struct{}

func (fakeEmailSender) SendCode(context.Context, string, string, time.Duration) error {
	return nil
}

type fakeWeChatExchanger struct{}

func (fakeWeChatExchanger) Exchange(context.Context, string) (WeChatIdentity, error) {
	return WeChatIdentity{}, nil
}

type fakeTokenManager struct{}

func (fakeTokenManager) IssueAccess(Principal, time.Time) (string, time.Time, error) {
	return "", time.Time{}, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

type fakeMergeParticipant struct{}

func (fakeMergeParticipant) Merge(context.Context, platformpostgres.Tx, uuid.UUID, uuid.UUID) error {
	return nil
}

type secondFakeMergeParticipant struct{}

func (secondFakeMergeParticipant) Merge(context.Context, platformpostgres.Tx, uuid.UUID, uuid.UUID) error {
	return nil
}

type fakeApplication struct{}

func (fakeApplication) RequestEmailLoginCode(context.Context, string, string) error { return nil }
func (fakeApplication) LoginEmail(context.Context, string, string, ClientKind) (LoginResult, error) {
	return LoginResult{}, nil
}
func (fakeApplication) LoginWeChat(context.Context, string, string, ClientKind) (LoginResult, error) {
	return LoginResult{}, nil
}
func (fakeApplication) Refresh(context.Context, string, ClientKind) (LoginResult, error) {
	return LoginResult{}, nil
}
func (fakeApplication) Logout(context.Context, Principal) error    { return nil }
func (fakeApplication) LogoutAll(context.Context, Principal) error { return nil }
func (fakeApplication) RequestBindEmailCode(context.Context, Principal, string, string) error {
	return nil
}
func (fakeApplication) BindEmail(context.Context, Principal, string, string) (BindResult, error) {
	return BindResult{}, nil
}
func (fakeApplication) BindWeChat(context.Context, Principal, string, string) (BindResult, error) {
	return BindResult{}, nil
}
func (fakeApplication) Me(context.Context, Principal) (AccountSummary, error) {
	return AccountSummary{}, nil
}

type identityLister interface {
	ListIdentities(context.Context, uuid.UUID) ([]ExternalIdentity, error)
}

var _ Application = fakeApplication{}
var _ identityLister = Tx(nil)
var _ io.Reader = (*strings.Reader)(nil)
