package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

type fakeService struct {
	calls map[string]int

	requestEmailCode func(context.Context, string, string) error
	loginEmail       func(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error)
	loginWeChat      func(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error)
	refresh          func(context.Context, string, identity.ClientKind) (identity.LoginResult, error)
	logout           func(context.Context, identity.Principal) error
	logoutAll        func(context.Context, identity.Principal) error
	requestBindCode  func(context.Context, identity.Principal, string, string) error
	bindEmail        func(context.Context, identity.Principal, string, string) (identity.BindResult, error)
	bindWeChat       func(context.Context, identity.Principal, string, string) (identity.BindResult, error)
	me               func(context.Context, identity.Principal) (identity.AccountSummary, error)
}

func (f *fakeService) called(name string) {
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[name]++
}
func (f *fakeService) RequestEmailLoginCode(ctx context.Context, email, ip string) error {
	f.called("email_code")
	if f.requestEmailCode != nil {
		return f.requestEmailCode(ctx, email, ip)
	}
	return nil
}
func (f *fakeService) LoginEmail(ctx context.Context, email, code string, client identity.ClientKind) (identity.LoginResult, error) {
	f.called("email_login")
	if f.loginEmail != nil {
		return f.loginEmail(ctx, email, code, client)
	}
	return loginResult(client, time.Date(2026, 7, 23, 5, 4, 3, 0, time.UTC)), nil
}
func (f *fakeService) LoginWeChat(ctx context.Context, code, ip string, client identity.ClientKind) (identity.LoginResult, error) {
	f.called("wechat_login")
	if f.loginWeChat != nil {
		return f.loginWeChat(ctx, code, ip, client)
	}
	return loginResult(client, time.Date(2026, 7, 23, 5, 4, 3, 0, time.UTC)), nil
}
func (f *fakeService) Refresh(ctx context.Context, token string, client identity.ClientKind) (identity.LoginResult, error) {
	f.called("refresh")
	if f.refresh != nil {
		return f.refresh(ctx, token, client)
	}
	return loginResult(client, time.Date(2026, 7, 23, 5, 4, 3, 0, time.UTC)), nil
}
func (f *fakeService) Logout(ctx context.Context, principal identity.Principal) error {
	f.called("logout")
	if f.logout != nil {
		return f.logout(ctx, principal)
	}
	return nil
}
func (f *fakeService) LogoutAll(ctx context.Context, principal identity.Principal) error {
	f.called("logout_all")
	if f.logoutAll != nil {
		return f.logoutAll(ctx, principal)
	}
	return nil
}
func (f *fakeService) RequestBindEmailCode(ctx context.Context, principal identity.Principal, email, ip string) error {
	f.called("bind_email_code")
	if f.requestBindCode != nil {
		return f.requestBindCode(ctx, principal, email, ip)
	}
	return nil
}
func (f *fakeService) BindEmail(ctx context.Context, principal identity.Principal, email, code string) (identity.BindResult, error) {
	f.called("bind_email")
	if f.bindEmail != nil {
		return f.bindEmail(ctx, principal, email, code)
	}
	return identity.BindResult{}, nil
}
func (f *fakeService) BindWeChat(ctx context.Context, principal identity.Principal, code, ip string) (identity.BindResult, error) {
	f.called("bind_wechat")
	if f.bindWeChat != nil {
		return f.bindWeChat(ctx, principal, code, ip)
	}
	return identity.BindResult{}, nil
}
func (f *fakeService) Me(ctx context.Context, principal identity.Principal) (identity.AccountSummary, error) {
	f.called("me")
	if f.me != nil {
		return f.me(ctx, principal)
	}
	return identity.AccountSummary{}, nil
}

func TestNewRequiresMandatoryDependencies(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { New(Config{}) })
	var nilService *fakeService
	require.Panics(t, func() {
		New(Config{Service: nilService, TokenParser: tokenParserFunc(func(string, time.Time) (identity.Principal, error) {
			return identity.Principal{}, nil
		})})
	})
	require.Panics(t, func() { New(Config{Service: &fakeService{}}) })
}

func TestExactTenRoutesAndMethodIsolation(t *testing.T) {
	t.Parallel()
	service := &fakeService{me: func(context.Context, identity.Principal) (identity.AccountSummary, error) {
		return accountSummary(uuid.New()), nil
	}}
	handler := testHandler(t, Config{
		Service: service,
		TokenParser: tokenParserFunc(func(string, time.Time) (identity.Principal, error) {
			return identity.Principal{UserID: uuid.New(), SessionID: uuid.New()}, nil
		}),
	})

	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/api/v1/auth/email/code", `{"email":"a@example.test"}`, http.StatusAccepted},
		{http.MethodPost, "/api/v1/auth/email/login", `{"email":"a@example.test","code":"123456","client_kind":"wechat_mini"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/auth/wechat/login", `{"code":"wx","client_kind":"wechat_mini"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token":"mini-token"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/auth/logout", "", http.StatusNoContent},
		{http.MethodPost, "/api/v1/auth/logout-all", "", http.StatusNoContent},
		{http.MethodPost, "/api/v1/auth/bind/email/code", `{"email":"a@example.test"}`, http.StatusAccepted},
		{http.MethodPost, "/api/v1/auth/bind/email", `{"email":"a@example.test","code":"123456"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/auth/bind/wechat", `{"code":"wx"}`, http.StatusOK},
		{http.MethodGet, "/api/v1/me", "", http.StatusOK},
	}
	for _, test := range tests {
		request := bearerRequest(test.method, test.path, test.body)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, "%s %s: %s", test.method, test.path, response.Body.String())
	}

	for _, target := range []string{"/api/v1/auth/email/code", "/api/v1/me", "/api/v1/unknown"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, target, nil))
		require.Contains(t, []int{http.StatusMethodNotAllowed, http.StatusNotFound}, response.Code)
	}
}

func TestJSONRequestsAreStrictAndBounded(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, Config{Service: &fakeService{}, TokenParser: allowTokenParser()})
	tests := []struct {
		name        string
		contentType []string
		body        string
		detail      string
	}{
		{name: "missing content type", body: `{}`, detail: "Content-Type"},
		{name: "wrong content type", contentType: []string{"text/plain"}, body: `{}`, detail: "Content-Type"},
		{name: "invalid parameter", contentType: []string{`application/json; charset="`}, body: `{}`, detail: "Content-Type"},
		{name: "duplicate content type", contentType: []string{"application/json", "application/json"}, body: `{}`, detail: "Content-Type"},
		{name: "unknown field", contentType: []string{"application/json; charset=utf-8"}, body: `{"email":"a@example.test","secret":"x"}`, detail: "请求"},
		{name: "trailing object", contentType: []string{"application/json"}, body: `{"email":"a@example.test"} {}`, detail: "请求"},
		{name: "array", contentType: []string{"application/json"}, body: `[]`, detail: "JSON"},
		{name: "null", contentType: []string{"application/json"}, body: `null`, detail: "JSON"},
		{name: "empty", contentType: []string{"application/json"}, body: ``, detail: "请求"},
		{name: "too large", contentType: []string{"application/json"}, body: `{"email":"` + strings.Repeat("a", 1<<20) + `"}`, detail: "过大"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email/code", strings.NewReader(test.body))
			for _, value := range test.contentType {
				request.Header.Add("Content-Type", value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			problem := requireProblem(t, response, http.StatusBadRequest, "invalid_request")
			require.Contains(t, problem.Detail, test.detail)
		})
	}
}

func TestRequestAtOneMiBBoundaryIsNotClassifiedAsTooLarge(t *testing.T) {
	t.Parallel()
	service := &fakeService{}
	handler := testHandler(t, Config{Service: service, TokenParser: allowTokenParser()})
	prefix, suffix := `{"email":"`, `"}`
	body := prefix + strings.Repeat("a", (1<<20)-len(prefix)-len(suffix)) + suffix
	require.Len(t, body, 1<<20)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email/code", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)
}

func TestClientIPUsesTrustedProxyValidation(t *testing.T) {
	t.Parallel()
	service := &fakeService{requestEmailCode: func(_ context.Context, email, ip string) error {
		require.Equal(t, "a@example.test", email)
		require.Equal(t, "198.51.100.4", ip)
		return nil
	}}
	trusted, err := httpx.ParseTrustedProxies([]string{"192.0.2.10"})
	require.NoError(t, err)
	handler := testHandler(t, Config{Service: service, TokenParser: allowTokenParser(), TrustedProxies: trusted})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email/code", strings.NewReader(`{"email":"a@example.test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "198.51.100.4")
	request.RemoteAddr = "192.0.2.10:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)

	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/email/code", strings.NewReader(`{"email":"a@example.test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "malformed"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	requireProblem(t, response, http.StatusBadRequest, "invalid_request")
}

func TestLoginAndRefreshUseClientSpecificTransport(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 5, 4, 3, 0, time.UTC)
	web := loginResult(identity.ClientWeb, now)
	mini := loginResult(identity.ClientWeChatMini, now)
	service := &fakeService{
		loginEmail: func(_ context.Context, _, _ string, client identity.ClientKind) (identity.LoginResult, error) {
			if client == identity.ClientWeb {
				return web, nil
			}
			return mini, nil
		},
		refresh: func(_ context.Context, token string, client identity.ClientKind) (identity.LoginResult, error) {
			if client == identity.ClientWeb {
				require.Equal(t, web.RefreshToken, token)
				return web, nil
			}
			require.Equal(t, mini.RefreshToken, token)
			return mini, nil
		},
	}
	handler := testHandler(t, Config{
		Service: service, TokenParser: allowTokenParser(), Clock: fixedClock{now},
		Cookie: CookiePolicy{Name: "__Secure-agri_refresh", Secure: true, AllowedOrigins: []string{"https://app.example.test"}},
	})

	webLogin := jsonRequest(http.MethodPost, "/api/v1/auth/email/login", `{"email":"a@example.test","code":"123456","client_kind":"web"}`)
	webResponse := httptest.NewRecorder()
	handler.ServeHTTP(webResponse, webLogin)
	require.Equal(t, http.StatusOK, webResponse.Code)
	require.NotContains(t, webResponse.Body.String(), "refresh_token")
	cookies := webResponse.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "__Secure-agri_refresh", cookies[0].Name)
	require.Equal(t, web.RefreshToken, cookies[0].Value)
	require.True(t, cookies[0].HttpOnly)
	require.True(t, cookies[0].Secure)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	require.Equal(t, "/api/v1/auth", cookies[0].Path)
	require.Equal(t, web.RefreshExpiresAt, cookies[0].Expires)
	require.Positive(t, cookies[0].MaxAge)

	miniLogin := jsonRequest(http.MethodPost, "/api/v1/auth/email/login", `{"email":"a@example.test","code":"123456","client_kind":"wechat_mini"}`)
	miniResponse := httptest.NewRecorder()
	handler.ServeHTTP(miniResponse, miniLogin)
	require.Equal(t, http.StatusOK, miniResponse.Code)
	require.Contains(t, miniResponse.Body.String(), `"refresh_token":"`+mini.RefreshToken+`"`)
	require.Empty(t, miniResponse.Header().Values("Set-Cookie"))
	require.Contains(t, miniResponse.Body.String(), `"expires_in":900`)

	webRefresh := jsonRequest(http.MethodPost, "/api/v1/auth/refresh", `{}`)
	webRefresh.AddCookie(&http.Cookie{Name: "__Secure-agri_refresh", Value: web.RefreshToken})
	webRefresh.Header.Set("Origin", "https://app.example.test")
	webRefreshResponse := httptest.NewRecorder()
	handler.ServeHTTP(webRefreshResponse, webRefresh)
	require.Equal(t, http.StatusOK, webRefreshResponse.Code, webRefreshResponse.Body.String())

	miniRefresh := jsonRequest(http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token":"`+mini.RefreshToken+`"}`)
	miniRefreshResponse := httptest.NewRecorder()
	handler.ServeHTTP(miniRefreshResponse, miniRefresh)
	require.Equal(t, http.StatusOK, miniRefreshResponse.Code)
}

func TestRefreshRejectsAmbiguousMissingDuplicateAndMismatchedTransport(t *testing.T) {
	t.Parallel()
	service := &fakeService{refresh: func(_ context.Context, _ string, client identity.ClientKind) (identity.LoginResult, error) {
		result := loginResult(client, time.Now().UTC())
		result.Client = identity.ClientWeb
		return result, nil
	}}
	handler := testHandler(t, Config{Service: service, TokenParser: allowTokenParser()})

	tests := []struct {
		name    string
		body    string
		cookies []*http.Cookie
	}{
		{name: "neither", body: `{}`},
		{name: "both", body: `{"refresh_token":"mini"}`, cookies: []*http.Cookie{{Name: "agri_refresh", Value: "web"}}},
		{name: "duplicate cookies", body: `{}`, cookies: []*http.Cookie{{Name: "agri_refresh", Value: "a"}, {Name: "agri_refresh", Value: "b"}}},
		{name: "empty cookie", body: `{}`, cookies: []*http.Cookie{{Name: "agri_refresh", Value: ""}}},
		{name: "durable client mismatch", body: `{"refresh_token":"mini"}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := jsonRequest(http.MethodPost, "/api/v1/auth/refresh", test.body)
			for _, cookie := range test.cookies {
				request.AddCookie(cookie)
			}
			if len(test.cookies) != 0 {
				request.Header.Set("Origin", "https://app.example.test")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			status, code := http.StatusBadRequest, "invalid_request"
			if test.name == "durable client mismatch" {
				status, code = http.StatusServiceUnavailable, "service_not_ready"
			}
			requireProblem(t, response, status, code)
		})
	}
}

func TestCookieOriginGuardAndLogoutClearing(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, Config{Service: &fakeService{}, TokenParser: allowTokenParser()})

	refresh := jsonRequest(http.MethodPost, "/api/v1/auth/refresh", `{}`)
	refresh.AddCookie(&http.Cookie{Name: "agri_refresh", Value: "web"})
	refresh.Header.Add("Origin", "https://app.example.test")
	refresh.Header.Add("Origin", "https://evil.example.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, refresh)
	requireProblem(t, response, http.StatusForbidden, "origin_not_allowed")

	logout := bearerRequest(http.MethodPost, "/api/v1/auth/logout", "")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, logout)
	require.Equal(t, http.StatusNoContent, response.Code)
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, -1, cookies[0].MaxAge)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	require.Equal(t, "/api/v1/auth", cookies[0].Path)
}

func TestBindMergeUsesSessionTransportAndNonMergeReturnsAccountOnly(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 5, 4, 3, 0, time.UTC)
	account := accountSummary(uuid.New())
	merged := loginResult(identity.ClientWeChatMini, now)
	merged.User = account.User
	service := &fakeService{bindEmail: func(_ context.Context, _ identity.Principal, email, code string) (identity.BindResult, error) {
		if email == "merge@example.test" {
			return identity.BindResult{Account: account, Session: &merged}, nil
		}
		return identity.BindResult{Account: account}, nil
	}}
	handler := testHandler(t, Config{Service: service, TokenParser: allowTokenParser(), Clock: fixedClock{now}})

	nonMerge := bearerRequest(http.MethodPost, "/api/v1/auth/bind/email", `{"email":"same@example.test","code":"123456"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, nonMerge)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "access_token")
	require.Contains(t, response.Body.String(), `"account"`)

	merge := bearerRequest(http.MethodPost, "/api/v1/auth/bind/email", `{"email":"merge@example.test","code":"123456"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, merge)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "access_token")
	require.Contains(t, response.Body.String(), "refresh_token")
}

func TestEndpointAwareErrorMappingAndSafeLogging(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		path   string
		err    error
		status int
		code   string
	}{
		{"email invalid code", "/api/v1/auth/email/login", identity.ErrCodeInvalid, 400, "invalid_email_code"},
		{"email expired code", "/api/v1/auth/email/login", identity.ErrCodeExpired, 400, "email_code_expired"},
		{"wechat invalid code", "/api/v1/auth/wechat/login", identity.ErrCodeInvalid, 400, "invalid_wechat_code"},
		{"access invalid", "/api/v1/auth/logout", identity.ErrTokenInvalid, 401, "invalid_access_token"},
		{"refresh invalid", "/api/v1/auth/refresh", identity.ErrTokenInvalid, 401, "invalid_refresh_token"},
		{"refresh reused", "/api/v1/auth/refresh", identity.ErrTokenReused, 401, "refresh_token_reused"},
		{"disabled", "/api/v1/auth/logout", identity.ErrAccountDisabled, 403, "account_disabled"},
		{"conflict", "/api/v1/auth/bind/email", identity.ErrConflict, 409, "identity_conflict"},
		{"rate limit", "/api/v1/auth/email/code", identity.ErrRateLimited, 429, "rate_limited"},
		{"state", "/api/v1/auth/logout", identity.ErrStateUnavailable, 503, "service_not_ready"},
		{"smtp", "/api/v1/auth/email/code", identity.ErrUpstreamUnavailable, 503, "email_delivery_unavailable"},
		{"wechat", "/api/v1/auth/wechat/login", identity.ErrUpstreamUnavailable, 503, "wechat_unavailable"},
		{"internal", "/api/v1/auth/logout", errors.New("db says email=a@example.test token=secret"), 500, "internal_error"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			service := errorService(test.path, test.err)
			handler := testHandler(t, Config{
				Service: service, TokenParser: allowTokenParser(),
				Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			})
			body := routeBody(test.path)
			request := bearerRequest(http.MethodPost, test.path, body)
			if test.path == "/api/v1/auth/refresh" {
				request = jsonRequest(http.MethodPost, test.path, `{"refresh_token":"mini-secret"}`)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			problem := requireProblem(t, response, test.status, test.code)
			require.NotEmpty(t, problem.TraceID)
			require.NotEmpty(t, problem.Detail)
			require.NotContains(t, response.Body.String(), "a@example.test")
			require.NotContains(t, response.Body.String(), "secret")
			require.NotContains(t, logs.String(), "a@example.test")
			require.NotContains(t, logs.String(), "mini-secret")
			require.NotContains(t, logs.String(), "token=secret")
			require.Contains(t, logs.String(), `"category"`)
			require.Contains(t, logs.String(), problem.TraceID)
		})
	}
}

func TestResponseAccountContainsOnlySafeFields(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	summary := accountSummary(userID)
	handler := testHandler(t, Config{
		Service: &fakeService{me: func(context.Context, identity.Principal) (identity.AccountSummary, error) {
			return summary, nil
		}},
		TokenParser: allowTokenParser(),
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, bearerRequest(http.MethodGet, "/api/v1/me", ""))
	require.Equal(t, http.StatusOK, response.Code)
	body := response.Body.String()
	require.Contains(t, body, "a***@example.test")
	require.NotContains(t, body, "alice@example.test")
	require.NotContains(t, body, "openid")
	require.NotContains(t, body, "union")
	require.NotContains(t, body, "merged_into")
	require.NotContains(t, body, "updated_at")
}

func jsonRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func allowTokenParser() TokenParser {
	return tokenParserFunc(func(string, time.Time) (identity.Principal, error) {
		return identity.Principal{UserID: uuid.New(), SessionID: uuid.New()}, nil
	})
}

func accountSummary(id uuid.UUID) identity.AccountSummary {
	return identity.AccountSummary{
		User:       identity.User{ID: id, Status: identity.UserActive, CreatedAt: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)},
		Identities: []identity.IdentitySummary{{Kind: identity.IdentityEmail, Display: "a***@example.test"}},
	}
}

func loginResult(client identity.ClientKind, now time.Time) identity.LoginResult {
	return identity.LoginResult{
		User:   identity.User{ID: uuid.New(), Status: identity.UserActive, CreatedAt: now.Add(-time.Hour)},
		Client: client, AccessToken: "access-token", RefreshToken: "refresh-token-" + string(client),
		AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
	}
}

func requireProblem(t *testing.T, response *httptest.ResponseRecorder, status int, code string) httpx.Problem {
	t.Helper()
	require.Equal(t, status, response.Code, response.Body.String())
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	var problem httpx.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, status, problem.Status)
	require.Equal(t, code, problem.Code)
	require.NotEmpty(t, problem.TraceID)
	return problem
}

func errorService(path string, err error) *fakeService {
	service := &fakeService{}
	switch path {
	case "/api/v1/auth/email/code":
		service.requestEmailCode = func(context.Context, string, string) error { return err }
	case "/api/v1/auth/email/login":
		service.loginEmail = func(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error) {
			return identity.LoginResult{}, err
		}
	case "/api/v1/auth/wechat/login":
		service.loginWeChat = func(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error) {
			return identity.LoginResult{}, err
		}
	case "/api/v1/auth/refresh":
		service.refresh = func(context.Context, string, identity.ClientKind) (identity.LoginResult, error) {
			return identity.LoginResult{}, err
		}
	case "/api/v1/auth/logout":
		service.logout = func(context.Context, identity.Principal) error { return err }
	case "/api/v1/auth/bind/email":
		service.bindEmail = func(context.Context, identity.Principal, string, string) (identity.BindResult, error) {
			return identity.BindResult{}, err
		}
	}
	return service
}

func routeBody(path string) string {
	switch path {
	case "/api/v1/auth/email/code":
		return `{"email":"a@example.test"}`
	case "/api/v1/auth/email/login":
		return `{"email":"a@example.test","code":"123456","client_kind":"web"}`
	case "/api/v1/auth/wechat/login":
		return `{"code":"wx","client_kind":"web"}`
	case "/api/v1/auth/bind/email":
		return `{"email":"a@example.test","code":"123456"}`
	default:
		return ""
	}
}
