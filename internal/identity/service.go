package identity

import (
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

// Policy contains Identity workflow limits and lifetimes.
type Policy struct {
	OTPPepper       []byte
	OTPTTL          time.Duration
	OTPAttempts     int
	OTPCooldown     time.Duration
	OTPEmailPerHour int
	OTPIPPerHour    int
	WeChatIPPerHour int
	RefreshTTL      time.Duration
	ReuseGrace      time.Duration
}

// Dependencies contains every adapter required by Identity application use
// cases. Clock and Random default to secure production implementations.
type Dependencies struct {
	Repository      Repository
	OTPStore        OTPStore
	EmailSender     EmailSender
	WeChatExchanger WeChatExchanger
	TokenManager    AccessTokenIssuer
	Clock           Clock
	// Random must be safe for concurrent use when a custom implementation is
	// injected. It defaults to crypto/rand.Reader.
	Random            io.Reader
	MergeParticipants []MergeParticipant
}

// Service coordinates Identity application use cases.
type Service struct {
	repository        Repository
	otpStore          OTPStore
	emailSender       EmailSender
	weChatExchanger   WeChatExchanger
	tokenManager      AccessTokenIssuer
	clock             Clock
	random            io.Reader
	mergeParticipants []MergeParticipant
	policy            Policy
}

// NewService validates adapters and constructs the Identity application
// service.
func NewService(dependencies Dependencies, policy Policy) (*Service, error) {
	missing := make([]string, 0, 5)
	if isNilDependency(dependencies.Repository) {
		missing = append(missing, "Repository")
	}
	if isNilDependency(dependencies.OTPStore) {
		missing = append(missing, "OTPStore")
	}
	if isNilDependency(dependencies.EmailSender) {
		missing = append(missing, "EmailSender")
	}
	if isNilDependency(dependencies.WeChatExchanger) {
		missing = append(missing, "WeChatExchanger")
	}
	if isNilDependency(dependencies.TokenManager) {
		missing = append(missing, "TokenManager")
	}
	for index, participant := range dependencies.MergeParticipants {
		if isNilDependency(participant) {
			missing = append(missing, fmt.Sprintf("MergeParticipants[%d]", index))
		}
	}
	if len(missing) != 0 {
		return nil, fmt.Errorf("identity: missing mandatory dependencies: %s", strings.Join(missing, ", "))
	}
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}

	clock := dependencies.Clock
	if isNilDependency(clock) {
		clock = utcClock{}
	}
	random := dependencies.Random
	if isNilDependency(random) {
		random = cryptorand.Reader
	}
	policy.OTPPepper = append([]byte(nil), policy.OTPPepper...)

	return &Service{
		repository:        dependencies.Repository,
		otpStore:          dependencies.OTPStore,
		emailSender:       dependencies.EmailSender,
		weChatExchanger:   dependencies.WeChatExchanger,
		tokenManager:      dependencies.TokenManager,
		clock:             clock,
		random:            random,
		mergeParticipants: append([]MergeParticipant(nil), dependencies.MergeParticipants...),
		policy:            policy,
	}, nil
}

func validatePolicy(policy Policy) error {
	switch {
	case len(policy.OTPPepper) < 32:
		return fmt.Errorf("%w: Policy.OTPPepper must contain at least 32 bytes", ErrInvalidRequest)
	case policy.OTPTTL <= 0:
		return fmt.Errorf("%w: Policy.OTPTTL must be positive", ErrInvalidRequest)
	case policy.OTPAttempts <= 0:
		return fmt.Errorf("%w: Policy.OTPAttempts must be positive", ErrInvalidRequest)
	case policy.OTPCooldown <= 0:
		return fmt.Errorf("%w: Policy.OTPCooldown must be positive", ErrInvalidRequest)
	case policy.OTPEmailPerHour <= 0:
		return fmt.Errorf("%w: Policy.OTPEmailPerHour must be positive", ErrInvalidRequest)
	case policy.OTPIPPerHour <= 0:
		return fmt.Errorf("%w: Policy.OTPIPPerHour must be positive", ErrInvalidRequest)
	case policy.WeChatIPPerHour <= 0:
		return fmt.Errorf("%w: Policy.WeChatIPPerHour must be positive", ErrInvalidRequest)
	case policy.RefreshTTL <= 0:
		return fmt.Errorf("%w: Policy.RefreshTTL must be positive", ErrInvalidRequest)
	case policy.ReuseGrace < 0:
		return fmt.Errorf("%w: Policy.ReuseGrace must not be negative", ErrInvalidRequest)
	case policy.ReuseGrace >= policy.RefreshTTL:
		return fmt.Errorf("%w: Policy.ReuseGrace must be shorter than Policy.RefreshTTL", ErrInvalidRequest)
	default:
		return nil
	}
}

func isNilDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type utcClock struct{}

func (utcClock) Now() time.Time { return time.Now().UTC() }
