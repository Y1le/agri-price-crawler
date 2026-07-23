package redisotp_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/Y1le/agri-price-crawler/internal/identity/redisotp"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestIssueEnforcesCooldownAndHourlyLimits(t *testing.T) {
	t.Run("cooldown", func(t *testing.T) {
		ctx, store, _ := setupStore(t)
		challenge := testChallenge("cooldown-email", "cooldown-ip")

		if err := store.Issue(ctx, challenge); err != nil {
			t.Fatal(err)
		}
		if err := store.Issue(ctx, challenge); !errors.Is(err, identity.ErrRateLimited) {
			t.Fatalf("second Issue() error = %v, want ErrRateLimited", err)
		}
	})

	t.Run("email hourly limit", func(t *testing.T) {
		ctx, store, _ := setupStore(t)
		challenge := testChallenge("limited-email", "email-limit-ip")
		challenge.MaxEmail = 2

		for attempt := 0; attempt < challenge.MaxEmail; attempt++ {
			challenge.Digest = fmt.Sprintf("email-digest-%d", attempt)
			if err := store.Issue(ctx, challenge); err != nil {
				t.Fatalf("Issue() attempt %d: %v", attempt+1, err)
			}
			if err := store.DeleteIfMatch(ctx, challenge); err != nil {
				t.Fatal(err)
			}
		}

		challenge.Digest = "email-digest-blocked"
		if err := store.Issue(ctx, challenge); !errors.Is(err, identity.ErrRateLimited) {
			t.Fatalf("Issue() over email limit error = %v, want ErrRateLimited", err)
		}
	})

	t.Run("IP hourly limit", func(t *testing.T) {
		ctx, store, _ := setupStore(t)
		for attempt := 0; attempt < 2; attempt++ {
			challenge := testChallenge(fmt.Sprintf("ip-email-%d", attempt), "limited-ip")
			challenge.MaxIP = 2
			if err := store.Issue(ctx, challenge); err != nil {
				t.Fatalf("Issue() attempt %d: %v", attempt+1, err)
			}
		}

		challenge := testChallenge("ip-email-blocked", "limited-ip")
		challenge.MaxIP = 2
		if err := store.Issue(ctx, challenge); !errors.Is(err, identity.ErrRateLimited) {
			t.Fatalf("Issue() over IP limit error = %v, want ErrRateLimited", err)
		}
	})
}

func TestVerifyDecrementsAttemptsAndDeletesAfterFifthFailure(t *testing.T) {
	ctx, store, _ := setupStore(t)
	challenge := testChallenge("attempt-email", "attempt-ip")
	challenge.Attempts = 5
	if err := store.Issue(ctx, challenge); err != nil {
		t.Fatal(err)
	}

	attempt := identity.OTPAttempt{
		EmailKey: challenge.EmailKey,
		Digest:   "wrong-digest",
		Purpose:  challenge.Purpose,
		Owner:    challenge.Owner,
	}
	for remaining := 4; remaining >= 0; remaining-- {
		if err := store.Verify(ctx, attempt); !errors.Is(err, identity.ErrCodeInvalid) {
			t.Fatalf("Verify() with %d attempts remaining error = %v, want ErrCodeInvalid", remaining, err)
		}
	}
	if err := store.Verify(ctx, attempt); !errors.Is(err, identity.ErrCodeExpired) {
		t.Fatalf("Verify() after fifth failure error = %v, want ErrCodeExpired", err)
	}
}

func TestVerifyConsumesChallengeOnce(t *testing.T) {
	ctx, store, _ := setupStore(t)
	challenge := testChallenge("consume-email", "consume-ip")
	if err := store.Issue(ctx, challenge); err != nil {
		t.Fatal(err)
	}

	attempt := matchingAttempt(challenge)
	if err := store.Verify(ctx, attempt); err != nil {
		t.Fatalf("first Verify(): %v", err)
	}
	if err := store.Verify(ctx, attempt); !errors.Is(err, identity.ErrCodeExpired) {
		t.Fatalf("second Verify() error = %v, want ErrCodeExpired", err)
	}
}

func TestVerifyReportsExpiredChallenge(t *testing.T) {
	ctx, store, _ := setupStore(t)
	challenge := testChallenge("expiry-email", "expiry-ip")
	challenge.TTL = 50 * time.Millisecond
	if err := store.Issue(ctx, challenge); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)
	if err := store.Verify(ctx, matchingAttempt(challenge)); !errors.Is(err, identity.ErrCodeExpired) {
		t.Fatalf("Verify() error = %v, want ErrCodeExpired", err)
	}
}

func TestVerifyIsolatesPurposeAndOwner(t *testing.T) {
	t.Run("purpose", func(t *testing.T) {
		ctx, store, _ := setupStore(t)
		login := testChallenge("purpose-email", "purpose-ip")
		login.Purpose = "login"
		login.Digest = "login-digest"
		bind := login
		bind.Purpose = "bind"
		bind.Owner = uuid.NewString()
		bind.Digest = "bind-digest"

		if err := store.Issue(ctx, login); err != nil {
			t.Fatal(err)
		}
		if err := store.Issue(ctx, bind); err != nil {
			t.Fatalf("Issue() for separate purpose: %v", err)
		}
		if err := store.Verify(ctx, identity.OTPAttempt{
			EmailKey: login.EmailKey,
			Digest:   bind.Digest,
			Purpose:  login.Purpose,
			Owner:    login.Owner,
		}); !errors.Is(err, identity.ErrCodeInvalid) {
			t.Fatalf("cross-purpose Verify() error = %v, want ErrCodeInvalid", err)
		}
		if err := store.Verify(ctx, matchingAttempt(login)); err != nil {
			t.Fatalf("login Verify(): %v", err)
		}
		if err := store.Verify(ctx, matchingAttempt(bind)); err != nil {
			t.Fatalf("bind Verify(): %v", err)
		}
	})

	t.Run("owner", func(t *testing.T) {
		ctx, store, _ := setupStore(t)
		challenge := testChallenge("owner-email", "owner-ip")
		challenge.Purpose = "bind"
		challenge.Owner = uuid.NewString()
		if err := store.Issue(ctx, challenge); err != nil {
			t.Fatal(err)
		}

		attempt := matchingAttempt(challenge)
		attempt.Owner = uuid.NewString()
		if err := store.Verify(ctx, attempt); !errors.Is(err, identity.ErrCodeInvalid) {
			t.Fatalf("wrong-owner Verify() error = %v, want ErrCodeInvalid", err)
		}
		if err := store.Verify(ctx, matchingAttempt(challenge)); err != nil {
			t.Fatalf("right-owner Verify(): %v", err)
		}
	})
}

func TestDeleteIfMatchDoesNotDeleteNewerChallengeOrHourlyCounters(t *testing.T) {
	ctx, store, _ := setupStore(t)
	oldChallenge := testChallenge("delete-email", "delete-ip")
	oldChallenge.MaxEmail = 2
	if err := store.Issue(ctx, oldChallenge); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteIfMatch(ctx, oldChallenge); err != nil {
		t.Fatal(err)
	}

	newChallenge := oldChallenge
	newChallenge.Digest = "newer-digest"
	if err := store.Issue(ctx, newChallenge); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteIfMatch(ctx, oldChallenge); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(ctx, matchingAttempt(newChallenge)); err != nil {
		t.Fatalf("newer challenge was deleted: %v", err)
	}

	anotherPurpose := newChallenge
	anotherPurpose.Purpose = "bind"
	anotherPurpose.Owner = uuid.NewString()
	anotherPurpose.Digest = "over-hourly-limit"
	if err := store.Issue(ctx, anotherPurpose); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatalf("Issue() after delivery cleanup error = %v, want preserved hourly rate limit", err)
	}
}

func TestAllowUsesAtomicFixedWindow(t *testing.T) {
	t.Run("limit and expiry", func(t *testing.T) {
		ctx, store, _ := setupStore(t)
		limit := identity.RateLimit{Key: "wechat-ip", Max: 2, Window: 100 * time.Millisecond}
		for attempt := 0; attempt < limit.Max; attempt++ {
			if err := store.Allow(ctx, limit); err != nil {
				t.Fatalf("Allow() attempt %d: %v", attempt+1, err)
			}
		}
		if err := store.Allow(ctx, limit); !errors.Is(err, identity.ErrRateLimited) {
			t.Fatalf("Allow() over limit error = %v, want ErrRateLimited", err)
		}

		time.Sleep(200 * time.Millisecond)
		if err := store.Allow(ctx, limit); err != nil {
			t.Fatalf("Allow() after fixed window expiry: %v", err)
		}
	})

	t.Run("concurrent callers cannot exceed maximum", func(t *testing.T) {
		ctx, store, _ := setupStore(t)
		limit := identity.RateLimit{Key: "concurrent-ip", Max: 8, Window: time.Minute}
		var allowed atomic.Int64
		var rateLimited atomic.Int64
		unexpected := make(chan error, 32)
		var wait sync.WaitGroup
		for caller := 0; caller < 32; caller++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				err := store.Allow(ctx, limit)
				switch {
				case err == nil:
					allowed.Add(1)
				case errors.Is(err, identity.ErrRateLimited):
					rateLimited.Add(1)
				default:
					unexpected <- err
				}
			}()
		}
		wait.Wait()
		close(unexpected)
		if err := <-unexpected; err != nil {
			t.Fatalf("Allow() unexpected error: %v", err)
		}
		if got := allowed.Load(); got != int64(limit.Max) {
			t.Fatalf("allowed = %d, want %d", got, limit.Max)
		}
		if got := rateLimited.Load(); got != int64(32-limit.Max) {
			t.Fatalf("rate limited = %d, want %d", got, 32-limit.Max)
		}
	})
}

func TestStoreMapsRedisFailuresWithoutLeakingState(t *testing.T) {
	const secretPrefix = "secret-prefix"
	const secretEmailKey = "secret-email-key"
	const secretDigest = "secret-code-digest"
	const unavailableAddress = "127.0.0.1:1"

	client := redis.NewClient(&redis.Options{
		Addr:          unavailableAddress,
		DialTimeout:   20 * time.Millisecond,
		ReadTimeout:   20 * time.Millisecond,
		WriteTimeout:  20 * time.Millisecond,
		MaxRetries:    -1,
		DialerRetries: 1,
	})
	t.Cleanup(func() { _ = client.Close() })
	store := redisotp.New(client, secretPrefix)
	challenge := testChallenge(secretEmailKey, "secret-ip-key")
	challenge.Digest = secretDigest

	err := store.Issue(context.Background(), challenge)
	if !errors.Is(err, identity.ErrStateUnavailable) {
		t.Fatalf("Issue() error = %v, want ErrStateUnavailable", err)
	}
	for _, secret := range []string{secretPrefix, secretEmailKey, secretDigest, unavailableAddress} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("Issue() error leaks %q: %v", secret, err)
		}
	}
}

func TestStoreKeysDoNotContainRawEmail(t *testing.T) {
	ctx, store, client := setupStore(t)
	const rawEmail = "Farmer.Private@example.com"
	challenge := testChallenge("5d2b1ce910b0288f7cc726687fc7644b45425c0af8b9bb5b91c3893b5d36d959", "hashed-ip")
	if err := store.Issue(ctx, challenge); err != nil {
		t.Fatal(err)
	}

	keys := scanKeys(t, ctx, client, "*")
	for _, key := range keys {
		if strings.Contains(strings.ToLower(key), strings.ToLower(rawEmail)) {
			t.Fatalf("Redis key exposes raw email: %q", key)
		}
	}
}

func setupStore(t *testing.T) (context.Context, identity.OTPStore, redis.UniversalClient) {
	t.Helper()
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping test Redis: %v", err)
	}

	prefix := "test:identity:otp:" + uuid.NewString()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		keys := scanKeys(t, cleanupCtx, client, prefix+"*")
		if len(keys) > 0 {
			if err := client.Del(cleanupCtx, keys...).Err(); err != nil {
				t.Errorf("delete test Redis keys: %v", err)
			}
		}
		if err := client.Close(); err != nil {
			t.Errorf("close test Redis client: %v", err)
		}
	})
	return ctx, redisotp.New(client, prefix), client
}

func scanKeys(t *testing.T, ctx context.Context, client redis.UniversalClient, pattern string) []string {
	t.Helper()
	var (
		cursor uint64
		keys   []string
	)
	for {
		batch, next, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			t.Fatalf("scan test Redis keys: %v", err)
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			return keys
		}
	}
}

func testChallenge(emailKey, ip string) identity.OTPChallenge {
	return identity.OTPChallenge{
		EmailKey: emailKey,
		Digest:   "expected-digest",
		Purpose:  "login",
		Owner:    "",
		IP:       ip,
		TTL:      time.Minute,
		Cooldown: time.Minute,
		Window:   time.Hour,
		MaxEmail: 5,
		MaxIP:    30,
		Attempts: 5,
	}
}

func matchingAttempt(challenge identity.OTPChallenge) identity.OTPAttempt {
	return identity.OTPAttempt{
		EmailKey: challenge.EmailKey,
		Digest:   challenge.Digest,
		Purpose:  challenge.Purpose,
		Owner:    challenge.Owner,
	}
}
