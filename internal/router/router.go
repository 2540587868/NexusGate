package router

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/util"
)

type Backend struct {
	Address string
	Weight  int
	Healthy bool
	Meta    map[string]string
}

type RouteTimeout struct {
	Connect time.Duration
	Read    time.Duration
	Write   time.Duration
	Total   time.Duration
}

type RouteRetry struct {
	MaxRetries      int
	RetryableStatus []int
}

type HeaderRewrite struct {
	Set    map[string]string
	Add    map[string]string
	Remove []string
}

type RewriteRule struct {
	Pattern     string
	Replacement string
}

type RouteRewrite struct {
	RequestHeader  HeaderRewrite
	ResponseHeader HeaderRewrite
	RequestBody    []RewriteRule
	ResponseBody   []RewriteRule
}

type Route struct {
	ID          string
	Match       MatchRule
	Backends    []*Backend
	Strategy    StrategyType
	Middlewares []string
	Canary      CanaryRule
	Timeout     RouteTimeout
	Retry       RouteRetry
	Streaming   bool
	Rewrite     RouteRewrite
}

type MatchRule struct {
	PathPrefix string
	PathExact  string
	PathRegex  string
	Methods    []string
	Headers    map[string]string
}

func compileRegex(pattern string) (*regexp.Regexp, error) {
	return util.CompileRegex(pattern)
}

type StrategyType string

const (
	StrategyConsistentHash StrategyType = "consistent_hash"
	StrategyWeightedRR     StrategyType = "weighted_round_robin"
	StrategyLeastConn      StrategyType = "least_conn"
	StrategyHeaderRoute    StrategyType = "header_route"
	StrategyIPHash         StrategyType = "ip_hash"
	StrategyCanary         StrategyType = "canary"
)

type Selector[A any] interface {
	Select(key A, backends []*Backend) (*Backend, error)
	Name() StrategyType
}

type Router struct {
	mu        sync.RWMutex
	routes    []*Route
	tree      *RadixTree
	selectors map[StrategyType]Selector[string]
	dirty     bool
}

func NewRouter() *Router {
	r := &Router{
		routes:    make([]*Route, 0),
		tree:      NewRadixTree(),
		selectors: make(map[StrategyType]Selector[string]),
		dirty:     true,
	}
	r.selectors[StrategyConsistentHash] = NewConsistentHash(150)
	r.selectors[StrategyWeightedRR] = NewWeightedRR()
	r.selectors[StrategyLeastConn] = NewLeastConn()
	r.selectors[StrategyHeaderRoute] = NewHeaderRoute("X-Service-Version")
	return r
}

func NewRouterWithConfig(virtualNodes int, headerRouteKey string) *Router {
	r := &Router{
		routes:    make([]*Route, 0),
		tree:      NewRadixTree(),
		selectors: make(map[StrategyType]Selector[string]),
		dirty:     true,
	}
	if virtualNodes <= 0 {
		virtualNodes = 150
	}
	r.selectors[StrategyConsistentHash] = NewConsistentHash(virtualNodes)
	r.selectors[StrategyWeightedRR] = NewWeightedRR()
	r.selectors[StrategyLeastConn] = NewLeastConn()
	r.selectors[StrategyIPHash] = NewIPHash()
	r.selectors[StrategyCanary] = NewCanaryStrategy(CanaryRule{})
	if headerRouteKey == "" {
		headerRouteKey = "X-Service-Version"
	}
	r.selectors[StrategyHeaderRoute] = NewHeaderRoute(headerRouteKey)
	return r
}

func (r *Router) AddRoute(route *Route) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = append(r.routes, route)
	r.dirty = true
}

func (r *Router) RemoveRoute(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, route := range r.routes {
		if route.ID == id {
			r.routes = append(r.routes[:i], r.routes[i+1:]...)
			return
		}
	}
}

func (r *Router) SwapRoutes(newRouter *Router) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = newRouter.routes
	for k, v := range newRouter.selectors {
		r.selectors[k] = v
	}
	r.dirty = true
}

func (r *Router) rebuildTree() {
	if !r.dirty {
		return
	}
	r.tree = NewRadixTree()
	for _, route := range r.routes {
		r.tree.Insert(route)
	}
	r.dirty = false
}

func (r *Router) UpdateBackendHealth(address string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, route := range r.routes {
		for _, b := range route.Backends {
			if b.Address == address {
				b.Healthy = healthy
			}
		}
	}
}

func (r *Router) Route(req *gateway.Request) (*Route, *Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	route := r.match(req)

	if route == nil {
		return nil, nil, gateway.NewGatewayError(gateway.ErrNoRoute,
			"no matching route", req.RouteKey())
	}

	healthyBackends := make([]*Backend, 0, len(route.Backends))
	for _, b := range route.Backends {
		if b.Healthy {
			healthyBackends = append(healthyBackends, b)
		}
	}
	if len(healthyBackends) == 0 {
		return route, nil, gateway.NewGatewayError(gateway.ErrBackendDown,
			"no healthy backends", route.ID)
	}

	selector, ok := r.selectors[route.Strategy]
	if !ok {
		selector = r.selectors[StrategyWeightedRR]
	}

	selectKey := req.RouteKey()
	if route.Strategy == StrategyHeaderRoute {
		if hr, ok := selector.(*HeaderRoute); ok {
			selectKey = req.Headers.Get(hr.HeaderName())
		}
	} else if route.Strategy == StrategyIPHash {
		selectKey = req.RemoteAddr
	} else if route.Strategy == StrategyCanary {
		selector = NewCanaryStrategy(route.Canary)
		selectKey = req.Headers.Get("Cookie")
		if selectKey == "" {
			selectKey = req.RemoteAddr
		}
	}

	backend, err := selector.Select(selectKey, healthyBackends)
	if err != nil {
		return route, nil, err
	}

	return route, backend, nil
}

func (r *Router) match(req *gateway.Request) *Route {
	r.rebuildTree()

	candidates := r.tree.Lookup(req.Path)

	for i := len(candidates) - 1; i >= 0; i-- {
		route := candidates[i]
		if !r.matchRule(req, &route.Match) {
			continue
		}
		if !r.matchMethod(req, route.Match.Methods) {
			continue
		}
		if len(route.Match.Headers) > 0 && !r.matchHeaders(req, route.Match.Headers) {
			continue
		}
		return route
	}

	return nil
}

func (r *Router) matchMethod(req *gateway.Request, methods []string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		if m == req.Method {
			return true
		}
	}
	return false
}

func (r *Router) matchRule(req *gateway.Request, rule *MatchRule) bool {
	if rule.PathExact != "" {
		return req.Path == rule.PathExact
	}
	if rule.PathPrefix != "" {
		return strings.HasPrefix(req.Path, rule.PathPrefix)
	}
	if rule.PathRegex != "" {
		re, err := compileRegex(rule.PathRegex)
		if err != nil {
			return false
		}
		return re.MatchString(req.Path)
	}
	return false
}

func (r *Router) matchHeaders(req *gateway.Request, headers map[string]string) bool {
	for key, expected := range headers {
		actual := req.Headers.Get(key)
		if actual != expected {
			return false
		}
	}
	return true
}

func (r *Router) Release(strategy StrategyType, address string) {
	r.mu.RLock()
	selector, ok := r.selectors[strategy]
	r.mu.RUnlock()
	if !ok {
		return
	}
	if lc, ok := selector.(*LeastConn); ok {
		lc.Release(address)
	}
}

func (r *Router) Routes() []*Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Route, len(r.routes))
	copy(result, r.routes)
	return result
}

func (r *Router) RouteCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.routes)
}

func (r *Router) UpdateBackends(routeID string, backends []*Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, route := range r.routes {
		if route.ID == routeID {
			route.Backends = backends
			return
		}
	}
}
