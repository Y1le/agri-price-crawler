package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Y1le/agri-price-crawler/internal/bootstrap"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.RunGateway(ctx, cfg, logger); err != nil {
		logger.Error("Gateway stopped", "error", err)
		stop()
		os.Exit(1)
	}
}
