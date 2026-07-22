package config_test

import (
	"strings"
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
	if got.Worker.PollInterval != time.Second || got.Worker.BatchSize != 4 || got.Worker.LeaseDuration != 5*time.Minute {
		t.Fatalf("got %+v", got.Worker)
	}
}

func TestLoadRejectsNonPositiveWorkerJobLeaseDuration(t *testing.T) {
	for _, value := range []string{"0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("WORKER_JOB_LEASE_DURATION", value)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load succeeded, want non-positive job lease duration error")
			}
			if !strings.Contains(err.Error(), "WORKER_JOB_LEASE_DURATION") {
				t.Fatalf("error = %q, want it to name WORKER_JOB_LEASE_DURATION", err)
			}
		})
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

func TestLoadRejectsNonPositiveWorkerPollInterval(t *testing.T) {
	for _, value := range []string{"0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/agri")
			t.Setenv("WORKER_POLL_INTERVAL", value)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load succeeded, want non-positive poll interval error")
			}
			if !strings.Contains(err.Error(), "WORKER_POLL_INTERVAL") {
				t.Fatalf("error = %q, want it to name WORKER_POLL_INTERVAL", err)
			}
		})
	}
}
