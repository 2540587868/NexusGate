package middleware

import (
	"fmt"
	"sync"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type TenantConfig struct {
	ID              string  `yaml:"id"`
	RateLimitRPS    float64 `yaml:"rate_limit_rps"`
	RateLimitBurst  int     `yaml:"rate_limit_burst"`
	MaxBodyBytes    int64   `yaml:"max_body_bytes"`
	AllowedPaths    []string `yaml:"allowed_paths"`
	BlockedPaths    []string `yaml:"blocked_paths"`
}

type TenantIsolationConfig struct {
	Tenants       []TenantConfig `yaml:"tenants"`
	HeaderName    string         `yaml:"header_name"`
	DefaultTenant string         `yaml:"default_tenant"`
}

type TenantLimiter struct {
	mu      sync.Mutex
	tenants map[string]*TenantConfig
	buckets map[string]*tenantBucket
}

type tenantBucket struct {
	tokens   float64
	lastTime int64
	rps      float64
	burst    int
}

func TenantIsolation(cfg TenantIsolationConfig) gateway.Middleware {
	limiter := &TenantLimiter{
		tenants: make(map[string]*TenantConfig),
		buckets: make(map[string]*tenantBucket),
	}

	for i := range cfg.Tenants {
		t := &cfg.Tenants[i]
		limiter.tenants[t.ID] = t
		limiter.buckets[t.ID] = &tenantBucket{
			tokens:   float64(t.RateLimitBurst),
			lastTime: time.Now().UnixNano(),
			rps:      t.RateLimitRPS,
			burst:    t.RateLimitBurst,
		}
	}

	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = "X-Tenant-ID"
	}

	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			tenantID := req.Headers.Get(headerName)
			if tenantID == "" {
				tenantID = cfg.DefaultTenant
			}

			if tenantID == "" {
				return next(req)
			}

			tenant, ok := limiter.tenants[tenantID]
			if !ok {
				if cfg.DefaultTenant == "" {
					return next(req)
				}
				tenant, ok = limiter.tenants[cfg.DefaultTenant]
				if !ok {
					return next(req)
				}
			}

			if !limiter.allow(tenantID) {
				return nil, gateway.NewGatewayError(gateway.ErrRateLimited,
					fmt.Sprintf("tenant %s rate limit exceeded", tenantID), "")
			}

			if len(tenant.BlockedPaths) > 0 {
				for _, blocked := range tenant.BlockedPaths {
					if req.Path == blocked || hasPrefix(req.Path, blocked) {
						return nil, gateway.NewGatewayError(gateway.ErrForbidden,
							fmt.Sprintf("tenant %s access denied to %s", tenantID, req.Path), "")
					}
				}
			}

			if len(tenant.AllowedPaths) > 0 {
				allowed := false
				for _, ap := range tenant.AllowedPaths {
					if req.Path == ap || hasPrefix(req.Path, ap) {
						allowed = true
						break
					}
				}
				if !allowed {
					return nil, gateway.NewGatewayError(gateway.ErrForbidden,
						fmt.Sprintf("tenant %s access denied to %s", tenantID, req.Path), "")
				}
			}

			return next(req)
		}
	}
}

func (tl *TenantLimiter) allow(tenantID string) bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	b, ok := tl.buckets[tenantID]
	if !ok || b.rps <= 0 {
		return true
	}

	now := time.Now().UnixNano()
	elapsed := now - b.lastTime
	b.tokens += float64(elapsed) / 1e9 * b.rps
	if b.tokens > float64(b.burst) {
		b.tokens = float64(b.burst)
	}
	b.lastTime = now

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}

func hasPrefix(s, prefix string) bool {
	return len(s) > len(prefix) && s[:len(prefix)] == prefix
}
