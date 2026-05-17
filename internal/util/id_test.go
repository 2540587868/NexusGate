package util

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateRandomID_Length(t *testing.T) {
	id := GenerateRandomID(16)
	if len(id) != 32 {
		t.Errorf("expected 32 hex chars for 16 bytes, got %d", len(id))
	}
}

func TestGenerateRandomID_DifferentEachCall(t *testing.T) {
	id1 := GenerateRandomID(16)
	id2 := GenerateRandomID(16)
	if id1 == id2 {
		t.Error("two random IDs should differ")
	}
}

func TestGenerateRandomID_ValidHex(t *testing.T) {
	id := GenerateRandomID(8)
	_, err := hex.DecodeString(id)
	if err != nil {
		t.Errorf("expected valid hex string, got error: %v", err)
	}
}

func TestGenerateRandomID_ZeroBytes(t *testing.T) {
	id := GenerateRandomID(0)
	if id != "" {
		t.Errorf("expected empty string for 0 bytes, got %q", id)
	}
}

func TestGenerateRandomHexID_Length(t *testing.T) {
	id := GenerateRandomHexID(8)
	if len(id) != 16 {
		t.Errorf("expected 16 hex chars for 8 bytes, got %d", len(id))
	}
}

func TestGenerateRandomHexID_DifferentEachCall(t *testing.T) {
	id1 := GenerateRandomHexID(16)
	id2 := GenerateRandomHexID(16)
	if id1 == id2 {
		t.Error("two random hex IDs should differ")
	}
}

func TestGenerateRandomHexID_ValidHex(t *testing.T) {
	id := GenerateRandomHexID(8)
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("expected hex char, got %c", c)
		}
	}
}

func TestGenerateRandomHexID_EvenLength(t *testing.T) {
	for _, n := range []int{1, 4, 8, 16} {
		id := GenerateRandomHexID(n)
		if len(id) != n*2 {
			t.Errorf("byteLen=%d: expected %d chars, got %d", n, n*2, len(id))
		}
	}
}
