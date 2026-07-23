package identity

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

var errUseCaseNotImplemented = errors.New("identity: use case not implemented")

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
	Repository        Repository
	OTPStore          OTPStore
	EmailSender       EmailSender
	WeChatExchanger   WeChatExchanger
	TokenManager      AccessTokenIssuer
	Clock             Clock
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

	clock := dependencies.Clock
	if isNilDependency(clock) {
		clock = utcClock{}
	}
	random := dependencies.Random
	if isNilDependency(random) {
		random = cryptorand.Reader
	}

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

func (s *Service) RequestEmailLoginCode(context.Context, string, string) error {
	return errUseCaseNotImplemented
}

func (s *Service) LoginEmail(context.Context, string, string, ClientKind) (LoginResult, error) {
	return LoginResult{}, errUseCaseNotImplemented
}

func (s *Service) LoginWeChat(context.Context, string, string, ClientKind) (LoginResult, error) {
	return LoginResult{}, errUseCaseNotImplemented
}

func (s *Service) Refresh(context.Context, string, ClientKind) (LoginResult, error) {
	return LoginResult{}, errUseCaseNotImplemented
}

func (s *Service) Logout(context.Context, Principal) error {
	return errUseCaseNotImplemented
}

func (s *Service) LogoutAll(context.Context, Principal) error {
	return errUseCaseNotImplemented
}

func (s *Service) RequestBindEmailCode(context.Context, Principal, string, string) error {
	return errUseCaseNotImplemented
}

func (s *Service) BindEmail(context.Context, Principal, string, string) (BindResult, error) {
	return BindResult{}, errUseCaseNotImplemented
}

func (s *Service) BindWeChat(context.Context, Principal, string, string) (BindResult, error) {
	return BindResult{}, errUseCaseNotImplemented
}

func (s *Service) Me(context.Context, Principal) (AccountSummary, error) {
	return AccountSummary{}, errUseCaseNotImplemented
}
