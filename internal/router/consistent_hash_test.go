package router

import (
	"testing"
)

func TestConsistentHashSelect(t *testing.T) {
	ch := NewConsistentHash(150)

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.3:8080", Weight: 1, Healthy: true},
	}

	b, err := ch.Select("user-123", backends)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if b == nil {
		t.Fatal("Select() returned nil backend")
	}
}

func TestConsistentHashStickyRouting(t *testing.T) {
	ch := NewConsistentHash(150)

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.3:8080", Weight: 1, Healthy: true},
	}

	first, _ := ch.Select("user-123", backends)
	for i := 0; i < 20; i++ {
		b, _ := ch.Select("user-123", backends)
		if b.Address != first.Address {
			t.Errorf("consistent hash should route same key to same backend, got %s then %s",
				first.Address, b.Address)
		}
	}
}

func TestConsistentHashDistribution(t *testing.T) {
	ch := NewConsistentHash(150)

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
		{Address: "10.0.0.3:8080", Weight: 1, Healthy: true},
	}

	counts := make(map[string]int)
	for i := 0; i < 3000; i++ {
		key := string(rune('a' + i%26)) + string(rune('0'+i%10))
		b, _ := ch.Select(key, backends)
		counts[b.Address]++
	}

	for addr, count := range counts {
		if count < 500 {
			t.Errorf("backend %s got only %d requests, distribution may be uneven", addr, count)
		}
	}
}

func TestConsistentHashSingleBackend(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
	}

	b, err := ch.Select("any-key", backends)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if b.Address != "10.0.0.1:8080" {
		t.Errorf("expected 10.0.0.1:8080, got %s", b.Address)
	}
}

func TestConsistentHashEmpty(t *testing.T) {
	ch := NewConsistentHash(150)
	_, err := ch.Select("key", []*Backend{})
	if err == nil {
		t.Error("expected error for empty backends, got nil")
	}
}
