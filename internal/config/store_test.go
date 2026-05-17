package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.Listen != ":8080" {
		t.Errorf("default listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Gateway.ShardCount != 8 {
		t.Errorf("default shard_count = %d, want 8", cfg.Gateway.ShardCount)
	}
	if cfg.Gateway.QueueSize != 4096 {
		t.Errorf("default queue_size = %d, want 4096", cfg.Gateway.QueueSize)
	}
	if cfg.Proxy.PoolSize != 256 {
		t.Errorf("default pool_size = %d, want 256", cfg.Proxy.PoolSize)
	}
}

func TestStoreLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nexusgate.yaml")

	yamlContent := []byte(`
server:
  listen: ":9090"
gateway:
  shard_count: 4
  queue_size: 2048
proxy:
  pool_size: 128
  pool_max_idle: 32
config:
  allow_private_backends: true
routes:
  - match:
      path_prefix: "/api/v1"
    backend:
      - address: "127.0.0.1:8081"
        weight: 1
    strategy: "weighted_round_robin"
`)

	if err := os.WriteFile(cfgPath, yamlContent, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := NewStore(cfgPath)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	cfg := store.Get()
	if cfg.Server.Listen != ":9090" {
		t.Errorf("listen = %q, want :9090", cfg.Server.Listen)
	}
	if cfg.Gateway.ShardCount != 4 {
		t.Errorf("shard_count = %d, want 4", cfg.Gateway.ShardCount)
	}
	if cfg.Gateway.QueueSize != 2048 {
		t.Errorf("queue_size = %d, want 2048", cfg.Gateway.QueueSize)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("routes count = %d, want 1", len(cfg.Routes))
	}
	if cfg.Routes[0].Match.PathPrefix != "/api/v1" {
		t.Errorf("route path_prefix = %q, want /api/v1", cfg.Routes[0].Match.PathPrefix)
	}
}

func TestStoreLoadNonExistent(t *testing.T) {
	store := NewStore("/nonexistent/path/nexusgate.yaml")
	if err := store.Load(); err != nil {
		t.Fatalf("Load() with non-existent file should use defaults, got error: %v", err)
	}

	cfg := store.Get()
	if cfg.Server.Listen != ":8080" {
		t.Errorf("should use default config, listen = %q", cfg.Server.Listen)
	}
}

func TestStoreUpdateCommit(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nexusgate.yaml")

	store := NewStore(cfgPath)
	store.Load()

	store.Update(func(cfg *Config) {
		cfg.Server.Listen = ":7070"
	})

	if store.Get().Server.Listen != ":8080" {
		t.Error("before Commit(), Get() should return old value")
	}

	store.Commit()

	if store.Get().Server.Listen != ":7070" {
		t.Errorf("after Commit(), Get() should return new value, got %q", store.Get().Server.Listen)
	}
}

func TestStoreRollback(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nexusgate.yaml")

	store := NewStore(cfgPath)
	store.Load()

	store.Update(func(cfg *Config) {
		cfg.Server.Listen = ":7070"
	})

	store.Rollback()

	if store.Get().Server.Listen != ":8080" {
		t.Errorf("after Rollback(), should revert to original, got %q", store.Get().Server.Listen)
	}
}

func TestBuildRoutes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routes = []RouteConfig{
		{
			Match: RouteMatchConfig{PathPrefix: "/api/v1/users"},
			Backend: []BackendConfig{
				{Address: "10.0.0.1:8080", Weight: 3},
				{Address: "10.0.0.2:8080", Weight: 1},
			},
			Strategy: "weighted_round_robin",
		},
		{
			Match: RouteMatchConfig{PathExact: "/health"},
			Backend: []BackendConfig{
				{Address: "10.0.0.1:8080", Weight: 1},
			},
			Strategy: "consistent_hash",
		},
	}

	routes := BuildRoutes(cfg)
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	if routes[0].Match.PathPrefix != "/api/v1/users" {
		t.Errorf("route 0 path_prefix = %q", routes[0].Match.PathPrefix)
	}
	if len(routes[0].Backends) != 2 {
		t.Errorf("route 0 backends = %d, want 2", len(routes[0].Backends))
	}
	if routes[0].Backends[0].Weight != 3 {
		t.Errorf("route 0 backend 0 weight = %d, want 3", routes[0].Backends[0].Weight)
	}
	if !routes[0].Backends[0].Healthy {
		t.Error("backends should start healthy")
	}
}
