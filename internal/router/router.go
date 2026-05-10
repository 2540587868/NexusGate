package router

import (
	"strings"
	"sync"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type Backend struct {
	Address string
	Weight  int
	Healthy bool
	Meta    map[string]string
}

type Route struct {
	ID          string
	Match       MatchRule
	Backends    []*Backend
	Strategy    StrategyType
	Middlewares []string
}

type MatchRule struct {
	PathPrefix string
	PathExact  string
	Methods    []string
	Headers    map[string]string
}

type StrategyType string

const (
	StrategyConsistentHash StrategyType = "consistent_hash"
	StrategyWeightedRR     StrategyType = "weighted_round_robin"
	StrategyLeastConn      StrategyType = "least_conn"
	StrategyHeaderRoute    StrategyType = "header_route"
)

type Selector[A any] interface {
	Select(key A, backends []*Backend) (*Backend, error)
	Name() StrategyType
}

type Router struct {
	mu        sync.RWMutex
	routes    []*Route
	selectors map[StrategyType]Selector[string]
}

func NewRouter() *Router {
	r := &Router{
		routes:    make([]*Route, 0),
		selectors: make(map[StrategyType]Selector[string]),
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
		selectors: make(map[StrategyType]Selector[string]),
	}
	if virtualNodes <= 0 {
		virtualNodes = 150
	}
	r.selectors[StrategyConsistentHash] = NewConsistentHash(virtualNodes)
	r.selectors[StrategyWeightedRR] = NewWeightedRR()
	r.selectors[StrategyLeastConn] = NewLeastConn()
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
	}

	backend, err := selector.Select(selectKey, healthyBackends)
	if err != nil {
		return route, nil, err
	}

	return route, backend, nil
}

func (r *Router) match(req *gateway.Request) *Route {
	for _, route := range r.routes {
		if !r.matchRule(req, &route.Match) {
			continue
		}
		if len(route.Match.Methods) > 0 {
			found := false
			for _, m := range route.Match.Methods {
				if m == req.Method {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if len(route.Match.Headers) > 0 {
			if !r.matchHeaders(req, route.Match.Headers) {
				continue
			}
		}
		return route
	}
	return nil
}

func (r *Router) matchRule(req *gateway.Request, rule *MatchRule) bool {
	if rule.PathExact != "" {
		return req.Path == rule.PathExact
	}
	if rule.PathPrefix != "" {
		return strings.HasPrefix(req.Path, rule.PathPrefix)
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
