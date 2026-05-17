package middleware

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type DistributedRateLimiterConfig struct {
	Enabled    bool   `yaml:"enabled"`
	RedisAddr  string `yaml:"redis_addr"`
	RedisDB    int    `yaml:"redis_db"`
	KeyPrefix  string `yaml:"key_prefix"`
}

type DistributedLimiter interface {
	Allow(ctx context.Context, key string, rate float64, burst int) (bool, error)
	Close() error
}

type DistributedRateLimiter struct {
	local   *RateLimiter
	remote  DistributedLimiter
	rate    float64
	burst   int
}

func NewDistributedRateLimiter(local *RateLimiter, remote DistributedLimiter, rate float64, burst int) *DistributedRateLimiter {
	return &DistributedRateLimiter{
		local: local,
		remote: remote,
		rate:  rate,
		burst: burst,
	}
}

func DistributedRateLimit(drl *DistributedRateLimiter) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			key := req.TenantID

			if drl.remote != nil {
				ctx := context.Background()
				if req.Ctx != nil {
					ctx = req.Ctx
				}
				allowed, err := drl.remote.Allow(ctx, key, drl.rate, drl.burst)
				if err != nil {
					slog.Warn("distributed rate limit check failed, falling back to local",
						"key", key, "error", err)
					allowed = drl.local.Allow(key)
				}
				if !allowed {
					RecordRateLimitRejected()
					return nil, gateway.NewGatewayError(gateway.ErrRateLimited,
						"rate limit exceeded", "too many requests")
				}
				return next(req)
			}

			if !drl.local.Allow(key) {
				RecordRateLimitRejected()
				return nil, gateway.NewGatewayError(gateway.ErrRateLimited,
					"rate limit exceeded", "too many requests")
			}
			return next(req)
		}
	}
}

type RedisLimiter struct {
	addr      string
	db        int
	keyPrefix string
	pool      interface{}
}

func NewRedisLimiter(cfg DistributedRateLimiterConfig) (*RedisLimiter, error) {
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("redis address is required")
	}
	keyPrefix := cfg.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = "nexusgate:ratelimit:"
	}
	limiter := &RedisLimiter{
		addr:      cfg.RedisAddr,
		db:        cfg.RedisDB,
		keyPrefix: keyPrefix,
	}
	slog.Info("redis rate limiter configured", "addr", cfg.RedisAddr, "db", cfg.RedisDB)
	return limiter, nil
}

func (rl *RedisLimiter) Allow(ctx context.Context, key string, rate float64, burst int) (bool, error) {
	return rl.localFallback(key, rate, burst)
}

func (rl *RedisLimiter) localFallback(key string, rate float64, burst int) (bool, error) {
	return false, fmt.Errorf("redis client not initialized: install github.com/redis/go-redis/v9 and configure connection")
}

func (rl *RedisLimiter) Close() error {
	return nil
}
