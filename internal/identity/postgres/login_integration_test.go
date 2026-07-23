package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/google/uuid"
)

func TestLoginWeChatConcurrentFirstLoginRetriesRealUniqueConflict(t *testing.T) {
	ctx, pool, durableRepository := setupRepository(t)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	barrier := newFirstIdentityLookupBarrier()
	repository := &firstLookupBarrierRepository{
		Repository: durableRepository,
		barrier:    barrier,
	}
	service, err := identity.NewService(identity.Dependencies{
		Repository:  repository,
		OTPStore:    integrationOTPStore{},
		EmailSender: integrationEmailSender{},
		WeChatExchanger: integrationWeChatExchanger{identity: identity.WeChatIdentity{
			AppID:   "wx-concurrent-app",
			OpenID:  "openid-concurrent-first-login",
			UnionID: "unionid-concurrent-first-login",
		}},
		TokenManager: integrationTokenIssuer{},
	}, integrationLoginPolicy())
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan identity.LoginResult, 2)
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for index := 0; index < 2; index++ {
		go func() {
			ready.Done()
			<-start
			result, err := service.LoginWeChat(
				ctx,
				"one-time-code",
				"198.51.100.25",
				identity.ClientWeChatMini,
			)
			results <- result
			errs <- err
		}()
	}
	ready.Wait()
	close(start)

	loginResults := make([]identity.LoginResult, 0, 2)
	for index := 0; index < 2; index++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("login %d: %v", index+1, err)
			}
			loginResults = append(loginResults, <-results)
		case <-ctx.Done():
			t.Fatalf("login %d timed out: %v", index+1, ctx.Err())
		}
	}
	if loginResults[0].User.ID == uuid.Nil ||
		loginResults[0].User.ID != loginResults[1].User.ID {
		t.Fatalf(
			"login users = %s and %s",
			loginResults[0].User.ID,
			loginResults[1].User.ID,
		)
	}
	if loginResults[0].RefreshToken == "" ||
		loginResults[1].RefreshToken == "" ||
		loginResults[0].RefreshToken == loginResults[1].RefreshToken {
		t.Fatalf("refresh tokens were not independently issued")
	}
	if got := barrier.lookupCount(); got != 3 {
		t.Fatalf("FindIdentity calls = %d, want 3 (two misses plus retry)", got)
	}
	if got := barrier.notFoundCount(); got != 2 {
		t.Fatalf("FindIdentity misses = %d, want 2", got)
	}

	var users, identities, sessions, tokens, orphans int
	err = pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM identity_users),
			(SELECT count(*) FROM identity_identities),
			(SELECT count(*) FROM identity_sessions),
			(SELECT count(*) FROM identity_refresh_tokens),
			(
				SELECT count(*)
				FROM identity_identities i
				LEFT JOIN identity_users u ON u.id = i.user_id
				WHERE u.id IS NULL
			) + (
				SELECT count(*)
				FROM identity_sessions s
				LEFT JOIN identity_users u ON u.id = s.user_id
				WHERE u.id IS NULL
			) + (
				SELECT count(*)
				FROM identity_refresh_tokens r
				LEFT JOIN identity_sessions s ON s.id = r.session_id
				WHERE s.id IS NULL
			)
	`).Scan(&users, &identities, &sessions, &tokens, &orphans)
	if err != nil {
		t.Fatal(err)
	}
	if users != 1 || identities != 1 || sessions != 2 || tokens != 2 || orphans != 0 {
		t.Fatalf(
			"users=%d identities=%d sessions=%d tokens=%d orphans=%d",
			users,
			identities,
			sessions,
			tokens,
			orphans,
		)
	}
}

type firstIdentityLookupBarrier struct {
	mu      sync.Mutex
	lookups int
	misses  int
	release chan struct{}
}

func newFirstIdentityLookupBarrier() *firstIdentityLookupBarrier {
	return &firstIdentityLookupBarrier{release: make(chan struct{})}
}

func (barrier *firstIdentityLookupBarrier) afterNotFound(ctx context.Context) error {
	barrier.mu.Lock()
	barrier.misses++
	count := barrier.misses
	if count == 2 {
		close(barrier.release)
	}
	barrier.mu.Unlock()

	if count > 2 {
		return nil
	}
	select {
	case <-barrier.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (barrier *firstIdentityLookupBarrier) lookupCount() int {
	barrier.mu.Lock()
	defer barrier.mu.Unlock()
	return barrier.lookups
}

func (barrier *firstIdentityLookupBarrier) recordLookup() {
	barrier.mu.Lock()
	defer barrier.mu.Unlock()
	barrier.lookups++
}

func (barrier *firstIdentityLookupBarrier) notFoundCount() int {
	barrier.mu.Lock()
	defer barrier.mu.Unlock()
	return barrier.misses
}

type firstLookupBarrierRepository struct {
	identity.Repository
	barrier *firstIdentityLookupBarrier
}

func (repository *firstLookupBarrierRepository) WithinTx(
	ctx context.Context,
	fn func(identity.Tx) error,
) error {
	return repository.Repository.WithinTx(ctx, func(tx identity.Tx) error {
		return fn(&firstLookupBarrierTx{
			Tx:      tx,
			barrier: repository.barrier,
		})
	})
}

type firstLookupBarrierTx struct {
	identity.Tx
	barrier *firstIdentityLookupBarrier
}

func (tx *firstLookupBarrierTx) FindIdentity(
	ctx context.Context,
	kind identity.IdentityKind,
	issuer string,
	subject string,
	forUpdate bool,
) (identity.ExternalIdentity, error) {
	tx.barrier.recordLookup()
	external, err := tx.Tx.FindIdentity(ctx, kind, issuer, subject, forUpdate)
	if !errors.Is(err, identity.ErrNotFound) {
		return external, err
	}
	if barrierErr := tx.barrier.afterNotFound(ctx); barrierErr != nil {
		return identity.ExternalIdentity{}, barrierErr
	}
	return external, err
}

type integrationOTPStore struct{}

func (integrationOTPStore) Issue(context.Context, identity.OTPChallenge) error {
	return nil
}
func (integrationOTPStore) Verify(context.Context, identity.OTPAttempt) error {
	return nil
}
func (integrationOTPStore) DeleteIfMatch(context.Context, identity.OTPChallenge) error {
	return nil
}
func (integrationOTPStore) Allow(context.Context, identity.RateLimit) error {
	return nil
}

type integrationEmailSender struct{}

func (integrationEmailSender) SendCode(
	context.Context,
	string,
	string,
	time.Duration,
) error {
	return nil
}

type integrationWeChatExchanger struct {
	identity identity.WeChatIdentity
}

func (exchanger integrationWeChatExchanger) Exchange(
	context.Context,
	string,
) (identity.WeChatIdentity, error) {
	return exchanger.identity, nil
}

type integrationTokenIssuer struct{}

func (integrationTokenIssuer) IssueAccess(
	principal identity.Principal,
	now time.Time,
) (string, time.Time, error) {
	return fmt.Sprintf("access:%s", principal.SessionID), now.Add(15 * time.Minute), nil
}

func integrationLoginPolicy() identity.Policy {
	return identity.Policy{
		OTPPepper:       []byte("0123456789abcdef0123456789abcdef"),
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

var _ identity.Repository = (*firstLookupBarrierRepository)(nil)
var _ identity.Tx = (*firstLookupBarrierTx)(nil)
