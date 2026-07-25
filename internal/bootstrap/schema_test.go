package bootstrap

import (
	"context"
	"os"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/testdb"
)

func TestRequiredSchemaVersionUsesMigrationCatalog(t *testing.T) {
	version, err := requiredSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("version = %d, want 5", version)
	}
}

func TestRequireSchemaVersionAcceptsLatestMigration(t *testing.T) {
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
	defer pool.Close()
	if err := migrate.Up(ctx, pool, migrationSources()...); err != nil {
		t.Fatal(err)
	}

	if err := requireSchemaVersion(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
