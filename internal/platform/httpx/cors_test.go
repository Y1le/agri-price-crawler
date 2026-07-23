package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func TestCORSAllowsOnlyCanonicalExactConfiguredOrigins(t *testing.T) {
	t.Parallel()

	handler, err := httpx.CORS([]string{
		"HTTPS://Example.COM:443/",
		"http://localhost:3000",
	}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, err)

	tests := []struct {
		name   string
		origin string
		allow  string
	}{
		{name: "canonical equivalent default port", origin: "https://example.com", allow: "https://example.com"},
		{name: "explicit equivalent default port", origin: "https://EXAMPLE.com:443", allow: "https://example.com"},
		{name: "configured non default port", origin: "http://localhost:3000", allow: "http://localhost:3000"},
		{name: "foreign host", origin: "https://example.com.evil.test"},
		{name: "foreign port", origin: "http://localhost:3001"},
		{name: "foreign scheme", origin: "http://example.com"},
		{name: "opaque null origin", origin: "null"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			require.Equal(t, test.allow, response.Header().Get("Access-Control-Allow-Origin"))
			require.Contains(t, response.Header().Values("Vary"), "Origin")
			if test.allow == "" {
				require.Empty(t, response.Header().Get("Access-Control-Allow-Credentials"))
			} else {
				require.Equal(t, "true", response.Header().Get("Access-Control-Allow-Credentials"))
			}
		})
	}
}

func TestCORSRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	invalidOrigins := []string{
		"*",
		"https://*.example.com",
		"https://user@example.com",
		"https://example.com/path",
		"https://example.com?query=yes",
		"https://example.com#fragment",
		"ftp://example.com",
		"https://example.com:bad",
		"https://example.com,https://evil.test",
		"",
	}
	for _, origin := range invalidOrigins {
		origin := origin
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			_, err := httpx.CORS([]string{origin}, http.NotFoundHandler())
			require.Error(t, err)
		})
	}
}

func TestCORSRejectsDuplicateOriginHeaders(t *testing.T) {
	t.Parallel()

	handler, err := httpx.CORS([]string{"https://example.com"}, http.NotFoundHandler())
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Add("Origin", "https://example.com")
	request.Header.Add("Origin", "https://example.com")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, response.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSHandlesAllowedPreflightWithoutCallingNext(t *testing.T) {
	t.Parallel()

	called := false
	handler, err := httpx.CORS([]string{"https://example.com"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodOptions, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type, X-Request-ID, Authorization")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.False(t, called)
	require.Equal(t, "GET, POST, OPTIONS", response.Header().Get("Access-Control-Allow-Methods"))
	require.Equal(t, "Authorization, Content-Type, X-Request-ID", response.Header().Get("Access-Control-Allow-Headers"))
	require.Equal(t, "true", response.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSRejectsDisallowedPreflightMethodOrHeaders(t *testing.T) {
	t.Parallel()

	handler, err := httpx.CORS([]string{"https://example.com"}, http.NotFoundHandler())
	require.NoError(t, err)
	tests := []struct {
		method  string
		headers string
	}{
		{method: http.MethodDelete},
		{method: http.MethodPost, headers: "Content-Type, X-Evil"},
		{method: http.MethodPost, headers: "Content-Type,,Authorization"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodOptions, "/", nil)
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("Access-Control-Request-Method", test.method)
		if test.headers != "" {
			request.Header.Set("Access-Control-Request-Headers", test.headers)
		}
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		require.Equal(t, http.StatusForbidden, response.Code)
	}
}

func TestRequireAllowedOriginProtectsCookieAuthenticatedUnsafeMethods(t *testing.T) {
	t.Parallel()

	allowed := []string{"https://example.com"}
	tests := []struct {
		name       string
		method     string
		cookie     []string
		origins    []string
		wantDenied bool
	}{
		{name: "matching origin", method: http.MethodPost, cookie: []string{"refresh=secret"}, origins: []string{"https://example.com"}},
		{name: "canonical matching origin", method: http.MethodPost, cookie: []string{"refresh=secret"}, origins: []string{"HTTPS://EXAMPLE.COM:443"}},
		{name: "missing origin", method: http.MethodPost, cookie: []string{"refresh=secret"}, wantDenied: true},
		{name: "foreign origin", method: http.MethodPost, cookie: []string{"refresh=secret"}, origins: []string{"https://evil.test"}, wantDenied: true},
		{name: "duplicate origin", method: http.MethodPost, cookie: []string{"refresh=secret"}, origins: []string{"https://example.com", "https://example.com"}, wantDenied: true},
		{name: "malformed origin", method: http.MethodPost, cookie: []string{"refresh=secret"}, origins: []string{"https://example.com/path"}, wantDenied: true},
		{name: "malformed cookie still protected", method: http.MethodPatch, cookie: []string{"not a valid cookie;"}, origins: []string{"https://evil.test"}, wantDenied: true},
		{name: "no cookie", method: http.MethodPost, origins: []string{"https://evil.test"}},
		{name: "empty cookie", method: http.MethodPost, cookie: []string{""}},
		{name: "safe GET", method: http.MethodGet, cookie: []string{"refresh=secret"}},
		{name: "safe HEAD", method: http.MethodHead, cookie: []string{"refresh=secret"}},
		{name: "safe OPTIONS", method: http.MethodOptions, cookie: []string{"refresh=secret"}},
		{name: "safe TRACE", method: http.MethodTrace, cookie: []string{"refresh=secret"}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(test.method, "/", nil)
			for _, cookie := range test.cookie {
				request.Header.Add("Cookie", cookie)
			}
			for _, origin := range test.origins {
				request.Header.Add("Origin", origin)
			}

			err := httpx.RequireAllowedOrigin(request, allowed)

			if test.wantDenied {
				require.ErrorIs(t, err, httpx.ErrOriginNotAllowed)
				require.Equal(t, "origin_not_allowed", err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRequireAllowedOriginReportsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("Cookie", "refresh=secret")
	request.Header.Set("Origin", "https://example.com")

	err := httpx.RequireAllowedOrigin(request, []string{"*"})

	require.Error(t, err)
	require.False(t, errors.Is(err, httpx.ErrOriginNotAllowed))
}
