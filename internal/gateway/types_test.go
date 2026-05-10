package gateway

import (
	"net/http"
	"testing"
)

func TestRequestRouteKey(t *testing.T) {
	req := &Request{Method: "GET", Path: "/api/v1/users"}
	if req.RouteKey() != "GET /api/v1/users" {
		t.Errorf("RouteKey() = %q, want %q", req.RouteKey(), "GET /api/v1/users")
	}

	if req.RouteKey() != "GET /api/v1/users" {
		t.Errorf("RouteKey() should be cached, second call returned different value")
	}
}

func TestRequestShardKey(t *testing.T) {
	req1 := &Request{TenantID: "tenant-A"}
	req2 := &Request{TenantID: "tenant-A"}
	req3 := &Request{TenantID: "tenant-B"}

	key1 := req1.ShardKey()
	key2 := req2.ShardKey()
	key3 := req3.ShardKey()

	if key1 != key2 {
		t.Errorf("same TenantID should produce same shard key, got %d and %d", key1, key2)
	}

	if key1 == key3 {
		t.Logf("note: different tenants may hash to same shard (both=%d), this is ok", key1)
	}

	if key1 == 0 {
		t.Errorf("shard key should not be zero for non-empty tenant")
	}
}

func TestGatewayErrorHTTPStatus(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		expected int
	}{
		{ErrBadRequest, http.StatusBadRequest},
		{ErrUnauthorized, http.StatusUnauthorized},
		{ErrForbidden, http.StatusForbidden},
		{ErrRateLimited, http.StatusTooManyRequests},
		{ErrCircuitOpen, http.StatusServiceUnavailable},
		{ErrNoRoute, http.StatusNotFound},
		{ErrBackendDown, http.StatusBadGateway},
		{ErrBackendTimeout, http.StatusGatewayTimeout},
		{ErrInternal, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		gwErr := NewGatewayError(tt.code, "test", "detail")
		if gwErr.HTTPStatus() != tt.expected {
			t.Errorf("ErrorCode %d: HTTPStatus() = %d, want %d", tt.code, gwErr.HTTPStatus(), tt.expected)
		}
	}
}

func TestGatewayErrorError(t *testing.T) {
	gwErr := NewGatewayError(ErrRateLimited, "rate limited", "too many requests")
	want := "[10004] rate limited: too many requests"
	if gwErr.Error() != want {
		t.Errorf("Error() = %q, want %q", gwErr.Error(), want)
	}
}

func TestDetermineProxyMode(t *testing.T) {
	tests := []struct {
		name     string
		req      *Request
		expected ProxyMode
	}{
		{
			name:     "GET request uses splice",
			req:      &Request{Method: "GET", Headers: http.Header{}},
			expected: ProxyModeSplice,
		},
		{
			name:     "HEAD request uses splice",
			req:      &Request{Method: "HEAD", Headers: http.Header{}},
			expected: ProxyModeSplice,
		},
		{
			name:     "DELETE request uses splice",
			req:      &Request{Method: "DELETE", Headers: http.Header{}},
			expected: ProxyModeSplice,
		},
		{
			name:     "POST with empty body uses splice",
			req:      &Request{Method: "POST", Headers: http.Header{"Content-Length": {""}}},
			expected: ProxyModeSplice,
		},
		{
			name:     "POST with zero content-length uses splice",
			req:      &Request{Method: "POST", Headers: http.Header{"Content-Length": {"0"}}},
			expected: ProxyModeSplice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineProxyMode(tt.req)
			if got != tt.expected {
				t.Errorf("DetermineProxyMode() = %v, want %v", got, tt.expected)
			}
		})
	}
}
