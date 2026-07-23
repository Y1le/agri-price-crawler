package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/google/uuid"
)

func TestRequestBindEmailCodeRequiresActiveMatchingSessionAndScopesProof(t *testing.T) {
	now := loginTestNow()
	user, session := bindUserSession(now, 10, ClientWeb)
	tests := []struct {
		name   string
		mutate func(*User, *Session, *Principal)
		want   error
	}{
		{name: "active"},
		{name: "revoked", mutate: func(_ *User, session *Session, _ *Principal) {
			session.RevokedAt = ptrTime(now)
		}, want: ErrTokenInvalid},
		{name: "expired", mutate: func(_ *User, session *Session, _ *Principal) {
			session.ExpiresAt = now
		}, want: ErrTokenInvalid},
		{name: "wrong user", mutate: func(_ *User, _ *Session, principal *Principal) {
			principal.UserID = loginUUID(99)
		}, want: ErrTokenInvalid},
		{name: "disabled", mutate: func(user *User, _ *Session, _ *Principal) {
			user.Status = UserDisabled
		}, want: ErrAccountDisabled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testUser, testSession := user, session
			principal := Principal{UserID: testUser.ID, SessionID: testSession.ID}
			if test.mutate != nil {
				test.mutate(&testUser, &testSession, &principal)
			}
			calls := make([]string, 0)
			repository := newBindRepository(&calls, testUser, testSession)
			otp := &loginOTPStore{calls: &calls}
			sender := &loginEmailSender{calls: &calls}
			service := newBindService(
				t, repository, otp, sender, &loginWeChatExchanger{calls: &calls},
				&incrementingReader{}, now, nil,
			)

			err := service.RequestBindEmailCode(
				context.Background(),
				principal,
				" Farmer@Example.COM ",
				"::ffff:192.0.2.1",
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if test.want != nil {
				if otp.issued != (OTPChallenge{}) || sender.recipient != "" {
					t.Fatalf("invalid principal caused proof side effects: %+v %q", otp.issued, sender.recipient)
				}
				return
			}
			if sender.recipient != "farmer@example.com" ||
				otp.issued.Purpose != "bind" ||
				otp.issued.Owner != user.ID.String() ||
				otp.issued.EmailKey != emailDigest("farmer@example.com") ||
				otp.issued.IP != loginIPDigest(t, "192.0.2.1") ||
				otp.issued.Digest != otpDigest(validPolicy().OTPPepper, sender.code) {
				t.Fatalf("challenge=%+v sender=%+v", otp.issued, sender)
			}
			assertOrderedCalls(t, calls,
				"lock-user:"+user.ID.String(),
				"lock-session:"+session.ID.String(),
				"otp-issue",
				"email-send",
			)
		})
	}
}

func TestRequestBindEmailCodeCompensatesSMTPFailureWithDetachedBoundedContext(t *testing.T) {
	now := loginTestNow()
	user, session := bindUserSession(now, 12, ClientWeb)
	calls := make([]string, 0)
	otp := &loginOTPStore{calls: &calls}
	ctx, cancel := context.WithCancel(context.Background())
	sender := &loginEmailSender{
		calls: &calls,
		err:   errors.New("secret smtp failure"),
		hook:  cancel,
	}
	service := newBindService(
		t, newBindRepository(&calls, user, session), otp, sender,
		&loginWeChatExchanger{}, &incrementingReader{}, now, nil,
	)

	err := service.RequestBindEmailCode(
		ctx,
		Principal{UserID: user.ID, SessionID: session.ID},
		"farmer@example.com",
		"192.0.2.1",
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if otp.deleted.Purpose != "bind" ||
		otp.deleted.Owner != user.ID.String() ||
		otp.deleteContextErr != nil ||
		!otp.deleteHasDeadline ||
		time.Until(otp.deleteDeadline) > smtpCleanupTimeout {
		t.Fatalf("cleanup = %+v contextErr=%v deadline=%v", otp.deleted, otp.deleteContextErr, otp.deleteDeadline)
	}
}

func TestBindEmailConsumesAccountOwnedProofBeforePostgres(t *testing.T) {
	now := loginTestNow()
	user, session := bindUserSession(now, 20, ClientWeb)
	calls := make([]string, 0)
	otp := &loginOTPStore{calls: &calls}
	repository := newBindRepository(&calls, user, session)
	service := newBindService(
		t, repository, otp, &loginEmailSender{}, &loginWeChatExchanger{},
		&incrementingReader{}, now, nil,
	)
	principal := Principal{UserID: user.ID, SessionID: session.ID}

	result, err := service.BindEmail(
		context.Background(), principal, " Farmer@Example.COM ", "123456",
	)
	if err != nil {
		t.Fatal(err)
	}
	if otp.verified.Purpose != "bind" ||
		otp.verified.Owner != user.ID.String() ||
		otp.verified.EmailKey != emailDigest("farmer@example.com") ||
		otp.verified.Digest != otpDigest(validPolicy().OTPPepper, "123456") {
		t.Fatalf("attempt = %+v", otp.verified)
	}
	if result.Session != nil || result.Account.User.ID != user.ID {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Account.Identities) != 1 ||
		result.Account.Identities[0].Display != "f***@example.com" {
		t.Fatalf("summary = %+v", result.Account)
	}
	if len(repository.state.identities) != 1 || len(repository.state.sessions) != 1 {
		t.Fatalf("state = %+v", repository.state)
	}

	otp.verifyErr = ErrCodeInvalid
	before := repository.transactions
	if _, err := service.BindEmail(
		context.Background(), principal, "farmer@example.com", "wrong",
	); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("error = %v", err)
	}
	if repository.transactions != before {
		t.Fatal("invalid proof opened PostgreSQL")
	}
}

func TestBindEmailRevalidatesPrincipalSessionBeforeChangingPostgres(t *testing.T) {
	now := loginTestNow()
	tests := []struct {
		name   string
		mutate func(*User, *Session, *Principal)
		want   error
	}{
		{name: "revoked session", mutate: func(_ *User, session *Session, _ *Principal) {
			session.RevokedAt = ptrTime(now)
		}, want: ErrTokenInvalid},
		{name: "expired session", mutate: func(_ *User, session *Session, _ *Principal) {
			session.ExpiresAt = now
		}, want: ErrTokenInvalid},
		{name: "wrong session owner", mutate: func(_ *User, session *Session, _ *Principal) {
			session.UserID = loginUUID(199)
		}, want: ErrTokenInvalid},
		{name: "wrong principal user", mutate: func(_ *User, _ *Session, principal *Principal) {
			principal.UserID = loginUUID(198)
		}, want: ErrTokenInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user, session := bindUserSession(now, 22, ClientWeb)
			principal := Principal{UserID: user.ID, SessionID: session.ID}
			test.mutate(&user, &session, &principal)
			repository := newBindRepository(&[]string{}, user, session)
			service := newBindService(
				t, repository, &loginOTPStore{}, &loginEmailSender{},
				&loginWeChatExchanger{}, &incrementingReader{}, now, nil,
			)

			result, err := service.BindEmail(
				context.Background(),
				principal,
				"new@example.com",
				"123456",
			)
			if !errors.Is(err, test.want) || !bindResultIsZero(result) {
				t.Fatalf("result=%+v error=%v, want %v", result, err, test.want)
			}
			if len(repository.state.identities) != 0 ||
				len(repository.state.merges) != 0 {
				t.Fatalf("invalid session mutated state: %+v", repository.state)
			}
		})
	}
}

func TestBindIdentityAlreadyOwnedIsIdempotentAndDoesNotReplaceSession(t *testing.T) {
	now := loginTestNow()
	user, session := bindUserSession(now, 30, ClientWeChatMini)
	external := loginTestIdentity(
		now, user, IdentityEmail, emailIdentityIssuer, "farmer@example.com",
	)
	repository := newBindRepository(&[]string{}, user, session, external)
	service := newBindService(
		t, repository, bindNoopOTPStore{}, &loginEmailSender{},
		&loginWeChatExchanger{}, &incrementingReader{}, now, nil,
	)

	result, err := service.BindEmail(
		context.Background(),
		Principal{UserID: user.ID, SessionID: session.ID},
		external.Subject,
		"123456",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Session != nil || len(repository.state.sessions) != 1 ||
		len(repository.state.tokens) != 0 || len(repository.state.merges) != 0 {
		t.Fatalf("idempotent bind changed sessions: result=%+v state=%+v", result, repository.state)
	}
}

func TestBindCrossAccountMergesAtomicallyAndReturnsInTransactionSummary(t *testing.T) {
	now := loginTestNow()
	current, currentSession := bindUserSession(now, 40, ClientWeChatMini)
	current.CreatedAt = now.Add(-time.Hour)
	target, targetSession := bindUserSession(now, 80, ClientWeb)
	target.CreatedAt = now.Add(-2 * time.Hour)
	currentIdentity := loginTestIdentity(
		now, current, IdentityWeChatMini, "wx-app", "current-openid",
	)
	targetIdentity := loginTestIdentity(
		now, target, IdentityEmail, emailIdentityIssuer, "target@example.com",
	)
	calls := make([]string, 0)
	repository := newBindRepository(
		&calls,
		current, target, currentSession, targetSession, currentIdentity, targetIdentity,
	)
	firstParticipant := &bindParticipant{name: "participant-1", calls: &calls}
	secondParticipant := &bindParticipant{name: "participant-2", calls: &calls}
	participants := []MergeParticipant{
		firstParticipant,
		secondParticipant,
	}
	service := newBindService(
		t, repository, &loginOTPStore{calls: &calls}, &loginEmailSender{},
		&loginWeChatExchanger{}, &incrementingReader{}, now, participants,
	)

	result, err := service.BindEmail(
		context.Background(),
		Principal{UserID: current.ID, SessionID: currentSession.ID},
		targetIdentity.Subject,
		"123456",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.User.ID != target.ID ||
		result.Session == nil ||
		result.Session.User.ID != target.ID ||
		result.Session.Client != currentSession.Client ||
		result.Session.AccessToken == "" ||
		result.Session.RefreshToken == "" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Account.Identities) != 2 {
		t.Fatalf("summary = %+v", result.Account)
	}
	merged := repository.state.users[current.ID]
	if merged.Status != UserMerged ||
		merged.MergedInto == nil ||
		*merged.MergedInto != target.ID {
		t.Fatalf("secondary = %+v", merged)
	}
	for _, external := range repository.state.identities {
		if external.UserID != target.ID {
			t.Fatalf("identity not reassigned: %+v", external)
		}
	}
	if len(repository.state.merges) != 1 {
		t.Fatalf("merges = %+v", repository.state.merges)
	}
	for _, participant := range []*bindParticipant{firstParticipant, secondParticipant} {
		if participant.primary != target.ID || participant.secondary != current.ID {
			t.Fatalf("%s merge args = %s <- %s", participant.name, participant.primary, participant.secondary)
		}
	}
	merge := repository.state.merges[0]
	if merge.primary != target.ID ||
		merge.secondary != current.ID ||
		merge.session != currentSession.ID {
		t.Fatalf("merge = %+v", merge)
	}
	active := 0
	for _, durableSession := range repository.state.sessions {
		if durableSession.RevokedAt == nil {
			active++
			if durableSession.UserID != target.ID ||
				durableSession.Client != currentSession.Client {
				t.Fatalf("new session = %+v", durableSession)
			}
		}
	}
	if active != 1 {
		t.Fatalf("active sessions = %d, state=%+v", active, repository.state.sessions)
	}
	assertOrderedCalls(t, calls, "participant-1", "participant-2", "reassign", "mark-merged", "record-merge")
	firstID, secondID := orderedUserIDs(current.ID, target.ID)
	assertOrderedCalls(t, calls, "lock-user:"+firstID.String(), "lock-user:"+secondID.String())
}

func TestBindMergeUsesUUIDTieBreakAndNeverClientPreference(t *testing.T) {
	now := loginTestNow()
	current, session := bindUserSession(now, 90, ClientWeb)
	target, targetSession := bindUserSession(now, 10, ClientWeChatMini)
	current.CreatedAt, target.CreatedAt = now.Add(-time.Hour), now.Add(-time.Hour)
	external := loginTestIdentity(
		now, target, IdentityEmail, emailIdentityIssuer, "tie@example.com",
	)
	repository := newBindRepository(&[]string{}, current, target, session, targetSession, external)
	service := newBindService(
		t, repository, bindNoopOTPStore{}, &loginEmailSender{},
		&loginWeChatExchanger{}, &incrementingReader{}, now, nil,
	)

	result, err := service.BindEmail(
		context.Background(),
		Principal{UserID: current.ID, SessionID: session.ID},
		external.Subject,
		"123456",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.User.ID != target.ID {
		t.Fatalf("primary = %s, want lower UUID %s", result.Account.User.ID, target.ID)
	}
}

func TestBindMergeParticipantOrCommitFailureRollsBackAndWithholdsCredentials(t *testing.T) {
	now := loginTestNow()
	for _, test := range []struct {
		name        string
		participant MergeParticipant
		failCommit  bool
	}{
		{
			name:        "participant",
			participant: &bindParticipant{name: "fail", err: errors.New("participant failed")},
		},
		{name: "commit", failCommit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			current, session := bindUserSession(now, 50, ClientWeb)
			target, targetSession := bindUserSession(now, 60, ClientWeChatMini)
			external := loginTestIdentity(
				now, target, IdentityEmail, emailIdentityIssuer, "rollback@example.com",
			)
			repository := newBindRepository(
				&[]string{}, current, target, session, targetSession, external,
			)
			repository.failCommit = test.failCommit
			var participants []MergeParticipant
			if test.participant != nil {
				participants = []MergeParticipant{test.participant}
			}
			service := newBindService(
				t, repository, &loginOTPStore{}, &loginEmailSender{},
				&loginWeChatExchanger{}, &incrementingReader{}, now, participants,
			)

			result, err := service.BindEmail(
				context.Background(),
				Principal{UserID: current.ID, SessionID: session.ID},
				external.Subject,
				"123456",
			)
			if err == nil || !bindResultIsZero(result) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if repository.state.users[current.ID].Status != UserActive ||
				repository.state.users[target.ID].Status != UserActive ||
				repository.state.sessions[session.ID].RevokedAt != nil ||
				len(repository.state.merges) != 0 {
				t.Fatalf("failed merge persisted state: %+v", repository.state)
			}
		})
	}
}

func TestBindRejectsDisabledOrMergedAccountsWithoutMutation(t *testing.T) {
	now := loginTestNow()
	tests := []struct {
		name          string
		currentStatus UserStatus
		targetStatus  UserStatus
		want          error
	}{
		{name: "current disabled", currentStatus: UserDisabled, targetStatus: UserActive, want: ErrAccountDisabled},
		{name: "target disabled", currentStatus: UserActive, targetStatus: UserDisabled, want: ErrAccountDisabled},
		{name: "target merged", currentStatus: UserActive, targetStatus: UserMerged, want: ErrConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current, session := bindUserSession(now, 70, ClientWeb)
			target, targetSession := bindUserSession(now, 71, ClientWeb)
			current.Status, target.Status = test.currentStatus, test.targetStatus
			if target.Status == UserMerged {
				target.MergedInto = &current.ID
			}
			external := loginTestIdentity(
				now, target, IdentityEmail, emailIdentityIssuer, "disabled@example.com",
			)
			repository := newBindRepository(
				&[]string{}, current, target, session, targetSession, external,
			)
			service := newBindService(
				t, repository, &loginOTPStore{}, &loginEmailSender{},
				&loginWeChatExchanger{}, &incrementingReader{}, now, nil,
			)

			result, err := service.BindEmail(
				context.Background(),
				Principal{UserID: current.ID, SessionID: session.ID},
				external.Subject,
				"123456",
			)
			if !errors.Is(err, test.want) || !bindResultIsZero(result) {
				t.Fatalf("result=%+v error=%v want=%v", result, err, test.want)
			}
			if len(repository.state.merges) != 0 {
				t.Fatalf("merges = %+v", repository.state.merges)
			}
		})
	}
}

func TestBindWeChatLimitsBeforeExchangeValidatesProviderAndDoesNotUseUnionIDToMerge(t *testing.T) {
	now := loginTestNow()
	user, session := bindUserSession(now, 100, ClientWeChatMini)
	other, _ := bindUserSession(now, 102, ClientWeb)
	otherIdentity := loginTestIdentity(
		now, other, IdentityWeChatMini, "other-wx-app", "other-open-id",
	)
	otherIdentity.UnionID = "shared-union"
	calls := make([]string, 0)
	repository := newBindRepository(&calls, user, other, session, otherIdentity)
	otp := &loginOTPStore{calls: &calls}
	exchanger := &loginWeChatExchanger{
		calls: &calls,
		result: WeChatIdentity{
			AppID: "wx-app", OpenID: "new-open-id", UnionID: "shared-union",
		},
	}
	service := newBindService(
		t, repository, otp, &loginEmailSender{}, exchanger,
		&incrementingReader{}, now, nil,
	)

	result, err := service.BindWeChat(
		context.Background(),
		Principal{UserID: user.ID, SessionID: session.ID},
		"temporary-code",
		"192.0.2.1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if otp.allowed.Key != "wechat-bind:"+loginIPDigest(t, "192.0.2.1") ||
		otp.allowed.Max != validPolicy().WeChatIPPerHour ||
		exchanger.code != "temporary-code" ||
		result.Account.User.ID != user.ID {
		t.Fatalf("allow=%+v exchange=%+v result=%+v", otp.allowed, exchanger, result)
	}
	if len(repository.state.merges) != 0 ||
		repository.state.identities[otherIdentity.ID].UserID != other.ID {
		t.Fatalf("shared UnionID caused merge: %+v", repository.state)
	}
	assertOrderedCalls(t, calls, "otp-allow", "wechat-exchange", "repository-tx")

	before := repository.transactions
	exchanger.result = WeChatIdentity{AppID: "wx-app", OpenID: "", UnionID: "shared-union"}
	if _, err := service.BindWeChat(
		context.Background(),
		Principal{UserID: user.ID, SessionID: session.ID},
		"bad-provider-code",
		"192.0.2.1",
	); !errors.Is(err, ErrUpstreamUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if repository.transactions != before {
		t.Fatal("invalid provider identity opened PostgreSQL")
	}
}

func TestBindRetriesWhenUnownedIdentityIsClaimedConcurrently(t *testing.T) {
	now := loginTestNow()
	current, session := bindUserSession(now, 110, ClientWeb)
	winner, winnerSession := bindUserSession(now, 120, ClientWeb)
	repository := newBindRepository(&[]string{}, current, session)
	repository.publishConflict = &bindConflictWinner{
		user:    winner,
		session: winnerSession,
		identity: loginTestIdentity(
			now, winner, IdentityEmail, emailIdentityIssuer, "race@example.com",
		),
	}
	service := newBindService(
		t, repository, &loginOTPStore{}, &loginEmailSender{},
		&loginWeChatExchanger{}, &incrementingReader{}, now, nil,
	)

	result, err := service.BindEmail(
		context.Background(),
		Principal{UserID: current.ID, SessionID: session.ID},
		"race@example.com",
		"123456",
	)
	if err != nil {
		t.Fatal(err)
	}
	if repository.transactions != 2 || result.Session == nil ||
		len(repository.state.merges) != 1 {
		t.Fatalf("transactions=%d result=%+v state=%+v", repository.transactions, result, repository.state)
	}
}

func TestSimultaneousCrossMergeUsesStableUserLockOrderWithoutDeadlock(t *testing.T) {
	now := loginTestNow()
	left, leftSession := bindUserSession(now, 130, ClientWeb)
	right, rightSession := bindUserSession(now, 140, ClientWeChatMini)
	leftEmail := loginTestIdentity(
		now, left, IdentityEmail, emailIdentityIssuer, "left@example.com",
	)
	rightEmail := loginTestIdentity(
		now, right, IdentityEmail, emailIdentityIssuer, "right@example.com",
	)
	calls := make([]string, 0)
	repository := newBindRepository(
		&calls, left, right, leftSession, rightSession, leftEmail, rightEmail,
	)
	service := newBindService(
		t, repository, bindNoopOTPStore{}, &loginEmailSender{},
		&loginWeChatExchanger{}, &incrementingReader{}, now, nil,
	)

	errs := make(chan error, 2)
	go func() {
		_, err := service.BindEmail(
			context.Background(),
			Principal{UserID: left.ID, SessionID: leftSession.ID},
			rightEmail.Subject,
			"123456",
		)
		errs <- err
	}()
	go func() {
		_, err := service.BindEmail(
			context.Background(),
			Principal{UserID: right.ID, SessionID: rightSession.ID},
			leftEmail.Subject,
			"123456",
		)
		errs <- err
	}()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	successes := 0
	for range 2 {
		select {
		case err := <-errs:
			if err == nil {
				successes++
			}
		case <-timer.C:
			t.Fatal("simultaneous cross merge deadlocked")
		}
	}
	if successes != 1 || len(repository.state.merges) != 1 {
		t.Fatalf("successes=%d merges=%+v", successes, repository.state.merges)
	}
	first, second := orderedUserIDs(left.ID, right.ID)
	orderedPairFound := false
	for index, call := range calls {
		if call == "lock-user:"+first.String() &&
			index+1 < len(calls) &&
			calls[index+1] == "lock-user:"+second.String() {
			orderedPairFound = true
		}
	}
	if !orderedPairFound {
		t.Fatalf("no stable cross-account lock pair in calls: %v", calls)
	}
}

func newBindService(
	t *testing.T,
	repository Repository,
	otp OTPStore,
	email EmailSender,
	wechat WeChatExchanger,
	random io.Reader,
	now time.Time,
	participants []MergeParticipant,
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
	dependencies.MergeParticipants = participants
	service, err := NewService(dependencies, validPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func bindUserSession(now time.Time, last byte, client ClientKind) (User, Session) {
	user := loginTestUser(now, last, UserActive)
	session := Session{
		ID:         loginUUID(last + 1),
		UserID:     user.ID,
		Client:     client,
		CreatedAt:  now.Add(-time.Hour),
		LastSeenAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
	}
	return user, session
}

type bindMergeRecord struct {
	id        uuid.UUID
	primary   uuid.UUID
	secondary uuid.UUID
	session   uuid.UUID
	at        time.Time
}

type bindState struct {
	users      map[uuid.UUID]User
	identities map[uuid.UUID]ExternalIdentity
	sessions   map[uuid.UUID]Session
	tokens     map[uuid.UUID]RefreshTokenRecord
	merges     []bindMergeRecord
}

func newBindState() bindState {
	return bindState{
		users:      make(map[uuid.UUID]User),
		identities: make(map[uuid.UUID]ExternalIdentity),
		sessions:   make(map[uuid.UUID]Session),
		tokens:     make(map[uuid.UUID]RefreshTokenRecord),
	}
}

func (s bindState) clone() bindState {
	cloned := newBindState()
	for id, value := range s.users {
		cloned.users[id] = value
	}
	for id, value := range s.identities {
		cloned.identities[id] = value
	}
	for id, value := range s.sessions {
		cloned.sessions[id] = value
	}
	for id, value := range s.tokens {
		cloned.tokens[id] = value
	}
	cloned.merges = append([]bindMergeRecord(nil), s.merges...)
	return cloned
}

type bindConflictWinner struct {
	user     User
	session  Session
	identity ExternalIdentity
}

type bindRepository struct {
	mu              sync.Mutex
	state           bindState
	calls           *[]string
	transactions    int
	failCommit      bool
	publishConflict *bindConflictWinner
}

func newBindRepository(calls *[]string, values ...any) *bindRepository {
	repository := &bindRepository{state: newBindState(), calls: calls}
	for _, value := range values {
		switch value := value.(type) {
		case User:
			repository.state.users[value.ID] = value
		case Session:
			repository.state.sessions[value.ID] = value
		case ExternalIdentity:
			repository.state.identities[value.ID] = value
		default:
			panic(fmt.Sprintf("unsupported bind fixture %T", value))
		}
	}
	return repository
}

func (r *bindRepository) WithinTx(_ context.Context, fn func(Tx) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	bindAppendCall(r.calls, "repository-tx")
	r.transactions++
	staged := r.state.clone()
	tx := &bindTx{state: &staged, repository: r}
	if err := fn(tx); err != nil {
		var retry *bindOwnershipChanged
		if errors.As(err, &retry) && r.publishConflict != nil {
			winner := r.publishConflict
			r.state.users[winner.user.ID] = winner.user
			r.state.sessions[winner.session.ID] = winner.session
			r.state.identities[winner.identity.ID] = winner.identity
			r.publishConflict = nil
		}
		return err
	}
	if r.failCommit {
		return ErrStateUnavailable
	}
	r.state = staged
	return nil
}

func (r *bindRepository) UserSummary(
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

type bindTx struct {
	state      *bindState
	repository *bindRepository
}

func (tx *bindTx) SQL() platformpostgres.Tx { return nil }

func (tx *bindTx) FindIdentity(
	_ context.Context,
	kind IdentityKind,
	issuer string,
	subject string,
	forUpdate bool,
) (ExternalIdentity, error) {
	if forUpdate {
		bindAppendCall(tx.repository.calls, "lock-identity:"+subject)
	} else {
		bindAppendCall(tx.repository.calls, "find-identity:"+subject)
	}
	for _, external := range tx.state.identities {
		if external.Kind == kind &&
			external.Issuer == issuer &&
			external.Subject == subject {
			return external, nil
		}
	}
	return ExternalIdentity{}, ErrNotFound
}

func (tx *bindTx) FindUser(_ context.Context, id uuid.UUID, forUpdate bool) (User, error) {
	if forUpdate {
		bindAppendCall(tx.repository.calls, "lock-user:"+id.String())
	}
	user, ok := tx.state.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (tx *bindTx) ListIdentities(
	_ context.Context,
	userID uuid.UUID,
) ([]ExternalIdentity, error) {
	bindAppendCall(tx.repository.calls, "list-identities")
	var identities []ExternalIdentity
	for _, external := range tx.state.identities {
		if external.UserID == userID {
			identities = append(identities, external)
		}
	}
	return identities, nil
}

func (tx *bindTx) InsertUser(context.Context, User) error { return errors.New("unexpected InsertUser") }

func (tx *bindTx) InsertIdentity(_ context.Context, external ExternalIdentity) error {
	if tx.repository.publishConflict != nil {
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
	bindAppendCall(tx.repository.calls, "insert-identity")
	return nil
}

func (tx *bindTx) UpdateIdentityUnionID(
	_ context.Context,
	id uuid.UUID,
	unionID string,
) error {
	external, ok := tx.state.identities[id]
	if !ok || external.UnionID != "" || unionID == "" {
		return ErrConflict
	}
	external.UnionID = unionID
	tx.state.identities[id] = external
	return nil
}

func (tx *bindTx) ReassignIdentities(
	_ context.Context,
	from uuid.UUID,
	to uuid.UUID,
) error {
	bindAppendCall(tx.repository.calls, "reassign")
	for id, external := range tx.state.identities {
		if external.UserID == from {
			external.UserID = to
			tx.state.identities[id] = external
		}
	}
	return nil
}

func (tx *bindTx) MarkUserMerged(
	_ context.Context,
	secondary uuid.UUID,
	primary uuid.UUID,
	at time.Time,
) error {
	bindAppendCall(tx.repository.calls, "mark-merged")
	user, ok := tx.state.users[secondary]
	if !ok || user.Status != UserActive {
		return ErrConflict
	}
	user.Status = UserMerged
	user.MergedInto = &primary
	user.UpdatedAt = at
	tx.state.users[secondary] = user
	return nil
}

func (tx *bindTx) InsertSession(_ context.Context, session Session) error {
	tx.state.sessions[session.ID] = session
	bindAppendCall(tx.repository.calls, "insert-session")
	return nil
}

func (tx *bindTx) FindSession(
	_ context.Context,
	id uuid.UUID,
	forUpdate bool,
) (Session, error) {
	if forUpdate {
		bindAppendCall(tx.repository.calls, "lock-session:"+id.String())
	}
	session, ok := tx.state.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (tx *bindTx) ExtendSession(context.Context, uuid.UUID, time.Time, time.Time) error {
	return errors.New("unexpected ExtendSession")
}

func (tx *bindTx) RevokeSession(context.Context, uuid.UUID, time.Time, string) error {
	return errors.New("unexpected RevokeSession")
}

func (tx *bindTx) RevokeUserSessions(
	_ context.Context,
	userID uuid.UUID,
	at time.Time,
	reason string,
) error {
	bindAppendCall(tx.repository.calls, "revoke-user:"+userID.String())
	for id, session := range tx.state.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			session.RevokedAt = &at
			tx.state.sessions[id] = session
			for tokenID, token := range tx.state.tokens {
				if token.SessionID == id && token.RevokedAt == nil {
					token.RevokedAt = &at
					tx.state.tokens[tokenID] = token
				}
			}
		}
	}
	if reason != "account_merged" {
		return fmt.Errorf("revoke reason = %q", reason)
	}
	return nil
}

func (tx *bindTx) InsertRefreshToken(
	_ context.Context,
	token RefreshTokenRecord,
) error {
	tx.state.tokens[token.ID] = token
	bindAppendCall(tx.repository.calls, "insert-refresh")
	return nil
}

func (tx *bindTx) FindRefreshToken(context.Context, [32]byte, bool) (RefreshTokenRecord, error) {
	return RefreshTokenRecord{}, errors.New("unexpected FindRefreshToken")
}

func (tx *bindTx) ConsumeRefreshToken(context.Context, uuid.UUID, time.Time, uuid.UUID) error {
	return errors.New("unexpected ConsumeRefreshToken")
}

func (tx *bindTx) RecordMerge(
	_ context.Context,
	id uuid.UUID,
	primary uuid.UUID,
	secondary uuid.UUID,
	session uuid.UUID,
	at time.Time,
) error {
	bindAppendCall(tx.repository.calls, "record-merge")
	tx.state.merges = append(tx.state.merges, bindMergeRecord{
		id: id, primary: primary, secondary: secondary, session: session, at: at,
	})
	return nil
}

type bindParticipant struct {
	name      string
	calls     *[]string
	err       error
	primary   uuid.UUID
	secondary uuid.UUID
}

type bindNoopOTPStore struct{}

func (bindNoopOTPStore) Issue(context.Context, OTPChallenge) error { return nil }
func (bindNoopOTPStore) Verify(context.Context, OTPAttempt) error  { return nil }
func (bindNoopOTPStore) DeleteIfMatch(context.Context, OTPChallenge) error {
	return nil
}
func (bindNoopOTPStore) Allow(context.Context, RateLimit) error { return nil }

func (p *bindParticipant) Merge(
	_ context.Context,
	_ platformpostgres.Tx,
	primary uuid.UUID,
	secondary uuid.UUID,
) error {
	bindAppendCall(p.calls, p.name)
	p.primary = primary
	p.secondary = secondary
	return p.err
}

func bindAppendCall(calls *[]string, call string) {
	if calls != nil {
		*calls = append(*calls, call)
	}
}

func assertOrderedCalls(t *testing.T, calls []string, expected ...string) {
	t.Helper()
	position := 0
	for _, call := range calls {
		if position < len(expected) && call == expected[position] {
			position++
		}
	}
	if position != len(expected) {
		t.Fatalf("calls = %v; missing ordered suffix %v from index %d", calls, expected, position)
	}
}

func bindResultIsZero(result BindResult) bool {
	return result.Account.User == (User{}) &&
		len(result.Account.Identities) == 0 &&
		result.Session == nil
}

func TestBindErrorsDoNotLeakProofOrProviderSecrets(t *testing.T) {
	now := loginTestNow()
	user, session := bindUserSession(now, 150, ClientWeb)
	secret := "provider-secret-material"
	service := newBindService(
		t,
		newBindRepository(&[]string{}, user, session),
		&loginOTPStore{},
		&loginEmailSender{},
		&loginWeChatExchanger{err: errors.New(secret)},
		&incrementingReader{},
		now,
		nil,
	)
	_, err := service.BindWeChat(
		context.Background(),
		Principal{UserID: user.ID, SessionID: session.ID},
		secret,
		"192.0.2.1",
	)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
}

var _ Repository = (*bindRepository)(nil)
var _ Tx = (*bindTx)(nil)
var _ MergeParticipant = (*bindParticipant)(nil)
