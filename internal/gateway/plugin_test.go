package gateway

import (
	"errors"
	"sync"
	"testing"
)

type mockPlugin struct {
	name     string
	initErr  error
	handleFn func(hook PluginHook, ctx *PluginContext) error
}

func (p *mockPlugin) Name() string { return p.name }
func (p *mockPlugin) Init(config map[string]interface{}) error { return p.initErr }
func (p *mockPlugin) Handle(hook PluginHook, ctx *PluginContext) error {
	if p.handleFn != nil {
		return p.handleFn(hook, ctx)
	}
	return nil
}

func TestPluginManagerRegister(t *testing.T) {
	pm := NewPluginManager()

	p := &mockPlugin{name: "test-plugin"}
	err := pm.Register(p, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := pm.Get("test-plugin")
	if !ok {
		t.Fatal("expected to find registered plugin")
	}
	if got.Name() != "test-plugin" {
		t.Errorf("expected plugin name 'test-plugin', got %q", got.Name())
	}
}

func TestPluginManagerRegisterInitError(t *testing.T) {
	pm := NewPluginManager()

	p := &mockPlugin{
		name:     "failing-plugin",
		initErr:  errors.New("init failed"),
	}
	err := pm.Register(p, nil)
	if err == nil {
		t.Fatal("expected error when plugin Init fails")
	}

	_, ok := pm.Get("failing-plugin")
	if ok {
		t.Error("plugin should not be registered after Init failure")
	}
}

func TestPluginManagerRegisterHook(t *testing.T) {
	pm := NewPluginManager()

	var called bool
	p := &mockPlugin{
		name: "hook-plugin",
		handleFn: func(hook PluginHook, ctx *PluginContext) error {
			called = true
			return nil
		},
	}

	pm.RegisterHook(HookPreForward, p)

	err := pm.Execute(HookPreForward, &PluginContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected hook to be called")
	}
}

func TestPluginManagerExecuteNoHooks(t *testing.T) {
	pm := NewPluginManager()

	err := pm.Execute(HookPreForward, &PluginContext{})
	if err != nil {
		t.Fatalf("expected no error with no hooks, got %v", err)
	}
}

func TestPluginManagerExecuteHookError(t *testing.T) {
	pm := NewPluginManager()

	p := &mockPlugin{
		name: "error-plugin",
		handleFn: func(hook PluginHook, ctx *PluginContext) error {
			return errors.New("hook error")
		},
	}

	pm.RegisterHook(HookOnError, p)

	err := pm.Execute(HookOnError, &PluginContext{})
	if err == nil {
		t.Fatal("expected error when hook returns error")
	}
}

func TestPluginManagerGetNotFound(t *testing.T) {
	pm := NewPluginManager()

	_, ok := pm.Get("nonexistent")
	if ok {
		t.Error("expected false for non-existent plugin")
	}
}

func TestPluginManagerList(t *testing.T) {
	pm := NewPluginManager()

	p1 := &mockPlugin{name: "plugin-a"}
	p2 := &mockPlugin{name: "plugin-b"}
	p3 := &mockPlugin{name: "plugin-c"}

	if err := pm.Register(p1, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pm.Register(p2, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pm.Register(p3, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := pm.List()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}

	found := make(map[string]bool)
	for _, n := range names {
		found[n] = true
	}
	for _, expected := range []string{"plugin-a", "plugin-b", "plugin-c"} {
		if !found[expected] {
			t.Errorf("expected %q in list", expected)
		}
	}
}

func TestPluginManagerConcurrentRegister(t *testing.T) {
	pm := NewPluginManager()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := string(rune('A' + idx%26)) + string(rune('0'+idx%10))
			p := &mockPlugin{name: name}
			_ = pm.Register(p, nil)
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pm.Execute(HookPreForward, &PluginContext{})
		}()
	}

	wg.Wait()

	names := pm.List()
	if len(names) == 0 {
		t.Error("expected some plugins to be registered after concurrent operations")
	}
}
