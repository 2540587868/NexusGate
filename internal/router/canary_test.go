package router

import (
	"testing"
)

func TestCanarySelectByHeader(t *testing.T) {
	rule := CanaryRule{
		Header: &CanaryHeaderRule{Name: "X-Canary", Value: "true"},
	}
	cs := NewCanaryStrategy(rule)

	backends := []*Backend{
		{Address: "stable:8080", Healthy: true},
		{Address: "canary:8080", Healthy: true, Meta: map[string]string{"canary": "true"}},
	}

	b, err := cs.Select("true", backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Address != "canary:8080" {
		t.Errorf("expected canary backend, got %s", b.Address)
	}
}

func TestCanarySelectByHeaderNoMatch(t *testing.T) {
	rule := CanaryRule{
		Header: &CanaryHeaderRule{Name: "X-Canary", Value: "true"},
	}
	cs := NewCanaryStrategy(rule)

	backends := []*Backend{
		{Address: "stable:8080", Healthy: true},
		{Address: "canary:8080", Healthy: true, Meta: map[string]string{"canary": "true"}},
	}

	b, err := cs.Select("false", backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Address != "stable:8080" {
		t.Errorf("expected stable backend, got %s", b.Address)
	}
}

func TestCanarySelectByCookie(t *testing.T) {
	rule := CanaryRule{
		Cookie: &CanaryCookieRule{Name: "canary", Value: "v2"},
	}
	cs := NewCanaryStrategy(rule)

	backends := []*Backend{
		{Address: "stable:8080", Healthy: true},
		{Address: "canary:8080", Healthy: true, Meta: map[string]string{"canary": "true"}},
	}

	b, err := cs.Select("canary=v2; other=abc", backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Address != "canary:8080" {
		t.Errorf("expected canary backend, got %s", b.Address)
	}
}

func TestCanarySelectByWeight(t *testing.T) {
	rule := CanaryRule{
		Weight: &CanaryWeightRule{CanaryWeight: 0, TotalWeight: 100},
	}
	cs := NewCanaryStrategy(rule)

	backends := []*Backend{
		{Address: "stable:8080", Healthy: true},
		{Address: "canary:8080", Healthy: true, Meta: map[string]string{"canary": "true"}},
	}

	stableCount := 0
	canaryCount := 0
	iterations := 1000

	for i := 0; i < iterations; i++ {
		b, err := cs.Select(string(rune(i)), backends)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.Address == "canary:8080" {
			canaryCount++
		} else {
			stableCount++
		}
	}

	if canaryCount > 0 {
		t.Errorf("with canary_weight=0, expected 0 canary selections, got %d", canaryCount)
	}
	if stableCount != iterations {
		t.Errorf("expected %d stable selections, got %d", iterations, stableCount)
	}
}

func TestCanaryNoBackends(t *testing.T) {
	rule := CanaryRule{
		Header: &CanaryHeaderRule{Name: "X-Canary", Value: "true"},
	}
	cs := NewCanaryStrategy(rule)

	_, err := cs.Select("true", []*Backend{})
	if err != ErrNoBackends {
		t.Errorf("expected ErrNoBackends, got %v", err)
	}
}

func TestCanarySingleBackend(t *testing.T) {
	rule := CanaryRule{
		Header: &CanaryHeaderRule{Name: "X-Canary", Value: "true"},
	}
	cs := NewCanaryStrategy(rule)

	backends := []*Backend{
		{Address: "only:8080", Healthy: true},
	}

	b, err := cs.Select("true", backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Address != "only:8080" {
		t.Errorf("expected only backend, got %s", b.Address)
	}
}

func TestCanaryNoRules(t *testing.T) {
	rule := CanaryRule{}
	cs := NewCanaryStrategy(rule)

	backends := []*Backend{
		{Address: "stable:8080", Healthy: true},
		{Address: "canary:8080", Healthy: true, Meta: map[string]string{"canary": "true"}},
	}

	b, err := cs.Select("any-key", backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Address != "stable:8080" {
		t.Errorf("expected stable backend with no rules, got %s", b.Address)
	}
}

func TestExtractCookieValue(t *testing.T) {
	tests := []struct {
		name         string
		cookieHeader string
		cookieName   string
		want         string
	}{
		{"found", "canary=v2; session=abc", "canary", "v2"},
		{"found reverse order", "session=abc; canary=v2", "canary", "v2"},
		{"not found", "session=abc", "canary", ""},
		{"empty header", "", "canary", ""},
		{"single cookie", "canary=v2", "canary", "v2"},
		{"with spaces", "canary=v2 ; other=abc", "canary", "v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCookieValue(tt.cookieHeader, tt.cookieName)
			if got != tt.want {
				t.Errorf("extractCookieValue(%q, %q) = %q, want %q", tt.cookieHeader, tt.cookieName, got, tt.want)
			}
		})
	}
}
