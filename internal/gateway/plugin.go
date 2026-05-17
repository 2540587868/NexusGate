package gateway

import (
	"fmt"
	"sync"
)

type PluginHook int

const (
	HookPreForward  PluginHook = iota
	HookPostForward
	HookOnError
	HookOnRouteMatch
)

type PluginContext struct {
	Request  *Request
	Response *Response
	Route    *RouteInfo
	Error    error
}

type RouteInfo struct {
	ID       string
	Path     string
	Strategy string
}

type Plugin interface {
	Name() string
	Init(config map[string]interface{}) error
	Handle(hook PluginHook, ctx *PluginContext) error
}

type PluginManager struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
	hooks   map[PluginHook][]Plugin
}

func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins: make(map[string]Plugin),
		hooks:   make(map[PluginHook][]Plugin),
	}
}

func (pm *PluginManager) Register(p Plugin, config map[string]interface{}) error {
	if err := p.Init(config); err != nil {
		return fmt.Errorf("plugin %s init failed: %w", p.Name(), err)
	}

	pm.mu.Lock()
	pm.plugins[p.Name()] = p
	pm.mu.Unlock()

	return nil
}

func (pm *PluginManager) RegisterHook(hook PluginHook, p Plugin) {
	pm.mu.Lock()
	pm.hooks[hook] = append(pm.hooks[hook], p)
	pm.mu.Unlock()
}

func (pm *PluginManager) Execute(hook PluginHook, ctx *PluginContext) error {
	pm.mu.RLock()
	plugins := make([]Plugin, len(pm.hooks[hook]))
	copy(plugins, pm.hooks[hook])
	pm.mu.RUnlock()

	for _, p := range plugins {
		if err := p.Handle(hook, ctx); err != nil {
			return fmt.Errorf("plugin %s hook %d failed: %w", p.Name(), hook, err)
		}
	}
	return nil
}

func (pm *PluginManager) Get(name string) (Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.plugins[name]
	return p, ok
}

func (pm *PluginManager) List() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	return names
}
