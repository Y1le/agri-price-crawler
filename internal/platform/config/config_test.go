package config_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

func clearIdentityEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "")
	for _, name := range []string{
		"IDENTITY_JWT_PRIVATE_KEY_BASE64",
		"IDENTITY_JWT_KEY_ID",
		"IDENTITY_JWT_ISSUER",
		"IDENTITY_JWT_AUDIENCE",
		"IDENTITY_JWT_ACCESS_TTL",
		"IDENTITY_REFRESH_TTL",
		"IDENTITY_REFRESH_REUSE_GRACE",
		"IDENTITY_ENABLED_CLIENTS",
		"IDENTITY_OTP_PEPPER_BASE64",
		"IDENTITY_OTP_TTL",
		"IDENTITY_OTP_ATTEMPTS",
		"IDENTITY_OTP_COOLDOWN",
		"IDENTITY_OTP_EMAIL_PER_HOUR",
		"IDENTITY_OTP_IP_PER_HOUR",
		"IDENTITY_WECHAT_APP_ID",
		"IDENTITY_WECHAT_APP_SECRET",
		"IDENTITY_WECHAT_BASE_URL",
		"IDENTITY_WECHAT_TIMEOUT",
		"IDENTITY_WECHAT_IP_PER_HOUR",
		"IDENTITY_EMAIL_DRIVER",
		"IDENTITY_SMTP_HOST",
		"IDENTITY_SMTP_PORT",
		"IDENTITY_SMTP_USERNAME",
		"IDENTITY_SMTP_PASSWORD",
		"IDENTITY_SMTP_FROM",
		"IDENTITY_SMTP_TLS_MODE",
		"IDENTITY_SMTP_TIMEOUT",
		"IDENTITY_COOKIE_NAME",
		"IDENTITY_COOKIE_SECURE",
		"IDENTITY_WEB_ALLOWED_ORIGINS",
		"IDENTITY_TRUSTED_PROXIES",
	} {
		t.Setenv(name, "")
		if name == "IDENTITY_ENABLED_CLIENTS" {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func setValidIdentityEnv(t *testing.T) {
	t.Helper()
	clearIdentityEnv(t)
	t.Setenv("IDENTITY_JWT_PRIVATE_KEY_BASE64", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, ed25519.SeedSize)))
	t.Setenv("IDENTITY_OTP_PEPPER_BASE64", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	t.Setenv("IDENTITY_WECHAT_APP_ID", "wx-test")
	t.Setenv("IDENTITY_WECHAT_APP_SECRET", "secret-test")
	t.Setenv("IDENTITY_EMAIL_DRIVER", "memory")
}

func TestLoadDefaults(t *testing.T) {
	clearIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://agri:agri@localhost:5432/agri?sslmode=disable")
	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment != "development" || got.Gateway.Addr != ":8080" {
		t.Fatalf("environment = %q, Gateway address = %q", got.Environment, got.Gateway.Addr)
	}
	if got.Redis.Addr != "localhost:6379" {
		t.Fatalf("got %+v", got.Redis)
	}
	if got.Worker.PollInterval != time.Second || got.Worker.BatchSize != 4 || got.Worker.LeaseDuration != 5*time.Minute {
		t.Fatalf("got %+v", got.Worker)
	}
}

func TestLoadRejectsNonPositiveWorkerJobLeaseDuration(t *testing.T) {
	for _, value := range []string{"0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			clearIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("WORKER_JOB_LEASE_DURATION", value)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load succeeded, want non-positive job lease duration error")
			}
			if !strings.Contains(err.Error(), "WORKER_JOB_LEASE_DURATION") {
				t.Fatalf("error = %q, want it to name WORKER_JOB_LEASE_DURATION", err)
			}
		})
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	clearIdentityEnv(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("want error")
	}
}

func TestLoadRejectsInvalidWorkerValues(t *testing.T) {
	clearIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	t.Setenv("WORKER_POLL_INTERVAL", "bad")
	if _, err := config.Load(); err == nil {
		t.Fatal("want duration error")
	}
	t.Setenv("WORKER_POLL_INTERVAL", "1s")
	t.Setenv("WORKER_BATCH_SIZE", "0")
	if _, err := config.Load(); err == nil {
		t.Fatal("want batch error")
	}
}

func TestLoadRejectsNonPositiveWorkerPollInterval(t *testing.T) {
	for _, value := range []string{"0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			clearIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("WORKER_POLL_INTERVAL", value)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load succeeded, want non-positive poll interval error")
			}
			if !strings.Contains(err.Error(), "WORKER_POLL_INTERVAL") {
				t.Fatalf("error = %q, want it to name WORKER_POLL_INTERVAL", err)
			}
		})
	}
}

func TestLoadIdentityDefaults(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")

	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Identity.JWT.PrivateKey) != ed25519.PrivateKeySize {
		t.Fatalf("private key length = %d, want %d", len(got.Identity.JWT.PrivateKey), ed25519.PrivateKeySize)
	}
	if got.Identity.JWT.KeyID != "identity-v1" ||
		got.Identity.JWT.Issuer != "agri-price-crawler" ||
		got.Identity.JWT.Audience != "agri-clients" ||
		got.Identity.JWT.AccessTTL != 15*time.Minute {
		t.Fatalf("JWT defaults do not match the documented non-secret values")
	}
	if got.Identity.RefreshTTL != 720*time.Hour || got.Identity.ReuseGrace != 10*time.Second {
		t.Fatalf("refresh defaults = ttl %v grace %v", got.Identity.RefreshTTL, got.Identity.ReuseGrace)
	}
	if strings.Join(got.Identity.EnabledClients, ",") != "web,wechat_mini" {
		t.Fatalf("enabled clients = %v", got.Identity.EnabledClients)
	}
	if len(got.Identity.OTP.Pepper) != 32 ||
		got.Identity.OTP.TTL != 10*time.Minute ||
		got.Identity.OTP.Attempts != 5 ||
		got.Identity.OTP.Cooldown != time.Minute ||
		got.Identity.OTP.EmailPerHour != 5 ||
		got.Identity.OTP.IPPerHour != 30 {
		t.Fatalf("OTP defaults do not match the documented non-secret values")
	}
	if got.Identity.WeChat.BaseURL != "https://api.weixin.qq.com" ||
		got.Identity.WeChat.Timeout != 5*time.Second ||
		got.Identity.WeChat.IPPerHour != 60 {
		t.Fatalf("WeChat non-secret defaults do not match the documented values")
	}
	if got.Identity.SMTP.Timeout != 5*time.Second {
		t.Fatalf("SMTP timeout = %v", got.Identity.SMTP.Timeout)
	}
	if got.Identity.Web.CookieName != "agri_refresh" || got.Identity.Web.CookieSecure {
		t.Fatalf("development Web defaults = %+v", got.Identity.Web)
	}
	if err := got.ValidateGateway(); err != nil {
		t.Fatalf("ValidateGateway() error = %v", err)
	}
}

func TestLoadIdentityProductionDefaults(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	t.Setenv("APP_ENV", "production")
	setValidSMTPEnv(t)
	t.Setenv("IDENTITY_WEB_ALLOWED_ORIGINS", "https://app.example.com")

	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity.Web.CookieName != "__Secure-agri_refresh" || !got.Identity.Web.CookieSecure {
		t.Fatalf("production Web defaults = %+v", got.Identity.Web)
	}
	if err := got.ValidateGateway(); err != nil {
		t.Fatalf("ValidateGateway() error = %v", err)
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	for _, value := range []string{"prod", "Production", "DEVELOPMENT", " production ", "staging"} {
		t.Run(value, func(t *testing.T) {
			clearIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("APP_ENV", value)

			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
				t.Fatalf("Load() error = %v, want APP_ENV validation error", err)
			}
		})
	}
}

func TestLoadWithoutGatewayIdentityCredentials(t *testing.T) {
	clearIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load() blocked Worker or Migrate: %v", err)
	}
	if len(got.Identity.JWT.PrivateKey) != 0 || len(got.Identity.OTP.Pepper) != 0 {
		t.Fatal("missing credentials were synthesized")
	}
	if err := got.ValidateGateway(); err == nil {
		t.Fatal("ValidateGateway() succeeded without Gateway Identity credentials")
	}
}

func TestIdentityGatewayValidationRejectsMissingCredentials(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		message string
	}{
		{name: "JWT signing seed", env: "IDENTITY_JWT_PRIVATE_KEY_BASE64", message: "IDENTITY_JWT_PRIVATE_KEY_BASE64"},
		{name: "OTP Pepper", env: "IDENTITY_OTP_PEPPER_BASE64", message: "IDENTITY_OTP_PEPPER_BASE64"},
		{name: "WeChat AppID", env: "IDENTITY_WECHAT_APP_ID", message: "IDENTITY_WECHAT_APP_ID"},
		{name: "WeChat AppSecret", env: "IDENTITY_WECHAT_APP_SECRET", message: "IDENTITY_WECHAT_APP_SECRET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv(tt.env, "")

			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			err = got.ValidateGateway()
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("ValidateGateway() error = %v, want field %s", err, tt.message)
			}
		})
	}
}

func TestIdentityGatewayValidationRejectsInvalidProductionSecurity(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		value   string
		message string
	}{
		{name: "insecure Cookie", env: "IDENTITY_COOKIE_SECURE", value: "false", message: "IDENTITY_COOKIE_SECURE"},
		{name: "Cookie without secure prefix", env: "IDENTITY_COOKIE_NAME", value: "agri_refresh", message: "IDENTITY_COOKIE_NAME"},
		{name: "missing origin", env: "IDENTITY_WEB_ALLOWED_ORIGINS", value: "", message: "IDENTITY_WEB_ALLOWED_ORIGINS"},
		{name: "wildcard origin", env: "IDENTITY_WEB_ALLOWED_ORIGINS", value: "*", message: "IDENTITY_WEB_ALLOWED_ORIGINS"},
		{name: "origin with path", env: "IDENTITY_WEB_ALLOWED_ORIGINS", value: "https://app.example.com/", message: "IDENTITY_WEB_ALLOWED_ORIGINS"},
		{name: "memory email", env: "IDENTITY_EMAIL_DRIVER", value: "memory", message: "IDENTITY_EMAIL_DRIVER"},
		{name: "plaintext SMTP", env: "IDENTITY_SMTP_TLS_MODE", value: "none", message: "IDENTITY_SMTP_TLS_MODE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("APP_ENV", "production")
			setValidSMTPEnv(t)
			t.Setenv("IDENTITY_WEB_ALLOWED_ORIGINS", "https://app.example.com")
			t.Setenv(tt.env, tt.value)

			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			err = got.ValidateGateway()
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("ValidateGateway() error = %v, want field %s", err, tt.message)
			}
		})
	}
}

func TestIdentityGatewayValidationRequiresHTTPSInProduction(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		value   string
		message string
	}{
		{name: "WeChat base URL", env: "IDENTITY_WECHAT_BASE_URL", value: "http://wechat.example.com", message: "IDENTITY_WECHAT_BASE_URL"},
		{name: "Web origin", env: "IDENTITY_WEB_ALLOWED_ORIGINS", value: "http://app.example.com", message: "IDENTITY_WEB_ALLOWED_ORIGINS"},
		{name: "one insecure Web origin", env: "IDENTITY_WEB_ALLOWED_ORIGINS", value: "https://app.example.com,http://admin.example.com", message: "IDENTITY_WEB_ALLOWED_ORIGINS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("APP_ENV", "production")
			setValidSMTPEnv(t)
			t.Setenv("IDENTITY_WEB_ALLOWED_ORIGINS", "https://app.example.com")
			t.Setenv(tt.env, tt.value)

			got, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			err = got.ValidateGateway()
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("ValidateGateway() error = %v, want field %s", err, tt.message)
			}
		})
	}
}

func TestIdentityGatewayValidationAllowsHTTPInDevelopment(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	t.Setenv("IDENTITY_WECHAT_BASE_URL", "http://127.0.0.1:18080")
	t.Setenv("IDENTITY_WEB_ALLOWED_ORIGINS", "http://localhost:3000")

	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := got.ValidateGateway(); err != nil {
		t.Fatalf("ValidateGateway() error = %v", err)
	}
}

func TestIdentityGatewayValidationUsesStrictOriginCanonicalization(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")

	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	for _, origin := range []string{
		"https://example.com:",
		"https://[2001:db8::1]:",
		"https://[example.com]",
		"https://[192.0.2.1]",
		"https://example.com.",
		"https://example..com",
		"https://-example.com",
		"https://example-.com",
		"https://exa_mple.com",
		"https://例子.example",
	} {
		candidate := got
		candidate.Identity.Web.AllowedOrigins = []string{origin}
		if err := candidate.ValidateGateway(); err == nil || !strings.Contains(err.Error(), "IDENTITY_WEB_ALLOWED_ORIGINS") {
			t.Fatalf("ValidateGateway() origin %q error = %v, want strict origin error", origin, err)
		}
	}

	for _, origin := range []string{
		"HTTPS://Example.COM:443",
		"https://xn--fsqu00a.example",
	} {
		candidate := got
		candidate.Identity.Web.AllowedOrigins = []string{origin}
		if err := candidate.ValidateGateway(); err != nil {
			t.Fatalf("ValidateGateway() origin %q error = %v", origin, err)
		}
	}
}

func TestIdentityGatewayValidationSMTP(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		value   string
		message string
	}{
		{name: "host", env: "IDENTITY_SMTP_HOST", value: "", message: "IDENTITY_SMTP_HOST"},
		{name: "port", env: "IDENTITY_SMTP_PORT", value: "0", message: "IDENTITY_SMTP_PORT"},
		{name: "username", env: "IDENTITY_SMTP_USERNAME", value: "", message: "IDENTITY_SMTP_USERNAME"},
		{name: "password", env: "IDENTITY_SMTP_PASSWORD", value: "", message: "IDENTITY_SMTP_PASSWORD"},
		{name: "from", env: "IDENTITY_SMTP_FROM", value: "", message: "IDENTITY_SMTP_FROM"},
		{name: "display-name from", env: "IDENTITY_SMTP_FROM", value: "Sender <sender@example.com>", message: "IDENTITY_SMTP_FROM"},
		{name: "whitespace from", env: "IDENTITY_SMTP_FROM", value: " sender@example.com", message: "IDENTITY_SMTP_FROM"},
		{name: "TLS mode", env: "IDENTITY_SMTP_TLS_MODE", value: "opportunistic", message: "IDENTITY_SMTP_TLS_MODE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			setValidSMTPEnv(t)
			t.Setenv(tt.env, tt.value)

			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			err = got.ValidateGateway()
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("ValidateGateway() error = %v, want field %s", err, tt.message)
			}
		})
	}
}

func TestIdentityGatewayValidationAllowsDevelopmentPlaintextSMTP(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	setValidSMTPEnv(t)
	t.Setenv("IDENTITY_SMTP_HOST", "mailpit")
	t.Setenv("IDENTITY_SMTP_TLS_MODE", "none")
	t.Setenv("IDENTITY_SMTP_USERNAME", "")
	t.Setenv("IDENTITY_SMTP_PASSWORD", "")

	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := got.ValidateGateway(); err != nil {
		t.Fatalf("ValidateGateway() error = %v", err)
	}
}

func TestIdentityGatewayValidationRejectsFutureAppClient(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	t.Setenv("IDENTITY_ENABLED_CLIENTS", "web,app")

	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	err = got.ValidateGateway()
	if err == nil || !strings.Contains(err.Error(), "IDENTITY_ENABLED_CLIENTS") {
		t.Fatalf("ValidateGateway() error = %v", err)
	}
}

func TestIdentityGatewayValidationRejectsExplicitEmptyClients(t *testing.T) {
	for _, value := range []string{"", " ", ",", " , "} {
		t.Run(strings.ReplaceAll(value, " ", "_"), func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("IDENTITY_ENABLED_CLIENTS", value)

			got, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			err = got.ValidateGateway()
			if err == nil || !strings.Contains(err.Error(), "IDENTITY_ENABLED_CLIENTS") {
				t.Fatalf("ValidateGateway() error = %v", err)
			}
		})
	}
}

func TestIdentityGatewayValidationRejectsUnsafeUpperBounds(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		value      string
		message    string
		enableSMTP bool
	}{
		{name: "Access TTL", env: "IDENTITY_JWT_ACCESS_TTL", value: "1h1s", message: "IDENTITY_JWT_ACCESS_TTL"},
		{name: "Refresh TTL", env: "IDENTITY_REFRESH_TTL", value: "2160h1s", message: "IDENTITY_REFRESH_TTL"},
		{name: "reuse grace", env: "IDENTITY_REFRESH_REUSE_GRACE", value: "1m1s", message: "IDENTITY_REFRESH_REUSE_GRACE"},
		{name: "OTP TTL", env: "IDENTITY_OTP_TTL", value: "15m1s", message: "IDENTITY_OTP_TTL"},
		{name: "OTP attempts", env: "IDENTITY_OTP_ATTEMPTS", value: "11", message: "IDENTITY_OTP_ATTEMPTS"},
		{name: "OTP cooldown", env: "IDENTITY_OTP_COOLDOWN", value: "1h1s", message: "IDENTITY_OTP_COOLDOWN"},
		{name: "OTP email limit", env: "IDENTITY_OTP_EMAIL_PER_HOUR", value: "100001", message: "IDENTITY_OTP_EMAIL_PER_HOUR"},
		{name: "OTP IP limit", env: "IDENTITY_OTP_IP_PER_HOUR", value: "100001", message: "IDENTITY_OTP_IP_PER_HOUR"},
		{name: "WeChat IP limit", env: "IDENTITY_WECHAT_IP_PER_HOUR", value: "100001", message: "IDENTITY_WECHAT_IP_PER_HOUR"},
		{name: "WeChat timeout", env: "IDENTITY_WECHAT_TIMEOUT", value: "31s", message: "IDENTITY_WECHAT_TIMEOUT"},
		{name: "SMTP timeout", env: "IDENTITY_SMTP_TIMEOUT", value: "31s", message: "IDENTITY_SMTP_TIMEOUT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			if tt.enableSMTP {
				setValidSMTPEnv(t)
			}
			t.Setenv(tt.env, tt.value)

			got, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			err = got.ValidateGateway()
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("ValidateGateway() error = %v, want field %s", err, tt.message)
			}
		})
	}
}

func TestIdentityGatewayValidationRequiresWholeMinuteOTPTTL(t *testing.T) {
	for _, value := range []string{"59s", "1m1s"} {
		t.Run(value, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("IDENTITY_OTP_TTL", value)

			got, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			err = got.ValidateGateway()
			if err == nil || !strings.Contains(err.Error(), "IDENTITY_OTP_TTL") {
				t.Fatalf("ValidateGateway() error = %v", err)
			}
		})
	}
}

func TestIdentityGatewayValidationTLSModesRequireCredentials(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		for _, credential := range []string{"IDENTITY_SMTP_USERNAME", "IDENTITY_SMTP_PASSWORD"} {
			name := mode + "_" + strings.TrimPrefix(credential, "IDENTITY_SMTP_")
			t.Run(name, func(t *testing.T) {
				setValidIdentityEnv(t)
				t.Setenv("DATABASE_URL", "postgres://localhost/agri")
				setValidSMTPEnv(t)
				t.Setenv("IDENTITY_SMTP_TLS_MODE", mode)
				t.Setenv(credential, "")

				got, err := config.Load()
				if err != nil {
					t.Fatal(err)
				}
				err = got.ValidateGateway()
				if err == nil || !strings.Contains(err.Error(), credential) {
					t.Fatalf("ValidateGateway() error = %v", err)
				}
			})
		}
	}
}

func TestIdentityGatewayValidationRequiresWholeSecondAccessTTL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "sub-second", value: "500ms", wantErr: true},
		{name: "fractional-second", value: "1500ms", wantErr: true},
		{name: "one-second boundary", value: "1s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("IDENTITY_JWT_ACCESS_TTL", tt.value)

			got, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			err = got.ValidateGateway()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "IDENTITY_JWT_ACCESS_TTL") {
					t.Fatalf("ValidateGateway() error = %v, want IDENTITY_JWT_ACCESS_TTL error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateGateway() error = %v", err)
			}
		})
	}
}

func TestIdentityGatewayValidationAcceptsDocumentedUpperBounds(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	setValidSMTPEnv(t)
	t.Setenv("IDENTITY_JWT_ACCESS_TTL", "1h")
	t.Setenv("IDENTITY_REFRESH_TTL", "2160h")
	t.Setenv("IDENTITY_REFRESH_REUSE_GRACE", "1m")
	t.Setenv("IDENTITY_OTP_TTL", "15m")
	t.Setenv("IDENTITY_OTP_ATTEMPTS", "10")
	t.Setenv("IDENTITY_OTP_COOLDOWN", "1h")
	t.Setenv("IDENTITY_OTP_EMAIL_PER_HOUR", "100000")
	t.Setenv("IDENTITY_OTP_IP_PER_HOUR", "100000")
	t.Setenv("IDENTITY_WECHAT_IP_PER_HOUR", "100000")
	t.Setenv("IDENTITY_WECHAT_TIMEOUT", "30s")
	t.Setenv("IDENTITY_SMTP_TIMEOUT", "30s")

	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := got.ValidateGateway(); err != nil {
		t.Fatalf("ValidateGateway() error = %v", err)
	}
}

func TestIdentityValidationErrorsDoNotLeakSecrets(t *testing.T) {
	setValidIdentityEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	t.Setenv("APP_ENV", "production")
	setValidSMTPEnv(t)
	t.Setenv("IDENTITY_WEB_ALLOWED_ORIGINS", "http://app.example.com")

	secrets := []string{
		os.Getenv("IDENTITY_JWT_PRIVATE_KEY_BASE64"),
		os.Getenv("IDENTITY_OTP_PEPPER_BASE64"),
		os.Getenv("IDENTITY_WECHAT_APP_SECRET"),
		os.Getenv("IDENTITY_SMTP_PASSWORD"),
	}
	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	err = got.ValidateGateway()
	if err == nil {
		t.Fatal("ValidateGateway() succeeded with insecure production origin")
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked a configured secret: %q", err)
		}
	}
}

func TestLoadIdentityRejectsMalformedValuesWithoutLeakingSecrets(t *testing.T) {
	tests := []struct {
		name   string
		env    string
		secret string
	}{
		{name: "Pepper base64", env: "IDENTITY_OTP_PEPPER_BASE64", secret: "not-base64-pepper"},
		{name: "JWT seed base64", env: "IDENTITY_JWT_PRIVATE_KEY_BASE64", secret: "not-base64-seed"},
		{name: "JWT seed length", env: "IDENTITY_JWT_PRIVATE_KEY_BASE64", secret: base64.StdEncoding.EncodeToString([]byte("short-secret"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidIdentityEnv(t)
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv(tt.env, tt.secret)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load() succeeded with malformed secret")
			}
			if !strings.Contains(err.Error(), tt.env) {
				t.Fatalf("error = %q, want variable name", err)
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("error leaked secret: %q", err)
			}
		})
	}
}

func setValidSMTPEnv(t *testing.T) {
	t.Helper()
	t.Setenv("IDENTITY_EMAIL_DRIVER", "smtp")
	t.Setenv("IDENTITY_SMTP_HOST", "smtp.example.com")
	t.Setenv("IDENTITY_SMTP_PORT", "587")
	t.Setenv("IDENTITY_SMTP_USERNAME", "mailer")
	t.Setenv("IDENTITY_SMTP_PASSWORD", "smtp-secret")
	t.Setenv("IDENTITY_SMTP_FROM", "noreply@example.com")
	t.Setenv("IDENTITY_SMTP_TLS_MODE", "starttls")
}
