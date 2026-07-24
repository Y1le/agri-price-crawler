package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	identitypostgres "github.com/Y1le/agri-price-crawler/internal/identity/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	pricingpostgres "github.com/Y1le/agri-price-crawler/internal/pricing/postgres"
)

// RunMigrate opens PostgreSQL and applies every registered module migration.
func RunMigrate(ctx context.Context, cfg config.Config, _ *slog.Logger) error {
	pool, err := openPostgres(ctx, cfg.Postgres.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := migrate.Up(ctx, pool, migrationSources()...); err != nil {
		return fmt.Errorf("migrate PostgreSQL: %w", err)
	}
	return nil
}

func migrationSources() []migrate.Source {
	return []migrate.Source{
		migrate.PlatformSource(),
		identitypostgres.Migrations(),
		pricingpostgres.Migrations(),
	}
}

func requiredSchemaVersion() (int64, error) {
	return migrate.LatestVersion(migrationSources()...)
}
