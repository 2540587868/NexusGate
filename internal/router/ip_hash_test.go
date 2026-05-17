package router

import (
	"testing"
)

func TestIPHashSelect(t *testing.T) {
	ih := NewIPHash()

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.3:8080", Weight: 1, Healthy: true},
	}

	first, err := ih.Select("192.168.1.100", backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := 0; i < 50; i++ {
		b, err := ih.Select("192.168.1.100", backends)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.Address != first.Address {
			t.Errorf("same key should always select same backend: first=%s, got=%s", first.Address, b.Address)
		}
	}
}

func TestIPHashDifferentKeys(t *testing.T) {
	ih := NewIPHash()

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.3:8080", Weight: 1, Healthy: true},
	}

	selected := make(map[string]bool)
	keys := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5"}

	for _, key := range keys {
		b, err := ih.Select(key, backends)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		selected[b.Address] = true
	}

	if len(selected) < 2 {
		t.Errorf("expected at least 2 different backends for different keys, got %d", len(selected))
	}
}

func TestIPHashNoBackends(t *testing.T) {
	ih := NewIPHash()

	_, err := ih.Select("192.168.1.1", []*Backend{})
	if err != ErrNoBackends {
		t.Errorf("expected ErrNoBackends, got %v", err)
	}
}

func TestIPHashSingleBackend(t *testing.T) {
	ih := NewIPHash()

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
	}

	for i := 0; i < 10; i++ {
		b, err := ih.Select("any-key", backends)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.Address != "10.0.0.1:8080" {
			t.Errorf("expected only backend, got %s", b.Address)
		}
	}
}

func TestIPHashDistribution(t *testing.T) {
	ih := NewIPHash()

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.3:8080", Weight: 1, Healthy: true},
	}

	counts := make(map[string]int)
	for i := 0; i < 3000; i++ {
		key := string(rune(i))
		b, err := ih.Select(key, backends)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		counts[b.Address]++
	}

	if len(counts) < 2 {
		t.Errorf("expected at least 2 different backends, got %d", len(counts))
	}

	for addr, count := range counts {
		ratio := float64(count) / 1000.0
		if ratio < 0.2 || ratio > 1.8 {
			t.Errorf("backend %s has unreasonable distribution: %d/3000", addr, count)
		}
	}
}
