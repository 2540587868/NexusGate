package util

import (
	"sync"
	"testing"
)

func TestCompileRegex_ValidPattern(t *testing.T) {
	re, err := CompileRegex(`^\d+$`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !re.MatchString("123") {
		t.Error("should match digits")
	}
	if re.MatchString("abc") {
		t.Error("should not match letters")
	}
}

func TestCompileRegex_InvalidPattern(t *testing.T) {
	_, err := CompileRegex(`[invalid`)
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestCompileRegex_CacheHit(t *testing.T) {
	regexCache = sync.Map{}
	re1, err1 := CompileRegex(`^test\d+$`)
	if err1 != nil {
		t.Fatalf("unexpected error: %v", err1)
	}
	re2, err2 := CompileRegex(`^test\d+$`)
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if re1 != re2 {
		t.Error("expected same regex instance from cache")
	}
}

func TestCompileRegex_DifferentPatterns(t *testing.T) {
	regexCache = sync.Map{}
	re1, _ := CompileRegex(`^abc$`)
	re2, _ := CompileRegex(`^def$`)
	if re1 == re2 {
		t.Error("different patterns should produce different instances")
	}
}

func TestCompileRegex_ConcurrentAccess(t *testing.T) {
	regexCache = sync.Map{}
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			CompileRegex(`^concurrent\d+$`)
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
