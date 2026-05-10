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

	rt := router.NewRouter()
	routes := config.BuildRoutes(cfg)
	for _, route := range routes {
		rt.AddRoute(route)
	}

	px := proxy.NewProxy(cfg.Proxy.PoolSize, cfg.Proxy.PoolMaxIdle).
		WithTimeouts(cfg.Proxy.ConnectTimeout, cfg.Proxy.ReadTimeout, cfg.Proxy.WriteTimeout)

	rl := middleware.NewRateLimiter(cfg.Middleware.RateLimit.RequestsPerSecond, cfg.Middleware.RateLimit.Burst)
	cb := middleware.NewCircuitBreaker(
		cfg.Middleware.CircuitBreaker.FailureThreshold,
		cfg.Middleware.CircuitBreaker.SuccessThreshold,
		cfg.Middleware.CircuitBreaker.Timeout,
	)

	chain := middleware.NewChain()

	traceExporter := middleware.NewOTelHTTPExporter(
		cfg.Middleware.Trace.Endpoint,
		cfg.Middleware.Trace.ServiceName,
	)
	chain = chain.Use(middleware.TraceWithConfig(middleware.TraceConfig{
		ServiceName: cfg.Middleware.Trace.ServiceName,
		Exporter:    traceExporter,
	}))
	chain = chain.Use(middleware.AccessLog)
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

	handler := chain.Then(buildHandler(rt, px))

	gw := gateway.NewGateway(handler, cfg.Gateway.QueueSize)

	recoverable := lifecycle.NewRecoverable(5)

	hc := lifecycle.NewHealthChecker(
		cfg.Lifecycle.HealthCheck.Interval,
		cfg.Lifecycle.HealthCheck.Timeout,
		cfg.Lifecycle.HealthCheck.UnhealthyThreshold,
	)
	hc.OnChange(func(address string, healthy bool) {
		if healthy {
			px.Pool().MarkHealthy(address)
		} else {
			px.Pool().MarkUnhealthy(address)
		}
		rt.UpdateBackendHealth(address, healthy)
	})

	for _, route := range routes {
		for _, b := range route.Backends {
			hc.Register(b.Address)
		}
	}

	recoverable.Go("health-checker", func(ctx context.Context) error {
		return hc.Run(ctx)
	})

	watcher := config.NewFileWatcher(store, 5*time.Second)
	watcher.OnChange(func(oldCfg, newCfg *config.Config) {
		slog.Info("config changed, applying hot reload")

		newRoutes := config.BuildRoutes(newCfg)
		newRt := router.NewRouter()
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

		slog.Info("config hot reload applied",
			"routes", len(newRoutes),
		)
	})

	graceful := lifecycle.NewGraceful(cfg.Lifecycle.GracefulTimeout)
	graceful.OnShutdown(func() error {
		slog.Info("stopping config watcher")
		watcher.Stop()
		return nil
	})
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

	parser := httparser.NewParser()

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

	go acceptConnections(listener, parser, gw)

	if cfg.Server.TLSListen != "" && cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
		tlsListener, err := gateway.NewTLSListener(cfg.Server.TLSListen, cfg.Server.TLSCert, cfg.Server.TLSKey)
		if err != nil {
			slog.Error("failed to create TLS listener", "address", cfg.Server.TLSListen, "error", err)
		} else {
			graceful.OnShutdown(func() error {
				slog.Info("closing TLS listener")
				tlsListener.Close()
				return nil
			})
			slog.Info("NexusGate TLS listening", "address", cfg.Server.TLSListen)
			go acceptConnections(tlsListener, parser, gw)
		}
	}

	recoverable.Go("config-watcher", func(ctx context.Context) error {
		return watcher.Start(ctx)
	})

	if cfg.Server.MetricsListen != "" {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", middleware.MetricsHandler())
			mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `{"status":"ok","version":%q,"commit":%q,"uptime":%q}`, version, gitCommit, time.Since(startTime).Truncate(time.Second))
			})
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
			slog.Info("NexusGate metrics listening", "address", cfg.Server.MetricsListen)
			if err := http.ListenAndServe(cfg.Server.MetricsListen, mux); err != nil {
				slog.Error("metrics server error", "error", err)
			}
		}()
	}

	if cfg.Server.DashboardListen != "" {
		go func() {
			dashSrv := dashboard.NewServer(store, hc, rt, version, gitCommit, buildTime)
			slog.Info("NexusGate dashboard listening", "address", cfg.Server.DashboardListen)
			if err := http.ListenAndServe(cfg.Server.DashboardListen, dashSrv.Handler()); err != nil {
				slog.Error("dashboard server error", "error", err)
			}
		}()
	}

	graceful.Wait()
}

func acceptConnections(listener net.Listener, parser *httparser.Parser, gw *gateway.Gateway) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Error("accept error", "error", err)
			continue
		}

		go handleConnection(conn, parser, gw)
	}
}

func handleConnection(conn net.Conn, parser *httparser.Parser, gw *gateway.Gateway) {
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
		slog.Debug("write response error", "error", err)
	}
}

func buildHandler(rt *router.Router, px *proxy.Proxy) gateway.Handler {
	return func(req *gateway.Request) (*gateway.Response, error) {
		_, backend, err := rt.Route(req)
		if err != nil {
			return nil, err
		}

		resp, err := px.Forward(req, backend)
		if err != nil {
			return nil, err
		}

		return resp, nil
	}
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
