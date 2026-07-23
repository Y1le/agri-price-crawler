// Package bootstrap assembles the dependencies for the new platform processes.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Y1le/agri-price-crawler/internal/gateway"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/rediscache"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunGateway assembles and runs the HTTP gateway until ctx is cancelled.
func RunGateway(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := openPostgres(ctx, cfg.Postgres.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := requireSchemaVersion(ctx, pool); err != nil {
		return err
	}

	redisClient := rediscache.Open(cfg.Redis)
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil && logger != nil {
		logger.Warn("Redis ping failed; continuing without Redis readiness", "error", err)
	}

	server := gateway.New(gateway.Config{
		Addr:   cfg.Gateway.Addr,
		Ready:  pool.Ping,
		Logger: logger,
	})
	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run Gateway: %w", err)
	}
	return nil
}

func openPostgres(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := platformpg.Open(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	return pool, nil
}

func requireSchemaVersion(ctx context.Context, pool *pgxpool.Pool) error {
	version, err := migrate.CurrentVersion(ctx, pool)
	if err != nil {
		return fmt.Errorf("check PostgreSQL schema version: %w", err)
	}
	requiredVersion, err := requiredSchemaVersion()
	if err != nil {
		return fmt.Errorf("determine required PostgreSQL schema version: %w", err)
	}
	if version != requiredVersion {
		return fmt.Errorf("PostgreSQL schema version is %d, want %d", version, requiredVersion)
	}
	return nil
}
