package dashboard

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nexusgate/nexusgate/internal/config"
	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/lifecycle"
	"github.com/nexusgate/nexusgate/internal/middleware"
	"github.com/nexusgate/nexusgate/internal/router"
	"gopkg.in/yaml.v3"
)

type Server struct {
	mu                 sync.RWMutex
	store              *config.Store
	hc                 *lifecycle.HealthChecker
	rt                 *router.Router
	gw                 *gateway.Gateway
	cb                 *middleware.CircuitBreaker
	version            string
	commit             string
	buildTime          string
	startTime          time.Time
	authToken          string
	sessions           map[string]*session
	loginAttempts      map[string]*loginAttempt
	maxLoginAttempts   int
	loginBlockDuration time.Duration
	loginWindowDuration time.Duration
	sseKeepaliveInterval time.Duration
}

type session struct {
	token     string
	createdAt time.Time
	expiresAt time.Time
}

type loginAttempt struct {
	count     int
	lastTry   time.Time
	blockedUntil time.Time
}

const (
	sessionDuration = 24 * time.Hour
)

func NewServer(store *config.Store, hc *lifecycle.HealthChecker, rt *router.Router, gw *gateway.Gateway, cb *middleware.CircuitBreaker, ver, commit, bt, authToken string) *Server {
	if authToken == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			panic("nexusgate: failed to generate random token: " + err.Error())
		}
		authToken = hex.EncodeToString(b)
		slog.Warn("dashboard_token not configured, generated random token",
			"action", "set server.dashboard_token in config to use a fixed token",
		)
	}
	cfg := store.Get()
	maxAttempts := cfg.Server.MaxLoginAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	blockDur := cfg.Server.LoginBlockDuration
	if blockDur <= 0 {
		blockDur = 15 * time.Minute
	}
	windowDur := cfg.Server.LoginWindowDuration
	if windowDur <= 0 {
		windowDur = 5 * time.Minute
	}
	keepalive := cfg.Server.SSEKeepaliveInterval
	if keepalive <= 0 {
		keepalive = 30 * time.Second
	}
	return &Server{
		store:               store,
		hc:                  hc,
		rt:                  rt,
		gw:                  gw,
		cb:                  cb,
		version:             ver,
		commit:              commit,
		buildTime:           bt,
		startTime:           time.Now(),
		authToken:           authToken,
		sessions:            make(map[string]*session),
		loginAttempts:       make(map[string]*loginAttempt),
		maxLoginAttempts:    maxAttempts,
		loginBlockDuration:  blockDur,
		loginWindowDuration: windowDur,
		sseKeepaliveInterval: keepalive,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/overview", s.handleOverview)
	mux.HandleFunc("/api/v1/routes", s.handleRoutes)
	mux.HandleFunc("/api/v1/routes/", s.handleRouteDetail)
	mux.HandleFunc("/api/v1/backends", s.handleBackends)
	mux.HandleFunc("/api/v1/config", s.handleConfig)
	mux.HandleFunc("/api/v1/topology", s.handleTopology)
	mux.HandleFunc("/api/v1/gateway", s.handleGateway)
	mux.HandleFunc("/api/v1/auth", s.handleAuth)
	mux.HandleFunc("/api/v1/circuitbreaker/reset", s.handleCircuitBreakerReset)
	mux.HandleFunc("/api/v1/ratelimit", s.handleRateLimit)
	mux.HandleFunc("/api/v1/docs", s.handleDocs)
	mux.HandleFunc("/api/v1/config/schema", s.handleConfigSchema)
	mux.HandleFunc("/api/v1/logs/stream", s.handleLogStream)
	mux.HandleFunc("/api/v1/config/edit", s.handleConfigEdit)
	mux.HandleFunc("/api/v1/metrics/prometheus", s.handlePrometheusMetrics)
	mux.HandleFunc("/api/v1/tenants", s.handleTenants)
	mux.HandleFunc("/api/v1/tenants/", s.handleTenantDetail)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(getStaticFS())))
	mux.HandleFunc("/", s.handleIndex)

	return s.authMiddleware(mux)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth" {
			next.ServeHTTP(w, r)
			return
		}

		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/static/") {
			if s.validateSessionCookie(r) {
				next.ServeHTTP(w, r)
				return
			}
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte(loginPageHTML))
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" && subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.authToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}

		if s.validateSessionCookie(r) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="NexusGate Dashboard"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (s *Server) validateSessionCookie(r *http.Request) bool {
	cookie, err := r.Cookie("nexusgate_session")
	if err != nil {
		return false
	}

	s.mu.Lock()
	sess, ok := s.sessions[cookie.Value]
	if !ok {
		s.mu.Unlock()
		return false
	}

	if time.Now().After(sess.expiresAt) {
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	return true
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := r.Header.Get("X-Real-IP")
	if clientIP == "" {
		clientIP = r.Header.Get("X-Forwarded-For")
	}
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}

	s.mu.Lock()
	attempt, ok := s.loginAttempts[clientIP]
	if !ok {
		attempt = &loginAttempt{}
		s.loginAttempts[clientIP] = attempt
	}

	if !attempt.blockedUntil.IsZero() && time.Now().Before(attempt.blockedUntil) {
		s.mu.Unlock()
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Until(attempt.blockedUntil).Seconds())))
		writeJSON(w, map[string]interface{}{
			"error":  "too many login attempts",
			"retry_after": int(time.Until(attempt.blockedUntil).Seconds()),
		})
		return
	}

	if time.Since(attempt.lastTry) > s.loginWindowDuration {
		attempt.count = 0
	}
	attempt.lastTry = time.Now()
	s.mu.Unlock()

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.authToken == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(s.authToken)) != 1 {
		s.mu.Lock()
		attempt.count++
		if attempt.count >= s.maxLoginAttempts {
			attempt.blockedUntil = time.Now().Add(s.loginBlockDuration)
			slog.Warn("dashboard login blocked due to too many attempts",
				"client_ip", clientIP,
				"attempts", attempt.count,
				"blocked_until", attempt.blockedUntil,
			)
		}
		s.mu.Unlock()

		writeJSON(w, map[string]interface{}{"error": "invalid token"})
		return
	}

	s.mu.Lock()
	attempt.count = 0
	attempt.blockedUntil = time.Time{}

	sessionToken := generateSessionToken()
	s.sessions[sessionToken] = &session{
		token:     sessionToken,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(sessionDuration),
	}
	s.mu.Unlock()

	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	http.SetCookie(w, &http.Cookie{
		Name:     "nexusgate_session",
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, map[string]interface{}{"status": "ok"})
}

func generateSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := s.store.Get()
	status := s.hc.Status()

	healthyCount := 0
	unhealthyCount := 0
	for _, bh := range status {
		if bh.Healthy {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	gatewayData := map[string]interface{}{
		"shardCount":            cfg.Gateway.ShardCount,
		"workerPerShard":        cfg.Gateway.WorkerPerShard,
		"queueSize":             cfg.Gateway.QueueSize,
		"slowRecoveryThreshold": cfg.Gateway.SlowRecoveryThreshold,
	}

	if s.gw != nil {
		stats := s.gw.Stats()
		var totalPending int64
		for _, p := range stats {
			totalPending += p
		}
		gatewayData["totalPending"] = totalPending
	}

	writeJSON(w, map[string]interface{}{
		"version":           s.version,
		"commit":            s.commit,
		"buildTime":         s.buildTime,
		"uptime":            time.Since(s.startTime).Truncate(time.Second).String(),
		"routes":            len(cfg.Routes),
		"backends":          len(status),
		"healthyBackends":   healthyCount,
		"unhealthyBackends": unhealthyCount,
		"gateway":           gatewayData,
	})
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listRoutes(w, r)
	case http.MethodPost:
		s.createRoute(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listRoutes(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	type routeInfo struct {
		ID       string                  `json:"id"`
		Match    config.RouteMatchConfig `json:"match"`
		Backends []config.BackendConfig  `json:"backends"`
		Strategy string                  `json:"strategy"`
	}

	routes := make([]routeInfo, 0, len(cfg.Routes))
	for i, rc := range cfg.Routes {
		routes = append(routes, routeInfo{
			ID:       formatRouteID(i, rc.Match.PathPrefix),
			Match:    rc.Match,
			Backends: rc.Backend,
			Strategy: rc.Strategy,
		})
	}

	writeJSON(w, map[string]interface{}{
		"routes": routes,
		"total":  len(routes),
	})
}

func (s *Server) createRoute(w http.ResponseWriter, r *http.Request) {
	var rc config.RouteConfig
	if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if rc.Match.PathPrefix == "" && rc.Match.PathExact == "" && rc.Match.PathRegex == "" {
		writeJSONError(w, "match must have at least one path matcher", http.StatusBadRequest)
		return
	}
	if len(rc.Backend) == 0 {
		writeJSONError(w, "at least one backend is required", http.StatusBadRequest)
		return
	}

	cfg := s.store.Get()
	if !cfg.ConfigStore.AllowPrivateBackends {
		for _, b := range rc.Backend {
			if config.IsPrivateAddress(b.Address) {
				writeJSONError(w, "backend address "+b.Address+" is a private/internal address (SSRF risk)", http.StatusBadRequest)
				return
			}
		}
	}

	s.store.Update(func(cfg *config.Config) {
		cfg.Routes = append(cfg.Routes, rc)
	})
	s.store.Commit()

	s.reloadRouter()

	cfg = s.store.Get()
	writeJSON(w, map[string]interface{}{
		"status": "created",
		"id":     fmt.Sprintf("route_%d", len(cfg.Routes)-1),
	})
}

func (s *Server) handleRouteDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := "/api/v1/routes/"
	if !strings.HasPrefix(path, prefix) {
		http.NotFound(w, r)
		return
	}

	subPath := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(subPath, "/", 2)

	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	routeID := parts[0]

	if len(parts) == 2 && parts[1] == "backends" {
		s.handleRouteBackends(w, r, routeID)
		return
	}

	if len(parts) == 2 && strings.HasPrefix(parts[1], "backends/") {
		backendAddr := strings.TrimPrefix(parts[1], "backends/")
		s.handleRouteBackendDetail(w, r, routeID, backendAddr)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getRoute(w, r, routeID)
	case http.MethodPut:
		s.updateRoute(w, r, routeID)
	case http.MethodDelete:
		s.deleteRoute(w, r, routeID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getRoute(w http.ResponseWriter, r *http.Request, routeID string) {
	idx, err := parseRouteIndex(routeID)
	if err != nil {
		writeJSONError(w, "invalid route id", http.StatusBadRequest)
		return
	}

	cfg := s.store.Get()
	if idx < 0 || idx >= len(cfg.Routes) {
		writeJSONError(w, "route not found", http.StatusNotFound)
		return
	}

	rc := cfg.Routes[idx]
	writeJSON(w, map[string]interface{}{
		"id":       routeID,
		"match":    rc.Match,
		"backends": rc.Backend,
		"strategy": rc.Strategy,
	})
}

func (s *Server) updateRoute(w http.ResponseWriter, r *http.Request, routeID string) {
	idx, err := parseRouteIndex(routeID)
	if err != nil {
		writeJSONError(w, "invalid route id", http.StatusBadRequest)
		return
	}

	var rc config.RouteConfig
	if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	found := false
	s.store.Update(func(cfg *config.Config) {
		if idx >= 0 && idx < len(cfg.Routes) {
			cfg.Routes[idx] = rc
			found = true
		}
	})
	s.store.Commit()

	if !found {
		writeJSONError(w, "route not found", http.StatusNotFound)
		return
	}

	s.reloadRouter()

	writeJSON(w, map[string]interface{}{"status": "updated", "id": routeID})
}

func (s *Server) deleteRoute(w http.ResponseWriter, r *http.Request, routeID string) {
	idx, err := parseRouteIndex(routeID)
	if err != nil {
		writeJSONError(w, "invalid route id", http.StatusBadRequest)
		return
	}

	found := false
	s.store.Update(func(cfg *config.Config) {
		if idx >= 0 && idx < len(cfg.Routes) {
			cfg.Routes = append(cfg.Routes[:idx], cfg.Routes[idx+1:]...)
			found = true
		}
	})
	s.store.Commit()

	if !found {
		writeJSONError(w, "route not found", http.StatusNotFound)
		return
	}

	s.reloadRouter()

	writeJSON(w, map[string]interface{}{"status": "deleted", "id": routeID})
}

func (s *Server) handleRouteBackends(w http.ResponseWriter, r *http.Request, routeID string) {
	if r.Method == http.MethodPost {
		s.addBackend(w, r, routeID)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) addBackend(w http.ResponseWriter, r *http.Request, routeID string) {
	idx, err := parseRouteIndex(routeID)
	if err != nil {
		writeJSONError(w, "invalid route id", http.StatusBadRequest)
		return
	}

	var bc config.BackendConfig
	if err := json.NewDecoder(r.Body).Decode(&bc); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if bc.Address == "" {
		writeJSONError(w, "backend address is required", http.StatusBadRequest)
		return
	}

	curCfg := s.store.Get()
	if !curCfg.ConfigStore.AllowPrivateBackends && config.IsPrivateAddress(bc.Address) {
		writeJSONError(w, "backend address "+bc.Address+" is a private/internal address (SSRF risk)", http.StatusBadRequest)
		return
	}

	s.store.Update(func(cfg *config.Config) {
		if idx >= 0 && idx < len(cfg.Routes) {
			cfg.Routes[idx].Backend = append(cfg.Routes[idx].Backend, bc)
		}
	})
	s.store.Commit()

	s.reloadRouter()

	if s.hc != nil {
		s.hc.Register(bc.Address)
	}

	writeJSON(w, map[string]interface{}{"status": "added", "address": bc.Address})
}

func (s *Server) handleRouteBackendDetail(w http.ResponseWriter, r *http.Request, routeID string, backendAddr string) {
	if r.Method == http.MethodDelete {
		s.removeBackend(w, r, routeID, backendAddr)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) removeBackend(w http.ResponseWriter, r *http.Request, routeID string, backendAddr string) {
	idx, err := parseRouteIndex(routeID)
	if err != nil {
		writeJSONError(w, "invalid route id", http.StatusBadRequest)
		return
	}

	s.store.Update(func(cfg *config.Config) {
		if idx >= 0 && idx < len(cfg.Routes) {
			for j, b := range cfg.Routes[idx].Backend {
				if b.Address == backendAddr {
					cfg.Routes[idx].Backend = append(cfg.Routes[idx].Backend[:j], cfg.Routes[idx].Backend[j+1:]...)
					break
				}
			}
		}
	})
	s.store.Commit()

	s.reloadRouter()

	writeJSON(w, map[string]interface{}{"status": "removed", "address": backendAddr})
}

func (s *Server) handleCircuitBreakerReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.cb != nil {
		s.cb.Reset()
		writeJSON(w, map[string]interface{}{
			"status":  "reset",
			"message": "circuit breaker state has been reset",
		})
		return
	}

	writeJSONError(w, "circuit breaker not configured", http.StatusNotFound)
}

func (s *Server) handleRateLimit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		RequestsPerSecond float64 `json:"requests_per_second"`
		Burst             int     `json:"burst"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.RequestsPerSecond <= 0 {
		writeJSONError(w, "requests_per_second must be positive", http.StatusBadRequest)
		return
	}
	if body.Burst <= 0 {
		writeJSONError(w, "burst must be positive", http.StatusBadRequest)
		return
	}

	s.store.Update(func(cfg *config.Config) {
		cfg.Middleware.RateLimit.RequestsPerSecond = body.RequestsPerSecond
		cfg.Middleware.RateLimit.Burst = body.Burst
	})
	s.store.Commit()

	writeJSON(w, map[string]interface{}{
		"status":             "updated",
		"requests_per_second": body.RequestsPerSecond,
		"burst":              body.Burst,
	})
}

func (s *Server) reloadRouter() {
	if s.rt == nil {
		return
	}

	cfg := s.store.Get()
	newRt := router.NewRouterWithConfig(
		cfg.Router.ConsistentHash.VirtualNodes,
		cfg.Router.HeaderRoute.Header,
	)
	routes := config.BuildRoutes(cfg)
	for _, route := range routes {
		newRt.AddRoute(route)
	}
	s.rt.SwapRoutes(newRt)
}

func parseRouteIndex(id string) (int, error) {
	if !strings.HasPrefix(id, "route_") {
		return -1, fmt.Errorf("invalid route id format")
	}
	numStr := strings.TrimPrefix(id, "route_")
	var idx int
	if _, err := fmt.Sscanf(numStr, "%d", &idx); err != nil {
		return -1, fmt.Errorf("invalid route id format")
	}
	return idx, nil
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	doc := buildOpenAPIDoc(s.version)
	writeJSON(w, doc)
}

func (s *Server) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, configSchema)
}

func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.WriteHeader(statusCode)
	writeJSON(w, map[string]interface{}{"error": message})
}

func (s *Server) handleConfigEdit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.store.Get()
		sanitized := sanitizeConfig(cfg)
		writeJSON(w, map[string]interface{}{
			"config":  sanitized,
			"format":  "yaml",
			"version": s.version,
		})
	case http.MethodPut:
		var body struct {
			Config string `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.Config == "" {
			writeJSONError(w, "config content is required", http.StatusBadRequest)
			return
		}

		var newCfg config.Config
		if err := yaml.Unmarshal([]byte(body.Config), &newCfg); err != nil {
			writeJSONError(w, fmt.Sprintf("invalid YAML: %v", err), http.StatusBadRequest)
			return
		}

		if err := validateConfigFromDashboard(&newCfg); err != nil {
			writeJSONError(w, fmt.Sprintf("validation failed: %v", err), http.StatusBadRequest)
			return
		}

		s.store.Update(func(cfg *config.Config) {
			*cfg = newCfg
		})
		s.store.Commit()

		s.reloadRouter()

		writeJSON(w, map[string]interface{}{
			"status":  "updated",
			"message": "configuration applied successfully",
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func validateConfigFromDashboard(cfg *config.Config) error {
	if cfg.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}
	if cfg.Gateway.ShardCount <= 0 {
		return fmt.Errorf("gateway.shard_count must be positive")
	}
	if cfg.Gateway.QueueSize <= 0 {
		return fmt.Errorf("gateway.queue_size must be positive")
	}
	for i, rc := range cfg.Routes {
		if rc.Match.PathPrefix == "" && rc.Match.PathExact == "" && rc.Match.PathRegex == "" {
			return fmt.Errorf("routes[%d]: match must have at least one path matcher", i)
		}
		if len(rc.Backend) == 0 {
			return fmt.Errorf("routes[%d]: at least one backend is required", i)
		}
		for j, b := range rc.Backend {
			if b.Address == "" {
				return fmt.Errorf("routes[%d].backend[%d]: address is required", i, j)
			}
			if !cfg.ConfigStore.AllowPrivateBackends && config.IsPrivateAddress(b.Address) {
				return fmt.Errorf("routes[%d].backend[%d]: address %q is a private/internal address (SSRF risk)", i, j, b.Address)
			}
		}
	}
	return nil
}

func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	middleware.MetricsHandler().ServeHTTP(w, r)
}

func (s *Server) handleBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.hc.Status()
	type backendInfo struct {
		Address          string    `json:"address"`
		Healthy          bool      `json:"healthy"`
		ConsecutiveFails int       `json:"consecutiveFails"`
		LastCheck        time.Time `json:"lastCheck"`
	}

	backends := make([]backendInfo, 0, len(status))
	for addr, bh := range status {
		backends = append(backends, backendInfo{
			Address:          addr,
			Healthy:          bh.Healthy,
			ConsecutiveFails: bh.ConsecutiveFails,
			LastCheck:        bh.LastCheck,
		})
	}

	writeJSON(w, map[string]interface{}{
		"backends": backends,
		"total":    len(backends),
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := s.store.Get()
	sanitized := sanitizeConfig(cfg)
	writeJSON(w, sanitized)
}

func sanitizeConfig(cfg *config.Config) *config.Config {
	cp := *cfg

	cp.Server = cfg.Server
	cp.Server.DashboardToken = "***"

	cp.Middleware = cfg.Middleware
	cp.Middleware.Auth = cfg.Middleware.Auth
	cp.Middleware.Auth.JWTHMACSecret = "***"
	sanitizedKeys := make([]config.APIKeyConfig, 0, len(cfg.Middleware.Auth.APIKeys))
	for _, k := range cfg.Middleware.Auth.APIKeys {
		sanitizedKeys = append(sanitizedKeys, config.APIKeyConfig{
			Key:      maskSecret(k.Key),
			TenantID: k.TenantID,
			Scopes:   k.Scopes,
			Active:   k.Active,
		})
	}
	cp.Middleware.Auth.APIKeys = sanitizedKeys

	cp.ConfigStore = cfg.ConfigStore
	cp.ConfigStore.Etcd = cfg.ConfigStore.Etcd
	cp.ConfigStore.Etcd.Password = "***"

	return &cp
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

type topologyNode struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Type    string `json:"type"`
	Healthy bool   `json:"healthy,omitempty"`
	Weight  int    `json:"weight,omitempty"`
}

type topologyEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := s.store.Get()
	status := s.hc.Status()

	var nodes []topologyNode
	var edges []topologyEdge

	nodes = append(nodes, topologyNode{
		ID:    "gateway",
		Label: "NexusGate",
		Type:  "gateway",
	})

	for i, rc := range cfg.Routes {
		routeID := formatRouteID(i, rc.Match.PathPrefix)
		routeLabel := rc.Match.PathPrefix
		if routeLabel == "" {
			routeLabel = rc.Match.PathExact
		}
		if routeLabel == "" {
			routeLabel = "/"
		}

		nodes = append(nodes, topologyNode{
			ID:    routeID,
			Label: routeLabel,
			Type:  "route",
		})
		edges = append(edges, topologyEdge{
			ID:     "gw_" + routeID,
			Source: "gateway",
			Target: routeID,
			Type:   "routes_to",
		})

		for _, bc := range rc.Backend {
			healthy := true
			if bh, ok := status[bc.Address]; ok {
				healthy = bh.Healthy
			}
			backendID := sanitizeID(bc.Address)
			nodes = append(nodes, topologyNode{
				ID:      backendID,
				Label:   bc.Address,
				Type:    "backend",
				Healthy: healthy,
				Weight:  bc.Weight,
			})
			edges = append(edges, topologyEdge{
				ID:     routeID + "_" + backendID,
				Source: routeID,
				Target: backendID,
				Type:   "forwards_to",
			})
		}
	}

	writeJSON(w, map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
	})
}

func (s *Server) handleGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := s.store.Get()

	result := map[string]interface{}{
		"config": map[string]interface{}{
			"shardCount":            cfg.Gateway.ShardCount,
			"workerPerShard":        cfg.Gateway.WorkerPerShard,
			"queueSize":             cfg.Gateway.QueueSize,
			"slowRecoveryThreshold": cfg.Gateway.SlowRecoveryThreshold,
		},
	}

	if s.gw != nil {
		stats := s.gw.Stats()
		type shardInfo struct {
			ID          int   `json:"id"`
			Pending     int64 `json:"pending"`
			QueueSize   int   `json:"queueSize"`
			Utilization float64 `json:"utilization"`
		}

		shards := make([]shardInfo, 0, len(stats))
		var totalPending int64
		for i := 0; i < cfg.Gateway.ShardCount; i++ {
			pending := stats[i]
			totalPending += pending
			utilization := 0.0
			if cfg.Gateway.QueueSize > 0 {
				utilization = float64(pending) / float64(cfg.Gateway.QueueSize) * 100
			}
			shards = append(shards, shardInfo{
				ID:          i,
				Pending:     pending,
				QueueSize:   cfg.Gateway.QueueSize,
				Utilization: utilization,
			})
		}

		result["runtime"] = map[string]interface{}{
			"totalPending": totalPending,
			"shards":       shards,
		}
	}

	writeJSON(w, result)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(getStaticFS(), "index.html")
	if err != nil {
		http.Error(w, "dashboard not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(data)
}

func formatRouteID(idx int, prefix string) string {
	if prefix != "" {
		return "route_" + sanitizeID(prefix)
	}
	return fmt.Sprintf("route_%d", idx)
}

func sanitizeID(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return strings.Trim(s, "_")
}

func (s *Server) handleTenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listTenants(w, r)
	case http.MethodPost:
		s.createTenant(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listTenants(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	writeJSON(w, map[string]interface{}{
		"tenants":     cfg.Middleware.Tenant.Tenants,
		"header_name": cfg.Middleware.Tenant.HeaderName,
		"default":     cfg.Middleware.Tenant.DefaultTenant,
		"total":       len(cfg.Middleware.Tenant.Tenants),
	})
}

func (s *Server) createTenant(w http.ResponseWriter, r *http.Request) {
	var tc middleware.TenantConfig
	if err := json.NewDecoder(r.Body).Decode(&tc); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if tc.ID == "" {
		writeJSONError(w, "tenant id is required", http.StatusBadRequest)
		return
	}
	if tc.RateLimitRPS <= 0 {
		tc.RateLimitRPS = 100
	}
	if tc.RateLimitBurst <= 0 {
		tc.RateLimitBurst = 200
	}

	s.store.Update(func(cfg *config.Config) {
		cfg.Middleware.Tenant.Tenants = append(cfg.Middleware.Tenant.Tenants, tc)
	})
	s.store.Commit()

	writeJSON(w, map[string]interface{}{"status": "created", "id": tc.ID})
}

func (s *Server) handleTenantDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := "/api/v1/tenants/"
	if !strings.HasPrefix(path, prefix) {
		http.NotFound(w, r)
		return
	}

	tenantID := strings.TrimPrefix(path, prefix)

	switch r.Method {
	case http.MethodPut:
		s.updateTenant(w, r, tenantID)
	case http.MethodDelete:
		s.deleteTenant(w, r, tenantID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) updateTenant(w http.ResponseWriter, r *http.Request, tenantID string) {
	var tc middleware.TenantConfig
	if err := json.NewDecoder(r.Body).Decode(&tc); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	tc.ID = tenantID

	found := false
	s.store.Update(func(cfg *config.Config) {
		for i, t := range cfg.Middleware.Tenant.Tenants {
			if t.ID == tenantID {
				cfg.Middleware.Tenant.Tenants[i] = tc
				found = true
				return
			}
		}
	})
	s.store.Commit()

	if !found {
		writeJSONError(w, "tenant not found", http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]interface{}{"status": "updated", "id": tenantID})
}

func (s *Server) deleteTenant(w http.ResponseWriter, r *http.Request, tenantID string) {
	found := false
	s.store.Update(func(cfg *config.Config) {
		for i, t := range cfg.Middleware.Tenant.Tenants {
			if t.ID == tenantID {
				cfg.Middleware.Tenant.Tenants = append(cfg.Middleware.Tenant.Tenants[:i], cfg.Middleware.Tenant.Tenants[i+1:]...)
				found = true
				return
			}
		}
	})
	s.store.Commit()

	if !found {
		writeJSONError(w, "tenant not found", http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]interface{}{"status": "deleted", "id": tenantID})
}

const loginPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>NexusGate - Login</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh}
.card{background:#1e293b;border-radius:12px;padding:32px;border:1px solid #334155;width:360px}
h1{font-size:20px;font-weight:600;color:#38bdf8;margin-bottom:24px;text-align:center}
input{width:100%;padding:10px 12px;border:1px solid #475569;border-radius:8px;background:#0f172a;color:#e2e8f0;font-size:14px;margin-bottom:16px}
button{width:100%;padding:10px;border:none;border-radius:8px;background:#2563eb;color:#fff;font-size:14px;font-weight:600;cursor:pointer}
button:hover{background:#1d4ed8}
.error{color:#f87171;font-size:13px;margin-bottom:12px;text-align:center;display:none}
</style>
</head>
<body>
<div class="card">
<h1>NexusGate</h1>
<div class="error" id="err">Invalid token</div>
<input type="password" id="token" placeholder="Enter dashboard token" autofocus>
<button onclick="login()">Login</button>
</div>
<script>
function login(){var t=document.getElementById('token').value;fetch('/api/v1/auth',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:t})}).then(r=>{if(r.ok){window.location.href='/'}else{document.getElementById('err').style.display='block'}}).catch(()=>{document.getElementById('err').style.display='block'})}
document.getElementById('token').addEventListener('keydown',function(e){if(e.key==='Enter')login()})
</script>
</body>
</html>`
