package middleware

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type mockDistributedLimiter struct {
	allow bool
	err   error
}

func (m *mockDistributedLimiter) Allow(ctx context.Context, key string, rate float64, burst int) (bool, error) {
	return m.allow, m.err
}

func (m *mockDistributedLimiter) Close() error { return nil }

func TestDistributedRateLimitLocalOnly(t *testing.T) {
	local := NewRateLimiter(100, 5)
	drl := NewDistributedRateLimiter(local, nil, 100, 5)
	mw := DistributedRateLimit(drl)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{TenantID: "tenant-A", Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestDistributedRateLimitLocalDenied(t *testing.T) {
	local := NewRateLimiter(1, 1)
	drl := NewDistributedRateLimiter(local, nil, 1, 1)
	mw := DistributedRateLimit(drl)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{TenantID: "tenant-A", Headers: http.Header{}}
	_, _ = handler(req)

	_, err := handler(req)
	if err == nil {
		t.Fatal("expected error when local limit exceeded, got nil")
	}

	gwErr, ok := err.(*gateway.GatewayError)
	if !ok {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if gwErr.Code != gateway.ErrRateLimited {
		t.Errorf("Code = %d, want %d", gwErr.Code, gateway.ErrRateLimited)
	}
}

func TestDistributedRateLimitRemoteAllowed(t *testing.T) {
	local := NewRateLimiter(1, 1)
	remote := &mockDistributedLimiter{allow: true, err: nil}
	drl := NewDistributedRateLimiter(local, remote, 100, 5)
	mw := DistributedRateLimit(drl)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{TenantID: "tenant-A", Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestDistributedRateLimitRemoteError(t *testing.T) {
	local := NewRateLimiter(100, 5)
	remote := &mockDistributedLimiter{allow: false, err: fmt.Errorf("connection refused")}
	drl := NewDistributedRateLimiter(local, remote, 100, 5)
	mw := DistributedRateLimit(drl)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{TenantID: "tenant-A", Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("expected fallback to local (allowed), got error %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestDistributedRateLimitRemoteDenied(t *testing.T) {
	local := NewRateLimiter(100, 5)
	remote := &mockDistributedLimiter{allow: false, err: nil}
	drl := NewDistributedRateLimiter(local, remote, 100, 5)
	mw := DistributedRateLimit(drl)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{TenantID: "tenant-A", Headers: http.Header{}}
	_, err := handler(req)
	if err == nil {
		t.Fatal("expected error when remote denies, got nil")
	}

	gwErr, ok := err.(*gateway.GatewayError)
	if !ok {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if gwErr.Code != gateway.ErrRateLimited {
		t.Errorf("Code = %d, want %d", gwErr.Code, gateway.ErrRateLimited)
	}
}

func TestNewRedisLimiter(t *testing.T) {
	cfg := DistributedRateLimiterConfig{
		RedisAddr: "",
	}
	_, err := NewRedisLimiter(cfg)
	if err == nil {
		t.Error("expected error for empty RedisAddr, got nil")
	}
}

func TestNewRedisLimiterWithAddr(t *testing.T) {
	cfg := DistributedRateLimiterConfig{
		RedisAddr: "localhost:6379",
		RedisDB:   0,
	}
	limiter, err := NewRedisLimiter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limiter.addr != "localhost:6379" {
		t.Errorf("addr = %q, want %q", limiter.addr, "localhost:6379")
	}
	if limiter.keyPrefix != "nexusgate:ratelimit:" {
		t.Errorf("keyPrefix = %q, want %q", limiter.keyPrefix, "nexusgate:ratelimit:")
	}
}

func TestRedisLimiterLocalFallback(t *testing.T) {
	cfg := DistributedRateLimiterConfig{
		RedisAddr: "localhost:6379",
	}
	limiter, _ := NewRedisLimiter(cfg)

	_, err := limiter.Allow(context.Background(), "key", 10, 5)
	if err == nil {
		t.Error("expected error from localFallback, got nil")
	}
}
