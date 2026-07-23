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
	"sync"
	"testing"
	"time"

	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/google/uuid"
)

func TestRequestEmailLoginCodeNormalizesHashesAndCleansUpSMTPFailure(t *testing.T) {
	now := loginTestNow()
	calls := make([]string, 0)
	otp := &loginOTPStore{calls: &calls}
	email := &loginEmailSender{calls: &calls}
	repository := newLoginRepository(&calls)
	service := newLoginService(
		t,
		repository,
		otp,
		email,
		&loginWeChatExchanger{},
		&loginRandom{bytes: append(
			append(
				uint32Bytes(^uint32(0)),
				uint32Bytes(42)...,
			),
			uint32Bytes(43)...,
		)},
		now,
	)

	if err := service.RequestEmailLoginCode(
		context.Background(),
		"  Farmer@Example.COM ",
		"::ffff:192.0.2.1",
	); err != nil {
		t.Fatal(err)
	}
	if got, want := calls, []string{"otp-issue", "email-send"}; !loginStringsEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if email.recipient != "farmer@example.com" || email.code != "000042" {
		t.Fatalf("email recipient=%q code=%q", email.recipient, email.code)
	}
	if email.ttl != 10*time.Minute {
		t.Fatalf("email ttl = %v", email.ttl)
	}
	wantEmailKey := loginSHA256Hex("farmer@example.com")
	wantIP := loginIPDigest(t, "192.0.2.1")
	wantDigest := loginHMAC(validPolicy().OTPPepper, "000042")
	if got := otp.issued; got.EmailKey != wantEmailKey ||
		got.IP != wantIP ||
		got.Digest != wantDigest ||
		got.Purpose != loginOTPPurpose ||
		got.Owner != "" ||
		got.TTL != 10*time.Minute ||
		got.Cooldown != time.Minute ||
		got.Window != time.Hour ||
		got.MaxEmail != 5 ||
		got.MaxIP != 30 ||
		got.Attempts != 5 {
		t.Fatalf("challenge = %+v", got)
	}
	if repository.transactions != 0 {
		t.Fatalf("database transactions = %d", repository.transactions)
	}

	calls = calls[:0]
	email.err = errors.New("smtp secret password and code 000042")
	err := service.RequestEmailLoginCode(
		context.Background(),
		"Farmer@Example.COM",
		"192.0.2.1",
	)
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "Farmer") ||
		strings.Contains(err.Error(), "000042") ||
		strings.Contains(err.Error(), "password") {
		t.Fatalf("error leaks sensitive material: %v", err)
	}
	if got, want := calls, []string{"otp-issue", "email-send", "otp-delete"}; !loginStringsEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if otp.deleted.EmailKey != otp.issued.EmailKey ||
		otp.deleted.Digest != otp.issued.Digest ||
		otp.deleted.Purpose != otp.issued.Purpose {
		t.Fatalf("deleted = %+v, issued = %+v", otp.deleted, otp.issued)
	}
	if repository.transactions != 0 {
		t.Fatalf("SMTP failure opened %d database transactions", repository.transactions)
	}
}

func TestRequestEmailLoginCodeRejectsInvalidInputBeforeSideEffects(t *testing.T) {
	tests := []struct {
		name  string
		email string
		ip    string
	}{
		{name: "email", email: "not-an-email", ip: "192.0.2.1"},
		{name: "IP", email: "farmer@example.com", ip: "192.0.2.999"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := make([]string, 0)
			service := newLoginService(
				t,
				newLoginRepository(&calls),
				&loginOTPStore{calls: &calls},
				&loginEmailSender{calls: &calls},
				&loginWeChatExchanger{calls: &calls},
				&loginRandom{},
				loginTestNow(),
			)
			err := service.RequestEmailLoginCode(
				context.Background(),
				test.email,
				test.ip,
			)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v", err)
			}
			if len(calls) != 0 {
				t.Fatalf("calls = %v", calls)
			}
		})
	}
}

func TestRequestEmailLoginCodeStopsBeforeSMTPWhenIssueFails(t *testing.T) {
	calls := make([]string, 0)
	repository := newLoginRepository(&calls)
	service := newLoginService(
		t,
		repository,
		&loginOTPStore{calls: &calls, issueErr: ErrRateLimited},
		&loginEmailSender{calls: &calls},
		&loginWeChatExchanger{calls: &calls},
		&loginRandom{bytes: uint32Bytes(7)},
		loginTestNow(),
	)
	err := service.RequestEmailLoginCode(
		context.Background(),
		"farmer@example.com",
		"192.0.2.1",
	)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("error = %v", err)
	}
	if got, want := calls, []string{"otp-issue"}; !loginStringsEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if repository.transactions != 0 {
		t.Fatalf("database transactions = %d", repository.transactions)
	}
}

func TestEmailLoginConsumesProofBeforeCreatingFirstOrRepeatSession(t *testing.T) {
	now := loginTestNow()
	for _, test := range []struct {
		name        string
		repeat      bool
		wantUsers   int
		wantIDs     int
		wantCommits int
	}{
		{name: "first login", wantUsers: 1, wantIDs: 1, wantCommits: 1},
		{name: "repeat login", repeat: true, wantUsers: 1, wantIDs: 1, wantCommits: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := make([]string, 0)
			repository := newLoginRepository(&calls)
			if test.repeat {
				user := loginTestUser(now, 1, UserActive)
				repository.state.users[user.ID] = user
				external := loginTestIdentity(now, user, IdentityEmail, "email", "farmer@example.com")
				repository.state.identities[external.ID] = external
			}
			otp := &loginOTPStore{calls: &calls}
			service := newLoginService(
				t,
				repository,
				otp,
				&loginEmailSender{calls: &calls},
				&loginWeChatExchanger{calls: &calls},
				&incrementingReader{next: 1},
				now,
			)

			result, err := service.LoginEmail(
				context.Background(),
				" Farmer@Example.COM ",
				"123456",
				ClientWeb,
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.User.Status != UserActive ||
				result.Client != ClientWeb ||
				result.AccessToken == "" ||
				result.RefreshToken == "" {
				t.Fatalf("result = %+v", result)
			}
			if len(calls) == 0 || calls[0] != "otp-verify" {
				t.Fatalf("calls = %v", calls)
			}
			if otp.verified.EmailKey != loginSHA256Hex("farmer@example.com") ||
				otp.verified.Digest != loginHMAC(validPolicy().OTPPepper, "123456") ||
				otp.verified.Purpose != loginOTPPurpose ||
				otp.verified.Owner != "" {
				t.Fatalf("attempt = %+v", otp.verified)
			}
			if len(repository.state.users) != test.wantUsers ||
				len(repository.state.identities) != test.wantIDs ||
				len(repository.state.sessions) != 1 ||
				len(repository.state.tokens) != 1 ||
				repository.commits != test.wantCommits {
				t.Fatalf("state = %+v commits=%d", repository.state, repository.commits)
			}
			for _, external := range repository.state.identities {
				if external.Kind != IdentityEmail ||
					external.Issuer != emailIdentityIssuer ||
					external.Subject != "farmer@example.com" ||
					external.UserID != result.User.ID {
					t.Fatalf("identity = %+v, result user = %s", external, result.User.ID)
				}
			}
		})
	}
}

func TestEmailLoginWrongOrExpiredOTPDoesNotOpenDatabase(t *testing.T) {
	for _, want := range []error{ErrCodeInvalid, ErrCodeExpired} {
		t.Run(want.Error(), func(t *testing.T) {
			calls := make([]string, 0)
			otp := &loginOTPStore{calls: &calls, verifyErr: want}
			repository := newLoginRepository(&calls)
			service := newLoginService(
				t,
				repository,
				otp,
				&loginEmailSender{},
				&loginWeChatExchanger{},
				&incrementingReader{next: 1},
				loginTestNow(),
			)
			result, err := service.LoginEmail(
				context.Background(),
				"farmer@example.com",
				"bad-code",
				ClientWeb,
			)
			if !errors.Is(err, want) || result != (LoginResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if repository.transactions != 0 || len(calls) != 1 || calls[0] != "otp-verify" {
				t.Fatalf("transactions=%d calls=%v", repository.transactions, calls)
			}
		})
	}
}

func TestEmailLoginRetriesUniqueIdentityConflictExactlyOnce(t *testing.T) {
	now := loginTestNow()
	calls := make([]string, 0)
	repository := newLoginRepository(&calls)
	winner := loginTestUser(now, 99, UserActive)
	repository.identityConflictCount = 1
	repository.conflictWinner = winner
	repository.publishConflictWinner = true
	service := newLoginService(
		t,
		repository,
		&loginOTPStore{calls: &calls},
		&loginEmailSender{},
		&loginWeChatExchanger{},
		&incrementingReader{next: 1},
		now,
	)

	result, err := service.LoginEmail(
		context.Background(),
		"farmer@example.com",
		"123456",
		ClientWeb,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID != winner.ID || repository.transactions != 2 || repository.commits != 1 {
		t.Fatalf("result=%+v transactions=%d commits=%d", result, repository.transactions, repository.commits)
	}
	if len(repository.state.users) != 1 || len(repository.state.identities) != 1 {
		t.Fatalf("state = %+v", repository.state)
	}

	repository = newLoginRepository(&calls)
	repository.identityConflictCount = 2
	repository.conflictWinner = winner
	service = newLoginService(
		t,
		repository,
		&loginOTPStore{},
		&loginEmailSender{},
		&loginWeChatExchanger{},
		&incrementingReader{next: 1},
		now,
	)
	result, err = service.LoginEmail(
		context.Background(),
		"other@example.com",
		"123456",
		ClientWeb,
	)
	if !errors.Is(err, ErrConflict) ||
		result != (LoginResult{}) ||
		repository.transactions != 2 ||
		repository.commits != 0 {
		t.Fatalf("result=%+v error=%v transactions=%d commits=%d", result, err, repository.transactions, repository.commits)
	}
}

func TestWeChatLoginLimitsAndExchangesBeforeDatabase(t *testing.T) {
	now := loginTestNow()
	calls := make([]string, 0)
	repository := newLoginRepository(&calls)
	wechat := &loginWeChatExchanger{
		calls: &calls,
		result: WeChatIdentity{
			AppID:   "wx-app-id",
			OpenID:  "provider-open-id",
			UnionID: "provider-union-id",
		},
	}
	otp := &loginOTPStore{calls: &calls}
	service := newLoginService(
		t,
		repository,
		otp,
		&loginEmailSender{},
		wechat,
		&incrementingReader{next: 1},
		now,
	)

	result, err := service.LoginWeChat(
		context.Background(),
		"one-time-code",
		"::ffff:198.51.100.7",
		ClientWeChatMini,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"otp-allow", "wechat-exchange", "repository-tx"}
	if len(calls) < len(wantPrefix) ||
		!loginStringsEqual(calls[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("calls = %v, want prefix %v", calls, wantPrefix)
	}
	wantLimit := RateLimit{
		Key:    weChatLoginRatePrefix + loginIPDigest(t, "198.51.100.7"),
		Max:    validPolicy().WeChatIPPerHour,
		Window: time.Hour,
	}
	if otp.allowed != wantLimit {
		t.Fatalf("limit = %+v, want %+v", otp.allowed, wantLimit)
	}
	if wechat.code != "one-time-code" {
		t.Fatalf("exchanged code = %q", wechat.code)
	}
	if result.User.Status != UserActive || result.Client != ClientWeChatMini {
		t.Fatalf("result = %+v", result)
	}
	if len(repository.state.identities) != 1 {
		t.Fatalf("identities = %+v", repository.state.identities)
	}
	for _, external := range repository.state.identities {
		if external.Issuer != "wx-app-id" ||
			external.Subject != "provider-open-id" ||
			external.UnionID != "provider-union-id" {
			t.Fatalf("identity = %+v", external)
		}
	}
}

func TestWeChatLoginRateOrUpstreamFailureDoesNotWriteDatabase(t *testing.T) {
	tests := []struct {
		name        string
		allowErr    error
		exchangeErr error
		want        error
		wantCalls   []string
	}{
		{
			name:      "rate limited",
			allowErr:  ErrRateLimited,
			want:      ErrRateLimited,
			wantCalls: []string{"otp-allow"},
		},
		{
			name:        "upstream unavailable",
			exchangeErr: fmt.Errorf("provider secret response: %w", ErrUpstreamUnavailable),
			want:        ErrUpstreamUnavailable,
			wantCalls:   []string{"otp-allow", "wechat-exchange"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := make([]string, 0)
			repository := newLoginRepository(&calls)
			service := newLoginService(
				t,
				repository,
				&loginOTPStore{calls: &calls, allowErr: test.allowErr},
				&loginEmailSender{},
				&loginWeChatExchanger{calls: &calls, err: test.exchangeErr},
				&incrementingReader{next: 1},
				loginTestNow(),
			)
			result, err := service.LoginWeChat(
				context.Background(),
				"temporary-secret-code",
				"203.0.113.9",
				ClientWeChatMini,
			)
			if !errors.Is(err, test.want) || result != (LoginResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if strings.Contains(err.Error(), "provider secret") ||
				strings.Contains(err.Error(), "temporary-secret-code") {
				t.Fatalf("error leaks provider material: %v", err)
			}
			if !loginStringsEqual(calls, test.wantCalls) || repository.transactions != 0 {
				t.Fatalf("calls=%v transactions=%d", calls, repository.transactions)
			}
		})
	}
}

func TestWeChatLoginRepeatUpdatesOnlyEmptyUnionIDAndNeverMergesByUnionID(t *testing.T) {
	now := loginTestNow()
	calls := make([]string, 0)
	repository := newLoginRepository(&calls)
	existingUser := loginTestUser(now, 1, UserActive)
	otherUser := loginTestUser(now, 2, UserActive)
	repository.state.users[existingUser.ID] = existingUser
	repository.state.users[otherUser.ID] = otherUser
	existing := loginTestIdentity(
		now,
		existingUser,
		IdentityWeChatMini,
		"wx-app-id",
		"existing-open-id",
	)
	repository.state.identities[existing.ID] = existing
	sameUnionOtherIdentity := loginTestIdentity(
		now,
		otherUser,
		IdentityWeChatMini,
		"other-wx-app",
		"other-open-id",
	)
	sameUnionOtherIdentity.ID = loginUUID(22)
	sameUnionOtherIdentity.UnionID = "shared-union"
	repository.state.identities[sameUnionOtherIdentity.ID] = sameUnionOtherIdentity
	service := newLoginService(
		t,
		repository,
		&loginOTPStore{calls: &calls},
		&loginEmailSender{},
		&loginWeChatExchanger{
			calls: &calls,
			result: WeChatIdentity{
				AppID:   "wx-app-id",
				OpenID:  "existing-open-id",
				UnionID: "shared-union",
			},
		},
		&incrementingReader{next: 1},
		now,
	)

	result, err := service.LoginWeChat(
		context.Background(),
		"code",
		"203.0.113.10",
		ClientWeChatMini,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID != existingUser.ID {
		t.Fatalf("logged into user %s, want %s", result.User.ID, existingUser.ID)
	}
	if repository.state.identities[existing.ID].UnionID != "shared-union" {
		t.Fatalf("existing identity = %+v", repository.state.identities[existing.ID])
	}
	if repository.state.identities[sameUnionOtherIdentity.ID].UserID != otherUser.ID ||
		len(repository.state.users) != 2 ||
		len(repository.state.identities) != 2 {
		t.Fatalf("UnionID caused merge: %+v", repository.state)
	}

	repository.state.identities[existing.ID] = ExternalIdentity{
		ID:         existing.ID,
		UserID:     existing.UserID,
		Kind:       existing.Kind,
		Issuer:     existing.Issuer,
		Subject:    existing.Subject,
		UnionID:    "original-union",
		VerifiedAt: existing.VerifiedAt,
		CreatedAt:  existing.CreatedAt,
	}
	service.weChatExchanger = &loginWeChatExchanger{result: WeChatIdentity{
		AppID: "wx-app-id", OpenID: "existing-open-id", UnionID: "replacement-union",
	}}
	if _, err := service.LoginWeChat(
		context.Background(),
		"code-2",
		"203.0.113.10",
		ClientWeChatMini,
	); err != nil {
		t.Fatal(err)
	}
	if got := repository.state.identities[existing.ID].UnionID; got != "original-union" {
		t.Fatalf("nonempty UnionID overwritten with %q", got)
	}
}

func TestWeChatLoginRejectsInvalidIPAndIncompleteProviderIdentityBeforeDatabase(t *testing.T) {
	tests := []struct {
		name      string
		sourceIP  string
		provider  WeChatIdentity
		wantCalls []string
		want      error
	}{
		{
			name:      "invalid source IP",
			sourceIP:  "203.0.113.999",
			wantCalls: nil,
			want:      ErrInvalidRequest,
		},
		{
			name:      "missing AppID",
			sourceIP:  "203.0.113.1",
			provider:  WeChatIdentity{OpenID: "openid"},
			wantCalls: []string{"otp-allow", "wechat-exchange"},
			want:      ErrUpstreamUnavailable,
		},
		{
			name:      "missing OpenID",
			sourceIP:  "203.0.113.1",
			provider:  WeChatIdentity{AppID: "appid"},
			wantCalls: []string{"otp-allow", "wechat-exchange"},
			want:      ErrUpstreamUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := make([]string, 0)
			repository := newLoginRepository(&calls)
			service := newLoginService(
				t,
				repository,
				&loginOTPStore{calls: &calls},
				&loginEmailSender{},
				&loginWeChatExchanger{calls: &calls, result: test.provider},
				&incrementingReader{next: 1},
				loginTestNow(),
			)
			result, err := service.LoginWeChat(
				context.Background(),
				"code",
				test.sourceIP,
				ClientWeChatMini,
			)
			if !errors.Is(err, test.want) || result != (LoginResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if !loginStringsEqual(calls, test.wantCalls) || repository.transactions != 0 {
				t.Fatalf("calls=%v transactions=%d", calls, repository.transactions)
			}
		})
	}
}

func TestEmailLoginDoesNotRetryNonIdentityConflict(t *testing.T) {
	calls := make([]string, 0)
	repository := newLoginRepository(&calls)
	repository.insertSessionErr = ErrConflict
	service := newLoginService(
		t,
		repository,
		&loginOTPStore{calls: &calls},
		&loginEmailSender{},
		&loginWeChatExchanger{},
		&incrementingReader{next: 1},
		loginTestNow(),
	)
	result, err := service.LoginEmail(
		context.Background(),
		"farmer@example.com",
		"123456",
		ClientWeb,
	)
	if !errors.Is(err, ErrConflict) ||
		result != (LoginResult{}) ||
		repository.transactions != 1 ||
		repository.commits != 0 {
		t.Fatalf(
			"result=%+v error=%v transactions=%d commits=%d",
			result,
			err,
			repository.transactions,
			repository.commits,
		)
	}
}

func TestLoginRejectsDisabledMergedAndClientAppWithoutCredentials(t *testing.T) {
	now := loginTestNow()
	for _, test := range []struct {
		name   string
		status UserStatus
		want   error
	}{
		{name: "disabled", status: UserDisabled, want: ErrAccountDisabled},
		{name: "merged", status: UserMerged, want: ErrConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := make([]string, 0)
			repository := newLoginRepository(&calls)
			user := loginTestUser(now, 1, test.status)
			repository.state.users[user.ID] = user
			external := loginTestIdentity(now, user, IdentityEmail, "email", "farmer@example.com")
			repository.state.identities[external.ID] = external
			service := newLoginService(
				t,
				repository,
				&loginOTPStore{calls: &calls},
				&loginEmailSender{},
				&loginWeChatExchanger{},
				&incrementingReader{next: 1},
				now,
			)
			result, err := service.LoginEmail(
				context.Background(),
				"farmer@example.com",
				"123456",
				ClientWeb,
			)
			if !errors.Is(err, test.want) || result != (LoginResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if len(repository.state.sessions) != 0 || len(repository.state.tokens) != 0 {
				t.Fatalf("credentials persisted: %+v", repository.state)
			}
		})
	}

	calls := make([]string, 0)
	service := newLoginService(
		t,
		newLoginRepository(&calls),
		&loginOTPStore{calls: &calls},
		&loginEmailSender{},
		&loginWeChatExchanger{calls: &calls},
		&incrementingReader{next: 1},
		now,
	)
	if result, err := service.LoginEmail(
		context.Background(),
		"farmer@example.com",
		"123456",
		ClientApp,
	); !errors.Is(err, ErrInvalidRequest) || result != (LoginResult{}) {
		t.Fatalf("email result=%+v error=%v", result, err)
	}
	if result, err := service.LoginWeChat(
		context.Background(),
		"code",
		"203.0.113.1",
		ClientApp,
	); !errors.Is(err, ErrInvalidRequest) || result != (LoginResult{}) {
		t.Fatalf("wechat result=%+v error=%v", result, err)
	}
	if len(calls) != 0 {
		t.Fatalf("unsupported client side effects = %v", calls)
	}
}

func TestLoginReturnsNoCredentialsWhenTransactionCommitFails(t *testing.T) {
	calls := make([]string, 0)
	repository := newLoginRepository(&calls)
	repository.failCommit = true
	service := newLoginService(
		t,
		repository,
		&loginOTPStore{calls: &calls},
		&loginEmailSender{},
		&loginWeChatExchanger{},
		&incrementingReader{next: 1},
		loginTestNow(),
	)
	result, err := service.LoginEmail(
		context.Background(),
		"farmer@example.com",
		"123456",
		ClientWeb,
	)
	if !errors.Is(err, ErrStateUnavailable) || result != (LoginResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if len(repository.state.users) != 0 ||
		len(repository.state.sessions) != 0 ||
		len(repository.state.tokens) != 0 {
		t.Fatalf("failed transaction persisted state: %+v", repository.state)
	}
}

func newLoginService(
	t *testing.T,
	repository Repository,
	otp OTPStore,
	email EmailSender,
	wechat WeChatExchanger,
	random io.Reader,
	now time.Time,
) *Service {
	t.Helper()
	dependencies := validDependencies()
	dependencies.Repository = repository
	dependencies.OTPStore = otp
	dependencies.EmailSender = email
	dependencies.WeChatExchanger = wechat
	dependencies.TokenManager = &loginTokenIssuer{}
	dependencies.Clock = fixedClock{now: now}
	dependencies.Random = random
	service, err := NewService(dependencies, validPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func loginTestNow() time.Time {
	return time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
}

func loginUUID(last byte) uuid.UUID {
	var id uuid.UUID
	id[15] = last
	id[6] = 0x40
	id[8] = 0x80
	return id
}

func loginTestUser(now time.Time, last byte, status UserStatus) User {
	return User{
		ID:        loginUUID(last),
		Status:    status,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
}

func loginTestIdentity(
	now time.Time,
	user User,
	kind IdentityKind,
	issuer string,
	subject string,
) ExternalIdentity {
	return ExternalIdentity{
		ID:         loginUUID(user.ID[15] + 10),
		UserID:     user.ID,
		Kind:       kind,
		Issuer:     issuer,
		Subject:    subject,
		VerifiedAt: now.Add(-time.Hour),
		CreatedAt:  now.Add(-time.Hour),
	}
}

func loginSHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func loginIPDigest(t *testing.T, raw string) string {
	t.Helper()
	address, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	return loginSHA256Hex(address.Unmap().String())
}

func loginHMAC(pepper []byte, code string) string {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(code))
	return fmt.Sprintf("%x", mac.Sum(nil))
}

func uint32Bytes(value uint32) []byte {
	result := make([]byte, 4)
	binary.BigEndian.PutUint32(result, value)
	return result
}

func loginStringsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type loginRandom struct {
	mu    sync.Mutex
	bytes []byte
}

func (r *loginRandom) Read(destination []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bytes) < len(destination) {
		return 0, io.ErrUnexpectedEOF
	}
	copy(destination, r.bytes[:len(destination)])
	r.bytes = r.bytes[len(destination):]
	return len(destination), nil
}

type loginOTPStore struct {
	calls     *[]string
	issued    OTPChallenge
	verified  OTPAttempt
	deleted   OTPChallenge
	allowed   RateLimit
	issueErr  error
	verifyErr error
	deleteErr error
	allowErr  error
}

func (s *loginOTPStore) Issue(_ context.Context, challenge OTPChallenge) error {
	loginAppendCall(s.calls, "otp-issue")
	s.issued = challenge
	return s.issueErr
}

func (s *loginOTPStore) Verify(_ context.Context, attempt OTPAttempt) error {
	loginAppendCall(s.calls, "otp-verify")
	s.verified = attempt
	return s.verifyErr
}

func (s *loginOTPStore) DeleteIfMatch(_ context.Context, challenge OTPChallenge) error {
	loginAppendCall(s.calls, "otp-delete")
	s.deleted = challenge
	return s.deleteErr
}

func (s *loginOTPStore) Allow(_ context.Context, limit RateLimit) error {
	loginAppendCall(s.calls, "otp-allow")
	s.allowed = limit
	return s.allowErr
}

type loginEmailSender struct {
	calls     *[]string
	recipient string
	code      string
	ttl       time.Duration
	err       error
}

func (s *loginEmailSender) SendCode(
	_ context.Context,
	recipient string,
	code string,
	ttl time.Duration,
) error {
	loginAppendCall(s.calls, "email-send")
	s.recipient = recipient
	s.code = code
	s.ttl = ttl
	return s.err
}

type loginWeChatExchanger struct {
	calls  *[]string
	code   string
	result WeChatIdentity
	err    error
}

func (e *loginWeChatExchanger) Exchange(
	_ context.Context,
	code string,
) (WeChatIdentity, error) {
	loginAppendCall(e.calls, "wechat-exchange")
	e.code = code
	return e.result, e.err
}

type loginTokenIssuer struct{}

func (*loginTokenIssuer) IssueAccess(
	principal Principal,
	now time.Time,
) (string, time.Time, error) {
	return "access:" + principal.SessionID.String(), now.Add(15 * time.Minute), nil
}

func loginAppendCall(calls *[]string, call string) {
	if calls != nil {
		*calls = append(*calls, call)
	}
}

type loginState struct {
	users      map[uuid.UUID]User
	identities map[uuid.UUID]ExternalIdentity
	sessions   map[uuid.UUID]Session
	tokens     map[uuid.UUID]RefreshTokenRecord
}

func newLoginState() loginState {
	return loginState{
		users:      make(map[uuid.UUID]User),
		identities: make(map[uuid.UUID]ExternalIdentity),
		sessions:   make(map[uuid.UUID]Session),
		tokens:     make(map[uuid.UUID]RefreshTokenRecord),
	}
}

func (state loginState) clone() loginState {
	cloned := newLoginState()
	for id, user := range state.users {
		cloned.users[id] = user
	}
	for id, external := range state.identities {
		cloned.identities[id] = external
	}
	for id, session := range state.sessions {
		cloned.sessions[id] = session
	}
	for id, token := range state.tokens {
		cloned.tokens[id] = token
	}
	return cloned
}

type loginRepository struct {
	mu                    sync.Mutex
	state                 loginState
	calls                 *[]string
	transactions          int
	commits               int
	failCommit            bool
	identityConflictCount int
	conflictWinner        User
	conflictIdentity      *ExternalIdentity
	publishConflictWinner bool
	insertSessionErr      error
}

func newLoginRepository(calls *[]string) *loginRepository {
	return &loginRepository{
		state: newLoginState(),
		calls: calls,
	}
}

func (r *loginRepository) WithinTx(ctx context.Context, fn func(Tx) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	loginAppendCall(r.calls, "repository-tx")
	r.transactions++
	if r.conflictIdentity != nil {
		external := *r.conflictIdentity
		r.state.users[r.conflictWinner.ID] = r.conflictWinner
		r.state.identities[external.ID] = external
		r.conflictIdentity = nil
	}
	staged := r.state.clone()
	tx := &loginTx{state: &staged, repository: r}
	if err := fn(tx); err != nil {
		return err
	}
	if r.failCommit {
		return ErrStateUnavailable
	}
	r.state = staged
	r.commits++
	return nil
}

func (r *loginRepository) UserSummary(
	_ context.Context,
	userID uuid.UUID,
) (User, []ExternalIdentity, error) {
	user, ok := r.state.users[userID]
	if !ok {
		return User{}, nil, ErrNotFound
	}
	var identities []ExternalIdentity
	for _, external := range r.state.identities {
		if external.UserID == userID {
			identities = append(identities, external)
		}
	}
	return user, identities, nil
}

type loginTx struct {
	state      *loginState
	repository *loginRepository
}

func (tx *loginTx) SQL() platformpostgres.Tx { return nil }

func (tx *loginTx) FindIdentity(
	_ context.Context,
	kind IdentityKind,
	issuer string,
	subject string,
	_ bool,
) (ExternalIdentity, error) {
	for _, external := range tx.state.identities {
		if external.Kind == kind &&
			external.Issuer == issuer &&
			external.Subject == subject {
			return external, nil
		}
	}
	return ExternalIdentity{}, ErrNotFound
}

func (tx *loginTx) FindUser(
	_ context.Context,
	id uuid.UUID,
	_ bool,
) (User, error) {
	user, ok := tx.state.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (tx *loginTx) ListIdentities(
	_ context.Context,
	userID uuid.UUID,
) ([]ExternalIdentity, error) {
	var identities []ExternalIdentity
	for _, external := range tx.state.identities {
		if external.UserID == userID {
			identities = append(identities, external)
		}
	}
	return identities, nil
}

func (tx *loginTx) InsertUser(_ context.Context, user User) error {
	if _, exists := tx.state.users[user.ID]; exists {
		return ErrConflict
	}
	tx.state.users[user.ID] = user
	return nil
}

func (tx *loginTx) InsertIdentity(
	_ context.Context,
	external ExternalIdentity,
) error {
	if tx.repository.identityConflictCount > 0 {
		tx.repository.identityConflictCount--
		if tx.repository.publishConflictWinner {
			competitor := external
			competitor.ID = loginUUID(88)
			competitor.UserID = tx.repository.conflictWinner.ID
			tx.repository.conflictIdentity = &competitor
		}
		return ErrConflict
	}
	for _, current := range tx.state.identities {
		if current.Kind == external.Kind &&
			current.Issuer == external.Issuer &&
			current.Subject == external.Subject {
			return ErrConflict
		}
	}
	tx.state.identities[external.ID] = external
	return nil
}

func (tx *loginTx) UpdateIdentityUnionID(
	_ context.Context,
	id uuid.UUID,
	unionID string,
) error {
	external, exists := tx.state.identities[id]
	if !exists || external.UnionID != "" || unionID == "" {
		return ErrConflict
	}
	external.UnionID = unionID
	tx.state.identities[id] = external
	return nil
}

func (tx *loginTx) ReassignIdentities(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (tx *loginTx) MarkUserMerged(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

func (tx *loginTx) InsertSession(_ context.Context, session Session) error {
	if tx.repository.insertSessionErr != nil {
		return tx.repository.insertSessionErr
	}
	tx.state.sessions[session.ID] = session
	return nil
}

func (tx *loginTx) FindSession(context.Context, uuid.UUID, bool) (Session, error) {
	return Session{}, ErrNotFound
}

func (tx *loginTx) ExtendSession(context.Context, uuid.UUID, time.Time, time.Time) error {
	return nil
}

func (tx *loginTx) RevokeSession(context.Context, uuid.UUID, time.Time, string) error {
	return nil
}

func (tx *loginTx) RevokeUserSessions(context.Context, uuid.UUID, time.Time, string) error {
	return nil
}

func (tx *loginTx) InsertRefreshToken(
	_ context.Context,
	token RefreshTokenRecord,
) error {
	tx.state.tokens[token.ID] = token
	return nil
}

func (tx *loginTx) FindRefreshToken(context.Context, [32]byte, bool) (RefreshTokenRecord, error) {
	return RefreshTokenRecord{}, ErrNotFound
}

func (tx *loginTx) ConsumeRefreshToken(context.Context, uuid.UUID, time.Time, uuid.UUID) error {
	return nil
}

func (tx *loginTx) RecordMerge(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	uuid.UUID,
	uuid.UUID,
	time.Time,
) error {
	return nil
}

var _ Repository = (*loginRepository)(nil)
var _ Tx = (*loginTx)(nil)
var _ OTPStore = (*loginOTPStore)(nil)
var _ EmailSender = (*loginEmailSender)(nil)
var _ WeChatExchanger = (*loginWeChatExchanger)(nil)
