package router

import (
	"testing"
)

func TestWeightedRRDistribution(t *testing.T) {
	wrr := NewWeightedRR()

	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 3, Healthy: true},
		{Address: "10.0.0.2:8080", Weight: 1, Healthy: true},
	}

	counts := make(map[string]int)
	for i := 0; i < 40; i++ {
		b, err := wrr.Select("key", backends)
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		counts[b.Address]++
	}

	ratio := float64(counts["10.0.0.1:8080"]) / float64(counts["10.0.0.2:8080"])
	if ratio < 2.0 || ratio > 4.0 {
		t.Errorf("expected ~3:1 ratio, got %d:%d = %.1f:1",
			counts["10.0.0.1:8080"], counts["10.0.0.2:8080"], ratio)
	}
}

func TestWeightedRRSingleBackend(t *testing.T) {
	wrr := NewWeightedRR()
	backends := []*Backend{
		{Address: "10.0.0.1:8080", Weight: 1, Healthy: true},
	}

	for i := 0; i < 10; i++ {
		b, err := wrr.Select("key", backends)
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		if b.Address != "10.0.0.1:8080" {
			t.Errorf("expected 10.0.0.1:8080, got %s", b.Address)
		}
	}
}

func TestWeightedRREmpty(t *testing.T) {
	wrr := NewWeightedRR()
	_, err := wrr.Select("key", []*Backend{})
	if err == nil {
		t.Error("expected error for empty backends, got nil")
	}
}

func TestWeightedRREqualWeights(t *testing.T) {
	wrr := NewWeightedRR()
	backends := []*Backend{
		{Address: "A", Weight: 1, Healthy: true},
		{Address: "B", Weight: 1, Healthy: true},
	}

	counts := make(map[string]int)
	for i := 0; i < 20; i++ {
		b, _ := wrr.Select("key", backends)
		counts[b.Address]++
	}

	if counts["A"] != 10 || counts["B"] != 10 {
		t.Errorf("expected 10:10 for equal weights, got %d:%d", counts["A"], counts["B"])
	}
}
