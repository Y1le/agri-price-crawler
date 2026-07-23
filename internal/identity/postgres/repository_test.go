package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/bootstrap"
	"github.com/Y1le/agri-price-crawler/internal/identity"
	identitypostgres "github.com/Y1le/agri-price-crawler/internal/identity/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	platformpostgres "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const truncateIdentityTables = `
	TRUNCATE
		identity_account_merges,
		identity_refresh_tokens,
		identity_sessions,
		identity_identities,
		identity_users
	CASCADE
`

func TestRepositoryLifecycle(t *testing.T) {
	ctx, pool, repository := setupRepository(t)
	now := time.Date(2026, time.July, 23, 8, 0, 0, 0, time.UTC)
	primary := newUser(now)
	secondary := newUser(now.Add(time.Minute))
	email := identity.ExternalIdentity{
		ID:         uuid.New(),
		UserID:     primary.ID,
		Kind:       identity.IdentityEmail,
		Issuer:     "email",
		Subject:    "farmer@example.com",
		VerifiedAt: now,
		CreatedAt:  now,
	}
	wechat := identity.ExternalIdentity{
		ID:         uuid.New(),
		UserID:     secondary.ID,
		Kind:       identity.IdentityWeChatMini,
		Issuer:     "wx-app-id",
		Subject:    "openid-1",
		UnionID:    "union-1",
		VerifiedAt: now.Add(time.Minute),
		CreatedAt:  now.Add(time.Minute),
	}
	session := newSession(primary.ID, identity.ClientWeb, now)
	oldToken := newRefreshToken(session.ID, 1, now)
	replacement := newRefreshToken(session.ID, 2, now.Add(time.Minute))

	err := repository.WithinTx(ctx, func(tx identity.Tx) error {
		for _, user := range []identity.User{primary, secondary} {
			if err := tx.InsertUser(ctx, user); err != nil {
				return err
			}
		}
		for _, external := range []identity.ExternalIdentity{email, wechat} {
			if err := tx.InsertIdentity(ctx, external); err != nil {
				return err
			}
		}
		if err := tx.InsertSession(ctx, session); err != nil {
			return err
		}
		if err := tx.InsertRefreshToken(ctx, oldToken); err != nil {
			return err
		}
		if err := tx.InsertRefreshToken(ctx, replacement); err != nil {
			return err
		}

		gotIdentity, err := tx.FindIdentity(
			ctx,
			identity.IdentityEmail,
			email.Issuer,
			email.Subject,
			true,
		)
		if err != nil || !externalIdentitiesEqual(gotIdentity, email) {
			t.Fatalf("FindIdentity() = %+v, %v; want %+v", gotIdentity, err, email)
		}
		gotUser, err := tx.FindUser(ctx, primary.ID, true)
		if err != nil || !usersEqual(gotUser, primary) {
			t.Fatalf("FindUser() = %+v, %v; want %+v", gotUser, err, primary)
		}
		identities, err := tx.ListIdentities(ctx, primary.ID)
		if err != nil || len(identities) != 1 || !externalIdentitiesEqual(identities[0], email) {
			t.Fatalf("ListIdentities() = %+v, %v; want email identity", identities, err)
		}
		gotSession, err := tx.FindSession(ctx, session.ID, true)
		if err != nil || !sessionsEqual(gotSession, session) {
			t.Fatalf("FindSession() = %+v, %v; want %+v", gotSession, err, session)
		}
		gotToken, err := tx.FindRefreshToken(ctx, oldToken.Hash, true)
		if err != nil || !refreshTokensEqual(gotToken, oldToken) {
			t.Fatalf("FindRefreshToken() = %+v, %v; want %+v", gotToken, err, oldToken)
		}
		if err := tx.ExtendSession(ctx, session.ID, now.Add(2*time.Minute), now.Add(31*24*time.Hour)); err != nil {
			return err
		}
		if err := tx.ConsumeRefreshToken(ctx, oldToken.ID, now.Add(2*time.Minute), replacement.ID); err != nil {
			return err
		}
		if err := tx.ReassignIdentities(ctx, secondary.ID, primary.ID); err != nil {
			return err
		}
		if err := tx.MarkUserMerged(ctx, secondary.ID, primary.ID, now.Add(3*time.Minute)); err != nil {
			return err
		}
		if err := tx.RecordMerge(
			ctx,
			uuid.New(),
			primary.ID,
			secondary.ID,
			session.ID,
			now.Add(3*time.Minute),
		); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	gotUser, gotIdentities, err := repository.UserSummary(ctx, primary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !usersEqual(gotUser, primary) {
		t.Fatalf("UserSummary user = %+v, want %+v", gotUser, primary)
	}
	if len(gotIdentities) != 2 ||
		!externalIdentitiesEqual(gotIdentities[0], email) ||
		!externalIdentitiesEqual(gotIdentities[1], wechatWithUser(wechat, primary.ID)) {
		t.Fatalf("UserSummary identities = %+v, want email and reassigned WeChat identity", gotIdentities)
	}

	err = repository.WithinTx(ctx, func(tx identity.Tx) error {
		gotSession, err := tx.FindSession(ctx, session.ID, false)
		if err != nil {
			return err
		}
		if !gotSession.LastSeenAt.Equal(now.Add(2*time.Minute)) ||
			!gotSession.ExpiresAt.Equal(now.Add(31*24*time.Hour)) {
			t.Fatalf("extended session = %+v", gotSession)
		}
		gotToken, err := tx.FindRefreshToken(ctx, oldToken.Hash, false)
		if err != nil {
			return err
		}
		if gotToken.ConsumedAt == nil || !gotToken.ConsumedAt.Equal(now.Add(2*time.Minute)) ||
			gotToken.ReplacementID == nil || *gotToken.ReplacementID != replacement.ID {
			t.Fatalf("consumed token = %+v", gotToken)
		}
		mergedUser, err := tx.FindUser(ctx, secondary.ID, false)
		if err != nil {
			return err
		}
		if mergedUser.Status != identity.UserMerged ||
			mergedUser.MergedInto == nil ||
			*mergedUser.MergedInto != primary.ID {
			t.Fatalf("merged user = %+v", mergedUser)
		}
		return tx.RevokeSession(ctx, session.ID, now.Add(4*time.Minute), "logout")
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSessionAndTokensRevoked(t, ctx, pool, []uuid.UUID{session.ID}, now.Add(4*time.Minute), "logout")

	var mergeCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM identity_account_merges
		WHERE primary_user_id = $1
		  AND secondary_user_id = $2
		  AND initiated_by_session_id = $3
	`, primary.ID, secondary.ID, session.ID).Scan(&mergeCount); err != nil {
		t.Fatal(err)
	}
	if mergeCount != 1 {
		t.Fatalf("merge count = %d, want 1", mergeCount)
	}
}

func TestRepositoryRollsBackErrorsAndPanics(t *testing.T) {
	ctx, pool, repository := setupRepository(t)
	now := time.Date(2026, time.July, 23, 9, 0, 0, 0, time.UTC)
	returnedErrorUser := newUser(now)
	callbackErr := errors.New("callback failed")

	err := repository.WithinTx(ctx, func(tx identity.Tx) error {
		if err := tx.InsertUser(ctx, returnedErrorUser); err != nil {
			return err
		}
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("WithinTx returned %v, want callback error", err)
	}
	assertUserAbsent(t, ctx, pool, returnedErrorUser.ID)

	panicUser := newUser(now.Add(time.Minute))
	const panicValue = "repository panic sentinel"
	func() {
		defer func() {
			if recovered := recover(); recovered != panicValue {
				t.Fatalf("recovered = %#v, want %q", recovered, panicValue)
			}
		}()
		_ = repository.WithinTx(ctx, func(tx identity.Tx) error {
			if err := tx.InsertUser(ctx, panicUser); err != nil {
				return err
			}
			panic(panicValue)
		})
	}()
	assertUserAbsent(t, ctx, pool, panicUser.ID)
}

func TestRepositoryConstraintsAndAffectedRowFences(t *testing.T) {
	ctx, pool, repository := setupRepository(t)
	now := time.Date(2026, time.July, 23, 10, 0, 0, 0, time.UTC)
	primary := newUser(now)
	secondary := newUser(now.Add(time.Minute))
	disabled := newUser(now.Add(2 * time.Minute))
	disabled.Status = identity.UserDisabled
	email := identity.ExternalIdentity{
		ID:         uuid.New(),
		UserID:     primary.ID,
		Kind:       identity.IdentityEmail,
		Issuer:     "email",
		Subject:    "unique@example.com",
		VerifiedAt: now,
		CreatedAt:  now,
	}
	session := newSession(primary.ID, identity.ClientWeb, now)
	token := newRefreshToken(session.ID, 3, now)
	replacement := newRefreshToken(session.ID, 4, now.Add(time.Minute))

	err := repository.WithinTx(ctx, func(tx identity.Tx) error {
		for _, user := range []identity.User{primary, secondary, disabled} {
			if err := tx.InsertUser(ctx, user); err != nil {
				return err
			}
		}
		if err := tx.InsertIdentity(ctx, email); err != nil {
			return err
		}
		if err := tx.InsertSession(ctx, session); err != nil {
			return err
		}
		if err := tx.InsertRefreshToken(ctx, token); err != nil {
			return err
		}
		return tx.InsertRefreshToken(ctx, replacement)
	})
	if err != nil {
		t.Fatal(err)
	}

	duplicate := email
	duplicate.ID = uuid.New()
	duplicate.UserID = secondary.ID
	err = repository.WithinTx(ctx, func(tx identity.Tx) error {
		return tx.InsertIdentity(ctx, duplicate)
	})
	if !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("duplicate identity error = %v, want ErrConflict", err)
	}

	err = repository.WithinTx(ctx, func(tx identity.Tx) error {
		if err := tx.UpdateIdentityUnionID(ctx, email.ID, "union-first"); err != nil {
			return err
		}
		if err := tx.UpdateIdentityUnionID(ctx, email.ID, "union-second"); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("second union update error = %v, want ErrConflict", err)
		}
		if err := tx.UpdateIdentityUnionID(ctx, email.ID, ""); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("empty union update error = %v, want ErrConflict", err)
		}
		if err := tx.ExtendSession(ctx, session.ID, now.Add(time.Minute), now.Add(31*24*time.Hour)); err != nil {
			return err
		}
		if err := tx.ConsumeRefreshToken(ctx, token.ID, now.Add(time.Minute), replacement.ID); err != nil {
			return err
		}
		if err := tx.ConsumeRefreshToken(ctx, token.ID, now.Add(2*time.Minute), replacement.ID); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("second consume error = %v, want ErrConflict", err)
		}
		if err := tx.MarkUserMerged(ctx, disabled.ID, primary.ID, now); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("disabled merge error = %v, want ErrConflict", err)
		}
		if err := tx.MarkUserMerged(ctx, secondary.ID, primary.ID, now); err != nil {
			return err
		}
		if err := tx.MarkUserMerged(ctx, secondary.ID, primary.ID, now); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("second merge error = %v, want ErrConflict", err)
		}
		if err := tx.RecordMerge(ctx, uuid.New(), primary.ID, secondary.ID, session.ID, now); err != nil {
			return err
		}
		if err := tx.RevokeSession(ctx, session.ID, now.Add(3*time.Minute), "logout"); err != nil {
			return err
		}
		if err := tx.RevokeSession(ctx, session.ID, now.Add(4*time.Minute), "logout"); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("second revoke error = %v, want ErrConflict", err)
		}
		if err := tx.ExtendSession(ctx, session.ID, now.Add(5*time.Minute), now.Add(32*24*time.Hour)); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("revoked session extension error = %v, want ErrConflict", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = repository.WithinTx(ctx, func(tx identity.Tx) error {
		return tx.RecordMerge(
			ctx,
			uuid.New(),
			primary.ID,
			secondary.ID,
			session.ID,
			now.Add(time.Minute),
		)
	})
	if !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("duplicate merge record error = %v, want ErrConflict", err)
	}

	var unionID string
	if err := pool.QueryRow(ctx, "SELECT union_id FROM identity_identities WHERE id = $1", email.ID).Scan(&unionID); err != nil {
		t.Fatal(err)
	}
	if unionID != "union-first" {
		t.Fatalf("union ID = %q, want first provider value", unionID)
	}
}

func TestRepositoryRevokeUserSessionsAlsoRevokesTokens(t *testing.T) {
	ctx, pool, repository := setupRepository(t)
	now := time.Date(2026, time.July, 23, 11, 0, 0, 0, time.UTC)
	user := newUser(now)
	sessions := []identity.Session{
		newSession(user.ID, identity.ClientWeb, now),
		newSession(user.ID, identity.ClientWeChatMini, now.Add(time.Minute)),
	}

	err := repository.WithinTx(ctx, func(tx identity.Tx) error {
		if err := tx.InsertUser(ctx, user); err != nil {
			return err
		}
		for index, session := range sessions {
			if err := tx.InsertSession(ctx, session); err != nil {
				return err
			}
			if err := tx.InsertRefreshToken(ctx, newRefreshToken(session.ID, byte(index+10), now)); err != nil {
				return err
			}
		}
		return tx.RevokeUserSessions(ctx, user.ID, now.Add(2*time.Minute), "logout_all")
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSessionAndTokensRevoked(
		t,
		ctx,
		pool,
		[]uuid.UUID{sessions[0].ID, sessions[1].ID},
		now.Add(2*time.Minute),
		"logout_all",
	)
}

func TestRepositoryForUpdateAndStateErrors(t *testing.T) {
	ctx, _, repository := setupRepository(t)
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	user := newUser(now)
	if err := repository.WithinTx(ctx, func(tx identity.Tx) error {
		return tx.InsertUser(ctx, user)
	}); err != nil {
		t.Fatal(err)
	}

	locked := make(chan struct{})
	release := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- repository.WithinTx(context.Background(), func(tx identity.Tx) error {
			if _, err := tx.FindUser(context.Background(), user.ID, true); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for row lock")
	}

	if _, _, err := repository.UserSummary(ctx, user.ID); err != nil {
		close(release)
		t.Fatalf("unlocked UserSummary blocked by row lock: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	err := repository.WithinTx(waitCtx, func(tx identity.Tx) error {
		_, err := tx.FindUser(waitCtx, user.ID, true)
		return err
	})
	cancel()
	if !errors.Is(err, identity.ErrStateUnavailable) {
		close(release)
		t.Fatalf("lock timeout error = %v, want ErrStateUnavailable", err)
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatalf("lock holder transaction: %v", err)
	}

	err = repository.WithinTx(ctx, func(tx identity.Tx) error {
		_, err := tx.FindUser(ctx, uuid.New(), false)
		return err
	})
	if !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("missing user error = %v, want ErrNotFound", err)
	}

	closedPool, err := pgxpool.New(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	closedRepository := identitypostgres.NewRepository(closedPool)
	closedPool.Close()
	err = closedRepository.WithinTx(ctx, func(identity.Tx) error { return nil })
	if !errors.Is(err, identity.ErrStateUnavailable) {
		t.Fatalf("closed pool error = %v, want ErrStateUnavailable", err)
	}

}

func TestRepositoryConcurrentIdentityConflict(t *testing.T) {
	ctx, _, repository := setupRepository(t)
	now := time.Date(2026, time.July, 23, 13, 0, 0, 0, time.UTC)
	users := []identity.User{newUser(now), newUser(now.Add(time.Minute))}
	if err := repository.WithinTx(ctx, func(tx identity.Tx) error {
		for _, user := range users {
			if err := tx.InsertUser(ctx, user); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, user := range users {
		user := user
		go func() {
			ready.Done()
			<-start
			external := identity.ExternalIdentity{
				ID:         uuid.New(),
				UserID:     user.ID,
				Kind:       identity.IdentityEmail,
				Issuer:     "email",
				Subject:    "race@example.com",
				VerifiedAt: now,
				CreatedAt:  now,
			}
			results <- repository.WithinTx(ctx, func(tx identity.Tx) error {
				return tx.InsertIdentity(ctx, external)
			})
		}()
	}
	ready.Wait()
	close(start)

	var successes, conflicts int
	for range users {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, identity.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent insert error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1 and 1", successes, conflicts)
	}
}

func setupRepository(t *testing.T) (context.Context, *pgxpool.Pool, identity.Repository) {
	t.Helper()

	databaseURL := testDatabaseURL(t)
	ctx := context.Background()
	testdb.LockSchema(t, ctx, databaseURL)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := bootstrap.RunMigrate(ctx, config.Config{
		Postgres: config.Postgres{URL: databaseURL},
	}, logger); err != nil {
		t.Fatal(err)
	}

	pool, err := platformpostgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, truncateIdentityTables); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, identitypostgres.NewRepository(pool)
}

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}
	return databaseURL
}

func newUser(now time.Time) identity.User {
	return identity.User{
		ID:        uuid.New(),
		Status:    identity.UserActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func newSession(userID uuid.UUID, client identity.ClientKind, now time.Time) identity.Session {
	return identity.Session{
		ID:         uuid.New(),
		UserID:     userID,
		Client:     client,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
	}
}

func newRefreshToken(sessionID uuid.UUID, hashByte byte, now time.Time) identity.RefreshTokenRecord {
	var hash [32]byte
	for index := range hash {
		hash[index] = hashByte
	}
	return identity.RefreshTokenRecord{
		ID:        uuid.New(),
		SessionID: sessionID,
		Hash:      hash,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
}

func usersEqual(left, right identity.User) bool {
	return left.ID == right.ID &&
		left.Status == right.Status &&
		uuidPointersEqual(left.MergedInto, right.MergedInto) &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		left.UpdatedAt.Equal(right.UpdatedAt)
}

func sessionsEqual(left, right identity.Session) bool {
	return left.ID == right.ID &&
		left.UserID == right.UserID &&
		left.Client == right.Client &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		left.LastSeenAt.Equal(right.LastSeenAt) &&
		left.ExpiresAt.Equal(right.ExpiresAt) &&
		timePointersEqual(left.RevokedAt, right.RevokedAt)
}

func externalIdentitiesEqual(left, right identity.ExternalIdentity) bool {
	return left.ID == right.ID &&
		left.UserID == right.UserID &&
		left.Kind == right.Kind &&
		left.Issuer == right.Issuer &&
		left.Subject == right.Subject &&
		left.UnionID == right.UnionID &&
		left.VerifiedAt.Equal(right.VerifiedAt) &&
		left.CreatedAt.Equal(right.CreatedAt)
}

func refreshTokensEqual(left, right identity.RefreshTokenRecord) bool {
	return left.ID == right.ID &&
		left.SessionID == right.SessionID &&
		left.Hash == right.Hash &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		left.ExpiresAt.Equal(right.ExpiresAt) &&
		timePointersEqual(left.ConsumedAt, right.ConsumedAt) &&
		timePointersEqual(left.RevokedAt, right.RevokedAt) &&
		uuidPointersEqual(left.ReplacementID, right.ReplacementID)
}

func uuidPointersEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func timePointersEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func wechatWithUser(external identity.ExternalIdentity, userID uuid.UUID) identity.ExternalIdentity {
	external.UserID = userID
	return external
}

func assertUserAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM identity_users WHERE id = $1", userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("user %s exists after rollback", userID)
	}
}

func assertSessionAndTokensRevoked(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	sessionIDs []uuid.UUID,
	revokedAt time.Time,
	reason string,
) {
	t.Helper()

	var sessionCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM identity_sessions
		WHERE id = ANY($1)
		  AND revoked_at = $2
		  AND revoked_reason = $3
	`, sessionIDs, revokedAt, reason).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != len(sessionIDs) {
		t.Fatalf("revoked session count = %d, want %d", sessionCount, len(sessionIDs))
	}

	var tokenCount, revokedTokenCount int
	if err := pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE revoked_at = $2)
		FROM identity_refresh_tokens
		WHERE session_id = ANY($1)
	`, sessionIDs, revokedAt).Scan(&tokenCount, &revokedTokenCount); err != nil {
		t.Fatal(err)
	}
	if tokenCount == 0 || revokedTokenCount != tokenCount {
		t.Fatalf("revoked token count = %d of %d, want every token revoked", revokedTokenCount, tokenCount)
	}
}
