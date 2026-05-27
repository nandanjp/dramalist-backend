package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"dramalist/review-service/config"
)

func Connect(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr(),
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return rdb, nil
}
