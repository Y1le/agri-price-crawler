package bootstrap

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	identitysmtp "github.com/Y1le/agri-price-crawler/internal/identity/smtp"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/rediscache"
	"github.com/Y1le/agri-price-crawler/internal/platform/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentityIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	redisAddress := os.Getenv("TEST_REDIS_ADDR")
	if databaseURL == "" || redisAddress == "" {
		t.Skip("TEST_DATABASE_URL and TEST_REDIS_ADDR are required")
	}

	ctx := context.Background()
	testdb.LockSchema(t, ctx, databaseURL)
	adminPool, err := platformpg.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	schemaName := fmt.Sprintf("identity_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop isolated Identity test schema: %v", err)
		}
	}()

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schemaName
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, pool, migrationSources()...); err != nil {
		t.Fatal(err)
	}

	weChatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not used by the email journey", http.StatusBadRequest)
	}))
	defer weChatServer.Close()

	cfg := identityIntegrationConfig(databaseURL, redisAddress, weChatServer.URL)
	if err := cfg.ValidateGateway(); err != nil {
		t.Fatalf("validate single-client Gateway config: %v", err)
	}
	redisClient := rediscache.Open(cfg.Redis)
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping Redis: %v", err)
	}

	email := identitysmtp.NewMemory()
	handler, err := buildGatewayHandler(
		pool,
		redisClient,
		cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		gatewayOverrides{
			Email:     email,
			Clock:     fixedBootstrapClock{now: time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)},
			Random:    &deterministicBootstrapReader{},
			OTPPrefix: "agri:test:identity:" + schemaName,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	healthRequest, err := http.NewRequest(http.MethodGet, server.URL+"/livez", nil)
	if err != nil {
		t.Fatal(err)
	}
	healthRequest.Header.Set("Origin", "http://localhost:3000")
	healthResponse, err := server.Client().Do(healthRequest)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, healthResponse, http.StatusOK)
	if healthResponse.Header.Get("X-Request-ID") == "" {
		t.Fatal("assembled Gateway did not add a request ID")
	}
	if got := healthResponse.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("CORS allow origin = %q", got)
	}
	if got := healthResponse.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("CORS allow credentials = %q", got)
	}

	address := fmt.Sprintf("integration-%d@example.com", time.Now().UnixNano())
	response := identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/email/code",
		`{"email":`+jsonString(address)+`}`, "")
	requireStatus(t, response, http.StatusAccepted)
	codes := email.Codes(address)
	if len(codes) != 1 {
		t.Fatalf("recorded email codes = %v, want exactly one", codes)
	}

	response = identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/email/login",
		`{"email":`+jsonString(address)+`,"code":`+jsonString(codes[0])+`,"client_kind":"web"}`, "")
	requireStatus(t, response, http.StatusBadRequest)

	response = identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/email/login",
		`{"email":`+jsonString(address)+`,"code":`+jsonString(codes[0])+`,"client_kind":"wechat_mini"}`, "")
	login := decodeIdentityLogin(t, response)

	response = identityRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/me", "", login.AccessToken)
	requireStatus(t, response, http.StatusOK)

	response = identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/refresh",
		`{"refresh_token":`+jsonString(login.RefreshToken)+`}`, "")
	rotated := decodeIdentityLogin(t, response)

	if err := redisClient.Close(); err != nil {
		t.Fatal(err)
	}
	response = identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/email/code",
		`{"email":"redis-down@example.com"}`, "")
	requireStatus(t, response, http.StatusServiceUnavailable)

	response = identityRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/me", "", rotated.AccessToken)
	requireStatus(t, response, http.StatusOK)
	response = identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/refresh",
		`{"refresh_token":`+jsonString(rotated.RefreshToken)+`}`, "")
	postgresOnly := decodeIdentityLogin(t, response)

	response = identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/logout", `{}`, postgresOnly.AccessToken)
	requireStatus(t, response, http.StatusOK)
	response = identityRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/refresh",
		`{"refresh_token":`+jsonString(postgresOnly.RefreshToken)+`}`, "")
	requireStatus(t, response, http.StatusUnauthorized)
}

func TestBuildGatewayHandlerWrapsComponentErrorsWithoutSecrets(t *testing.T) {
	cfg := identityIntegrationConfig("", "", "https://api.weixin.qq.com")
	cfg.Identity.EmailDriver = "smtp"
	cfg.Identity.SMTP = config.SMTP{
		Host:     "smtp.example.com",
		Port:     0,
		Username: "integration-user",
		Password: "must-not-appear-in-errors",
		From:     "sender@example.com",
		TLSMode:  "starttls",
		Timeout:  time.Second,
	}

	_, err := buildGatewayHandler(nil, nil, cfg, nil, gatewayOverrides{})
	if err == nil || !strings.Contains(err.Error(), "construct Identity SMTP sender") {
		t.Fatalf("error = %v, want SMTP component context", err)
	}
	if strings.Contains(err.Error(), cfg.Identity.SMTP.Password) {
		t.Fatalf("error leaked SMTP password: %v", err)
	}
}

func TestBuildGatewayHandlerWrapsTrustedProxyError(t *testing.T) {
	cfg := identityIntegrationConfig("", "", "https://api.weixin.qq.com")
	cfg.Identity.Web.TrustedProxies = []string{"not-an-ip"}

	_, err := buildGatewayHandler(nil, nil, cfg, nil, gatewayOverrides{})
	if err == nil || !strings.Contains(err.Error(), "construct Identity trusted-proxy policy") {
		t.Fatalf("error = %v, want trusted-proxy component context", err)
	}
}

func TestBuildGatewayHandlerRejectsMissingStateDependencies(t *testing.T) {
	cfg := identityIntegrationConfig("", "", "https://api.weixin.qq.com")
	redisClient := rediscache.Open(config.Redis{Addr: "127.0.0.1:1"})
	defer redisClient.Close()

	if _, err := buildGatewayHandler(nil, redisClient, cfg, nil, gatewayOverrides{}); err == nil ||
		!strings.Contains(err.Error(), "construct Identity PostgreSQL repository") {
		t.Fatalf("nil pool error = %v", err)
	}
	if _, err := buildGatewayHandler(&pgxpool.Pool{}, nil, cfg, nil, gatewayOverrides{}); err == nil ||
		!strings.Contains(err.Error(), "construct Identity Redis OTP store") {
		t.Fatalf("nil Redis error = %v", err)
	}
}

func TestBuildGatewayHandlerReturnsWebPolicyErrorsWithoutPanicking(t *testing.T) {
	tests := map[string]struct {
		mutate func(*config.Config)
		want   string
	}{
		"cookie": {
			mutate: func(cfg *config.Config) { cfg.Identity.Web.CookieName = "bad cookie" },
			want:   "construct Identity cookie policy",
		},
		"origin": {
			mutate: func(cfg *config.Config) {
				cfg.Identity.Web.AllowedOrigins = []string{"https://example.test/path"}
			},
			want: "construct Gateway CORS policy",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := identityIntegrationConfig("", "", "https://api.weixin.qq.com")
			test.mutate(&cfg)
			_, err := buildGatewayHandler(nil, nil, cfg, nil, gatewayOverrides{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

type fixedBootstrapClock struct {
	now time.Time
}

func (clock fixedBootstrapClock) Now() time.Time { return clock.now }

type deterministicBootstrapReader struct {
	mu     sync.Mutex
	offset uint64
}

func (reader *deterministicBootstrapReader) Read(destination []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	for index := range destination {
		destination[index] = byte(reader.offset)
		reader.offset++
	}
	return len(destination), nil
}

type identityLoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func identityIntegrationConfig(databaseURL, redisAddress, weChatBaseURL string) config.Config {
	return config.Config{
		Environment: "development",
		Gateway:     config.Gateway{Addr: ":0"},
		Postgres:    config.Postgres{URL: databaseURL},
		Redis:       config.Redis{Addr: redisAddress},
		Identity: config.Identity{
			JWT: config.IdentityJWT{
				PrivateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize)),
				KeyID:      "integration-key",
				Issuer:     "integration-issuer",
				Audience:   "integration-audience",
				AccessTTL:  15 * time.Minute,
			},
			OTP: config.IdentityOTP{
				Pepper:       bytes.Repeat([]byte{0x22}, 32),
				TTL:          10 * time.Minute,
				Attempts:     5,
				Cooldown:     time.Minute,
				EmailPerHour: 5,
				IPPerHour:    30,
			},
			RefreshTTL:     30 * 24 * time.Hour,
			ReuseGrace:     10 * time.Second,
			WeChat:         config.WeChat{AppID: "wx-integration", AppSecret: "integration-secret", BaseURL: weChatBaseURL, Timeout: time.Second, IPPerHour: 60},
			SMTP:           config.SMTP{Timeout: time.Second},
			EmailDriver:    "memory",
			EnabledClients: []string{"wechat_mini"},
			Web: config.WebSecurity{
				CookieName:     "agri_refresh",
				AllowedOrigins: []string{"http://localhost:3000"},
				TrustedProxies: []string{"127.0.0.1"},
			},
		},
	}
}

func identityRequest(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body string,
	accessToken string,
) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func requireStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != want {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want %d; body = %s", response.StatusCode, want, body)
	}
}

func decodeIdentityLogin(t *testing.T, response *http.Response) identityLoginResponse {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 200; body = %s", response.StatusCode, body)
	}
	var result identityLoginResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("login response lacks credentials: %+v", result)
	}
	return result
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
