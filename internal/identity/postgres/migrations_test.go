package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/bootstrap"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
)

func TestMigrationsCreateIdentitySchema(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{Postgres: config.Postgres{URL: url}}
	if err := bootstrap.RunMigrate(ctx, cfg, logger); err != nil {
		t.Fatal(err)
	}

	pool, err = platformpg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var tableCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name = ANY($1)
	`, []string{
		"identity_users",
		"identity_identities",
		"identity_sessions",
		"identity_refresh_tokens",
		"identity_account_merges",
	}).Scan(&tableCount)
	if err != nil || tableCount != 5 {
		t.Fatalf("identity table count=%d err=%v", tableCount, err)
	}

	const primaryUserID = "00000000-0000-0000-0000-000000000001"
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity_users (id, status, created_at, updated_at)
		VALUES ($1, 'active', now(), now())
	`, primaryUserID); err != nil {
		t.Fatal(err)
	}

	const invalidUserID = "00000000-0000-0000-0000-000000000002"
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity_users (id, status, merged_into_user_id, created_at, updated_at)
		VALUES ($1, 'active', $2, now(), now())
	`, invalidUserID, primaryUserID); err == nil {
		t.Fatal("active user with merged_into_user_id was accepted")
	}
}
