package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nexusgate/nexusgate/internal/middleware"
	"github.com/nexusgate/nexusgate/internal/router"
)

type ServerConfig struct {
	Listen                string        `yaml:"listen"`
	TLSListen             string        `yaml:"tls_listen"`
	TLSCert               string        `yaml:"tls_cert"`
	TLSKey                string        `yaml:"tls_key"`
	TLSClientCA           string        `yaml:"tls_client_ca"`
	TLSClientVerify       bool          `yaml:"tls_client_verify"`
	MetricsListen         string        `yaml:"metrics_listen"`
	DashboardListen       string        `yaml:"dashboard_listen"`
	DashboardToken        string        `yaml:"dashboard_token"`
	MaxLoginAttempts      int           `yaml:"max_login_attempts"`
	LoginBlockDuration    time.Duration `yaml:"login_block_duration"`
	LoginWindowDuration   time.Duration `yaml:"login_window_duration"`
	SSEKeepaliveInterval  time.Duration `yaml:"sse_keepalive_interval"`
}

type GatewayConfig struct {
	ShardCount               int           `yaml:"shard_count"`
	WorkerPerShard           int           `yaml:"worker_per_shard"`
	QueueSize                int           `yaml:"queue_size"`
	SlowRecoveryThreshold    float64       `yaml:"slow_recovery_threshold"`
	SyncTimeout              time.Duration `yaml:"sync_timeout"`
	SlowRecoveryBatchSize    int           `yaml:"slow_recovery_batch_size"`
}

type ProxyConfig struct {
	DefaultMode              string        `yaml:"default_mode"`
	ConnectTimeout           time.Duration `yaml:"connect_timeout"`
	ReadTimeout              time.Duration `yaml:"read_timeout"`
	WriteTimeout             time.Duration `yaml:"write_timeout"`
	IdleConnTimeout          time.Duration `yaml:"idle_conn_timeout"`
	KeepAlive                time.Duration `yaml:"keep_alive"`
	MirrorTimeout            time.Duration `yaml:"mirror_timeout"`
	WebSocketConnectTimeout  time.Duration `yaml:"websocket_connect_timeout"`
	PoolSize                 int           `yaml:"pool_size"`
	PoolMaxIdle              int           `yaml:"pool_max_idle"`
	MaxRequestBodyBytes      int64         `yaml:"max_request_body_bytes"`
	MaxResponseBodyBytes     int64         `yaml:"max_response_body_bytes"`
	Retry                    RetryConfig   `yaml:"retry"`
}

type RetryConfig struct {
	MaxRetries       int           `yaml:"max_retries"`
	RetryableStatus  []int         `yaml:"retryable_statuses"`
	BackoffBase      time.Duration `yaml:"backoff_base"`
	BackoffMax       time.Duration `yaml:"backoff_max"`
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
	Order          []string                          `yaml:"order"`
	Trace          TraceConfig                       `yaml:"trace"`
	Auth           AuthConfig                        `yaml:"auth"`
	RateLimit      RateLimitConfig                   `yaml:"ratelimit"`
	CORS           CORSConfig                        `yaml:"cors"`
	CircuitBreaker CircuitBreakerConfig              `yaml:"circuitbreaker"`
	Tenant         middleware.TenantIsolationConfig   `yaml:"tenant"`
}

type TraceConfig struct {
	ServiceName string `yaml:"service_name"`
	Endpoint    string `yaml:"endpoint"`
}

type RateLimitConfig struct {
	RequestsPerSecond float64                               `yaml:"requests_per_second"`
	Burst             int                                   `yaml:"burst"`
	CleanupInterval   time.Duration                         `yaml:"cleanup_interval"`
	BucketExpiry      time.Duration                         `yaml:"bucket_expiry"`
	MaxBuckets        int                                   `yaml:"max_buckets"`
	Distributed       middleware.DistributedRateLimiterConfig `yaml:"distributed"`
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
	Etcd                 EtcdProviderConfig `yaml:"etcd"`
	Cache                CacheConfig        `yaml:"cache"`
	AllowPrivateBackends bool               `yaml:"allow_private_backends"`
	WatchInterval        time.Duration      `yaml:"watch_interval"`
}

type LifecycleConfig struct {
	GracefulTimeout      time.Duration    `yaml:"graceful_timeout"`
	Recoverable          bool             `yaml:"recoverable"`
	RecoverableMaxBackoff time.Duration   `yaml:"recoverable_max_backoff"`
	HealthCheck          HealthCheckConfig `yaml:"health_check"`
}

type HealthCheckConfig struct {
	Interval           time.Duration `yaml:"interval"`
	Timeout            time.Duration `yaml:"timeout"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold"`
	Path               string        `yaml:"path"`
}

type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	AccessLog  bool   `yaml:"access_log"`
}

type RouteMatchConfig struct {
	PathPrefix string            `yaml:"path_prefix"`
	PathExact  string            `yaml:"path_exact"`
	PathRegex  string            `yaml:"path_regex"`
	Methods    []string          `yaml:"methods"`
	Headers    map[string]string `yaml:"headers"`
}

type BackendConfig struct {
	Address string            `yaml:"address"`
	Weight  int               `yaml:"weight"`
	Meta    map[string]string `yaml:"meta"`
}

type RouteConfig struct {
	Match       RouteMatchConfig    `yaml:"match"`
	Backend     []BackendConfig     `yaml:"backend"`
	Strategy    string              `yaml:"strategy"`
	Middlewares []string            `yaml:"middleware"`
	Canary      router.CanaryRule   `yaml:"canary"`
	Timeout     RouteTimeoutConfig  `yaml:"timeout"`
	Retry       RouteRetryConfig    `yaml:"retry"`
	Streaming   bool                `yaml:"streaming"`
	Rewrite     RouteRewriteConfig  `yaml:"rewrite"`
}

type RouteRewriteConfig struct {
	RequestHeader  HeaderRewriteConfig `yaml:"request_header"`
	ResponseHeader HeaderRewriteConfig `yaml:"response_header"`
	RequestBody    []RewriteRuleConfig `yaml:"request_body"`
	ResponseBody   []RewriteRuleConfig `yaml:"response_body"`
}

type HeaderRewriteConfig struct {
	Set    map[string]string `yaml:"set"`
	Add    map[string]string `yaml:"add"`
	Remove []string          `yaml:"remove"`
}

type RewriteRuleConfig struct {
	Pattern     string `yaml:"pattern"`
	Replacement string `yaml:"replacement"`
}

type RouteTimeoutConfig struct {
	Connect time.Duration `yaml:"connect"`
	Read    time.Duration `yaml:"read"`
	Write   time.Duration `yaml:"write"`
	Total   time.Duration `yaml:"total"`
}

type RouteRetryConfig struct {
	MaxRetries      int   `yaml:"max_retries"`
	RetryableStatus []int `yaml:"retryable_status"`
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
			Listen:               ":8080",
			MaxLoginAttempts:     5,
			LoginBlockDuration:   15 * time.Minute,
			LoginWindowDuration:  5 * time.Minute,
			SSEKeepaliveInterval: 30 * time.Second,
		},
		Gateway: GatewayConfig{
			ShardCount:            8,
			WorkerPerShard:        1,
			QueueSize:             4096,
			SlowRecoveryThreshold: 0.9,
			SyncTimeout:           30 * time.Second,
			SlowRecoveryBatchSize: 16,
		},
		Proxy: ProxyConfig{
			DefaultMode:             "splice",
			ConnectTimeout:          5 * time.Second,
			ReadTimeout:             30 * time.Second,
			WriteTimeout:            30 * time.Second,
			IdleConnTimeout:         90 * time.Second,
			KeepAlive:               30 * time.Second,
			MirrorTimeout:           5 * time.Second,
			WebSocketConnectTimeout: 10 * time.Second,
			PoolSize:                256,
			PoolMaxIdle:             64,
			MaxResponseBodyBytes:    10 << 20,
			Retry: RetryConfig{
				MaxRetries:      2,
				RetryableStatus: []int{502, 503, 504},
				BackoffBase:     100 * time.Millisecond,
				BackoffMax:      5 * time.Second,
			},
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
				CleanupInterval:   5 * time.Minute,
				BucketExpiry:      10 * time.Minute,
				MaxBuckets:        100000,
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
			GracefulTimeout:       30 * time.Second,
			Recoverable:           true,
			RecoverableMaxBackoff: 30 * time.Second,
			HealthCheck: HealthCheckConfig{
				Interval:           10 * time.Second,
				Timeout:            5 * time.Second,
				UnhealthyThreshold: 3,
				Path:               "/health",
			},
		},
		Logging: LoggingConfig{
			Level:     "info",
			Format:    "json",
			AccessLog: true,
		},
		ConfigStore: ConfigStoreConfig{
			WatchInterval: 5 * time.Second,
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

	applyEnvOverrides(cfg)

	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	s.mu.Lock()
	s.dirty.Store(cfg)
	s.clean.Store(cfg)
	s.mu.Unlock()

	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("NEXUSGATE_SERVER_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("NEXUSGATE_SERVER_TLS_LISTEN"); v != "" {
		cfg.Server.TLSListen = v
	}
	if v := os.Getenv("NEXUSGATE_SERVER_TLS_CERT"); v != "" {
		cfg.Server.TLSCert = v
	}
	if v := os.Getenv("NEXUSGATE_SERVER_TLS_KEY"); v != "" {
		cfg.Server.TLSKey = v
	}
	if v := os.Getenv("NEXUSGATE_SERVER_METRICS_LISTEN"); v != "" {
		cfg.Server.MetricsListen = v
	}
	if v := os.Getenv("NEXUSGATE_SERVER_DASHBOARD_LISTEN"); v != "" {
		cfg.Server.DashboardListen = v
	}
	if v := os.Getenv("NEXUSGATE_SERVER_DASHBOARD_TOKEN"); v != "" {
		cfg.Server.DashboardToken = v
	}
	if v := os.Getenv("NEXUSGATE_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("NEXUSGATE_LOGGING_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := os.Getenv("NEXUSGATE_GATEWAY_SHARD_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Gateway.ShardCount = n
		}
	}
	if v := os.Getenv("NEXUSGATE_GATEWAY_WORKER_PER_SHARD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Gateway.WorkerPerShard = n
		}
	}
	if v := os.Getenv("NEXUSGATE_GATEWAY_QUEUE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Gateway.QueueSize = n
		}
	}
	if v := os.Getenv("NEXUSGATE_PROXY_CONNECT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Proxy.ConnectTimeout = d
		}
	}
	if v := os.Getenv("NEXUSGATE_PROXY_READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Proxy.ReadTimeout = d
		}
	}
	if v := os.Getenv("NEXUSGATE_PROXY_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Proxy.WriteTimeout = d
		}
	}
	if v := os.Getenv("NEXUSGATE_RATELIMIT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.Middleware.RateLimit.RequestsPerSecond = f
		}
	}
	if v := os.Getenv("NEXUSGATE_RATELIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Middleware.RateLimit.Burst = n
		}
	}
	if v := os.Getenv("NEXUSGATE_AUTH_TYPE"); v != "" {
		cfg.Middleware.Auth.Type = AuthTypeConfig(v)
	}
	if v := os.Getenv("NEXUSGATE_AUTH_JWT_SECRET"); v != "" {
		cfg.Middleware.Auth.JWTHMACSecret = v
	}
	if v := os.Getenv("NEXUSGATE_CONFIG_ALLOW_PRIVATE_BACKENDS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ConfigStore.AllowPrivateBackends = b
		}
	}
}

func (s *Store) Get() *Config {
	return s.clean.Load().(*Config)
}

func (s *Store) ReloadWithEnvOverrides() {
	cfg := s.Get()
	applyEnvOverrides(cfg)
	s.mu.Lock()
	s.dirty.Store(cfg)
	s.clean.Store(cfg)
	s.mu.Unlock()
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
				PathRegex:  rc.Match.PathRegex,
				Methods:    rc.Match.Methods,
				Headers:    rc.Match.Headers,
			},
			Backends:    backends,
			Strategy:    strategy,
			Middlewares: rc.Middlewares,
			Canary:      rc.Canary,
			Timeout: router.RouteTimeout{
				Connect: rc.Timeout.Connect,
				Read:    rc.Timeout.Read,
				Write:   rc.Timeout.Write,
				Total:   rc.Timeout.Total,
			},
			Retry: router.RouteRetry{
				MaxRetries:      rc.Retry.MaxRetries,
				RetryableStatus: rc.Retry.RetryableStatus,
			},
			Streaming: rc.Streaming,
			Rewrite: router.RouteRewrite{
				RequestHeader: router.HeaderRewrite{
					Set:    rc.Rewrite.RequestHeader.Set,
					Add:    rc.Rewrite.RequestHeader.Add,
					Remove: rc.Rewrite.RequestHeader.Remove,
				},
				ResponseHeader: router.HeaderRewrite{
					Set:    rc.Rewrite.ResponseHeader.Set,
					Add:    rc.Rewrite.ResponseHeader.Add,
					Remove: rc.Rewrite.ResponseHeader.Remove,
				},
				RequestBody:  convertRewriteRules(rc.Rewrite.RequestBody),
				ResponseBody: convertRewriteRules(rc.Rewrite.ResponseBody),
			},
		}
		routes = append(routes, route)
	}
	return routes
}

func convertRewriteRules(rules []RewriteRuleConfig) []router.RewriteRule {
	if len(rules) == 0 {
		return nil
	}
	result := make([]router.RewriteRule, len(rules))
	for i, r := range rules {
		result[i] = router.RewriteRule{
			Pattern:     r.Pattern,
			Replacement: r.Replacement,
		}
	}
	return result
}

func validateConfig(cfg *Config) error {
	if cfg.Server.Listen == "" && cfg.Server.TLSListen == "" {
		return fmt.Errorf("server must have at least one of listen or tls_listen configured")
	}
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
	if cfg.Server.TLSListen != "" {
		if cfg.Server.TLSCert == "" {
			return fmt.Errorf("server.tls_cert is required when tls_listen is set")
		}
		if cfg.Server.TLSKey == "" {
			return fmt.Errorf("server.tls_key is required when tls_listen is set")
		}
	}
	if cfg.Middleware.Auth.Type == "jwt" && cfg.Middleware.Auth.JWTHMACSecret == "" {
		return fmt.Errorf("middleware.auth.jwt_hmac_secret is required when auth type is jwt")
	}
	for i, rc := range cfg.Routes {
		if rc.Match.PathPrefix == "" && rc.Match.PathExact == "" && rc.Match.PathRegex == "" {
			return fmt.Errorf("routes[%d].match must have at least one path matcher (path_prefix, path_exact, or path_regex)", i)
		}
		if len(rc.Backend) == 0 {
			return fmt.Errorf("routes[%d].backend must have at least one backend", i)
		}
		for j, b := range rc.Backend {
			if b.Address == "" {
				return fmt.Errorf("routes[%d].backend[%d].address is required", i, j)
			}
			if b.Weight < 0 {
				return fmt.Errorf("routes[%d].backend[%d].weight must be non-negative, got %d", i, j, b.Weight)
			}
		}
	}
	if err := ValidateNoSSRF(cfg); err != nil {
		return err
	}
	return nil
}

func WarnUnusedConfig(cfg *Config) {
	unused := []string{}
	if cfg.Plugin.Dir != "" && cfg.Plugin.Dir != "/etc/nexusgate/plugins" {
		unused = append(unused, "plugin.dir (plugin system not implemented)")
	}
	if cfg.Plugin.HotReload {
		unused = append(unused, "plugin.hot_reload (plugin system not implemented)")
	}
	if len(cfg.Middleware.Order) > 0 {
		unused = append(unused, "middleware.order (middleware chain order is hardcoded)")
	}
	for _, rc := range cfg.Routes {
		if len(rc.Middlewares) > 0 {
			unused = append(unused, "routes.middleware (per-route middleware chain not supported)")
			break
		}
	}
	if len(unused) > 0 {
		slog.Warn("config fields defined but not used", "fields", unused, "hint", "these fields are reserved for future features")
	}
}
