package router

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestRadixTreeInsertAndLookup(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert(&Route{ID: "users", Match: MatchRule{PathPrefix: "/api/v1/users"}, Strategy: StrategyWeightedRR})
	tree.Insert(&Route{ID: "orders", Match: MatchRule{PathPrefix: "/api/v1/orders"}, Strategy: StrategyWeightedRR})
	tree.Insert(&Route{ID: "api", Match: MatchRule{PathPrefix: "/api"}, Strategy: StrategyWeightedRR})

	results := tree.Lookup("/api/v1/users/123")
	if len(results) == 0 {
		t.Fatal("expected at least one match for /api/v1/users/123")
	}

	found := false
	for _, r := range results {
		if r.ID == "users" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'users' route in results")
	}

	found = false
	for _, r := range results {
		if r.ID == "api" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'api' route in results (prefix match)")
	}
}

func TestRadixTreeExactMatch(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert(&Route{ID: "health", Match: MatchRule{PathExact: "/health"}, Strategy: StrategyWeightedRR})
	tree.Insert(&Route{ID: "api", Match: MatchRule{PathPrefix: "/api"}, Strategy: StrategyWeightedRR})

	results := tree.Lookup("/health")
	if len(results) == 0 {
		t.Fatal("expected match for /health")
	}
	if results[0].ID != "health" {
		t.Errorf("expected 'health' route, got %q", results[0].ID)
	}

	results = tree.Lookup("/healthz")
	if len(results) != 0 {
		t.Error("expected no match for /healthz with exact /health")
	}
}

func TestRadixTreeLongestPrefix(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert(&Route{ID: "api", Match: MatchRule{PathPrefix: "/api"}, Strategy: StrategyWeightedRR})
	tree.Insert(&Route{ID: "users", Match: MatchRule{PathPrefix: "/api/v1/users"}, Strategy: StrategyWeightedRR})
	tree.Insert(&Route{ID: "orders", Match: MatchRule{PathPrefix: "/api/v1/orders"}, Strategy: StrategyWeightedRR})

	route := tree.LongestPrefixMatch("/api/v1/users/123")
	if route == nil {
		t.Fatal("expected longest prefix match")
	}
	if route.ID != "users" {
		t.Errorf("expected 'users' as longest prefix, got %q", route.ID)
	}
}

func TestRadixTreeAllRoutes(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert(&Route{ID: "a", Match: MatchRule{PathPrefix: "/a"}, Strategy: StrategyWeightedRR})
	tree.Insert(&Route{ID: "b", Match: MatchRule{PathPrefix: "/b"}, Strategy: StrategyWeightedRR})
	tree.Insert(&Route{ID: "c", Match: MatchRule{PathPrefix: "/c"}, Strategy: StrategyWeightedRR})

	all := tree.AllRoutes()
	if len(all) != 3 {
		t.Errorf("expected 3 routes, got %d", len(all))
	}
}

func TestRadixTreeEmptyPath(t *testing.T) {
	tree := NewRadixTree()
	tree.Insert(&Route{ID: "root", Match: MatchRule{PathPrefix: "/"}, Strategy: StrategyWeightedRR})

	results := tree.Lookup("/")
	if len(results) == 0 {
		t.Fatal("expected match for /")
	}
}

func BenchmarkRadixTreeLookup(b *testing.B) {
	tree := NewRadixTree()
	paths := []string{
		"/api/v1/users", "/api/v1/orders", "/api/v1/products",
		"/api/v2/users", "/api/v2/orders", "/api/v2/products",
		"/api/v1/auth/login", "/api/v1/auth/register", "/api/v1/auth/refresh",
		"/api/v1/payments", "/api/v1/notifications", "/api/v1/search",
		"/health", "/metrics", "/ready", "/version",
		"/internal/debug/pprof", "/internal/debug/vars",
	}
	for i, p := range paths {
		tree.Insert(&Route{ID: fmt.Sprintf("route-%d", i), Match: MatchRule{PathPrefix: p}, Strategy: StrategyWeightedRR})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Lookup("/api/v1/users/12345/profile")
	}
}

func BenchmarkRouterLinearMatch(b *testing.B) {
	r := NewRouter()
	paths := []string{
		"/api/v1/users", "/api/v1/orders", "/api/v1/products",
		"/api/v2/users", "/api/v2/orders", "/api/v2/products",
		"/api/v1/auth/login", "/api/v1/auth/register", "/api/v1/auth/refresh",
		"/api/v1/payments", "/api/v1/notifications", "/api/v1/search",
		"/health", "/metrics", "/ready", "/version",
		"/internal/debug/pprof", "/internal/debug/vars",
	}
	for i, p := range paths {
		r.AddRoute(&Route{
			ID:       fmt.Sprintf("route-%d", i),
			Match:    MatchRule{PathPrefix: p},
			Backends: []*Backend{{Address: "127.0.0.1:8080", Weight: 1, Healthy: true}},
			Strategy: StrategyWeightedRR,
		})
	}

	req := &gateway.Request{Method: "GET", Path: "/api/v1/users/12345/profile", Headers: http.Header{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Route(req)
	}
}

func BenchmarkRouterRadixMatch(b *testing.B) {
	r := NewRouter()
	paths := []string{
		"/api/v1/users", "/api/v1/orders", "/api/v1/products",
		"/api/v2/users", "/api/v2/orders", "/api/v2/products",
		"/api/v1/auth/login", "/api/v1/auth/register", "/api/v1/auth/refresh",
		"/api/v1/payments", "/api/v1/notifications", "/api/v1/search",
		"/health", "/metrics", "/ready", "/version",
		"/internal/debug/pprof", "/internal/debug/vars",
	}
	for i, p := range paths {
		r.AddRoute(&Route{
			ID:       fmt.Sprintf("route-%d", i),
			Match:    MatchRule{PathPrefix: p},
			Backends: []*Backend{{Address: "127.0.0.1:8080", Weight: 1, Healthy: true}},
			Strategy: StrategyWeightedRR,
		})
	}

	req := &gateway.Request{Method: "GET", Path: "/api/v1/users/12345/profile", Headers: http.Header{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Route(req)
	}
}
