package opsgateway

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const rollbackIdempotencyTTL = 24 * time.Hour

type redisClient interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

func rollbackRedisKey(req RollbackRequest) string {
	return "idemp:rollback:" + req.IncidentID + ":" + req.TargetService
}
