package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/Y1le/agri-price-crawler/internal/platform/jobs"
)

// RunWorker assembles and runs the durable job worker until ctx is cancelled.
func RunWorker(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := openPostgres(ctx, cfg.Postgres.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := requireSchemaVersion(ctx, pool); err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("determine Worker hostname: %w", err)
	}
	workerID := fmt.Sprintf("%s-%d", hostname, os.Getpid())
	repository := jobs.NewPostgresRepository(pool)
	runner := jobs.NewRunner(repository, workerID, cfg.Worker.BatchSize, cfg.Worker.LeaseDuration, logger)
	if err := runner.Run(ctx, cfg.Worker.PollInterval); err != nil {
		return fmt.Errorf("run Worker: %w", err)
	}
	return nil
}
