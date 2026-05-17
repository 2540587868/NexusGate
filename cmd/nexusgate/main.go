package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nexusgate/nexusgate/internal/config"
	"github.com/nexusgate/nexusgate/internal/dashboard"
	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/httparser"
	"github.com/nexusgate/nexusgate/internal/lifecycle"
	"github.com/nexusgate/nexusgate/internal/middleware"
	"github.com/nexusgate/nexusgate/internal/proxy"
	"github.com/nexusgate/nexusgate/internal/router"
)

var (
	version   = "dev"
	gitCommit = "none"
	buildTime = "unknown"
	startTime = time.Now()
)

func main() {
	configPath := flag.String("config", "configs/nexusgate.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "show version info")
	flag.Parse()

	if *showVersion {
		fmt.Printf("NexusGate %s (commit: %s, built: %s)\n", version, gitCommit, buildTime)
		os.Exit(0)
	}

	slog.Info("starting NexusGate", "config", *configPath, "version", version, "commit", gitCommit)

	store := config.NewStore(*configPath)
	if err := store.Load(); err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	cfg := store.Get()

	initLogging(cfg)
	config.WarnUnusedConfig(cfg)

	rt := initRouter(cfg)
	px := initProxy(cfg)
	mwChain, cb, rl := initMiddleware(cfg, rt, px)
	gw := gateway.NewGateway(mwChain, cfg.Gateway.ShardCount, cfg.Gateway.QueueSize).
		WithSlowRecoveryThreshold(cfg.Gateway.SlowRecoveryThreshold).
		WithWorkerPerShard(cfg.Gateway.WorkerPerShard).
		WithSyncTimeout(cfg.Gateway.SyncTimeout).
		WithSlowRecoveryBatchSize(cfg.Gateway.SlowRecoveryBatchSize)

	recoverable := lifecycle.NewRecoverableWithBackoff(5, cfg.Lifecycle.RecoverableMaxBackoff)
	parser := httparser.NewParser()
	if cfg.Proxy.MaxRequestBodyBytes > 0 {
		parser = parser.WithMaxBodyBytes(cfg.Proxy.MaxRequestBodyBytes)
	}
	graceful := lifecycle.NewGraceful(cfg.Lifecycle.GracefulTimeout)

	if len(cfg.ConfigStore.Etcd.Endpoints) > 0 {
		etcdProvider := config.NewEtcdProvider(cfg.ConfigStore.Etcd, store)
		if err := etcdProvider.Connect(context.Background()); err != nil {
			slog.Warn("failed to connect to etcd, using file config only", "error", err)
		} else {
			if err := etcdProvider.Load(context.Background()); err != nil {
				slog.Warn("failed to load config from etcd, using file config", "error", err)
			} else {
				cfg = store.Get()
				slog.Info("using etcd config")
			}
			if cfg.ConfigStore.Etcd.Watch {
				recoverable.Go("etcd-watcher", func(ctx context.Context) error {
					return etcdProvider.StartWatch(ctx)
				})
				graceful.OnShutdown(func() error {
					slog.Info("stopping etcd provider")
					etcdProvider.Stop()
					return nil
				})
			}
		}
	}

	var hc *lifecycle.HealthChecker
	if cfg.Lifecycle.Recoverable {
		hc = initHealthChecker(cfg, rt, px, recoverable)
		initConfigWatcher(store, rt, hc, recoverable, graceful, rl, cb)
	} else {
		hc = initHealthChecker(cfg, rt, px, nil)
		initConfigWatcher(store, rt, hc, nil, graceful, rl, cb)
	}
	initListeners(cfg, parser, gw, px, rt, graceful)
	initMetricsServer(cfg, rt, graceful)
	initDashboardServer(cfg, store, hc, rt, gw, cb, graceful)

	graceful.OnShutdown(func() error {
		slog.Info("closing gateway")
		gw.Close()
		return nil
	})
	graceful.OnShutdown(func() error {
		slog.Info("stopping recoverable goroutines")
		recoverable.StopAll()
		return nil
	})

	graceful.Wait()
}

func initLogging(cfg *config.Config) {
	var slogLevel slog.Level
	switch cfg.Logging.Level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	var slogHandler slog.Handler
	switch cfg.Logging.Format {
	case "text":
		slogHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
	default:
		slogHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
	}
	slog.SetDefault(slog.New(slogHandler))
}

func initRouter(cfg *config.Config) *router.Router {
	rt := router.NewRouterWithConfig(
		cfg.Router.ConsistentHash.VirtualNodes,
		cfg.Router.HeaderRoute.Header,
	)
	routes := config.BuildRoutes(cfg)
	for _, route := range routes {
		rt.AddRoute(route)
	}
	return rt
}

func initProxy(cfg *config.Config) *proxy.Proxy {
	p := proxy.NewProxy(cfg.Proxy.PoolSize, cfg.Proxy.PoolMaxIdle).
		WithTimeouts(cfg.Proxy.ConnectTimeout, cfg.Proxy.ReadTimeout, cfg.Proxy.WriteTimeout).
		WithMaxResponseBodyBytes(cfg.Proxy.MaxResponseBodyBytes).
		WithIdleConnTimeout(cfg.Proxy.IdleConnTimeout).
		WithKeepAlive(cfg.Proxy.KeepAlive).
		WithMirrorTimeout(cfg.Proxy.MirrorTimeout).
		WithWebSocketConnectTimeout(cfg.Proxy.WebSocketConnectTimeout)

	if cfg.Proxy.Retry.MaxRetries > 0 {
		retryStatuses := cfg.Proxy.Retry.RetryableStatus
		if len(retryStatuses) == 0 {
			retryStatuses = []int{502, 503, 504}
		}
		backoffBase := cfg.Proxy.Retry.BackoffBase
		if backoffBase == 0 {
			backoffBase = 100 * time.Millisecond
		}
		backoffMax := cfg.Proxy.Retry.BackoffMax
		if backoffMax == 0 {
			backoffMax = 5 * time.Second
		}
		p.WithRetryPolicy(&proxy.RetryPolicy{
			MaxRetries:      cfg.Proxy.Retry.MaxRetries,
			RetryableStatus: retryStatuses,
			Backoff: &proxy.ExponentialBackoff{
				Base:   backoffBase,
				Max:    backoffMax,
				Jitter: true,
			},
		})
	}

	if cfg.Proxy.DefaultMode != "" {
		slog.Info("proxy default mode configured", "mode", cfg.Proxy.DefaultMode)
	}
	return p
}

func initMiddleware(cfg *config.Config, rt *router.Router, px *proxy.Proxy) (gateway.Handler, *middleware.CircuitBreaker, *middleware.RateLimiter) {
	rl := middleware.NewRateLimiterWithConfig(
		cfg.Middleware.RateLimit.RequestsPerSecond,
		cfg.Middleware.RateLimit.Burst,
		cfg.Middleware.RateLimit.MaxBuckets,
		cfg.Middleware.RateLimit.CleanupInterval,
		cfg.Middleware.RateLimit.BucketExpiry,
	)
	cb := middleware.NewCircuitBreaker(
		cfg.Middleware.CircuitBreaker.FailureThreshold,
		cfg.Middleware.CircuitBreaker.SuccessThreshold,
		cfg.Middleware.CircuitBreaker.Timeout,
	)

	chain := middleware.NewChain()
	chain = chain.Use(middleware.RequestID)
	chain = chain.Use(middleware.TraceWithConfig(middleware.TraceConfig{
		ServiceName: cfg.Middleware.Trace.ServiceName,
		Exporter:    middleware.NewOTelHTTPExporter(cfg.Middleware.Trace.Endpoint, cfg.Middleware.Trace.ServiceName),
	}))
	if cfg.Logging.AccessLog {
		chain = chain.Use(middleware.AccessLog)
	}
	chain = chain.Use(middleware.Auth(buildAuthConfig(cfg)))
	chain = chain.Use(middleware.RateLimit(rl))
	chain = chain.Use(middleware.CORS(middleware.CORSOptions{
		AllowOrigins:     cfg.Middleware.CORS.AllowOrigins,
		AllowMethods:     cfg.Middleware.CORS.AllowMethods,
		AllowHeaders:     cfg.Middleware.CORS.AllowHeaders,
		AllowCredentials: cfg.Middleware.CORS.AllowCredentials,
		ExposeHeaders:    cfg.Middleware.CORS.ExposeHeaders,
		MaxAge:           cfg.Middleware.CORS.MaxAge,
	}))
	chain = chain.Use(middleware.CircuitBreakerMiddleware(cb))

	if len(cfg.Middleware.Tenant.Tenants) > 0 {
		chain = chain.Use(middleware.TenantIsolation(cfg.Middleware.Tenant))
	}

	return chain.Then(buildHandler(rt, px)), cb, rl
}

func initHealthChecker(cfg *config.Config, rt *router.Router, px *proxy.Proxy, recoverable *lifecycle.Recoverable) *lifecycle.HealthChecker {
	hc := lifecycle.NewHealthChecker(
		cfg.Lifecycle.HealthCheck.Interval,
		cfg.Lifecycle.HealthCheck.Timeout,
		cfg.Lifecycle.HealthCheck.UnhealthyThreshold,
	).WithCheckPath(cfg.Lifecycle.HealthCheck.Path)
	hc.OnChange(func(address string, healthy bool) {
		if healthy {
			px.Pool().MarkHealthy(address)
		} else {
			px.Pool().MarkUnhealthy(address)
		}
		rt.UpdateBackendHealth(address, healthy)
	})

	routes := config.BuildRoutes(cfg)
	for _, route := range routes {
		for _, b := range route.Backends {
			hc.Register(b.Address)
		}
	}

	if recoverable != nil {
		recoverable.Go("health-checker", func(ctx context.Context) error {
			return hc.Run(ctx)
		})
	} else {
		go func() {
			hc.Run(context.Background())
		}()
	}
	return hc
}

func initConfigWatcher(store *config.Store, rt *router.Router, hc *lifecycle.HealthChecker, recoverable *lifecycle.Recoverable, graceful *lifecycle.Graceful, rl *middleware.RateLimiter, cb *middleware.CircuitBreaker) {
	watcher := config.NewFileWatcher(store, store.Get().ConfigStore.WatchInterval)
	watcher.OnChange(func(oldCfg, newCfg *config.Config) {
		slog.Info("config changed, applying hot reload")
		newRoutes := config.BuildRoutes(newCfg)
		newRt := router.NewRouterWithConfig(
			newCfg.Router.ConsistentHash.VirtualNodes,
			newCfg.Router.HeaderRoute.Header,
		)
		for _, route := range newRoutes {
			newRt.AddRoute(route)
		}
		rt.SwapRoutes(newRt)

		hc.StopAll()
		for _, route := range newRoutes {
			for _, b := range route.Backends {
				hc.Register(b.Address)
			}
		}

		rl.UpdateRate(newCfg.Middleware.RateLimit.RequestsPerSecond)
		rl.UpdateBurst(newCfg.Middleware.RateLimit.Burst)
		cb.UpdateConfig(
			newCfg.Middleware.CircuitBreaker.FailureThreshold,
			newCfg.Middleware.CircuitBreaker.SuccessThreshold,
			newCfg.Middleware.CircuitBreaker.Timeout,
		)

		slog.Info("config hot reload applied", "routes", len(newRoutes))
	})

	if recoverable != nil {
		recoverable.Go("config-watcher", func(ctx context.Context) error {
			return watcher.Start(ctx)
		})
	} else {
		go func() {
			watcher.Start(context.Background())
		}()
	}

	graceful.OnShutdown(func() error {
		slog.Info("stopping config watcher")
		watcher.Stop()
		return nil
	})
}

func initListeners(cfg *config.Config, parser *httparser.Parser, gw *gateway.Gateway, px *proxy.Proxy, rt *router.Router, graceful *lifecycle.Graceful) {
	listener, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		slog.Error("failed to listen", "address", cfg.Server.Listen, "error", err)
		os.Exit(1)
	}
	graceful.OnShutdown(func() error {
		slog.Info("closing listener")
		listener.Close()
		return nil
	})
	slog.Info("NexusGate listening", "address", cfg.Server.Listen)
	go acceptConnections(listener, parser, gw, px, rt)

	if cfg.Server.TLSListen != "" && cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
		tlsListener, err := gateway.NewTLSListenerWithClientAuth(cfg.Server.TLSListen, gateway.TLSConfig{
			CertFile:     cfg.Server.TLSCert,
			KeyFile:      cfg.Server.TLSKey,
			ClientCAFile: cfg.Server.TLSClientCA,
			ClientVerify: cfg.Server.TLSClientVerify,
		})
		if err != nil {
			slog.Error("failed to create TLS listener", "address", cfg.Server.TLSListen, "error", err)
		} else {
			graceful.OnShutdown(func() error {
				slog.Info("closing TLS listener")
				tlsListener.Close()
				return nil
			})
			slog.Info("NexusGate TLS listening", "address", cfg.Server.TLSListen)
			go acceptConnections(tlsListener, parser, gw, px, rt)
		}
	}
}

func initMetricsServer(cfg *config.Config, rt *router.Router, graceful *lifecycle.Graceful) {
	if cfg.Server.MetricsListen == "" {
		return
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", middleware.MetricsHandler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ok","version":%q,"commit":%q,"uptime":%q}`, version, gitCommit, time.Since(startTime).Truncate(time.Second))
		})
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if rt != nil && rt.RouteCount() > 0 {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `{"status":"ready","routes":%d}`, rt.RouteCount())
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, `{"status":"not ready","routes":0}`)
			}
		})

		pprofMux := http.NewServeMux()
		pprofMux.HandleFunc("/", pprof.Index)
		pprofMux.HandleFunc("/cmdline", pprof.Cmdline)
		pprofMux.HandleFunc("/profile", pprof.Profile)
		pprofMux.HandleFunc("/symbol", pprof.Symbol)
		pprofMux.HandleFunc("/trace", pprof.Trace)
		mux.Handle("/debug/pprof/", http.StripPrefix("/debug/pprof", tokenAuthMiddleware(cfg.Server.DashboardToken, pprofMux)))

		slog.Info("NexusGate metrics listening", "address", cfg.Server.MetricsListen)
		if err := http.ListenAndServe(cfg.Server.MetricsListen, mux); err != nil {
			slog.Error("metrics server error", "error", err)
		}
	}()
}

func tokenAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == token {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func initDashboardServer(cfg *config.Config, store *config.Store, hc *lifecycle.HealthChecker, rt *router.Router, gw *gateway.Gateway, cb *middleware.CircuitBreaker, graceful *lifecycle.Graceful) {
	if cfg.Server.DashboardListen == "" {
		return
	}
	go func() {
		dashSrv := dashboard.NewServer(store, hc, rt, gw, cb, version, gitCommit, buildTime, cfg.Server.DashboardToken)
		slog.Info("NexusGate dashboard listening", "address", cfg.Server.DashboardListen)
		if err := http.ListenAndServe(cfg.Server.DashboardListen, dashSrv.Handler()); err != nil {
			slog.Error("dashboard server error", "error", err)
		}
	}()
}

func acceptConnections(listener net.Listener, parser *httparser.Parser, gw *gateway.Gateway, px *proxy.Proxy, rt *router.Router) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Error("accept error", "error", err)
			continue
		}
		go handleConnection(conn, parser, gw, px, rt)
	}
}

func handleConnection(conn net.Conn, parser *httparser.Parser, gw *gateway.Gateway, px *proxy.Proxy, rt *router.Router) {
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := parser.ParseRequest(conn)
	if err != nil {
		if gwErr, ok := err.(*gateway.GatewayError); ok {
			httparser.WriteErrorResponse(conn, gwErr)
		}
		return
	}
	req.Ctx = ctx

	if proxy.IsWebSocketUpgrade(req) {
		route, backend, routeErr := rt.Route(req)
		if routeErr != nil {
			if gwErr, ok := routeErr.(*gateway.GatewayError); ok {
				httparser.WriteErrorResponse(conn, gwErr)
			} else {
				conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			}
			return
		}
		if wsErr := px.ForwardWebSocket(req, backend, route); wsErr != nil {
			if gwErr, ok := wsErr.(*gateway.GatewayError); ok {
				httparser.WriteErrorResponse(conn, gwErr)
			}
		}
		if route != nil {
			rt.Release(route.Strategy, backend.Address)
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
		slog.Debug("write response error", "error", err)
	}
}

func buildHandler(rt *router.Router, px *proxy.Proxy) gateway.Handler {
	return func(req *gateway.Request) (*gateway.Response, error) {
		route, backend, err := rt.Route(req)
		if err != nil {
			return nil, err
		}

		if route != nil && hasRewriteRules(route.Rewrite) {
			applyRequestRewrite(req, route.Rewrite)
		}

		var resp *gateway.Response

		if route != nil && route.Streaming {
			resp, err = px.ForwardStream(req, backend, route)
		} else {
			resp, err = px.Forward(req, backend, route)
		}

		if route != nil {
			rt.Release(route.Strategy, backend.Address)
		}
		if err != nil {
			return nil, err
		}

		if route != nil && hasRewriteRules(route.Rewrite) {
			resp = applyResponseRewrite(resp, route.Rewrite)
		}

		return resp, nil
	}
}

func applyRequestRewrite(req *gateway.Request, rw router.RouteRewrite) {
	if len(rw.RequestHeader.Set) > 0 || len(rw.RequestHeader.Add) > 0 || len(rw.RequestHeader.Remove) > 0 {
		applyHeaderRewrite(req.Headers, rw.RequestHeader)
	}

	if len(rw.RequestBody) > 0 && len(req.Body) > 0 {
		body := string(req.Body)
		for _, rule := range rw.RequestBody {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			body = re.ReplaceAllString(body, rule.Replacement)
		}
		req.Body = []byte(body)
	}
}

func applyResponseRewrite(resp *gateway.Response, rw router.RouteRewrite) *gateway.Response {
	if resp == nil {
		return nil
	}

	if len(rw.ResponseHeader.Set) > 0 || len(rw.ResponseHeader.Add) > 0 || len(rw.ResponseHeader.Remove) > 0 {
		applyHeaderRewrite(resp.Headers, rw.ResponseHeader)
	}

	if len(rw.ResponseBody) > 0 && len(resp.Body) > 0 {
		body := string(resp.Body)
		for _, rule := range rw.ResponseBody {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			body = re.ReplaceAllString(body, rule.Replacement)
		}
		resp.Body = []byte(body)
	}

	return resp
}

func applyHeaderRewrite(headers http.Header, rw router.HeaderRewrite) {
	if headers == nil {
		return
	}
	for _, key := range rw.Remove {
		headers.Del(key)
	}
	for key, value := range rw.Set {
		headers.Set(key, value)
	}
	for key, value := range rw.Add {
		headers.Add(key, value)
	}
}

func hasRewriteRules(rw router.RouteRewrite) bool {
	if len(rw.RequestHeader.Set) > 0 || len(rw.RequestHeader.Add) > 0 || len(rw.RequestHeader.Remove) > 0 {
		return true
	}
	if len(rw.ResponseHeader.Set) > 0 || len(rw.ResponseHeader.Add) > 0 || len(rw.ResponseHeader.Remove) > 0 {
		return true
	}
	if len(rw.RequestBody) > 0 || len(rw.ResponseBody) > 0 {
		return true
	}
	return false
}

func convertToMiddlewareRules(rules []router.RewriteRule) []middleware.RewriteRule {
	if len(rules) == 0 {
		return nil
	}
	result := make([]middleware.RewriteRule, len(rules))
	for i, r := range rules {
		result[i] = middleware.RewriteRule{
			Pattern:     r.Pattern,
			Replacement: r.Replacement,
		}
	}
	return result
}

func buildAuthConfig(cfg *config.Config) middleware.AuthConfig {
	authCfg := middleware.AuthConfig{
		Type:           middleware.AuthType(cfg.Middleware.Auth.Type),
		APIKeyHeader:   cfg.Middleware.Auth.APIKeyHeader,
		JWTHMACSecret:  cfg.Middleware.Auth.JWTHMACSecret,
		JWTAllowedAlgs: cfg.Middleware.Auth.JWTAllowedAlgs,
		SkipPaths:      cfg.Middleware.Auth.SkipPaths,
		APIKeys:        make(map[string]middleware.APIKeyEntry),
	}
	for _, k := range cfg.Middleware.Auth.APIKeys {
		authCfg.APIKeys[k.Key] = middleware.APIKeyEntry{
			TenantID: k.TenantID,
			Scopes:   k.Scopes,
			Active:   k.Active,
		}
	}
	if authCfg.Type == "" {
		authCfg.Type = middleware.AuthTypeNone
	}
	return authCfg
}
