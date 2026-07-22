package config_test

import (
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://agri:agri@localhost:5432/agri?sslmode=disable")
	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment != "development" || got.Gateway.Addr != ":8080" {
		t.Fatalf("got %+v", got)
	}
	if got.Redis.Addr != "localhost:6379" {
		t.Fatalf("got %+v", got.Redis)
	}
	if got.Worker.PollInterval != time.Second || got.Worker.BatchSize != 4 {
		t.Fatalf("got %+v", got.Worker)
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("want error")
	}
}

func TestLoadRejectsInvalidWorkerValues(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/agri")
	t.Setenv("WORKER_POLL_INTERVAL", "bad")
	if _, err := config.Load(); err == nil {
		t.Fatal("want duration error")
	}
	t.Setenv("WORKER_POLL_INTERVAL", "1s")
	t.Setenv("WORKER_BATCH_SIZE", "0")
	if _, err := config.Load(); err == nil {
		t.Fatal("want batch error")
	}
}
