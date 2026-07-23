package wechat_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/Y1le/agri-price-crawler/internal/identity/wechat"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

const (
	testAppID      = "wx-test-app"
	testAppSecret  = "app-secret-must-not-leak"
	testCode       = "temporary-code-must-not-leak"
	testOpenID     = "openid-must-not-leak"
	testUnionID    = "union-id"
	testSessionKey = "session-key-must-not-leak"
)

func TestClientImplementsWeChatExchanger(t *testing.T) {
	var _ identity.WeChatExchanger = (*wechat.Client)(nil)
}

func TestExchangeSendsExactRequestAndReturnsDurableIdentity(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- request.Clone(request.Context())
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"openid":"` + testOpenID + `",
			"unionid":"` + testUnionID + `",
			"session_key":"` + testSessionKey + `"
		}`))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, time.Second)
	got, err := client.Exchange(context.Background(), testCode)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if got != (identity.WeChatIdentity{AppID: testAppID, OpenID: testOpenID, UnionID: testUnionID}) {
		t.Fatalf("Exchange() = %#v", got)
	}

	request := <-requests
	if request.Method != http.MethodGet {
		t.Fatalf("method = %q, want GET", request.Method)
	}
	if request.URL.Path != "/sns/jscode2session" {
		t.Fatalf("path = %q, want /sns/jscode2session", request.URL.Path)
	}
	wantQuery := (url.Values{
		"appid":      {testAppID},
		"secret":     {testAppSecret},
		"js_code":    {testCode},
		"grant_type": {"authorization_code"},
	}).Encode()
	if request.URL.RawQuery != wantQuery {
		t.Fatalf("query = %q", request.URL.RawQuery)
	}
	if values := request.URL.Query(); len(values) != 4 {
		t.Fatalf("query parameter count = %d, want 4", len(values))
	}
}

func TestExchangeMapsProviderErrors(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		want       error
		wantNoSent error
	}{
		{
			name:       "invalid code",
			response:   `{"errcode":40029,"errmsg":"invalid code ` + testCode + `"}`,
			want:       identity.ErrCodeInvalid,
			wantNoSent: identity.ErrUpstreamUnavailable,
		},
		{
			name:       "consumed code",
			response:   `{"errcode":40163,"errmsg":"code been used ` + testCode + `"}`,
			want:       identity.ErrCodeInvalid,
			wantNoSent: identity.ErrUpstreamUnavailable,
		},
		{
			name:       "system busy",
			response:   `{"errcode":-1,"errmsg":"system busy ` + testSessionKey + `"}`,
			want:       identity.ErrUpstreamUnavailable,
			wantNoSent: identity.ErrCodeInvalid,
		},
		{
			name:       "provider throttled",
			response:   `{"errcode":45011,"errmsg":"frequency limit ` + testOpenID + `"}`,
			want:       identity.ErrUpstreamUnavailable,
			wantNoSent: identity.ErrCodeInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(test.response))
			}))
			t.Cleanup(server.Close)

			_, err := newTestClient(t, server.URL, time.Second).Exchange(context.Background(), testCode)
			if !errors.Is(err, test.want) {
				t.Fatalf("Exchange() error = %v, want %v", err, test.want)
			}
			if errors.Is(err, test.wantNoSent) {
				t.Fatalf("Exchange() error = %v, must not match %v", err, test.wantNoSent)
			}
			assertSafeError(t, err, test.response)
		})
	}
}

func TestExchangeMapsHTTPAndTransportFailures(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			providerResponse := `provider response ` + testOpenID + ` ` + testSessionKey
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(status)
				_, _ = response.Write([]byte(providerResponse))
			}))
			t.Cleanup(server.Close)

			_, err := newTestClient(t, server.URL, time.Second).Exchange(context.Background(), testCode)
			if !errors.Is(err, identity.ErrUpstreamUnavailable) {
				t.Fatalf("Exchange() error = %v, want ErrUpstreamUnavailable", err)
			}
			assertSafeError(t, err, providerResponse)
		})
	}

	t.Run("client timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}))
		t.Cleanup(server.Close)

		_, err := newTestClient(t, server.URL, 20*time.Millisecond).Exchange(context.Background(), testCode)
		if !errors.Is(err, identity.ErrUpstreamUnavailable) {
			t.Fatalf("Exchange() error = %v, want ErrUpstreamUnavailable", err)
		}
		assertSafeError(t, err)
	})
}

func TestExchangeRejectsMalformedSuccessfulResponses(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "missing openid",
			response: `{"openid":"","unionid":"` + testUnionID + `","session_key":"` + testSessionKey + `"}`,
		},
		{
			name:     "invalid JSON",
			response: `{"openid":"` + testOpenID + `","session_key":"` + testSessionKey,
		},
		{
			name:     "trailing JSON",
			response: `{"openid":"` + testOpenID + `"}{"session_key":"` + testSessionKey + `"}`,
		},
		{
			name:     "body over 64 KiB",
			response: `{"openid":"` + testOpenID + `","padding":"` + strings.Repeat("provider-payload-marker", 4_000) + `"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(test.response))
			}))
			t.Cleanup(server.Close)

			_, err := newTestClient(t, server.URL, time.Second).Exchange(context.Background(), testCode)
			if !errors.Is(err, identity.ErrUpstreamUnavailable) {
				t.Fatalf("Exchange() error = %v, want wrapped ErrUpstreamUnavailable", err)
			}
			assertSafeError(t, err, test.response)
		})
	}
}

func TestExchangePreservesCallerCancellation(t *testing.T) {
	t.Run("before response headers", func(t *testing.T) {
		requestStarted := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			close(requestStarted)
			<-request.Context().Done()
		}))
		t.Cleanup(server.Close)

		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		client := newTestClient(t, server.URL, time.Second)
		go func() {
			_, err := client.Exchange(ctx, testCode)
			result <- err
		}()
		<-requestStarted
		cancel()

		assertCanceledError(t, <-result)
	})

	t.Run("while reading response body", func(t *testing.T) {
		headersSent := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusOK)
			if err := http.NewResponseController(response).Flush(); err != nil {
				return
			}
			close(headersSent)
			<-request.Context().Done()
		}))
		t.Cleanup(server.Close)

		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		client := newTestClient(t, server.URL, time.Second)
		go func() {
			_, err := client.Exchange(ctx, testCode)
			result <- err
		}()
		<-headersSent
		cancel()

		assertCanceledError(t, <-result)
	})
}

func TestNewRejectsInvalidConfigurationWithoutLeakingSecrets(t *testing.T) {
	tests := []config.WeChat{
		{AppSecret: testAppSecret, BaseURL: "https://api.weixin.qq.com", Timeout: time.Second},
		{AppID: testAppID, BaseURL: "https://api.weixin.qq.com", Timeout: time.Second},
		{AppID: testAppID, AppSecret: testAppSecret, BaseURL: "://invalid/" + testAppSecret, Timeout: time.Second},
		{AppID: testAppID, AppSecret: testAppSecret, BaseURL: "https://api.weixin.qq.com", Timeout: 0},
	}
	for _, cfg := range tests {
		if _, err := wechat.New(cfg); err == nil {
			t.Fatalf("New(%#v) error = nil", redactedConfig(cfg))
		} else {
			assertSafeError(t, err)
		}
	}
}

func newTestClient(t *testing.T, baseURL string, timeout time.Duration) *wechat.Client {
	t.Helper()
	client, err := wechat.New(config.WeChat{
		AppID:     testAppID,
		AppSecret: testAppSecret,
		BaseURL:   baseURL,
		Timeout:   timeout,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func assertSafeError(t *testing.T, err error, providerResponses ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	for _, secret := range []string{testAppSecret, testCode, testOpenID, testSessionKey} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks sensitive value %q: %v", secret, err)
		}
	}
	for _, response := range providerResponses {
		if strings.Contains(err.Error(), response) {
			t.Fatalf("error leaks provider response: %v", err)
		}
	}
}

func assertCanceledError(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Exchange() error = %v, want context.Canceled", err)
	}
	if errors.Is(err, identity.ErrUpstreamUnavailable) {
		t.Fatalf("caller cancellation must not be classified as upstream unavailable: %v", err)
	}
	assertSafeError(t, err)
}

func redactedConfig(cfg config.WeChat) config.WeChat {
	cfg.AppSecret = "[redacted]"
	return cfg
}
