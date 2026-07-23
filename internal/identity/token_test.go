package identity_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const (
	testKeyID    = "identity-test-v1"
	testIssuer   = "agri-price-crawler"
	testAudience = "agri-clients"
)

var (
	testNow  = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	testSeed = bytes.Repeat([]byte{1}, ed25519.SeedSize)
	testKey  = ed25519.NewKeyFromSeed(testSeed)
)

func TestTokenManagerRoundTrip(t *testing.T) {
	manager := newTestTokenManager(t)
	want := identity.Principal{UserID: uuid.New(), SessionID: uuid.New()}

	raw, expiresAt, err := manager.IssueAccess(want, testNow)
	if err != nil {
		t.Fatalf("IssueAccess() error = %v", err)
	}
	if wantExpiresAt := testNow.Add(15 * time.Minute); !expiresAt.Equal(wantExpiresAt) {
		t.Fatalf("IssueAccess() expiresAt = %v, want %v", expiresAt, wantExpiresAt)
	}

	parser := jwt.Parser{SkipClaimsValidation: true}
	token, _, err := parser.ParseUnverified(raw, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("ParseUnverified() error = %v", err)
	}
	if got := token.Header["alg"]; got != jwt.SigningMethodEdDSA.Alg() {
		t.Fatalf("alg = %v, want %q", got, jwt.SigningMethodEdDSA.Alg())
	}
	if got := token.Header["kid"]; got != testKeyID {
		t.Fatalf("kid = %v, want %q", got, testKeyID)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type = %T, want jwt.MapClaims", token.Claims)
	}
	for claim, want := range map[string]string{
		"iss": testIssuer,
		"sub": want.UserID.String(),
		"sid": want.SessionID.String(),
	} {
		if got := claims[claim]; got != want {
			t.Errorf("%s = %v, want %q", claim, got, want)
		}
	}
	audience, ok := claims["aud"].([]any)
	if !ok || len(audience) != 1 || audience[0] != testAudience {
		t.Errorf("aud = %v, want [%q]", claims["aud"], testAudience)
	}
	if jti, ok := claims["jti"].(string); !ok || uuid.Validate(jti) != nil {
		t.Errorf("jti = %v, want UUID string", claims["jti"])
	}
	if got := int64(claims["iat"].(float64)); got != testNow.Unix() {
		t.Errorf("iat = %d, want %d", got, testNow.Unix())
	}
	if got := int64(claims["exp"].(float64)); got != expiresAt.Unix() {
		t.Errorf("exp = %d, want %d", got, expiresAt.Unix())
	}

	got, err := manager.ParseAccess(raw, testNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("ParseAccess() error = %v", err)
	}
	if got != want {
		t.Fatalf("ParseAccess() = %+v, want %+v", got, want)
	}

	var _ identity.AccessTokenIssuer = manager
}

func TestNewTokenManagerRejectsInvalidConfig(t *testing.T) {
	valid := identity.TokenConfig{
		PrivateKey: testKey,
		KeyID:      testKeyID,
		Issuer:     testIssuer,
		Audience:   testAudience,
		AccessTTL:  15 * time.Minute,
	}

	tests := []struct {
		name   string
		mutate func(*identity.TokenConfig)
	}{
		{name: "missing private key", mutate: func(c *identity.TokenConfig) { c.PrivateKey = nil }},
		{name: "short private key", mutate: func(c *identity.TokenConfig) { c.PrivateKey = ed25519.PrivateKey("short") }},
		{name: "missing key id", mutate: func(c *identity.TokenConfig) { c.KeyID = "" }},
		{name: "missing issuer", mutate: func(c *identity.TokenConfig) { c.Issuer = "" }},
		{name: "missing audience", mutate: func(c *identity.TokenConfig) { c.Audience = "" }},
		{name: "zero ttl", mutate: func(c *identity.TokenConfig) { c.AccessTTL = 0 }},
		{name: "negative ttl", mutate: func(c *identity.TokenConfig) { c.AccessTTL = -time.Second }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.mutate(&config)
			if _, err := identity.NewTokenManager(config); err == nil {
				t.Fatal("NewTokenManager() error = nil, want non-nil")
			}
		})
	}
}

func TestTokenManagerRejectsInvalidPrincipal(t *testing.T) {
	manager := newTestTokenManager(t)
	tests := []identity.Principal{
		{UserID: uuid.Nil, SessionID: uuid.New()},
		{UserID: uuid.New(), SessionID: uuid.Nil},
	}
	for _, principal := range tests {
		if raw, _, err := manager.IssueAccess(principal, testNow); !errors.Is(err, identity.ErrTokenInvalid) || raw != "" {
			t.Fatalf("IssueAccess(%+v) = %q, _, %v; want empty token and ErrTokenInvalid", principal, raw, err)
		}
	}
}

func TestTokenManagerRejectsInvalidTokens(t *testing.T) {
	manager := newTestTokenManager(t)
	userID := uuid.New()
	sessionID := uuid.New()
	validClaims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": testIssuer,
			"aud": testAudience,
			"sub": userID.String(),
			"sid": sessionID.String(),
			"jti": uuid.NewString(),
			"iat": testNow.Unix(),
			"exp": testNow.Add(15 * time.Minute).Unix(),
		}
	}

	otherKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, ed25519.SeedSize))
	tests := []struct {
		name string
		raw  func(t *testing.T) string
	}{
		{name: "empty", raw: func(*testing.T) string { return "" }},
		{name: "broken compact token", raw: func(*testing.T) string { return "not.a.jwt" }},
		{name: "wrong algorithm", raw: func(t *testing.T) string {
			return signToken(t, jwt.SigningMethodHS256, []byte("not-an-ed25519-key"), testKeyID, validClaims())
		}},
		{name: "missing key id", raw: func(t *testing.T) string {
			return signToken(t, jwt.SigningMethodEdDSA, testKey, "", validClaims())
		}},
		{name: "wrong key id", raw: func(t *testing.T) string {
			return signToken(t, jwt.SigningMethodEdDSA, testKey, "old-key", validClaims())
		}},
		{name: "wrong signature", raw: func(t *testing.T) string {
			return signToken(t, jwt.SigningMethodEdDSA, otherKey, testKeyID, validClaims())
		}},
		{name: "wrong issuer", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["iss"] = "other-issuer"
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "missing issuer", raw: func(t *testing.T) string {
			claims := validClaims()
			delete(claims, "iss")
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "wrong audience", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["aud"] = "other-clients"
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "missing audience", raw: func(t *testing.T) string {
			claims := validClaims()
			delete(claims, "aud")
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "expired", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["exp"] = testNow.Unix()
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "missing expiry", raw: func(t *testing.T) string {
			claims := validClaims()
			delete(claims, "exp")
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "future issued at", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["iat"] = testNow.Add(time.Second).Unix()
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "missing issued at", raw: func(t *testing.T) string {
			claims := validClaims()
			delete(claims, "iat")
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "expiry before issued at", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["exp"] = testNow.Add(-time.Second).Unix()
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "malformed user id", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["sub"] = "not-a-uuid"
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "nil user id", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["sub"] = uuid.Nil.String()
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "malformed session id", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["sid"] = "not-a-uuid"
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "nil session id", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["sid"] = uuid.Nil.String()
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "missing token id", raw: func(t *testing.T) string {
			claims := validClaims()
			delete(claims, "jti")
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
		{name: "malformed token id", raw: func(t *testing.T) string {
			claims := validClaims()
			claims["jti"] = "not-a-uuid"
			return signToken(t, jwt.SigningMethodEdDSA, testKey, testKeyID, claims)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := tt.raw(t)
			got, err := manager.ParseAccess(raw, testNow)
			if !errors.Is(err, identity.ErrTokenInvalid) {
				t.Fatalf("ParseAccess() error = %v, want ErrTokenInvalid", err)
			}
			if got != (identity.Principal{}) {
				t.Fatalf("ParseAccess() principal = %+v, want zero value", got)
			}
			if err != identity.ErrTokenInvalid {
				t.Fatalf("ParseAccess() exposed internal error %q", err)
			}
			if raw != "" && bytes.Contains([]byte(err.Error()), []byte(raw)) {
				t.Fatal("ParseAccess() error echoed raw token")
			}
		})
	}
}

func TestTokenManagerUsesInjectedTimeWithoutMutatingJWTGlobal(t *testing.T) {
	before := reflect.ValueOf(jwt.TimeFunc).Pointer()
	manager := newTestTokenManager(t)
	principal := identity.Principal{UserID: uuid.New(), SessionID: uuid.New()}

	raw, _, err := manager.IssueAccess(principal, testNow)
	if err != nil {
		t.Fatalf("IssueAccess() error = %v", err)
	}
	if _, err := manager.ParseAccess(raw, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("ParseAccess() error = %v", err)
	}

	after := reflect.ValueOf(jwt.TimeFunc).Pointer()
	if after != before {
		t.Fatal("token manager mutated jwt.TimeFunc")
	}
}

func TestTokenManagerConcurrentUse(t *testing.T) {
	manager := newTestTokenManager(t)
	const goroutines = 32
	const iterations = 20

	var wait sync.WaitGroup
	errs := make(chan error, goroutines)
	for range goroutines {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range iterations {
				want := identity.Principal{UserID: uuid.New(), SessionID: uuid.New()}
				raw, _, err := manager.IssueAccess(want, testNow)
				if err != nil {
					errs <- err
					return
				}
				got, err := manager.ParseAccess(raw, testNow.Add(time.Minute))
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- errors.New("parsed principal does not match issued principal")
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestNewRefreshToken(t *testing.T) {
	randomBytes := bytes.Repeat([]byte{0xff}, 32)
	raw, hash, err := identity.NewRefreshToken(bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	wantRaw := base64.RawURLEncoding.EncodeToString(randomBytes)
	if raw != wantRaw {
		t.Fatalf("NewRefreshToken() raw = %q, want %q", raw, wantRaw)
	}
	if len(raw) != 43 {
		t.Fatalf("NewRefreshToken() encoded length = %d, want 43", len(raw))
	}
	if wantHash := sha256.Sum256([]byte(wantRaw)); hash != wantHash {
		t.Fatalf("NewRefreshToken() hash = %x, want %x", hash, wantHash)
	}
	if got := identity.HashRefreshToken(raw); got != hash {
		t.Fatalf("HashRefreshToken() = %x, want %x", got, hash)
	}
}

func TestNewRefreshTokenRejectsBrokenRandomSource(t *testing.T) {
	tests := []struct {
		name   string
		random io.Reader
	}{
		{name: "nil", random: nil},
		{name: "short read", random: bytes.NewReader(bytes.Repeat([]byte{1}, 31))},
		{name: "reader error", random: errorReader{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, hash, err := identity.NewRefreshToken(tt.random)
			if err == nil {
				t.Fatal("NewRefreshToken() error = nil, want non-nil")
			}
			if raw != "" {
				t.Fatalf("NewRefreshToken() raw = %q, want empty", raw)
			}
			if hash != ([32]byte{}) {
				t.Fatalf("NewRefreshToken() hash = %x, want zero", hash)
			}
		})
	}
}

func newTestTokenManager(t *testing.T) *identity.TokenManager {
	t.Helper()
	manager, err := identity.NewTokenManager(identity.TokenConfig{
		PrivateKey: testKey,
		KeyID:      testKeyID,
		Issuer:     testIssuer,
		Audience:   testAudience,
		AccessTTL:  15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	return manager
}

func signToken(t *testing.T, method jwt.SigningMethod, key any, keyID string, claims jwt.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	if keyID != "" {
		token.Header["kid"] = keyID
	}
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return raw
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("random source failed")
}
