package migrate_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
)

func TestLatestVersionSortsGlobalVersionsAcrossSources(t *testing.T) {
	source := migrate.NewSource("test", fstest.MapFS{
		"000002_second.up.sql": {Data: []byte("SELECT 2")},
		"000001_first.up.sql":  {Data: []byte("SELECT 1")},
	}, ".")

	version, err := migrate.LatestVersion(source)
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("version = %d, want 2", version)
	}
}

func TestLatestVersionRejectsDuplicateGlobalVersion(t *testing.T) {
	a := migrate.NewSource("a", fstest.MapFS{
		"000001_a.up.sql": {Data: []byte("SELECT 1")},
	}, ".")
	b := migrate.NewSource("b", fstest.MapFS{
		"000001_b.up.sql": {Data: []byte("SELECT 2")},
	}, ".")

	_, err := migrate.LatestVersion(a, b)
	if err == nil || !strings.Contains(err.Error(), "duplicate migration version 1") {
		t.Fatalf("error = %v, want duplicate version", err)
	}
}

func TestUpIsIdempotentAndCreatesJobLeaseIndex(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool, err := platformpg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS platform_outbox, platform_jobs, platform_schema_migrations"); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}

	version, err := migrate.CurrentVersion(ctx, pool)
	if err != nil || version != 2 {
		t.Fatalf("version=%d err=%v", version, err)
	}

	var indexName *string
	if err := pool.QueryRow(ctx, "SELECT to_regclass('platform_jobs_running_locked_at_id_idx')::text").Scan(&indexName); err != nil {
		t.Fatal(err)
	}
	if indexName == nil || *indexName != "platform_jobs_running_locked_at_id_idx" {
		t.Fatalf("lease index = %v, want platform_jobs_running_locked_at_id_idx", indexName)
	}
}
