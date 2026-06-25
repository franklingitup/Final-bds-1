package database

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// NewRedisClient creates and verifies a Redis client from configuration.
func NewRedisClient(ctx context.Context, cfg config.Config) (*redis.Client, error) {
	if cfg.Redis.URL == "" {
		return nil, fmt.Errorf("database: REDIS_URL is required")
	}
	opts, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return nil, fmt.Errorf("database: parse redis url: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("database: redis ping: %w", err)
	}
	return client, nil
}
