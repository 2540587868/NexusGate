package config

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nexusgate/nexusgate/internal/router"
)

type ServerConfig struct {
	Listen       string `yaml:"listen"`
	TLSListen    string `yaml:"tls_listen"`
	TLSCert      string `yaml:"tls_cert"`
	TLSKey       string `yaml:"tls_key"`
	MetricsListen string `yaml:"metrics_listen"`
}

type GatewayConfig struct {
	ShardCount               int     `yaml:"shard_count"`
	WorkerPerShard           int     `yaml:"worker_per_shard"`
	QueueSize                int     `yaml:"queue_size"`
	SlowRecoveryThreshold    float64 `yaml:"slow_recovery_threshold"`
}

type ProxyConfig struct {
	DefaultMode    string        `yaml:"default_mode"`
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	PoolSize       int           `yaml:"pool_size"`
	PoolMaxIdle    int           `yaml:"pool_max_idle"`
}

type RouterConfig struct {
	DefaultStrategy string                       `yaml:"default_strategy"`
	ConsistentHash  ConsistentHashConfig         `yaml:"consistent_hash"`
	WeightedRR      WeightedRRConfig             `yaml:"weighted_round_robin"`
	LeastConn       LeastConnConfig              `yaml:"least_conn"`
	HeaderRoute     HeaderRouteConfig            `yaml:"header_route"`
}

type ConsistentHashConfig struct {
	VirtualNodes int `yaml:"virtual_nodes"`
}

type WeightedRRConfig struct{}

type LeastConnConfig struct{}

type HeaderRouteConfig struct {
	Header string `yaml:"header"`
}

type MiddlewareConfig struct {
	Order          []string                `yaml:"order"`
	Trace          TraceConfig             `yaml:"trace"`
	Auth           AuthConfig              `yaml:"auth"`
	RateLimit      RateLimitConfig         `yaml:"ratelimit"`
	CORS           CORSConfig              `yaml:"cors"`
	CircuitBreaker CircuitBreakerConfig    `yaml:"circuitbreaker"`
}

type TraceConfig struct {
	ServiceName string `yaml:"service_name"`
	Endpoint    string `yaml:"endpoint"`
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

type CORSConfig struct {
	AllowOrigins     []string `yaml:"allow_origins"`
	AllowMethods     []string `yaml:"allow_methods"`
	AllowHeaders     []string `yaml:"allow_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	ExposeHeaders    []string `yaml:"expose_headers"`
	MaxAge           int      `yaml:"max_age"`
}

type AuthConfig struct {
	Type           AuthTypeConfig `yaml:"type"`
	APIKeyHeader   string         `yaml:"api_key_header"`
	APIKeys        []APIKeyConfig `yaml:"api_keys"`
	JWTHMACSecret  string         `yaml:"jwt_hmac_secret"`
	JWTAllowedAlgs []string       `yaml:"jwt_allowed_algs"`
	SkipPaths      []string       `yaml:"skip_paths"`
}

type AuthTypeConfig string

const (
	AuthTypeConfigAPIKey AuthTypeConfig = "apikey"
	AuthTypeConfigJWT    AuthTypeConfig = "jwt"
	AuthTypeConfigNone   AuthTypeConfig = "none"
)

type APIKeyConfig struct {
	Key      string   `yaml:"key"`
	TenantID string   `yaml:"tenant_id"`
	Scopes   []string `yaml:"scopes"`
	Active   bool     `yaml:"active"`
}

type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failure_threshold"`
	SuccessThreshold int           `yaml:"success_threshold"`
	Timeout          time.Duration `yaml:"timeout"`
}

type PluginConfig struct {
	Dir       string `yaml:"dir"`
	HotReload bool   `yaml:"hot_reload"`
}

type EtcdConfig struct {
	Endpoints []string `yaml:"endpoints"`
	Prefix    string   `yaml:"prefix"`
	Watch     bool     `yaml:"watch"`
}

type CacheConfig struct {
	MissThreshold int `yaml:"miss_threshold"`
}

type ConfigStoreConfig struct {
	Etcd  EtcdProviderConfig `yaml:"etcd"`
	Cache CacheConfig        `yaml:"cache"`
}

type LifecycleConfig struct {
	GracefulTimeout time.Duration   `yaml:"graceful_timeout"`
	Recoverable     bool            `yaml:"recoverable"`
	HealthCheck     HealthCheckConfig `yaml:"health_check"`
}

type HealthCheckConfig struct {
	Interval           time.Duration `yaml:"interval"`
	Timeout            time.Duration `yaml:"timeout"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold"`
}

type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	AccessLog  bool   `yaml:"access_log"`
}

type RouteMatchConfig struct {
	PathPrefix string            `yaml:"path_prefix"`
	PathExact  string            `yaml:"path_exact"`
	Methods    []string          `yaml:"methods"`
	Headers    map[string]string `yaml:"headers"`
}

type BackendConfig struct {
	Address string            `yaml:"address"`
	Weight  int               `yaml:"weight"`
	Meta    map[string]string `yaml:"meta"`
}

type RouteConfig struct {
	Match       RouteMatchConfig `yaml:"match"`
	Backend     []BackendConfig  `yaml:"backend"`
	Strategy    string           `yaml:"strategy"`
	Middlewares []string         `yaml:"middleware"`
}

type Config struct {
	Server     ServerConfig      `yaml:"server"`
	Gateway    GatewayConfig     `yaml:"gateway"`
	Proxy      ProxyConfig       `yaml:"proxy"`
	Router     RouterConfig      `yaml:"router"`
	Middleware MiddlewareConfig   `yaml:"middleware"`
	Plugin     PluginConfig      `yaml:"plugin"`
	ConfigStore ConfigStoreConfig `yaml:"config"`
	Lifecycle  LifecycleConfig   `yaml:"lifecycle"`
	Logging    LoggingConfig     `yaml:"logging"`
	Routes     []RouteConfig     `yaml:"routes"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Listen: ":8080",
		},
		Gateway: GatewayConfig{
			ShardCount:            8,
			WorkerPerShard:        1,
			QueueSize:             4096,
			SlowRecoveryThreshold: 0.9,
		},
		Proxy: ProxyConfig{
			DefaultMode:    "splice",
			ConnectTimeout: 5 * time.Second,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
			PoolSize:       256,
			PoolMaxIdle:    64,
		},
		Router: RouterConfig{
			DefaultStrategy: "consistent_hash",
			ConsistentHash: ConsistentHashConfig{
				VirtualNodes: 150,
			},
		},
		Middleware: MiddlewareConfig{
			Order: []string{"trace", "ratelimit", "cors", "circuitbreaker"},
			Trace: TraceConfig{
				ServiceName: "nexusgate",
				Endpoint:    "http://otel-collector:4318",
			},
			RateLimit: RateLimitConfig{
				RequestsPerSecond: 10000,
				Burst:             20000,
			},
			CORS: CORSConfig{
			AllowOrigins:     []string{},
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
			AllowHeaders:     []string{"Authorization", "Content-Type", "X-API-Key", "X-Request-ID"},
			AllowCredentials: true,
			MaxAge:           3600,
		},
			CircuitBreaker: CircuitBreakerConfig{
				FailureThreshold: 5,
				SuccessThreshold: 3,
				Timeout:          30 * time.Second,
			},
		},
		Plugin: PluginConfig{
			Dir:       "/etc/nexusgate/plugins",
			HotReload: true,
		},
		Lifecycle: LifecycleConfig{
			GracefulTimeout: 30 * time.Second,
			Recoverable:     true,
			HealthCheck: HealthCheckConfig{
				Interval:           10 * time.Second,
				Timeout:            5 * time.Second,
				UnhealthyThreshold: 3,
			},
		},
		Logging: LoggingConfig{
			Level:     "info",
			Format:    "json",
			AccessLog: true,
		},
	}
}

type Store struct {
	mu    sync.RWMutex
	dirty atomic.Value
	clean atomic.Value
	path  string
}

func NewStore(path string) *Store {
	s := &Store{
		path: path,
	}
	cfg := DefaultConfig()
	s.dirty.Store(cfg)
	s.clean.Store(cfg)
	return s
}

func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			s.dirty.Store(cfg)
			s.clean.Store(cfg)
			return nil
		}
		return fmt.Errorf("read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}

	s.mu.Lock()
	s.dirty.Store(cfg)
	s.clean.Store(cfg)
	s.mu.Unlock()

	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return nil
}

func (s *Store) Get() *Config {
	return s.clean.Load().(*Config)
}

func (s *Store) Update(fn func(*Config)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.dirty.Load().(*Config)
	updated := deepCopy(current)
	fn(updated)
	s.dirty.Store(updated)
}

func (s *Store) Commit() {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirty := s.dirty.Load().(*Config)
	clean := deepCopy(dirty)
	s.clean.Store(clean)
}

func (s *Store) Rollback() {
	s.mu.Lock()
	defer s.mu.Unlock()

	clean := s.clean.Load().(*Config)
	s.dirty.Store(clean)
}

func (s *Store) Save() error {
	s.mu.RLock()
	cfg := s.dirty.Load().(*Config)
	s.mu.RUnlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

func (s *Store) fileInfo() (os.FileInfo, error) {
	return os.Stat(s.path)
}

func deepCopy(cfg *Config) *Config {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		copy := *cfg
		return &copy
	}
	var result Config
	if err := yaml.Unmarshal(data, &result); err != nil {
		copy := *cfg
		return &copy
	}
	return &result
}

func BuildRoutes(cfg *Config) []*router.Route {
	routes := make([]*router.Route, 0, len(cfg.Routes))
	for i, rc := range cfg.Routes {
		backends := make([]*router.Backend, 0, len(rc.Backend))
		for _, bc := range rc.Backend {
			backends = append(backends, &router.Backend{
				Address: bc.Address,
				Weight:  bc.Weight,
				Healthy: true,
				Meta:    bc.Meta,
			})
		}

		strategy := router.StrategyType(rc.Strategy)
		if strategy == "" {
			strategy = router.StrategyType(cfg.Router.DefaultStrategy)
		}

		route := &router.Route{
			ID: fmt.Sprintf("route_%d", i),
			Match: router.MatchRule{
				PathPrefix: rc.Match.PathPrefix,
				PathExact:  rc.Match.PathExact,
				Methods:    rc.Match.Methods,
				Headers:    rc.Match.Headers,
			},
			Backends:    backends,
			Strategy:    strategy,
			Middlewares: rc.Middlewares,
		}
		routes = append(routes, route)
	}
	return routes
}

func validateConfig(cfg *Config) error {
	if cfg.Proxy.ConnectTimeout < 0 {
		return fmt.Errorf("proxy.connect_timeout must be non-negative, got %v", cfg.Proxy.ConnectTimeout)
	}
	if cfg.Proxy.ReadTimeout < 0 {
		return fmt.Errorf("proxy.read_timeout must be non-negative, got %v", cfg.Proxy.ReadTimeout)
	}
	if cfg.Proxy.WriteTimeout < 0 {
		return fmt.Errorf("proxy.write_timeout must be non-negative, got %v", cfg.Proxy.WriteTimeout)
	}
	if cfg.Proxy.PoolSize < 0 {
		return fmt.Errorf("proxy.pool_size must be non-negative, got %d", cfg.Proxy.PoolSize)
	}
	if cfg.Gateway.ShardCount <= 0 {
		return fmt.Errorf("gateway.shard_count must be positive, got %d", cfg.Gateway.ShardCount)
	}
	if cfg.Gateway.QueueSize <= 0 {
		return fmt.Errorf("gateway.queue_size must be positive, got %d", cfg.Gateway.QueueSize)
	}
	if cfg.Middleware.RateLimit.RequestsPerSecond < 0 {
		return fmt.Errorf("middleware.ratelimit.requests_per_second must be non-negative, got %v", cfg.Middleware.RateLimit.RequestsPerSecond)
	}
	if cfg.Middleware.CircuitBreaker.FailureThreshold <= 0 {
		return fmt.Errorf("middleware.circuitbreaker.failure_threshold must be positive, got %d", cfg.Middleware.CircuitBreaker.FailureThreshold)
	}
	if cfg.Lifecycle.GracefulTimeout < 0 {
		return fmt.Errorf("lifecycle.graceful_timeout must be non-negative, got %v", cfg.Lifecycle.GracefulTimeout)
	}
	return nil
}
