package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nexusgate/nexusgate/internal/config"
	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/httparser"
	"github.com/nexusgate/nexusgate/internal/middleware"
	"github.com/nexusgate/nexusgate/internal/proxy"
	"github.com/nexusgate/nexusgate/internal/router"
)

func startBackendServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(listener)
	t.Cleanup(func() { srv.Close() })
	return listener.Addr().String()
}

func startGateway(t *testing.T, rt *router.Router, px *proxy.Proxy) string {
	t.Helper()

	rl := middleware.NewRateLimiter(10000, 20000)
	cb := middleware.NewCircuitBreaker(5, 3, 30*time.Second)

	chain := middleware.NewChain()
	chain = chain.Use(middleware.Trace)
	chain = chain.Use(middleware.AccessLog)
	chain = chain.Use(middleware.RateLimit(rl))
	chain = chain.Use(middleware.CORS(middleware.CORSOptions{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
	}))
	chain = chain.Use(middleware.CircuitBreakerMiddleware(cb))

	handler := chain.Then(buildHandler(rt, px))

	gw := gateway.NewGateway(handler, 1024)
	t.Cleanup(func() { gw.Close() })

	parser := httparser.NewParser()

	gwListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("gateway listen: %v", err)
	}

	go func() {
		for {
			conn, err := gwListener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				req, err := parser.ParseRequest(conn)
				if err != nil {
					if gwErr, ok := err.(*gateway.GatewayError); ok {
						httparser.WriteErrorResponse(conn, gwErr)
					}
					return
				}
				resp, err := gw.DispatchSync(req)
				if err != nil {
					if gwErr, ok := err.(*gateway.GatewayError); ok {
						httparser.WriteErrorResponse(conn, gwErr)
					}
					return
				}
				if err := httparser.WriteResponse(conn, resp); err != nil {
					return
				}
			}(conn)
		}
	}()

	t.Cleanup(func() { gwListener.Close() })
	return gwListener.Addr().String()
}

func TestBuildAuthConfigNone(t *testing.T) {
	cfg := &config.Config{}
	authCfg := buildAuthConfig(cfg)

	if authCfg.Type != middleware.AuthTypeNone {
		t.Errorf("empty config should default to none, got %q", authCfg.Type)
	}
}

func TestBuildAuthConfigAPIKey(t *testing.T) {
	cfg := &config.Config{
		Middleware: config.MiddlewareConfig{
			Auth: config.AuthConfig{
				Type:         config.AuthTypeConfigAPIKey,
				APIKeyHeader: "X-Custom-Key",
				APIKeys: []config.APIKeyConfig{
					{Key: "test-key", TenantID: "tenant1", Active: true, Scopes: []string{"read"}},
				},
			},
		},
	}

	authCfg := buildAuthConfig(cfg)

	if authCfg.Type != middleware.AuthTypeAPIKey {
		t.Errorf("expected apikey type, got %q", authCfg.Type)
	}
	if authCfg.APIKeyHeader != "X-Custom-Key" {
		t.Errorf("expected X-Custom-Key header, got %q", authCfg.APIKeyHeader)
	}
	entry, ok := authCfg.APIKeys["test-key"]
	if !ok {
		t.Fatal("test-key not found in APIKeys map")
	}
	if entry.TenantID != "tenant1" {
		t.Errorf("expected tenant1, got %q", entry.TenantID)
	}
	if !entry.Active {
		t.Error("key should be active")
	}
}

func TestBuildAuthConfigJWT(t *testing.T) {
	cfg := &config.Config{
		Middleware: config.MiddlewareConfig{
			Auth: config.AuthConfig{
				Type:           config.AuthTypeConfigJWT,
				JWTHMACSecret:  "my-secret",
				JWTAllowedAlgs: []string{"HS256"},
				SkipPaths:      []string{"/healthz", "/public/*"},
			},
		},
	}

	authCfg := buildAuthConfig(cfg)

	if authCfg.Type != middleware.AuthTypeJWT {
		t.Errorf("expected jwt type, got %q", authCfg.Type)
	}
	if authCfg.JWTHMACSecret != "my-secret" {
		t.Errorf("expected my-secret, got %q", authCfg.JWTHMACSecret)
	}
	if len(authCfg.SkipPaths) != 2 {
		t.Errorf("expected 2 skip paths, got %d", len(authCfg.SkipPaths))
	}
}

func TestBuildHandlerRouteMiss(t *testing.T) {
	rt := router.NewRouter()
	px := proxy.NewProxy(10, 5)

	handler := buildHandler(rt, px)

	req := &gateway.Request{
		Method:  "GET",
		Path:    "/nonexistent",
		Headers: http.Header{},
	}

	_, err := handler(req)
	if err == nil {
		t.Error("expected error for missing route")
	}
}

func TestBuildHandlerRouteHit(t *testing.T) {
	backendHandler := http.NewServeMux()
	backendHandler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintf(w, "hello from backend")
	})
	backendAddr := startBackendServer(t, backendHandler)

	rt := router.NewRouter()
	rt.AddRoute(&router.Route{
		ID:     "test",
		Match:  router.MatchRule{PathPrefix: "/api"},
		Backends: []*router.Backend{
			{Address: backendAddr, Weight: 1, Healthy: true},
		},
		Strategy: router.StrategyWeightedRR,
	})

	px := proxy.NewProxy(10, 5)
	handler := buildHandler(rt, px)

	req := &gateway.Request{
		Method:  "GET",
		Path:    "/api/test",
		Headers: http.Header{},
		Scheme:  "http",
	}

	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestIntegrationGatewayProxy(t *testing.T) {
	backendHandler := http.NewServeMux()
	backendHandler.HandleFunc("/api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"users": ["alice", "bob"], "method": "%s"}`, r.Method)
	})
	backendHandler.HandleFunc("/api/v1/orders", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"orders": [1, 2, 3]}`)
	})
	backendHandler.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintf(w, "ok")
	})

	backendAddr := startBackendServer(t, backendHandler)

	rt := router.NewRouter()
	rt.AddRoute(&router.Route{
		ID: "users",
		Match: router.MatchRule{PathPrefix: "/api/v1/users"},
		Backends: []*router.Backend{
			{Address: backendAddr, Weight: 1, Healthy: true},
		},
		Strategy: router.StrategyWeightedRR,
	})
	rt.AddRoute(&router.Route{
		ID: "orders",
		Match: router.MatchRule{PathPrefix: "/api/v1/orders"},
		Backends: []*router.Backend{
			{Address: backendAddr, Weight: 1, Healthy: true},
		},
		Strategy: router.StrategyWeightedRR,
	})
	rt.AddRoute(&router.Route{
		ID: "health",
		Match: router.MatchRule{PathExact: "/health"},
		Backends: []*router.Backend{
			{Address: backendAddr, Weight: 1, Healthy: true},
		},
		Strategy: router.StrategyWeightedRR,
	})

	px := proxy.NewProxy(10, 5)

	gwAddr := startGateway(t, rt, px)

	time.Sleep(100 * time.Millisecond)

	t.Run("proxy GET /api/v1/users", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/users", gwAddr))
		if err != nil {
			t.Fatalf("GET error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "alice") {
			t.Errorf("response body should contain 'alice', got: %s", body)
		}
	})

	t.Run("proxy GET /health", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/health", gwAddr))
		if err != nil {
			t.Fatalf("GET error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("no route returns 404", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/unknown/path", gwAddr))
		if err != nil {
			t.Fatalf("GET error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 404 {
			t.Errorf("expected 404 for unknown path, got %d", resp.StatusCode)
		}
	})
}

func TestIntegrationConfigDriven(t *testing.T) {
	backendHandler := http.NewServeMux()
	backendHandler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintf(w, "backend response from %s", r.URL.Path)
	})
	backendAddr := startBackendServer(t, backendHandler)

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nexusgate.yaml")
	yamlContent := fmt.Sprintf(`
server:
  listen: ":8080"
gateway:
  shard_count: 4
  queue_size: 1024
routes:
  - match:
      path_prefix: "/api"
    backend:
      - address: "%s"
        weight: 1
    strategy: "weighted_round_robin"
`, backendAddr)

	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := config.NewStore(cfgPath)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg := store.Get()

	rt := router.NewRouter()
	routes := config.BuildRoutes(cfg)
	for _, route := range routes {
		rt.AddRoute(route)
	}

	px := proxy.NewProxy(cfg.Proxy.PoolSize, cfg.Proxy.PoolMaxIdle)
	gwAddr := startGateway(t, rt, px)

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/test", gwAddr))
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "backend response") {
		t.Errorf("unexpected body: %s", body)
	}
}
