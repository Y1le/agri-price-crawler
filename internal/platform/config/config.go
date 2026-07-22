// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment string
	Gateway     Gateway
	Postgres    Postgres
	Redis       Redis
	Worker      Worker
}

type Gateway struct {
	Addr string
}

type Postgres struct {
	URL string
}

type Redis struct {
	Addr     string
	Username string
	Password string
	DB       int
}

type Worker struct {
	PollInterval time.Duration
	BatchSize    int
}

// Load reads configuration from environment variables and validates its values.
func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	redisDB, err := intEnv("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	if redisDB < 0 {
		return Config{}, fmt.Errorf("REDIS_DB must be at least zero")
	}

	pollInterval, err := durationEnv("WORKER_POLL_INTERVAL", time.Second)
	if err != nil {
		return Config{}, err
	}

	batchSize, err := intEnv("WORKER_BATCH_SIZE", 4)
	if err != nil {
		return Config{}, err
	}
	if batchSize < 1 || batchSize > 100 {
		return Config{}, fmt.Errorf("WORKER_BATCH_SIZE must be between 1 and 100")
	}

	return Config{
		Environment: stringEnv("APP_ENV", "development"),
		Gateway:     Gateway{Addr: stringEnv("GATEWAY_ADDR", ":8080")},
		Postgres:    Postgres{URL: databaseURL},
		Redis: Redis{
			Addr:     stringEnv("REDIS_ADDR", "localhost:6379"),
			Username: os.Getenv("REDIS_USERNAME"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       redisDB,
		},
		Worker: Worker{PollInterval: pollInterval, BatchSize: batchSize},
	}, nil
}

func stringEnv(name, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}

func intEnv(name string, defaultValue int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

func durationEnv(name string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}
