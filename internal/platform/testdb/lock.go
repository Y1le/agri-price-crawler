// Package testdb provides helpers for integration tests that share a database.
package testdb

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Keep this distinct from the production migration advisory lock.
const schemaAdvisoryLockID int64 = 71422027

// LockSchema serializes integration tests that mutate the shared public schema.
// The session-level advisory lock is held until all test defers have run.
func LockSchema(t testing.TB, ctx context.Context, databaseURL string) {
	t.Helper()

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect for shared PostgreSQL test lock: %v", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", schemaAdvisoryLockID); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("acquire shared PostgreSQL test lock: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var unlocked bool
		unlockErr := conn.QueryRow(
			cleanupCtx,
			"SELECT pg_advisory_unlock($1)",
			schemaAdvisoryLockID,
		).Scan(&unlocked)
		closeErr := conn.Close(cleanupCtx)

		if unlockErr != nil {
			t.Errorf("release shared PostgreSQL test lock: %v", unlockErr)
		} else if !unlocked {
			t.Error("release shared PostgreSQL test lock: lock was not held")
		}
		if closeErr != nil {
			t.Errorf("close shared PostgreSQL test lock connection: %v", closeErr)
		}
	})
}
