package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexusgate/nexusgate/internal/config"
	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/lifecycle"
	"github.com/nexusgate/nexusgate/internal/router"
)

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()

	store := config.NewStore("")
	cfg := &config.Config{
		Server: config.ServerConfig{
			DashboardToken: "test-token-123",
		},
		Gateway: config.GatewayConfig{
			ShardCount:            4,
			WorkerPerShard:        1,
			QueueSize:             1024,
			SlowRecoveryThreshold: 0.9,
		},
		Middleware: config.MiddlewareConfig{
			Auth: config.AuthConfig{
				JWTHMACSecret: "secret-key-for-testing",
				APIKeys: []config.APIKeyConfig{
					{Key: "ak-12345678", TenantID: "tenant1", Active: true},
				},
			},
		},
		Routes: []config.RouteConfig{
			{
				Match: config.RouteMatchConfig{PathPrefix: "/api"},
				Backend: []config.BackendConfig{
					{Address: "127.0.0.1:8081", Weight: 3},
				},
				Strategy: "weighted_round_robin",
			},
		},
	}
	store.Update(func(c *config.Config) { *c = *cfg })
	store.Commit()

	hc := lifecycle.NewHealthChecker(5*time.Second, 3*time.Second, 3)
	rt := router.NewRouter()
	gw := gateway.NewGateway(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	}, 4, 1024)

	srv := NewServer(store, hc, rt, gw, nil, "0.5.0", "abc1234", "2026-01-01", "test-token-123")

	cleanup := func() {
		gw.Close()
	}

	return srv, cleanup
}

func TestDashboardAuth(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	t.Run("unauthenticated_api_returns_401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/overview", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("authenticated_with_bearer_token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/overview", nil)
		req.Header.Set("Authorization", "Bearer test-token-123")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("wrong_token_returns_401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/overview", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("auth_endpoint_login", func(t *testing.T) {
		body := `{"token":"test-token-123"}`
		req := httptest.NewRequest("POST", "/api/v1/auth", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["status"] != "ok" {
			t.Errorf("expected status ok, got %v", resp["status"])
		}
		found := false
		for _, c := range w.Result().Cookies() {
			if c.Name == "nexusgate_session" {
				found = true
				if !c.HttpOnly {
					t.Error("expected HttpOnly cookie")
				}
				if c.SameSite != http.SameSiteStrictMode {
					t.Error("expected SameSiteStrictMode")
				}
			}
		}
		if !found {
			t.Error("expected nexusgate_session cookie to be set")
		}
	})

	t.Run("auth_endpoint_invalid_token", func(t *testing.T) {
		body := `{"token":"wrong"}`
		req := httptest.NewRequest("POST", "/api/v1/auth", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["error"] == nil {
			t.Error("expected error response for invalid token")
		}
	})

	t.Run("auth_endpoint_rate_limiting", func(t *testing.T) {
		srv2, cleanup2 := newTestServer(t)
		defer cleanup2()
		handler2 := srv2.Handler()

		for i := 0; i < srv2.maxLoginAttempts; i++ {
			body := `{"token":"wrong"}`
			req := httptest.NewRequest("POST", "/api/v1/auth", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler2.ServeHTTP(w, req)
		}

		body := `{"token":"test-token-123"}`
		req := httptest.NewRequest("POST", "/api/v1/auth", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler2.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["error"] == nil || resp["error"] != "too many login attempts" {
			t.Errorf("expected rate limit error, got %v", resp["error"])
		}
	})

	t.Run("session_cookie_auth", func(t *testing.T) {
		srv3, cleanup3 := newTestServer(t)
		defer cleanup3()
		handler3 := srv3.Handler()

		loginBody := `{"token":"test-token-123"}`
		loginReq := httptest.NewRequest("POST", "/api/v1/auth", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginW := httptest.NewRecorder()
		handler3.ServeHTTP(loginW, loginReq)

		var sessionCookie *http.Cookie
		for _, c := range loginW.Result().Cookies() {
			if c.Name == "nexusgate_session" {
				sessionCookie = c
				break
			}
		}
		if sessionCookie == nil {
			t.Fatal("expected session cookie after login")
		}

		req := httptest.NewRequest("GET", "/api/v1/overview", nil)
		req.AddCookie(sessionCookie)
		w := httptest.NewRecorder()
		handler3.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 with session cookie, got %d", w.Code)
		}
	})
}

func TestDashboardOverview(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["version"] != "0.5.0" {
		t.Errorf("expected version 0.5.0, got %v", resp["version"])
	}
	if resp["routes"] != nil {
		routesCount := int(resp["routes"].(float64))
		if routesCount != 1 {
			t.Errorf("expected 1 route, got %d", routesCount)
		}
	}
	gwData, ok := resp["gateway"].(map[string]interface{})
	if !ok {
		t.Fatal("expected gateway object in overview")
	}
	if gwData["shardCount"] == nil {
		t.Error("expected shardCount in gateway data")
	}
}

func TestDashboardGateway(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/v1/gateway", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	cfgData, ok := resp["config"].(map[string]interface{})
	if !ok {
		t.Fatal("expected config object in gateway response")
	}
	if cfgData["shardCount"] == nil || cfgData["queueSize"] == nil {
		t.Error("expected shardCount and queueSize in gateway config")
	}

	rtData, ok := resp["runtime"].(map[string]interface{})
	if !ok {
		t.Fatal("expected runtime object in gateway response")
	}
	shards, ok := rtData["shards"].([]interface{})
	if !ok || len(shards) != 4 {
		t.Errorf("expected 4 shards, got %d", len(shards))
	}
}

func TestDashboardConfigSanitization(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	serverData, ok := resp["Server"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected Server object in config response, got %T", resp["Server"])
	}
	if serverData["DashboardToken"] != "***" {
		t.Errorf("expected DashboardToken to be masked, got %v", serverData["DashboardToken"])
	}

	mwData, ok := resp["Middleware"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected Middleware object in config response, got %T", resp["Middleware"])
	}
	authData, ok := mwData["Auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected Auth object in Middleware, got %T", mwData["Auth"])
	}
	if authData["JWTHMACSecret"] != "***" {
		t.Errorf("expected JWTHMACSecret to be masked, got %v", authData["JWTHMACSecret"])
	}
}

func TestDashboardRoutes(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/v1/routes", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	routes, ok := resp["routes"].([]interface{})
	if !ok {
		t.Fatalf("expected routes array, got %T", resp["routes"])
	}
	if len(routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes))
	}
}

func TestDashboardTopology(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/v1/topology", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	nodes, ok := resp["nodes"].([]interface{})
	if !ok {
		t.Fatal("expected nodes array in topology")
	}
	if len(nodes) == 0 {
		t.Error("expected at least one node in topology")
	}
}

func TestDashboardMethodNotAllowed(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	endpoints := []string{"/api/v1/overview", "/api/v1/backends", "/api/v1/config", "/api/v1/topology", "/api/v1/gateway"}
	for _, ep := range endpoints {
		req := httptest.NewRequest("POST", ep, nil)
		req.Header.Set("Authorization", "Bearer test-token-123")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: expected 405, got %d", ep, w.Code)
		}
	}
}

func TestNewServerAutoToken(t *testing.T) {
	store := config.NewStore("")
	cfg := &config.Config{}
	store.Update(func(c *config.Config) { *c = *cfg })
	store.Commit()

	srv := NewServer(store, nil, nil, nil, nil, "1.0", "abc", "now", "")
	if srv.authToken == "" {
		t.Error("expected auto-generated token when empty string provided")
	}
	if len(srv.authToken) != 32 {
		t.Errorf("expected 32-char hex token, got %d chars", len(srv.authToken))
	}
}
