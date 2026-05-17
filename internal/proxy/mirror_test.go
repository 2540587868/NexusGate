package proxy

import (
	"net/http"
	"testing"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/router"
)

func TestNewRequestMirrorDefaults(t *testing.T) {
	px := NewProxy(10, 5)
	cfg := MirrorConfig{}
	rm := NewRequestMirror(px, cfg)

	if rm.config.Timeout != 5*time.Second {
		t.Errorf("default Timeout = %v, want 5s", rm.config.Timeout)
	}
	if rm.config.SampleRate != 1.0 {
		t.Errorf("default SampleRate = %v, want 1.0", rm.config.SampleRate)
	}
}

func TestMirrorShouldMirrorSampleRate1(t *testing.T) {
	px := NewProxy(10, 5)
	cfg := MirrorConfig{
		Backends:   []string{"http://127.0.0.1:9999"},
		SampleRate: 1.0,
	}
	rm := NewRequestMirror(px, cfg)

	for i := 0; i < 100; i++ {
		if !rm.ShouldMirror() {
			t.Errorf("ShouldMirror() = false at call %d, want always true with SampleRate 1.0", i)
		}
	}
}

func TestMirrorShouldMirrorNoBackends(t *testing.T) {
	px := NewProxy(10, 5)
	cfg := MirrorConfig{
		Backends:   []string{},
		SampleRate: 1.0,
	}
	rm := NewRequestMirror(px, cfg)

	if rm.ShouldMirror() {
		t.Error("ShouldMirror() = true with no backends, want false")
	}
}

func TestMirrorShouldMirrorSampleRate(t *testing.T) {
	px := NewProxy(10, 5)
	cfg := MirrorConfig{
		Backends:   []string{"http://127.0.0.1:9999"},
		SampleRate: 0.5,
	}
	rm := NewRequestMirror(px, cfg)

	total := 10000
	mirrored := 0
	for i := 0; i < total; i++ {
		if rm.ShouldMirror() {
			mirrored++
		}
	}

	ratio := float64(mirrored) / float64(total)
	if ratio < 0.4 || ratio > 0.6 {
		t.Errorf("mirrored ratio = %.2f, want roughly 0.5 (mirrored=%d total=%d)", ratio, mirrored, total)
	}
}

func TestMirrorStats(t *testing.T) {
	px := NewProxy(10, 5)
	cfg := MirrorConfig{
		Backends:   []string{"127.0.0.1:1"},
		SampleRate: 1.0,
		Timeout:    1 * time.Second,
		Async:      false,
	}
	rm := NewRequestMirror(px, cfg)

	req := &gateway.Request{
		Method:  "GET",
		Path:    "/test",
		Host:    "example.com",
		Headers: http.Header{},
		Scheme:  "http",
	}
	resp := &gateway.Response{
		StatusCode: 200,
		Headers:    http.Header{},
	}

	rm.Mirror(req, resp)

	sent, failed := rm.Stats()
	if sent != 1 {
		t.Errorf("TotalSent = %d, want 1", sent)
	}
	if failed != 1 {
		t.Errorf("TotalFailed = %d, want 1 (backend unreachable)", failed)
	}
}

func TestBuildMirrorConfig(t *testing.T) {
	route := &router.Route{
		ID:       "route_1",
		Strategy: router.StrategyWeightedRR,
		Backends: []*router.Backend{
			{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
			{Address: "10.0.0.2:9090", Weight: 1, Healthy: true, Meta: map[string]string{"mirror": "true"}},
			{Address: "10.0.0.3:7070", Weight: 1, Healthy: true, Meta: map[string]string{"mirror": "true", "canary": "v2"}},
		},
	}

	cfg := BuildMirrorConfig(route, 5*time.Second)

	if len(cfg.Backends) != 2 {
		t.Fatalf("len(Backends) = %d, want 2", len(cfg.Backends))
	}
	found := map[string]bool{}
	for _, b := range cfg.Backends {
		found[b] = true
	}
	if !found["10.0.0.2:9090"] {
		t.Error("expected backend 10.0.0.2:9090 in mirror backends")
	}
	if !found["10.0.0.3:7070"] {
		t.Error("expected backend 10.0.0.3:7070 in mirror backends")
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("SampleRate = %v, want 1.0", cfg.SampleRate)
	}
	if !cfg.Async {
		t.Error("Async = false, want true")
	}
}
