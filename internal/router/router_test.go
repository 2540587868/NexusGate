package router

import (
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestRouterPathPrefixMatch(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID: "users",
		Match: MatchRule{PathPrefix: "/api/v1/users"},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyWeightedRR,
	})

	req := &gateway.Request{Method: "GET", Path: "/api/v1/users/123", Headers: http.Header{}}
	route, backend, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if route.ID != "users" {
		t.Errorf("expected route 'users', got %q", route.ID)
	}
	if backend.Address != "10.0.0.1:8080" {
		t.Errorf("expected backend '10.0.0.1:8080', got %q", backend.Address)
	}
}

func TestRouterPathExactMatch(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID: "health",
		Match: MatchRule{PathExact: "/health"},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyWeightedRR,
	})

	req := &gateway.Request{Method: "GET", Path: "/health", Headers: http.Header{}}
	route, _, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if route.ID != "health" {
		t.Errorf("expected route 'health', got %q", route.ID)
	}

	req2 := &gateway.Request{Method: "GET", Path: "/health/check", Headers: http.Header{}}
	_, _, err = r.Route(req2)
	if err == nil {
		t.Error("expected error for non-exact match, got nil")
	}
}

func TestRouterMethodMatch(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID: "create-user",
		Match: MatchRule{
			PathPrefix: "/api/v1/users",
			Methods:    []string{"POST"},
		},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyWeightedRR,
	})

	postReq := &gateway.Request{Method: "POST", Path: "/api/v1/users", Headers: http.Header{}}
	_, _, err := r.Route(postReq)
	if err != nil {
		t.Fatalf("Route() POST error = %v", err)
	}

	getReq := &gateway.Request{Method: "GET", Path: "/api/v1/users", Headers: http.Header{}}
	_, _, err = r.Route(getReq)
	if err == nil {
		t.Error("expected error for GET on POST-only route, got nil")
	}
}

func TestRouterNoMatch(t *testing.T) {
	r := NewRouter()
	req := &gateway.Request{Method: "GET", Path: "/unknown", Headers: http.Header{}}
	_, _, err := r.Route(req)
	if err == nil {
		t.Error("expected error for unmatched route, got nil")
	}
}

func TestRouterNoHealthyBackends(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID: "unhealthy",
		Match: MatchRule{PathPrefix: "/api"},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: false},
		},
		Strategy: StrategyWeightedRR,
	})

	req := &gateway.Request{Method: "GET", Path: "/api/test", Headers: http.Header{}}
	_, _, err := r.Route(req)
	if err == nil {
		t.Error("expected error for no healthy backends, got nil")
	}
}

func TestRouterRemoveRoute(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID: "removeme",
		Match: MatchRule{PathPrefix: "/api"},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyWeightedRR,
	})

	if len(r.Routes()) != 1 {
		t.Fatalf("expected 1 route, got %d", len(r.Routes()))
	}

	r.RemoveRoute("removeme")
	if len(r.Routes()) != 0 {
		t.Errorf("expected 0 routes after removal, got %d", len(r.Routes()))
	}
}

func TestRouterUpdateBackends(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID: "updateme",
		Match: MatchRule{PathPrefix: "/api"},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyWeightedRR,
	})

	newBackends := []*Backend{
		{Address: "10.0.0.2:8080", Weight: 2, Healthy: true},
		{Address: "10.0.0.3:8080", Weight: 1, Healthy: true},
	}
	r.UpdateBackends("updateme", newBackends)

	req := &gateway.Request{Method: "GET", Path: "/api/test", Headers: http.Header{}}
	_, backend, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	if backend.Address == "10.0.0.1:8080" {
		t.Error("backend should have been updated, got old address")
	}
}

func TestRouterLeastConnRelease(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID:       "lc-route",
		Match:    MatchRule{PathPrefix: "/api"},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyLeastConn,
	})

	req := &gateway.Request{Method: "GET", Path: "/api/test", Headers: http.Header{}}
	_, backend, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	lc := r.selectors[StrategyLeastConn].(*LeastConn)
	count := lc.GetConnectionCount(backend.Address)
	if count != 1 {
		t.Errorf("expected connection count 1 after Select, got %d", count)
	}

	r.Release(StrategyLeastConn, backend.Address)
	count = lc.GetConnectionCount(backend.Address)
	if count != 0 {
		t.Errorf("expected connection count 0 after Release, got %d", count)
	}
}

func TestRouterLeastConnReleaseBalance(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID:       "lc-route",
		Match:    MatchRule{PathPrefix: "/api"},
		Backends: []*Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
			{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyLeastConn,
	})

	lc := r.selectors[StrategyLeastConn].(*LeastConn)

	for i := 0; i < 100; i++ {
		req := &gateway.Request{Method: "GET", Path: "/api/test", Headers: http.Header{}}
		_, backend, err := r.Route(req)
		if err != nil {
			t.Fatalf("Route() error = %v", err)
		}
		r.Release(StrategyLeastConn, backend.Address)
	}

	count1 := lc.GetConnectionCount("10.0.0.1:8080")
	count2 := lc.GetConnectionCount("10.0.0.2:8080")
	if count1 != 0 || count2 != 0 {
		t.Errorf("expected all connection counts to be 0 after balanced Select/Release, got %d and %d", count1, count2)
	}
}

func TestRouterHeaderMatch(t *testing.T) {
	r := NewRouter()
	r.AddRoute(&Route{
		ID: "header-route",
		Match: MatchRule{
			PathPrefix: "/api",
			Headers:    map[string]string{"X-Version": "v2"},
		},
		Backends: []*Backend{
			{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		},
		Strategy: StrategyWeightedRR,
	})

	req := &gateway.Request{Method: "GET", Path: "/api/test", Headers: http.Header{}}
	req.Headers.Set("X-Version", "v2")
	route, _, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route() with matching header should succeed: %v", err)
	}
	if route.ID != "header-route" {
		t.Errorf("expected route 'header-route', got %q", route.ID)
	}

	reqNoHeader := &gateway.Request{Method: "GET", Path: "/api/test", Headers: http.Header{}}
	_, _, err = r.Route(reqNoHeader)
	if err == nil {
		t.Error("expected error for missing required header, got nil")
	}

	reqWrongHeader := &gateway.Request{Method: "GET", Path: "/api/test", Headers: http.Header{}}
	reqWrongHeader.Headers.Set("X-Version", "v1")
	_, _, err = r.Route(reqWrongHeader)
	if err == nil {
		t.Error("expected error for wrong header value, got nil")
	}
}
