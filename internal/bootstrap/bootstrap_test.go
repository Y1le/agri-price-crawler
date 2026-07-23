package bootstrap_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

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
	cfg := config.Config{Postgres: config.Postgres{URL: "://invalid"}}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
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
