package middleware

import (
	"net/http"
	"testing"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestRateLimiterAllowsWithinBurst(t *testing.T) {
	rl := NewRateLimiter(100, 5)

	for i := 0; i < 5; i++ {
		if !rl.Allow("tenant-A") {
			t.Errorf("request %d should be allowed within burst=5", i+1)
		}
	}
}

func TestRateLimiterBlocksOverBurst(t *testing.T) {
	rl := NewRateLimiter(1, 3)

	for i := 0; i < 3; i++ {
		rl.Allow("tenant-A")
	}

	if rl.Allow("tenant-A") {
		t.Error("request over burst limit should be blocked")
	}
}

func TestRateLimiterDifferentTenants(t *testing.T) {
	rl := NewRateLimiter(1, 2)

	rl.Allow("tenant-A")
	rl.Allow("tenant-A")

	if rl.Allow("tenant-A") {
		t.Error("tenant-A should be blocked")
	}
	if !rl.Allow("tenant-B") {
		t.Error("tenant-B should still be allowed (independent bucket)")
	}
}

func TestRateLimiterTokenRefill(t *testing.T) {
	rl := NewRateLimiter(1000, 2)

	rl.Allow("tenant-A")
	rl.Allow("tenant-A")

	if rl.Allow("tenant-A") {
		t.Error("should be blocked after burst exhausted")
	}

	time.Sleep(2 * time.Millisecond)

	if !rl.Allow("tenant-A") {
		t.Error("should be allowed after token refill")
	}
}

func TestRateLimiterEmptyKey(t *testing.T) {
	rl := NewRateLimiter(10, 5)

	if !rl.Allow("") {
		t.Error("empty key should be allowed")
	}
}

func TestRateLimiterMaxBuckets(t *testing.T) {
	rl := NewRateLimiter(10, 1)
	rl.maxBuckets = 3

	rl.Allow("a")
	rl.Allow("b")
	rl.Allow("c")
	rl.Allow("d")

	if len(rl.buckets) > rl.maxBuckets+1 {
		t.Errorf("buckets should be near maxBuckets, got %d", len(rl.buckets))
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	rl := NewRateLimiter(10, 1)
	rl.cleanupInterval = 1 * time.Millisecond

	rl.Allow("old-tenant")

	rl.mu.Lock()
	if bucket, ok := rl.buckets["old-tenant"]; ok {
		bucket.lastTime = time.Now().Add(-11 * time.Minute)
	}
	rl.lastCleanup = time.Now().Add(-11 * time.Minute)
	rl.mu.Unlock()

	rl.Allow("new-tenant")

	rl.mu.Lock()
	_, exists := rl.buckets["old-tenant"]
	rl.mu.Unlock()
	if exists {
		t.Error("old bucket should have been cleaned up")
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	rl := NewRateLimiter(1, 2)
	mw := RateLimit(rl)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{TenantID: "test", Headers: http.Header{}}

	resp, err := handler(req)
	if err != nil {
		t.Fatalf("first request should succeed, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCircuitBreakerClosedState(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 100*1e6)

	if cb.State() != StateClosed {
		t.Error("circuit breaker should start in closed state")
	}

	for i := 0; i < 5; i++ {
		if !cb.Allow() {
			t.Errorf("closed state should allow request %d", i+1)
		}
		cb.RecordSuccess()
	}

	if cb.State() != StateClosed {
		t.Error("circuit breaker should remain closed after successes")
	}
}

func TestCircuitBreakerOpensOnFailures(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 1e9)

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateOpen {
		t.Error("circuit breaker should be open after 3 failures")
	}

	if cb.Allow() {
		t.Error("open circuit breaker should block requests")
	}
}

func TestCircuitBreakerHalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 50*1e6)

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateOpen {
		t.Fatal("should be open")
	}

	time.Sleep(60 * time.Millisecond)

	if !cb.Allow() {
		t.Error("should allow after timeout (half-open)")
	}

	if cb.State() != StateHalfOpen {
		t.Error("should be half-open after timeout")
	}
}

func TestCircuitBreakerClosesAfterSuccessInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 50*1e6)

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	time.Sleep(60 * time.Millisecond)
	cb.Allow()

	if cb.State() != StateHalfOpen {
		t.Fatal("should be half-open")
	}

	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Errorf("should be closed after %d successes in half-open, got state=%d", 2, cb.State())
	}
}

func TestCircuitBreakerReopensOnHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 50*1e6)

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	time.Sleep(60 * time.Millisecond)
	cb.Allow()

	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Error("should reopen after failure in half-open")
	}
}

func TestCircuitBreakerHalfOpenLimitsConcurrent(t *testing.T) {
	cb := NewCircuitBreaker(1, 3, 50*1e6)

	cb.RecordFailure()

	time.Sleep(60 * time.Millisecond)

	allowed := 0
	for i := 0; i < 5; i++ {
		if cb.Allow() {
			allowed++
		}
	}

	if allowed != 3 {
		t.Errorf("half-open should limit to successThreshold concurrent requests, got %d allowed", allowed)
	}
}

func TestCircuitBreakerRecordSuccessInClosedResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 1e9)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Error("success in closed state should reset failure count")
	}

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateClosed {
		t.Error("failure count should have been reset, need 3 more to open")
	}
}

func TestCircuitBreakerMiddleware(t *testing.T) {
	cb := NewCircuitBreaker(2, 1, 1e9)
	mw := CircuitBreakerMiddleware(cb)

	failHandler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return nil, gateway.NewGatewayError(gateway.ErrBackendDown, "down", "")
	})

	req := &gateway.Request{Headers: http.Header{}}

	_, _ = failHandler(req)
	_, _ = failHandler(req)

	_, err := failHandler(req)
	if err == nil {
		t.Error("expected error when circuit is open")
	}

	gwErr, ok := err.(*gateway.GatewayError)
	if !ok {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if gwErr.Code != gateway.ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %d", gwErr.Code)
	}
}

func TestChainOrder(t *testing.T) {
	var order []string

	mw1 := func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			order = append(order, "mw1-before")
			resp, err := next(req)
			order = append(order, "mw1-after")
			return resp, err
		}
	}

	mw2 := func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			order = append(order, "mw2-before")
			resp, err := next(req)
			order = append(order, "mw2-after")
			return resp, err
		}
	}

	chain := NewChain(mw1, mw2)
	handler := chain.Then(func(req *gateway.Request) (*gateway.Response, error) {
		order = append(order, "handler")
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	handler(req)

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("call %d: expected %q, got %q", i, v, order[i])
		}
	}
}

func TestChainEmpty(t *testing.T) {
	chain := NewChain()
	handler := chain.Then(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestChainUse(t *testing.T) {
	var called bool

	chain := NewChain()
	chain = chain.Use(func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			called = true
			return next(req)
		}
	})

	handler := chain.Then(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	handler(req)

	if !called {
		t.Error("middleware added via Use should be called")
	}
}

func TestCORSWithMatchingOrigin(t *testing.T) {
	opts := CORSOptions{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET", "POST"},
	}
	mw := CORS(opts)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Headers: http.Header{"Origin": {"https://example.com"}},
	}
	resp, _ := handler(req)

	if resp.Headers.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("expected Allow-Origin to be https://example.com, got %q", resp.Headers.Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSWithNonMatchingOrigin(t *testing.T) {
	opts := CORSOptions{
		AllowOrigins: []string{"https://example.com"},
	}
	mw := CORS(opts)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Headers: http.Header{"Origin": {"https://evil.com"}},
	}
	resp, _ := handler(req)

	if resp.Headers.Get("Access-Control-Allow-Origin") != "" {
		t.Error("non-matching origin should not get CORS headers")
	}
}

func TestCORSWithWildcardOrigin(t *testing.T) {
	opts := CORSOptions{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET"},
	}
	mw := CORS(opts)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Headers: http.Header{"Origin": {"https://any.com"}},
	}
	resp, _ := handler(req)

	if resp.Headers.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("wildcard should set Allow-Origin to *, got %q", resp.Headers.Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSWithCredentialsAndWildcard(t *testing.T) {
	opts := CORSOptions{
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
		AllowMethods:     []string{"GET"},
	}
	mw := CORS(opts)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Headers: http.Header{"Origin": {"https://any.com"}},
	}
	resp, _ := handler(req)

	origin := resp.Headers.Get("Access-Control-Allow-Origin")
	if origin != "https://any.com" {
		t.Errorf("credentials+wildcard should echo origin, got %q", origin)
	}
}

func TestCORSEmptyOriginsDeniesAll(t *testing.T) {
	opts := CORSOptions{
		AllowOrigins: []string{},
		AllowMethods: []string{"GET"},
	}
	mw := CORS(opts)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Headers: http.Header{"Origin": {"https://example.com"}},
	}
	resp, _ := handler(req)

	if resp.Headers.Get("Access-Control-Allow-Origin") != "" {
		t.Error("empty AllowOrigins should deny all origins")
	}
}

func TestCORSPreflight(t *testing.T) {
	opts := CORSOptions{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Content-Type"},
		MaxAge:       3600,
	}
	mw := CORS(opts)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		t.Error("preflight should not call next handler")
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Method:  "OPTIONS",
		Headers: http.Header{"Origin": {"https://example.com"}},
	}
	resp, err := handler(req)

	if err != nil {
		t.Fatalf("preflight should not return error, got %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight should return 204, got %d", resp.StatusCode)
	}
	if resp.Headers.Get("Access-Control-Allow-Methods") != "GET, POST" {
		t.Errorf("unexpected Allow-Methods: %q", resp.Headers.Get("Access-Control-Allow-Methods"))
	}
	if resp.Headers.Get("Access-Control-Max-Age") != "3600" {
		t.Errorf("unexpected Max-Age: %q", resp.Headers.Get("Access-Control-Max-Age"))
	}
}

func TestCORSNoOriginHeader(t *testing.T) {
	opts := CORSOptions{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET"},
	}
	mw := CORS(opts)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Headers: http.Header{},
	}
	resp, _ := handler(req)

	if resp.Headers.Get("Access-Control-Allow-Origin") != "" {
		t.Error("no Origin header should not set CORS headers")
	}
}
