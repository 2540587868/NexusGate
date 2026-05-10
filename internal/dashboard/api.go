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
	mu        sync.RWMutex
	store     *config.Store
	hc        *lifecycle.HealthChecker
	rt        *router.Router
	version   string
	commit    string
	buildTime string
	startTime time.Time
}

func NewServer(store *config.Store, hc *lifecycle.HealthChecker, rt *router.Router, ver, commit, bt string) *Server {
	return &Server{
		store:     store,
		hc:        hc,
		rt:        rt,
		version:   ver,
		commit:    commit,
		buildTime: bt,
		startTime: time.Now(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/overview", s.handleOverview)
	mux.HandleFunc("/api/v1/routes", s.handleRoutes)
	mux.HandleFunc("/api/v1/backends", s.handleBackends)
	mux.HandleFunc("/api/v1/config", s.handleConfig)
	mux.HandleFunc("/api/v1/topology", s.handleTopology)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(getStaticFS())))
	mux.HandleFunc("/", s.handleIndex)

	return mux
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
