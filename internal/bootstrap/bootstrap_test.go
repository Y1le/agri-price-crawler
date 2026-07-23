package bootstrap_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/bootstrap"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/testdb"
)

func TestRunFunctionsWrapPostgresConnectionFailure(t *testing.T) {
	tests := map[string]func(context.Context, config.Config, *slog.Logger) error{
		"gateway": bootstrap.RunGateway,
		"worker":  bootstrap.RunWorker,
		"migrate": bootstrap.RunMigrate,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{Postgres: config.Postgres{URL: "://invalid"}}
			if name == "gateway" {
				cfg = validGatewayConfig()
				cfg.Postgres.URL = "://invalid"
			}
			err := run(context.Background(), cfg, logger)
			if err == nil {
				t.Fatal("want PostgreSQL connection error")
			}
			if !strings.Contains(err.Error(), "PostgreSQL") {
				t.Fatalf("error = %q, want it to contain PostgreSQL", err)
			}
		})
	}
}

func TestRunGatewayValidatesConfigurationBeforeOpeningPostgres(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{Postgres: config.Postgres{URL: "://invalid"}}

	err := bootstrap.RunGateway(context.Background(), cfg, logger)
	if err == nil {
		t.Fatal("RunGateway() succeeded without Identity configuration")
	}
	if !strings.Contains(err.Error(), "validate Gateway configuration") {
		t.Fatalf("error = %q, want Gateway validation context", err)
	}
	if strings.Contains(err.Error(), "PostgreSQL") {
		t.Fatalf("error = %q, PostgreSQL was reached before configuration validation", err)
	}
}

func TestRunMigrateAppliesLatestSchemaVersion(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	testdb.LockSchema(t, ctx, url)
	pool, err := platformpg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS
			identity_account_merges,
			identity_refresh_tokens,
			identity_sessions,
			identity_identities,
			identity_users,
			platform_outbox,
			platform_jobs,
			platform_schema_migrations
		CASCADE
	`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	pool.Close()

	cfg := config.Config{Postgres: config.Postgres{URL: url}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := bootstrap.RunMigrate(ctx, cfg, logger); err != nil {
		t.Fatal(err)
	}

	pool, err = platformpg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	version, err := migrate.CurrentVersion(ctx, pool)
	if err != nil || version != 3 {
		t.Fatalf("version=%d err=%v", version, err)
	}
}

func validGatewayConfig() config.Config {
	return config.Config{
		Environment: "development",
		Identity: config.Identity{
			JWT: config.IdentityJWT{
				PrivateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)),
				KeyID:      "test-key",
				Issuer:     "test-issuer",
				Audience:   "test-audience",
				AccessTTL:  15 * time.Minute,
			},
			OTP: config.IdentityOTP{
				Pepper:       bytes.Repeat([]byte{9}, 32),
				TTL:          10 * time.Minute,
				Attempts:     5,
				Cooldown:     time.Minute,
				EmailPerHour: 5,
				IPPerHour:    30,
			},
			RefreshTTL:     30 * 24 * time.Hour,
			ReuseGrace:     10 * time.Second,
			WeChat:         config.WeChat{AppID: "wx-test", AppSecret: "test-secret", BaseURL: "https://api.weixin.qq.com", Timeout: 5 * time.Second, IPPerHour: 60},
			SMTP:           config.SMTP{Timeout: 5 * time.Second},
			EmailDriver:    "memory",
			EnabledClients: []string{"web", "wechat_mini"},
			Web:            config.WebSecurity{CookieName: "agri_refresh"},
		},
	}
}
