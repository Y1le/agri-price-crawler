// Package migrate applies embedded, forward-only PostgreSQL schema migrations.
package migrate

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockID int64 = 71422026

type migration struct {
	version int64
	name    string
	sql     string
}

// Up applies every catalog migration that has not previously been recorded.
// With no explicit sources, Up applies the shared Platform migrations.
func Up(ctx context.Context, pool *pgxpool.Pool, sources ...Source) (err error) {
	migrations, err := loadSources(defaultSources(sources))
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS platform_schema_migrations (
			version BIGINT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create migration tracking table: %w", err)
	}

	for _, migration := range migrations {
		var applied bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM platform_schema_migrations WHERE version = $1)", migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", migration.version, err)
		}
		if applied {
			continue
		}

		if _, err := tx.Exec(ctx, migration.sql); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", migration.version, migration.name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO platform_schema_migrations (version) VALUES ($1)", migration.version); err != nil {
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	return nil
}

// CurrentVersion returns the most recently applied migration version, or zero when none exist.
func CurrentVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var version int64
	if err := pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM platform_schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("query current migration version: %w", err)
	}
	return version, nil
}

func migrationVersion(name string) (int64, error) {
	prefix, _, found := strings.Cut(name, "_")
	if !found || !strings.HasSuffix(name, ".up.sql") {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}

	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("invalid migration version in %q", name)
	}
	return version, nil
}
