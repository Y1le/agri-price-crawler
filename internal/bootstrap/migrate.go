package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
)

// RunMigrate opens PostgreSQL and applies every pending platform migration.
func RunMigrate(ctx context.Context, cfg config.Config, _ *slog.Logger) error {
	pool, err := openPostgres(ctx, cfg.Postgres.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := migrate.Up(ctx, pool); err != nil {
		return fmt.Errorf("migrate PostgreSQL: %w", err)
	}
	return nil
}
