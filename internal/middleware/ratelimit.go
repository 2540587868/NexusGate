package middleware

import (
	"sync"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type RateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*tokenBucket
	rate        float64
	burst       int
	maxBuckets  int
	lastCleanup time.Time
	cleanupInterval time.Duration
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{
		buckets:          make(map[string]*tokenBucket),
		rate:             rate,
		burst:            burst,
		maxBuckets:       100000,
		lastCleanup:      time.Now(),
		cleanupInterval:  5 * time.Minute,
	}
}

func RateLimit(limiter *RateLimiter) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			key := req.TenantID
			if !limiter.Allow(key) {
				RecordRateLimitRejected()
				return nil, gateway.NewGatewayError(gateway.ErrRateLimited,
					"rate limit exceeded", "too many requests")
			}
			return next(req)
		}
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	if now.Sub(rl.lastCleanup) > rl.cleanupInterval {
		rl.cleanup(now)
		rl.lastCleanup = now
	}

	bucket, ok := rl.buckets[key]
	if !ok {
		if len(rl.buckets) >= rl.maxBuckets {
			rl.cleanup(now)
		}
		bucket = &tokenBucket{
			tokens:   float64(rl.burst),
			lastTime: now,
		}
		rl.buckets[key] = bucket
	}

	elapsed := now.Sub(bucket.lastTime).Seconds()
	bucket.tokens += elapsed * rl.rate
	if bucket.tokens > float64(rl.burst) {
		bucket.tokens = float64(rl.burst)
	}
	bucket.lastTime = now

	if bucket.tokens < 1 {
		return false
	}

	bucket.tokens--
	return true
}

func (rl *RateLimiter) cleanup(now time.Time) {
	expiryThreshold := 10 * time.Minute
	for key, bucket := range rl.buckets {
		if now.Sub(bucket.lastTime) > expiryThreshold {
			delete(rl.buckets, key)
		}
	}
}
