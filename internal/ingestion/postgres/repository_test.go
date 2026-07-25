package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/ingestion"
	ingestionpostgres "github.com/Y1le/agri-price-crawler/internal/ingestion/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/testdb"
	pricingpostgres "github.com/Y1le/agri-price-crawler/internal/pricing/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBatchRepositoryResumesRetryableBatchAndProtectsRawRecords(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool := openIsolatedIngestionPool(t, ctx, databaseURL)
	if err := migrate.Up(ctx, pool, migrate.PlatformSource(), pricingpostgres.Migrations(), ingestionpostgres.Migrations()); err != nil {
		t.Fatal(err)
	}
	date, err := ingestion.ParseBusinessDate("2026-07-25")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC)
	repository := ingestionpostgres.NewBatchRepository(pool, 30*24*time.Hour, func() time.Time { return now })

	batch, created, err := repository.Start(ctx, "huinong", date, 42)
	if err != nil || !created || batch.State != ingestion.BatchStateRunning || batch.Revision != 1 {
		t.Fatalf("Start() = %+v, %t, %v", batch, created, err)
	}
	if err := repository.AppendRaw(ctx, batch.ID, ingestion.RawRecord{
		SourceRecordKey: "page-1:row-1",
		Payload:         []byte(`{"price":"1.20","authorization":"must-not-persist","device_id":"must-not-persist"}`),
		ObservedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendRaw(ctx, batch.ID, ingestion.RawRecord{
		SourceRecordKey: "page-1:row-1",
		Payload:         []byte(`{"price":"1.20","authorization":"must-not-persist","device_id":"must-not-persist"}`),
		ObservedAt:      now,
	}); err != nil {
		t.Fatalf("identical raw append error = %v", err)
	}
	if err := repository.AppendRaw(ctx, batch.ID, ingestion.RawRecord{
		SourceRecordKey: "page-1:row-1",
		Payload:         []byte(`{"price":"1.21"}`),
		ObservedAt:      now,
	}); !errors.Is(err, ingestion.ErrDuplicateConflict) {
		t.Fatalf("conflicting raw append error = %v", err)
	}
	pending, err := repository.ListPending(ctx, batch.ID)
	var storedPayload map[string]string
	if len(pending) == 1 {
		_ = json.Unmarshal(pending[0].Payload, &storedPayload)
	}
	if err != nil || len(pending) != 1 || storedPayload["price"] != "1.20" || storedPayload["authorization"] != "" || storedPayload["device_id"] != "" || !pending[0].ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("pending raw records = %+v err = %v", pending, err)
	}

	if err := repository.Fail(ctx, batch.ID, ingestion.FailureSourceRetryable); err != nil {
		t.Fatal(err)
	}
	resumed, created, err := repository.Start(ctx, "huinong", date, 42)
	if err != nil || created || resumed.ID != batch.ID || resumed.State != ingestion.BatchStateRunning {
		t.Fatalf("retry Start() = %+v, %t, %v", resumed, created, err)
	}
	if err := repository.Fail(ctx, batch.ID, ingestion.FailureExpectedCountMismatch); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Start(ctx, "huinong", date, 42); !errors.Is(err, ingestion.ErrBatchNotRestartable) {
		t.Fatalf("permanent retry error = %v", err)
	}
}

func TestBatchRepositoryCreatesOneScheduledBatchConcurrently(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool := openIsolatedIngestionPool(t, ctx, databaseURL)
	if err := migrate.Up(ctx, pool, migrate.PlatformSource(), pricingpostgres.Migrations(), ingestionpostgres.Migrations()); err != nil {
		t.Fatal(err)
	}
	date, err := ingestion.ParseBusinessDate("2026-07-25")
	if err != nil {
		t.Fatal(err)
	}
	repository := ingestionpostgres.NewBatchRepository(pool, 30*24*time.Hour, time.Now)

	const callers = 2
	results := make(chan ingestion.Batch, callers)
	errorsCh := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			batch, _, err := repository.Start(ctx, "huinong", date, 99)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- batch
		}()
	}
	group.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	var IDs []uuid.UUID
	for batch := range results {
		IDs = append(IDs, batch.ID)
	}
	if len(IDs) != callers || IDs[0] != IDs[1] {
		t.Fatalf("concurrent batch IDs = %v", IDs)
	}
}

func TestBatchRepositoryRejectsInvalidRepairAndPrunesOnlyExpiredRawRecords(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool := openIsolatedIngestionPool(t, ctx, databaseURL)
	if err := migrate.Up(ctx, pool, migrate.PlatformSource(), pricingpostgres.Migrations(), ingestionpostgres.Migrations()); err != nil {
		t.Fatal(err)
	}
	date, err := ingestion.ParseBusinessDate("2026-07-25")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC)
	repository := ingestionpostgres.NewBatchRepository(pool, 24*time.Hour, func() time.Time { return now })
	if _, err := repository.StartRepair(ctx, "huinong", date, ingestion.RepairRequest{}); !errors.Is(err, ingestion.ErrInvalidRepair) {
		t.Fatalf("empty repair error = %v", err)
	}
	if _, err := repository.StartRepair(ctx, "huinong", date, ingestion.RepairRequest{Actor: "ops", Reason: "correct source"}); !errors.Is(err, ingestion.ErrRepairNotAllowed) {
		t.Fatalf("repair without a published batch error = %v", err)
	}

	batch, _, err := repository.Start(ctx, "huinong", date, 77)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendRaw(ctx, batch.ID, ingestion.RawRecord{
		SourceRecordKey: "precise",
		Payload:         []byte(`{"price":12345678901234567890.1234}`),
		ObservedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := repository.ListPending(ctx, batch.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPending() = %+v, %v", pending, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(pending[0].Payload))
	decoder.UseNumber()
	var stored map[string]json.Number
	if err := decoder.Decode(&stored); err != nil || stored["price"].String() != "12345678901234567890.1234" {
		t.Fatalf("stored payload = %s, decode error = %v", pending[0].Payload, err)
	}
	deleted, err := repository.PruneExpired(ctx, now.Add(24*time.Hour+time.Second), 10)
	if err != nil || deleted != 1 {
		t.Fatalf("PruneExpired() = %d, %v", deleted, err)
	}
	if _, _, err := repository.Start(ctx, "huinong", date, 77); err != nil {
		t.Fatalf("pruning raw records changed batch state: %v", err)
	}
}

func openIsolatedIngestionPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	testdb.LockSchema(t, ctx, databaseURL)
	adminPool, err := platformpg.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := fmt.Sprintf("ingestion_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop isolated Ingestion schema: %v", err)
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
