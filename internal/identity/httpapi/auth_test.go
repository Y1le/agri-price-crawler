package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type tokenParserFunc func(string, time.Time) (identity.Principal, error)

func (f tokenParserFunc) ParseAccess(token string, now time.Time) (identity.Principal, error) {
	return f(token, now)
}

func TestAuthenticationAcceptsCaseInsensitiveBearerAndPassesPrincipal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 23, 5, 4, 3, 0, time.UTC)
	principal := identity.Principal{UserID: uuid.New(), SessionID: uuid.New()}
	service := &fakeService{
		me: func(_ context.Context, got identity.Principal) (identity.AccountSummary, error) {
			require.Equal(t, principal, got)
			return accountSummary(principal.UserID), nil
		},
	}
	parser := tokenParserFunc(func(token string, parsedAt time.Time) (identity.Principal, error) {
		require.Equal(t, "signed-token", token)
		require.Equal(t, now, parsedAt)
		return principal, nil
	})
	handler := testHandler(t, Config{Service: service, TokenParser: parser, Clock: fixedClock{now}})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("Authorization", "bEaReR   signed-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, service.calls["me"])
}

func TestAuthenticationRejectsAmbiguousOrMalformedAuthorization(t *testing.T) {
	t.Parallel()

	tests := map[string]func(http.Header){
		"missing":       func(http.Header) {},
		"duplicate":     func(h http.Header) { h.Add("Authorization", "Bearer a"); h.Add("Authorization", "Bearer b") },
		"wrong scheme":  func(h http.Header) { h.Set("Authorization", "Basic abc") },
		"empty token":   func(h http.Header) { h.Set("Authorization", "Bearer ") },
		"extra field":   func(h http.Header) { h.Set("Authorization", "Bearer a b") },
		"comma":         func(h http.Header) { h.Set("Authorization", "Bearer a,b") },
		"leading space": func(h http.Header) { h.Set("Authorization", " Bearer a") },
		"tab separator": func(h http.Header) { h.Set("Authorization", "Bearer\ta") },
		"not token68":   func(h http.Header) { h.Set("Authorization", "Bearer a:token") },
		"bad padding":   func(h http.Header) { h.Set("Authorization", "Bearer a=b") },
		"only padding":  func(h http.Header) { h.Set("Authorization", "Bearer ==") },
	}
	for name, configure := range tests {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service := &fakeService{}
			handler := testHandler(t, Config{
				Service: service,
				TokenParser: tokenParserFunc(func(string, time.Time) (identity.Principal, error) {
					t.Fatal("parser must not be called for malformed Authorization")
					return identity.Principal{}, nil
				}),
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
			configure(request.Header)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			requireProblem(t, response, http.StatusUnauthorized, "invalid_access_token")
			require.Zero(t, service.calls["me"])
		})
	}
}

func TestAuthenticationMapsParserFailureWithoutLeakingToken(t *testing.T) {
	t.Parallel()

	const rawToken = "secret-access-token"
	handler := testHandler(t, Config{
		Service: &fakeService{},
		TokenParser: tokenParserFunc(func(string, time.Time) (identity.Principal, error) {
			return identity.Principal{}, errors.New("jwt library says token=" + rawToken)
		}),
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	requireProblem(t, response, http.StatusUnauthorized, "invalid_access_token")
	require.NotContains(t, response.Body.String(), rawToken)
	require.NotEmpty(t, response.Header().Get("X-Request-ID"))
}

func testHandler(t *testing.T, config Config) http.Handler {
	t.Helper()
	if config.Cookie.Name == "" {
		config.Cookie = CookiePolicy{
			Name:           "agri_refresh",
			AllowedOrigins: []string{"https://app.example.test"},
		}
	}
	if config.Clock == nil {
		config.Clock = fixedClock{time.Date(2026, 7, 23, 5, 4, 3, 0, time.UTC)}
	}
	return httpx.WithRequestID(New(config))
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func bearerRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer access")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
