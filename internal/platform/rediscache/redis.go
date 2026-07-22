// Package rediscache creates Redis clients for the new platform runtime.
package rediscache

import (
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/redis/go-redis/v9"
)

// Open creates a Redis client from the runtime configuration without connecting to Redis.
func Open(config config.Redis) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Username: config.Username,
		Password: config.Password,
		DB:       config.DB,
	})
}
