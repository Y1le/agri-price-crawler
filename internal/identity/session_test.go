package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/google/uuid"
)

func TestCreateSessionPersistsBeforeIssuingAccess(t *testing.T) {
	now := sessionTestNow()
	user := sessionTestUser(now)
	repository := newSessionRepository(user)
	var calls []string
	repository.calls = &calls
	issuer := &sessionTokenIssuer{calls: &calls}
	service := newSessionService(t, repository, issuer, now)

	var result LoginResult
	err := repository.WithinTx(context.Background(), func(tx Tx) error {
		var err error
		result, err = service.createSession(
			context.Background(),
			tx,
			user,
			ClientWeb,
			now,
		)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.User != user || result.Client != ClientWeb {
		t.Fatalf("result = %+v", result)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("credentials missing: %+v", result)
	}
	if result.RefreshExpiresAt != now.Add(30*24*time.Hour) {
		t.Fatalf("refresh expiry = %v", result.RefreshExpiresAt)
	}
	if got, want := calls, []string{"insert-session", "insert-refresh", "issue-access"}; !equalStrings(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if len(repository.state.sessions) != 1 || len(repository.state.tokens) != 1 {
		t.Fatalf("durable state = %+v", repository.state)
	}
	for _, token := range repository.state.tokens {
		if token.Hash != HashRefreshToken(result.RefreshToken) {
			t.Fatal("stored refresh hash does not match returned plaintext")
		}
		if token.ExpiresAt != result.RefreshExpiresAt {
			t.Fatalf("token expiry = %v", token.ExpiresAt)
		}
	}
}

func TestCreateSessionReturnsNoResultWhenDurableInsertOrSigningFails(t *testing.T) {
	now := sessionTestNow()
	user := sessionTestUser(now)
	tests := []struct {
		name      string
		failAt    string
		issuerErr error
	}{
		{name: "session insert", failAt: "insert-session"},
		{name: "refresh insert", failAt: "insert-refresh"},
		{name: "access signing", issuerErr: ErrTokenInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newSessionRepository(user)
			repository.failAt[test.failAt] = ErrStateUnavailable
			issuer := &sessionTokenIssuer{err: test.issuerErr}
			service := newSessionService(t, repository, issuer, now)

			var callbackResult LoginResult
			err := repository.WithinTx(context.Background(), func(tx Tx) error {
				var err error
				callbackResult, err = service.createSession(
					context.Background(),
					tx,
					user,
					ClientWeb,
					now,
				)
				if err != nil {
					callbackResult = LoginResult{}
				}
				return err
			})
			if err == nil || callbackResult != (LoginResult{}) {
				t.Fatalf("result = %+v, error = %v", callbackResult, err)
			}
			if len(repository.state.sessions) != 0 || len(repository.state.tokens) != 0 {
				t.Fatalf("failed transaction committed: %+v", repository.state)
			}
		})
	}
}

func TestCreateSessionRejectsDisabledClientBeforeMutation(t *testing.T) {
	now := sessionTestNow()
	user := sessionTestUser(now)
	repository := newSessionRepository(user)
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)
	service.policy.EnabledClients = []ClientKind{ClientWeChatMini}

	err := repository.WithinTx(context.Background(), func(tx Tx) error {
		result, err := service.createSession(context.Background(), tx, user, ClientWeb, now)
		if result != (LoginResult{}) {
			t.Fatalf("result = %+v", result)
		}
		return err
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v", err)
	}
	if len(repository.state.sessions) != 0 || len(repository.state.tokens) != 0 {
		t.Fatalf("disabled client mutated durable state: %+v", repository.state)
	}
}

func TestRefreshRotatesAtomicallyInSessionThenTokenLockOrder(t *testing.T) {
	now := sessionTestNow()
	user, session, oldRaw, oldToken := sessionTestLogin(now)
	repository := newSessionRepository(user)
	repository.state.sessions[session.ID] = session
	repository.state.tokens[oldToken.ID] = oldToken
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	result, err := service.Refresh(context.Background(), oldRaw, ClientWeb)
	if err != nil {
		t.Fatal(err)
	}
	if result.User != user || result.Client != session.Client {
		t.Fatalf("result durable fields = %+v", result)
	}
	if result.RefreshToken == "" || result.RefreshToken == oldRaw || result.AccessToken == "" {
		t.Fatalf("rotated credentials = %+v", result)
	}
	if result.RefreshExpiresAt != now.Add(30*24*time.Hour) {
		t.Fatalf("refresh expiry = %v", result.RefreshExpiresAt)
	}
	wantPrefix := []string{
		"find-refresh:false",
		"find-session:true",
		"find-refresh:true",
		"find-user:false",
		"insert-refresh",
		"consume-refresh",
		"extend-session",
	}
	if !equalStrings(repository.callsPrefix(len(wantPrefix)), wantPrefix) {
		t.Fatalf("calls = %v, want prefix %v", *repository.calls, wantPrefix)
	}

	durableOld := repository.state.tokens[oldToken.ID]
	if durableOld.ConsumedAt == nil || *durableOld.ConsumedAt != now ||
		durableOld.ReplacementID == nil {
		t.Fatalf("old token = %+v", durableOld)
	}
	replacement, ok := repository.state.tokens[*durableOld.ReplacementID]
	if !ok || replacement.Hash != HashRefreshToken(result.RefreshToken) {
		t.Fatalf("replacement = %+v, found = %v", replacement, ok)
	}
	durableSession := repository.state.sessions[session.ID]
	if durableSession.LastSeenAt != now || durableSession.ExpiresAt != result.RefreshExpiresAt {
		t.Fatalf("extended session = %+v", durableSession)
	}
}

func TestRefreshRejectsClientMismatchWithoutConsumption(t *testing.T) {
	now := sessionTestNow()
	user, session, raw, token := sessionTestLogin(now)
	repository := newSessionRepository(user)
	repository.state.sessions[session.ID] = session
	repository.state.tokens[token.ID] = token
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	result, err := service.Refresh(context.Background(), raw, ClientWeChatMini)
	if !errors.Is(err, ErrTokenInvalid) || result != (LoginResult{}) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if repository.state.tokens[token.ID].ConsumedAt != nil {
		t.Fatal("client mismatch consumed the token")
	}
	if repository.commits != 0 {
		t.Fatalf("commits = %d", repository.commits)
	}
}

func TestRefreshRejectsMissingTokenAndUnsupportedClient(t *testing.T) {
	now := sessionTestNow()
	user := sessionTestUser(now)
	repository := newSessionRepository(user)
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	if result, err := service.Refresh(
		context.Background(),
		"not-present",
		ClientWeb,
	); !errors.Is(err, ErrTokenInvalid) || result != (LoginResult{}) {
		t.Fatalf("missing token result = %+v, error = %v", result, err)
	}
	initialCalls := len(*repository.calls)
	if result, err := service.Refresh(
		context.Background(),
		"not-present",
		ClientApp,
	); !errors.Is(err, ErrInvalidRequest) || result != (LoginResult{}) {
		t.Fatalf("unsupported client result = %+v, error = %v", result, err)
	}
	if len(*repository.calls) != initialCalls {
		t.Fatal("unsupported client opened a transaction")
	}
}

func TestRefreshRejectsDisabledClientBeforeOpeningTransaction(t *testing.T) {
	now := sessionTestNow()
	user := sessionTestUser(now)
	repository := newSessionRepository(user)
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)
	service.policy.EnabledClients = []ClientKind{ClientWeChatMini}

	if result, err := service.Refresh(
		context.Background(),
		"not-present",
		ClientWeb,
	); !errors.Is(err, ErrInvalidRequest) || result != (LoginResult{}) {
		t.Fatalf("disabled client result = %+v, error = %v", result, err)
	}
	if len(*repository.calls) != 0 {
		t.Fatalf("disabled client opened a transaction: %v", *repository.calls)
	}
}

func TestRefreshRejectsTokenWhoseLockedSessionChanged(t *testing.T) {
	now := sessionTestNow()
	user, session, raw, token := sessionTestLogin(now)
	repository := newSessionRepository(user)
	repository.state.sessions[session.ID] = session
	repository.state.tokens[token.ID] = token
	changed := token
	changed.SessionID = uuid.MustParse("00000000-0000-4000-8000-000000000099")
	repository.lockedRefreshOverride = &changed
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	result, err := service.Refresh(context.Background(), raw, ClientWeb)
	if !errors.Is(err, ErrTokenInvalid) || result != (LoginResult{}) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if repository.state.tokens[token.ID].ConsumedAt != nil {
		t.Fatal("changed token ownership was mutated")
	}
}

func TestRefreshRejectsInvalidDurableState(t *testing.T) {
	now := sessionTestNow()
	tests := []struct {
		name   string
		mutate func(*User, *Session, *RefreshTokenRecord)
		want   error
	}{
		{
			name: "expired refresh token",
			mutate: func(_ *User, _ *Session, token *RefreshTokenRecord) {
				token.ExpiresAt = now
			},
			want: ErrTokenInvalid,
		},
		{
			name: "revoked refresh token",
			mutate: func(_ *User, _ *Session, token *RefreshTokenRecord) {
				token.RevokedAt = ptrTime(now.Add(-time.Minute))
			},
			want: ErrTokenInvalid,
		},
		{
			name: "expired session",
			mutate: func(_ *User, session *Session, _ *RefreshTokenRecord) {
				session.ExpiresAt = now
			},
			want: ErrTokenInvalid,
		},
		{
			name: "revoked session",
			mutate: func(_ *User, session *Session, _ *RefreshTokenRecord) {
				session.RevokedAt = ptrTime(now.Add(-time.Minute))
			},
			want: ErrTokenInvalid,
		},
		{
			name: "disabled user",
			mutate: func(user *User, _ *Session, _ *RefreshTokenRecord) {
				user.Status = UserDisabled
			},
			want: ErrAccountDisabled,
		},
		{
			name: "merged user",
			mutate: func(user *User, _ *Session, _ *RefreshTokenRecord) {
				user.Status = UserMerged
			},
			want: ErrTokenInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user, session, raw, token := sessionTestLogin(now)
			test.mutate(&user, &session, &token)
			repository := newSessionRepository(user)
			repository.state.sessions[session.ID] = session
			repository.state.tokens[token.ID] = token
			service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

			result, err := service.Refresh(context.Background(), raw, ClientWeb)
			if !errors.Is(err, test.want) || result != (LoginResult{}) {
				t.Fatalf("result = %+v, error = %v, want %v", result, err, test.want)
			}
			if repository.state.tokens[token.ID].ConsumedAt != token.ConsumedAt {
				t.Fatal("invalid durable state mutated token")
			}
		})
	}
}

func TestRefreshConsumedTokenWithinGraceDoesNotMutate(t *testing.T) {
	now := sessionTestNow()
	user, session, raw, token := sessionTestLogin(now)
	token.ConsumedAt = ptrTime(now.Add(-5 * time.Second))
	token.ReplacementID = ptrUUID(uuid.MustParse("00000000-0000-4000-8000-000000000099"))
	repository := newSessionRepository(user)
	repository.state.sessions[session.ID] = session
	repository.state.tokens[token.ID] = token
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	result, err := service.Refresh(context.Background(), raw, ClientWeb)
	if !errors.Is(err, ErrTokenInvalid) || result != (LoginResult{}) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if repository.commits != 0 || repository.state.sessions[session.ID].RevokedAt != nil {
		t.Fatalf("grace retry mutated state: %+v", repository.state)
	}
}

func TestRefreshReuseOutsideGraceCommitsRevocationThenReturnsError(t *testing.T) {
	now := sessionTestNow()
	user, session, raw, token := sessionTestLogin(now)
	token.ConsumedAt = ptrTime(now.Add(-11 * time.Second))
	token.ReplacementID = ptrUUID(uuid.MustParse("00000000-0000-4000-8000-000000000099"))
	repository := newSessionRepository(user)
	repository.state.sessions[session.ID] = session
	repository.state.tokens[token.ID] = token
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	result, err := service.Refresh(context.Background(), raw, ClientWeb)
	if !errors.Is(err, ErrTokenReused) || result != (LoginResult{}) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if repository.commits != 1 {
		t.Fatalf("commits = %d", repository.commits)
	}
	if repository.state.sessions[session.ID].RevokedAt == nil {
		t.Fatal("reused token did not revoke session")
	}
	if got := repository.revokeReasons[session.ID]; got != "refresh_token_reused" {
		t.Fatalf("revoke reason = %q", got)
	}
}

func TestRefreshReturnsNoCredentialsAndRollsBackOnMutationOrCommitFailure(t *testing.T) {
	now := sessionTestNow()
	tests := []struct {
		name       string
		failAt     string
		failCommit bool
	}{
		{name: "replacement insert", failAt: "insert-refresh"},
		{name: "consume", failAt: "consume-refresh"},
		{name: "extend", failAt: "extend-session"},
		{name: "commit", failCommit: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user, session, raw, token := sessionTestLogin(now)
			repository := newSessionRepository(user)
			repository.state.sessions[session.ID] = session
			repository.state.tokens[token.ID] = token
			repository.failAt[test.failAt] = ErrStateUnavailable
			repository.failCommit = test.failCommit
			service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

			result, err := service.Refresh(context.Background(), raw, ClientWeb)
			if err == nil || result != (LoginResult{}) {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
			if repository.state.tokens[token.ID] != token ||
				repository.state.sessions[session.ID] != session {
				t.Fatalf("failed refresh changed durable state: %+v", repository.state)
			}
		})
	}
}

func TestLogoutRevokesOnlyPrincipalSessionAndIsIdempotent(t *testing.T) {
	now := sessionTestNow()
	user, current, _, currentToken := sessionTestLogin(now)
	other := current
	other.ID = uuid.MustParse("00000000-0000-4000-8000-000000000012")
	otherToken := currentToken
	otherToken.ID = uuid.MustParse("00000000-0000-4000-8000-000000000013")
	otherToken.SessionID = other.ID
	otherToken.Hash = HashRefreshToken("other")
	repository := newSessionRepository(user)
	repository.state.sessions[current.ID] = current
	repository.state.sessions[other.ID] = other
	repository.state.tokens[currentToken.ID] = currentToken
	repository.state.tokens[otherToken.ID] = otherToken
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)
	principal := Principal{UserID: user.ID, SessionID: current.ID}

	if err := service.Logout(context.Background(), principal); err != nil {
		t.Fatal(err)
	}
	if repository.state.sessions[current.ID].RevokedAt == nil {
		t.Fatal("current session remains active")
	}
	if repository.state.sessions[other.ID].RevokedAt != nil {
		t.Fatal("other session was revoked")
	}
	if got := repository.revokeReasons[current.ID]; got != "logout" {
		t.Fatalf("reason = %q", got)
	}
	if err := service.Logout(context.Background(), principal); err != nil {
		t.Fatalf("repeated logout = %v", err)
	}
	if err := service.Logout(context.Background(), Principal{
		UserID:    user.ID,
		SessionID: uuid.MustParse("00000000-0000-4000-8000-000000000099"),
	}); err != nil {
		t.Fatalf("missing-session logout = %v", err)
	}
}

func TestLogoutRejectsSessionOwnedByAnotherUser(t *testing.T) {
	now := sessionTestNow()
	user, session, _, token := sessionTestLogin(now)
	repository := newSessionRepository(user)
	repository.state.sessions[session.ID] = session
	repository.state.tokens[token.ID] = token
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	err := service.Logout(context.Background(), Principal{
		UserID:    uuid.MustParse("00000000-0000-4000-8000-000000000099"),
		SessionID: session.ID,
	})
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("error = %v", err)
	}
	if repository.state.sessions[session.ID].RevokedAt != nil {
		t.Fatal("foreign principal revoked the session")
	}
}

func TestLogoutAllLocksUserBeforePrincipalSessionAndRevokesAll(t *testing.T) {
	now := sessionTestNow()
	user, current, _, currentToken := sessionTestLogin(now)
	other := current
	other.ID = uuid.MustParse("00000000-0000-4000-8000-000000000012")
	repository := newSessionRepository(user)
	repository.state.sessions[current.ID] = current
	repository.state.sessions[other.ID] = other
	repository.state.tokens[currentToken.ID] = currentToken
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	err := service.LogoutAll(context.Background(), Principal{
		UserID: user.ID, SessionID: current.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"find-user:true", "find-session:true", "revoke-user-sessions"}
	if !equalStrings(repository.callsPrefix(len(want)), want) {
		t.Fatalf("calls = %v, want prefix %v", *repository.calls, want)
	}
	for id, session := range repository.state.sessions {
		if session.RevokedAt == nil {
			t.Fatalf("session %s remains active", id)
		}
	}
	if got := repository.userRevokeReason; got != "logout_all" {
		t.Fatalf("reason = %q", got)
	}
}

func TestLogoutAllRejectsDisabledUserOrInactivePrincipalBeforeBulkRevoke(t *testing.T) {
	now := sessionTestNow()
	tests := []struct {
		name   string
		mutate func(*User, *Session)
		want   error
	}{
		{
			name: "disabled user",
			mutate: func(user *User, _ *Session) {
				user.Status = UserDisabled
			},
			want: ErrAccountDisabled,
		},
		{
			name: "merged user",
			mutate: func(user *User, _ *Session) {
				user.Status = UserMerged
			},
			want: ErrTokenInvalid,
		},
		{
			name: "revoked principal session",
			mutate: func(_ *User, session *Session) {
				session.RevokedAt = ptrTime(now.Add(-time.Minute))
			},
			want: ErrTokenInvalid,
		},
		{
			name: "expired principal session",
			mutate: func(_ *User, session *Session) {
				session.ExpiresAt = now
			},
			want: ErrTokenInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user, session, _, token := sessionTestLogin(now)
			test.mutate(&user, &session)
			repository := newSessionRepository(user)
			repository.state.sessions[session.ID] = session
			repository.state.tokens[token.ID] = token
			service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

			err := service.LogoutAll(context.Background(), Principal{
				UserID: user.ID, SessionID: session.ID,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if repository.userRevokeReason != "" {
				t.Fatal("invalid principal reached bulk revocation")
			}
		})
	}
}

func TestMeReturnsOnlyMaskedIdentitySummaries(t *testing.T) {
	now := sessionTestNow()
	user := sessionTestUser(now)
	repository := newSessionRepository(user)
	repository.summaryIdentities = []ExternalIdentity{
		{Kind: IdentityEmail, Subject: "农夫@example.com"},
		{Kind: IdentityEmail, Subject: "@empty.example"},
		{
			Kind:    IdentityWeChatMini,
			Issuer:  "wx-app-id",
			Subject: "secret-open-id",
			UnionID: "secret-union-id",
		},
	}
	service := newSessionService(t, repository, &sessionTokenIssuer{}, now)

	summary, err := service.Me(context.Background(), Principal{UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := []IdentitySummary{
		{Kind: IdentityEmail, Display: "农***@example.com"},
		{Kind: IdentityEmail, Display: "***@empty.example"},
		{Kind: IdentityWeChatMini, Display: "已绑定微信"},
	}
	if summary.User != user || !equalIdentitySummaries(summary.Identities, want) {
		t.Fatalf("summary = %+v, want identities %+v", summary, want)
	}
	for _, identity := range summary.Identities {
		if identity.Display == "secret-open-id" || identity.Display == "secret-union-id" {
			t.Fatal("provider identifier leaked")
		}
	}
}

func TestMeMapsInactiveOrMissingUsersToStableErrors(t *testing.T) {
	now := sessionTestNow()
	tests := []struct {
		name string
		user User
		err  error
		want error
	}{
		{name: "disabled", user: User{ID: uuid.New(), Status: UserDisabled}, want: ErrAccountDisabled},
		{name: "merged", user: User{ID: uuid.New(), Status: UserMerged}, want: ErrTokenInvalid},
		{
			name: "missing",
			user: User{ID: uuid.MustParse("00000000-0000-4000-8000-000000000091")},
			err:  ErrNotFound,
			want: ErrTokenInvalid,
		},
		{
			name: "database unavailable",
			user: User{ID: uuid.MustParse("00000000-0000-4000-8000-000000000092")},
			err:  ErrStateUnavailable,
			want: ErrStateUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newSessionRepository(test.user)
			repository.summaryErr = test.err
			service := newSessionService(t, repository, &sessionTokenIssuer{}, now)
			summary, err := service.Me(context.Background(), Principal{UserID: test.user.ID})
			if !errors.Is(err, test.want) ||
				summary.User != (User{}) ||
				summary.Identities != nil {
				t.Fatalf("summary = %+v, error = %v, want %v", summary, err, test.want)
			}
		})
	}
}

func newSessionService(
	t *testing.T,
	repository Repository,
	issuer AccessTokenIssuer,
	now time.Time,
) *Service {
	t.Helper()
	dependencies := validDependencies()
	dependencies.Repository = repository
	dependencies.TokenManager = issuer
	dependencies.Clock = fixedClock{now: now}
	dependencies.Random = &incrementingReader{next: 1}
	service, err := NewService(dependencies, validPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func sessionTestNow() time.Time {
	return time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
}

func sessionTestUser(now time.Time) User {
	return User{
		ID:        uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		Status:    UserActive,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
}

func sessionTestLogin(now time.Time) (User, Session, string, RefreshTokenRecord) {
	user := sessionTestUser(now)
	session := Session{
		ID:         uuid.MustParse("00000000-0000-4000-8000-000000000010"),
		UserID:     user.ID,
		Client:     ClientWeb,
		CreatedAt:  now.Add(-time.Hour),
		LastSeenAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(time.Hour),
	}
	raw := "old-refresh-token"
	token := RefreshTokenRecord{
		ID:        uuid.MustParse("00000000-0000-4000-8000-000000000011"),
		SessionID: session.ID,
		Hash:      HashRefreshToken(raw),
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(time.Hour),
	}
	return user, session, raw, token
}

type sessionState struct {
	users    map[uuid.UUID]User
	sessions map[uuid.UUID]Session
	tokens   map[uuid.UUID]RefreshTokenRecord
}

func (s sessionState) clone() sessionState {
	cloned := sessionState{
		users:    make(map[uuid.UUID]User, len(s.users)),
		sessions: make(map[uuid.UUID]Session, len(s.sessions)),
		tokens:   make(map[uuid.UUID]RefreshTokenRecord, len(s.tokens)),
	}
	for id, user := range s.users {
		cloned.users[id] = user
	}
	for id, session := range s.sessions {
		cloned.sessions[id] = session
	}
	for id, token := range s.tokens {
		cloned.tokens[id] = token
	}
	return cloned
}

type sessionRepository struct {
	mu                    sync.Mutex
	state                 sessionState
	calls                 *[]string
	failAt                map[string]error
	failCommit            bool
	commits               int
	summaryIdentities     []ExternalIdentity
	summaryErr            error
	revokeReasons         map[uuid.UUID]string
	userRevokeReason      string
	lockedRefreshOverride *RefreshTokenRecord
}

func newSessionRepository(users ...User) *sessionRepository {
	state := sessionState{
		users:    make(map[uuid.UUID]User),
		sessions: make(map[uuid.UUID]Session),
		tokens:   make(map[uuid.UUID]RefreshTokenRecord),
	}
	for _, user := range users {
		if user.ID != uuid.Nil {
			state.users[user.ID] = user
		}
	}
	calls := make([]string, 0)
	return &sessionRepository{
		state:         state,
		calls:         &calls,
		failAt:        make(map[string]error),
		revokeReasons: make(map[uuid.UUID]string),
	}
}

func (r *sessionRepository) WithinTx(ctx context.Context, fn func(Tx) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	staged := r.state.clone()
	tx := &sessionTx{state: &staged, calls: r.calls, failAt: r.failAt, repository: r}
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

func (r *sessionRepository) UserSummary(
	_ context.Context,
	userID uuid.UUID,
) (User, []ExternalIdentity, error) {
	if r.summaryErr != nil {
		return User{}, nil, r.summaryErr
	}
	user, ok := r.state.users[userID]
	if !ok {
		return User{}, nil, ErrNotFound
	}
	return user, append([]ExternalIdentity(nil), r.summaryIdentities...), nil
}

func (r *sessionRepository) callsPrefix(length int) []string {
	if length > len(*r.calls) {
		length = len(*r.calls)
	}
	return append([]string(nil), (*r.calls)[:length]...)
}

type sessionTx struct {
	state      *sessionState
	calls      *[]string
	failAt     map[string]error
	repository *sessionRepository
}

func (tx *sessionTx) call(name string) error {
	*tx.calls = append(*tx.calls, name)
	return tx.failAt[name]
}

func (tx *sessionTx) SQL() platformpostgres.Tx { return nil }

func (tx *sessionTx) FindIdentity(
	context.Context,
	IdentityKind,
	string,
	string,
	bool,
) (ExternalIdentity, error) {
	return ExternalIdentity{}, ErrNotFound
}

func (tx *sessionTx) FindUser(
	_ context.Context,
	id uuid.UUID,
	forUpdate bool,
) (User, error) {
	if err := tx.call(fmt.Sprintf("find-user:%t", forUpdate)); err != nil {
		return User{}, err
	}
	user, ok := tx.state.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (tx *sessionTx) ListIdentities(context.Context, uuid.UUID) ([]ExternalIdentity, error) {
	return nil, nil
}

func (tx *sessionTx) InsertUser(_ context.Context, user User) error {
	tx.state.users[user.ID] = user
	return nil
}

func (tx *sessionTx) InsertIdentity(context.Context, ExternalIdentity) error { return nil }
func (tx *sessionTx) UpdateIdentityUnionID(context.Context, uuid.UUID, string) error {
	return nil
}
func (tx *sessionTx) ReassignIdentities(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (tx *sessionTx) MarkUserMerged(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

func (tx *sessionTx) InsertSession(_ context.Context, session Session) error {
	if err := tx.call("insert-session"); err != nil {
		return err
	}
	tx.state.sessions[session.ID] = session
	return nil
}

func (tx *sessionTx) FindSession(
	_ context.Context,
	id uuid.UUID,
	forUpdate bool,
) (Session, error) {
	if err := tx.call(fmt.Sprintf("find-session:%t", forUpdate)); err != nil {
		return Session{}, err
	}
	session, ok := tx.state.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (tx *sessionTx) ExtendSession(
	_ context.Context,
	id uuid.UUID,
	lastSeenAt time.Time,
	expiresAt time.Time,
) error {
	if err := tx.call("extend-session"); err != nil {
		return err
	}
	session, ok := tx.state.sessions[id]
	if !ok || session.RevokedAt != nil {
		return ErrConflict
	}
	session.LastSeenAt = lastSeenAt
	session.ExpiresAt = expiresAt
	tx.state.sessions[id] = session
	return nil
}

func (tx *sessionTx) RevokeSession(
	_ context.Context,
	id uuid.UUID,
	revokedAt time.Time,
	reason string,
) error {
	if err := tx.call("revoke-session"); err != nil {
		return err
	}
	session, ok := tx.state.sessions[id]
	if !ok {
		return ErrNotFound
	}
	if session.RevokedAt != nil {
		return ErrConflict
	}
	session.RevokedAt = ptrTime(revokedAt)
	tx.state.sessions[id] = session
	tx.repository.revokeReasons[id] = reason
	for tokenID, token := range tx.state.tokens {
		if token.SessionID == id && token.RevokedAt == nil {
			token.RevokedAt = ptrTime(revokedAt)
			tx.state.tokens[tokenID] = token
		}
	}
	return nil
}

func (tx *sessionTx) RevokeUserSessions(
	_ context.Context,
	userID uuid.UUID,
	revokedAt time.Time,
	reason string,
) error {
	if err := tx.call("revoke-user-sessions"); err != nil {
		return err
	}
	tx.repository.userRevokeReason = reason
	for id, session := range tx.state.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			session.RevokedAt = ptrTime(revokedAt)
			tx.state.sessions[id] = session
			tx.repository.revokeReasons[id] = reason
			for tokenID, token := range tx.state.tokens {
				if token.SessionID == id && token.RevokedAt == nil {
					token.RevokedAt = ptrTime(revokedAt)
					tx.state.tokens[tokenID] = token
				}
			}
		}
	}
	return nil
}

func (tx *sessionTx) InsertRefreshToken(
	_ context.Context,
	token RefreshTokenRecord,
) error {
	if err := tx.call("insert-refresh"); err != nil {
		return err
	}
	tx.state.tokens[token.ID] = token
	return nil
}

func (tx *sessionTx) FindRefreshToken(
	_ context.Context,
	hash [32]byte,
	forUpdate bool,
) (RefreshTokenRecord, error) {
	if err := tx.call(fmt.Sprintf("find-refresh:%t", forUpdate)); err != nil {
		return RefreshTokenRecord{}, err
	}
	for _, token := range tx.state.tokens {
		if token.Hash == hash {
			if forUpdate && tx.repository.lockedRefreshOverride != nil {
				return *tx.repository.lockedRefreshOverride, nil
			}
			return token, nil
		}
	}
	return RefreshTokenRecord{}, ErrNotFound
}

func (tx *sessionTx) ConsumeRefreshToken(
	_ context.Context,
	id uuid.UUID,
	consumedAt time.Time,
	replacementID uuid.UUID,
) error {
	if err := tx.call("consume-refresh"); err != nil {
		return err
	}
	token, ok := tx.state.tokens[id]
	if !ok || token.ConsumedAt != nil || token.RevokedAt != nil {
		return ErrConflict
	}
	token.ConsumedAt = ptrTime(consumedAt)
	token.ReplacementID = ptrUUID(replacementID)
	tx.state.tokens[id] = token
	return nil
}

func (tx *sessionTx) RecordMerge(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	uuid.UUID,
	uuid.UUID,
	time.Time,
) error {
	return nil
}

type sessionTokenIssuer struct {
	calls *[]string
	err   error
}

func (i *sessionTokenIssuer) IssueAccess(
	principal Principal,
	now time.Time,
) (string, time.Time, error) {
	if i.calls != nil {
		*i.calls = append(*i.calls, "issue-access")
	}
	if i.err != nil {
		return "", time.Time{}, i.err
	}
	return "access:" + principal.SessionID.String(), now.Add(15 * time.Minute), nil
}

type incrementingReader struct {
	mu   sync.Mutex
	next byte
}

func (r *incrementingReader) Read(bytes []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range bytes {
		bytes[index] = r.next
		r.next++
	}
	return len(bytes), nil
}

func ptrTime(value time.Time) *time.Time { return &value }
func ptrUUID(value uuid.UUID) *uuid.UUID { return &value }

func equalStrings(left, right []string) bool {
	return len(left) == len(right) && bytes.Equal(
		[]byte(fmt.Sprint(left)),
		[]byte(fmt.Sprint(right)),
	)
}

func equalIdentitySummaries(left, right []IdentitySummary) bool {
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

var _ io.Reader = (*incrementingReader)(nil)
var _ Repository = (*sessionRepository)(nil)
var _ Tx = (*sessionTx)(nil)
