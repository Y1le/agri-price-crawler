package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/testdb"
	"github.com/Y1le/agri-price-crawler/internal/pricing"
	pricingpostgres "github.com/Y1le/agri-price-crawler/internal/pricing/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogRepositoryUsesPricingMigrationAndKeysetQueries(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool := openIsolatedPool(t, ctx, databaseURL)
	if err := migrate.Up(ctx, pool, pricingpostgres.Migrations()); err != nil {
		t.Fatal(err)
	}

	catalog := pricing.NewCatalog(pricingpostgres.NewCatalogRepository(pool))
	products, err := catalog.ListProducts(ctx, pricing.ProductQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(products.Items) != 1 || products.NextCursor == "" {
		t.Fatalf("first product page = %+v", products)
	}
	next, err := catalog.ListProducts(ctx, pricing.ProductQuery{Limit: 1, Cursor: products.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].ID == products.Items[0].ID || next.NextCursor != "" {
		t.Fatalf("second product page = %+v", next)
	}

	categoryID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	if _, err := pool.Exec(ctx, `INSERT INTO pricing_products (id, category_id, name, slug, pinyin) VALUES ($1, $2, '重复', 'tomato', 'chongfu')`, uuid.New(), categoryID); err == nil {
		t.Fatal("duplicate product slug was accepted")
	}
	pinyinProducts, err := catalog.ListProducts(ctx, pricing.ProductQuery{Q: "xihong"})
	if err != nil || len(pinyinProducts.Items) != 1 || pinyinProducts.Items[0].Slug != "tomato" {
		t.Fatalf("pinyin product lookup = %+v err = %v", pinyinProducts, err)
	}
	parentID := uuid.MustParse("30000000-0000-0000-0000-000000000001")
	regions, err := catalog.ListRegions(ctx, pricing.RegionQuery{ParentID: &parentID})
	if err != nil {
		t.Fatal(err)
	}
	if len(regions.Items) != 1 || regions.Items[0].NationalCode != "370700" {
		t.Fatalf("child regions = %+v", regions)
	}
}

func openIsolatedPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	testdb.LockSchema(t, ctx, databaseURL)
	adminPool, err := platformpg.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := fmt.Sprintf("pricing_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop isolated Pricing schema: %v", err)
		}
	})

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schemaName
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
