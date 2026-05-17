package middleware

import (
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestTenantIsolationNoHeader(t *testing.T) {
	cfg := TenantIsolationConfig{
		Tenants: []TenantConfig{
			{ID: "tenant1", RateLimitRPS: 100, RateLimitBurst: 10},
		},
	}

	mw := TenantIsolation(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Path:    "/api/data",
		Headers: http.Header{},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("no header and no default should pass through, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTenantIsolationDefaultTenant(t *testing.T) {
	cfg := TenantIsolationConfig{
		DefaultTenant: "tenant1",
		Tenants: []TenantConfig{
			{ID: "tenant1", RateLimitRPS: 100, RateLimitBurst: 10},
		},
	}

	mw := TenantIsolation(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Path:    "/api/data",
		Headers: http.Header{},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("default tenant should be used, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTenantIsolationRateLimitExceeded(t *testing.T) {
	cfg := TenantIsolationConfig{
		Tenants: []TenantConfig{
			{ID: "limited", RateLimitRPS: 1, RateLimitBurst: 1},
		},
	}

	mw := TenantIsolation(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Path:    "/api/data",
		Headers: http.Header{},
	}
	req.Headers.Set("X-Tenant-ID", "limited")

	resp, err := handler(req)
	if err != nil {
		t.Fatalf("first request should succeed, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	_, err = handler(req)
	if err == nil {
		t.Fatal("second request should be rate limited")
	}
	gwErr, ok := err.(*gateway.GatewayError)
	if !ok {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if gwErr.Code != gateway.ErrRateLimited {
		t.Errorf("expected ErrRateLimited, got %d", gwErr.Code)
	}
}

func TestTenantIsolationBlockedPath(t *testing.T) {
	cfg := TenantIsolationConfig{
		Tenants: []TenantConfig{
			{
				ID:             "blocked-tenant",
				RateLimitRPS:   100,
				RateLimitBurst: 10,
				BlockedPaths:   []string{"/admin"},
			},
		},
	}

	mw := TenantIsolation(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Path:    "/admin/settings",
		Headers: http.Header{},
	}
	req.Headers.Set("X-Tenant-ID", "blocked-tenant")

	_, err := handler(req)
	if err == nil {
		t.Fatal("blocked path should return error")
	}
	gwErr, ok := err.(*gateway.GatewayError)
	if !ok {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if gwErr.Code != gateway.ErrForbidden {
		t.Errorf("expected ErrForbidden, got %d", gwErr.Code)
	}
}

func TestTenantIsolationAllowedPaths(t *testing.T) {
	cfg := TenantIsolationConfig{
		Tenants: []TenantConfig{
			{
				ID:             "restricted",
				RateLimitRPS:   100,
				RateLimitBurst: 10,
				AllowedPaths:   []string{"/api/public"},
			},
		},
	}

	mw := TenantIsolation(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Path:    "/api/public/data",
		Headers: http.Header{},
	}
	req.Headers.Set("X-Tenant-ID", "restricted")

	resp, err := handler(req)
	if err != nil {
		t.Fatalf("allowed path should pass, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTenantIsolationAllowedPathsDenied(t *testing.T) {
	cfg := TenantIsolationConfig{
		Tenants: []TenantConfig{
			{
				ID:             "restricted",
				RateLimitRPS:   100,
				RateLimitBurst: 10,
				AllowedPaths:   []string{"/api/public"},
			},
		},
	}

	mw := TenantIsolation(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Path:    "/api/private/data",
		Headers: http.Header{},
	}
	req.Headers.Set("X-Tenant-ID", "restricted")

	_, err := handler(req)
	if err == nil {
		t.Fatal("non-allowed path should return error")
	}
	gwErr, ok := err.(*gateway.GatewayError)
	if !ok {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if gwErr.Code != gateway.ErrForbidden {
		t.Errorf("expected ErrForbidden, got %d", gwErr.Code)
	}
}

func TestTenantIsolationUnknownTenant(t *testing.T) {
	cfg := TenantIsolationConfig{
		Tenants: []TenantConfig{
			{ID: "tenant1", RateLimitRPS: 100, RateLimitBurst: 10},
		},
	}

	mw := TenantIsolation(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Path:    "/api/data",
		Headers: http.Header{},
	}
	req.Headers.Set("X-Tenant-ID", "unknown-tenant")

	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unknown tenant with no default should pass through, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTenantHasPrefix(t *testing.T) {
	tests := []struct {
		s      string
		prefix string
		want   bool
	}{
		{"/admin/settings", "/admin", true},
		{"/admin", "/admin", false},
		{"/admins", "/admin", true},
		{"/api/data", "/admin", false},
		{"", "/admin", false},
		{"/a", "/admin", false},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.prefix, func(t *testing.T) {
			got := hasPrefix(tt.s, tt.prefix)
			if got != tt.want {
				t.Errorf("hasPrefix(%q, %q) = %v, want %v", tt.s, tt.prefix, got, tt.want)
			}
		})
	}
}
