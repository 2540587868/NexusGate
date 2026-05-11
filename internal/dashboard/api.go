package dashboard

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nexusgate/nexusgate/internal/config"
	"github.com/nexusgate/nexusgate/internal/lifecycle"
	"github.com/nexusgate/nexusgate/internal/router"
)

type Server struct {
	mu         sync.RWMutex
	store      *config.Store
	hc         *lifecycle.HealthChecker
	rt         *router.Router
	version    string
	commit     string
	buildTime  string
	startTime  time.Time
	authToken  string
}

func NewServer(store *config.Store, hc *lifecycle.HealthChecker, rt *router.Router, ver, commit, bt, authToken string) *Server {
	return &Server{
		store:     store,
		hc:        hc,
		rt:        rt,
		version:   ver,
		commit:    commit,
		buildTime: bt,
		startTime: time.Now(),
		authToken: authToken,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/overview", s.handleOverview)
	mux.HandleFunc("/api/v1/routes", s.handleRoutes)
	mux.HandleFunc("/api/v1/backends", s.handleBackends)
	mux.HandleFunc("/api/v1/config", s.handleConfig)
	mux.HandleFunc("/api/v1/topology", s.handleTopology)
	mux.HandleFunc("/api/v1/auth", s.handleAuth)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(getStaticFS())))
	mux.HandleFunc("/", s.handleIndex)

	return s.authMiddleware(mux)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		if r.URL.Path == "/api/v1/auth" {
			next.ServeHTTP(w, r)
			return
		}

		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/static/") {
			cookie, err := r.Cookie("nexusgate_token")
			if err == nil && cookie.Value == s.authToken {
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
			if len(parts) == 2 && parts[0] == "Bearer" && parts[1] == s.authToken {
				next.ServeHTTP(w, r)
				return
			}
		}

		cookie, err := r.Cookie("nexusgate_token")
		if err == nil && cookie.Value == s.authToken {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="NexusGate Dashboard"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.authToken == "" || body.Token != s.authToken {
		writeJSON(w, map[string]interface{}{"error": "invalid token"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "nexusgate_token",
		Value:    s.authToken,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, map[string]interface{}{"status": "ok"})
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

	writeJSON(w, map[string]interface{}{
		"version":          s.version,
		"commit":           s.commit,
		"buildTime":        s.buildTime,
		"uptime":           time.Since(s.startTime).Truncate(time.Second).String(),
		"routes":           len(cfg.Routes),
		"backends":         len(status),
		"healthyBackends":  healthyCount,
		"unhealthyBackends": unhealthyCount,
		"gateway": map[string]interface{}{
			"shardCount": cfg.Gateway.ShardCount,
			"queueSize":  cfg.Gateway.QueueSize,
		},
	})
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
	writeJSON(w, cfg)
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
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
function login(){var t=document.getElementById('token').value;fetch('/api/v1/auth',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:t})}).then(r=>{if(r.ok){document.cookie='nexusgate_token='+t+';path=/;max-age=86400';window.location.href='/'}else{document.getElementById('err').style.display='block'}}).catch(()=>{document.getElementById('err').style.display='block'})}
document.getElementById('token').addEventListener('keydown',function(e){if(e.key==='Enter')login()})
</script>
</body>
</html>`
