package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/router"
)

func TestProxyForwardGET(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/test" {
			t.Errorf("backend received path %q, want /api/test", r.URL.Path)
		}
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Error("expected X-Forwarded-For header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	px := NewProxy(10, 5)
	addr := backend.Listener.Addr().String()

	req := &gateway.Request{
		Method:     "GET",
		Path:       "/api/test",
		Host:       "example.com",
		Headers:    http.Header{},
		RemoteAddr: "192.168.1.1:12345",
		Scheme:     "http",
	}

	backendNode := &router.Backend{Address: addr, Weight: 1, Healthy: true}
	resp, err := px.Forward(req, backendNode, nil)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if string(resp.Body) != `{"status":"ok"}` {
		t.Errorf("Body = %q, want {\"status\":\"ok\"}", string(resp.Body))
	}
}

func TestProxyForwardPOST(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("backend received method %q, want POST", r.Method)
		}
		if r.ContentLength != 15 {
			t.Errorf("Content-Length = %d, want 15", r.ContentLength)
		}
		w.WriteHeader(201)
		w.Write([]byte("created"))
	}))
	defer backend.Close()

	px := NewProxy(10, 5)
	addr := backend.Listener.Addr().String()

	req := &gateway.Request{
		Method:     "POST",
		Path:       "/api/items",
		Host:       "example.com",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"name":"test"}`),
		RemoteAddr: "192.168.1.1:12345",
		Scheme:     "http",
	}

	backendNode := &router.Backend{Address: addr, Weight: 1, Healthy: true}
	resp, err := px.Forward(req, backendNode, nil)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}

	if resp.StatusCode != 201 {
		t.Errorf("StatusCode = %d, want 201", resp.StatusCode)
	}
}

func TestProxyForwardBackendDown(t *testing.T) {
	px := NewProxy(10, 5)

	req := &gateway.Request{
		Method:     "GET",
		Path:       "/api/test",
		Host:       "example.com",
		Headers:    http.Header{},
		RemoteAddr: "192.168.1.1:12345",
		Scheme:     "http",
	}

	backendNode := &router.Backend{Address: "127.0.0.1:1", Weight: 1, Healthy: true}
	_, err := px.Forward(req, backendNode, nil)
	if err == nil {
		t.Error("expected error for unreachable backend, got nil")
	}
}

func TestProxyWithTimeouts(t *testing.T) {
	px := NewProxy(10, 5).WithTimeouts(1*time.Second, 5*time.Second, 5*time.Second)

	if px.httpClient.Timeout != 5*time.Second {
		t.Errorf("httpClient.Timeout = %v, want 5s", px.httpClient.Timeout)
	}
}

func TestRetryPolicyDefault(t *testing.T) {
	policy := DefaultRetryPolicy()

	if policy.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", policy.MaxRetries)
	}
	if !IsRetryableStatus(502, policy) {
		t.Error("502 should be retryable")
	}
	if !IsRetryableStatus(503, policy) {
		t.Error("503 should be retryable")
	}
	if IsRetryableStatus(400, policy) {
		t.Error("400 should not be retryable")
	}
}

func TestExponentialBackoff(t *testing.T) {
	eb := &ExponentialBackoff{
		Base:   100 * time.Millisecond,
		Max:    5 * time.Second,
		Jitter: false,
	}

	d0 := eb.Next(0)
	if d0 != 100*time.Millisecond {
		t.Errorf("Next(0) = %v, want 100ms", d0)
	}

	d1 := eb.Next(1)
	if d1 != 200*time.Millisecond {
		t.Errorf("Next(1) = %v, want 200ms", d1)
	}

	d2 := eb.Next(2)
	if d2 != 400*time.Millisecond {
		t.Errorf("Next(2) = %v, want 400ms", d2)
	}

	dBig := eb.Next(100)
	if dBig != 5*time.Second {
		t.Errorf("Next(100) = %v, want 5s (capped)", dBig)
	}
}

func TestExponentialBackoffWithJitter(t *testing.T) {
	eb := &ExponentialBackoff{
		Base:   100 * time.Millisecond,
		Max:    5 * time.Second,
		Jitter: true,
	}

	for i := 0; i < 100; i++ {
		d := eb.Next(i)
		if d <= 0 {
			t.Errorf("Next(%d) should be positive, got %v", i, d)
		}
		if d > 5*time.Second {
			t.Errorf("Next(%d) should not exceed max, got %v", i, d)
		}
	}
}

func TestFixedBackoff(t *testing.T) {
	fb := &FixedBackoff{Interval: 500 * time.Millisecond}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, 500 * time.Millisecond},
		{5, 500 * time.Millisecond},
		{100, 500 * time.Millisecond},
	}
	for _, tt := range tests {
		got := fb.Next(tt.attempt)
		if got != tt.expected {
			t.Errorf("FixedBackoff.Next(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestIsRetryableStatus(t *testing.T) {
	policy := DefaultRetryPolicy()

	tests := []struct {
		code     int
		expected bool
	}{
		{502, true},
		{503, true},
		{504, true},
		{400, false},
		{401, false},
		{404, false},
		{500, false},
	}
	for _, tt := range tests {
		got := IsRetryableStatus(tt.code, policy)
		if got != tt.expected {
			t.Errorf("IsRetryableStatus(%d) = %v, want %v", tt.code, got, tt.expected)
		}
	}
}

func TestIsRetryableStatusNilPolicy(t *testing.T) {
	if IsRetryableStatus(502, nil) {
		t.Error("nil policy should not retry any status")
	}
}

func TestRetryPolicyCustomStatus(t *testing.T) {
	policy := &RetryPolicy{
		MaxRetries:      1,
		RetryableStatus: []int{500, 502},
		Backoff:         &FixedBackoff{Interval: 10 * time.Millisecond},
	}

	if !IsRetryableStatus(500, policy) {
		t.Error("500 should be retryable with custom policy")
	}
	if IsRetryableStatus(503, policy) {
		t.Error("503 should not be retryable with custom policy")
	}
}

func TestForwardRetrySwitchesBackend(t *testing.T) {
	callCount := 0
	goodBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer goodBackend.Close()

	px := NewProxy(10, 5).WithRetryPolicy(&RetryPolicy{
		MaxRetries:      2,
		RetryableStatus: []int{502, 503, 504},
		Backoff:         &FixedBackoff{Interval: 10 * time.Millisecond},
	})

	req := &gateway.Request{
		Method:     "GET",
		Path:       "/api/test",
		Host:       "example.com",
		Headers:    http.Header{},
		RemoteAddr: "192.168.1.1:12345",
		Scheme:     "http",
	}

	route := &router.Route{
		ID:       "route_0",
		Strategy: router.StrategyWeightedRR,
		Backends: []*router.Backend{
			{Address: "127.0.0.1:1", Weight: 1, Healthy: true},
			{Address: goodBackend.Listener.Addr().String(), Weight: 1, Healthy: true},
		},
	}

	backend := route.Backends[0]
	resp, err := px.Forward(req, backend, route)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if callCount != 1 {
		t.Errorf("good backend called %d times, want 1", callCount)
	}
}
