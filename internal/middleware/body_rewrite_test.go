package middleware

import (
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestBodyRewriteRequestHeaderSet(t *testing.T) {
	cfg := BodyRewriteConfig{
		RequestHeader: HeaderRewrite{
			Set: map[string]string{"X-Custom": "value"},
		},
	}

	mw := BodyRewrite(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		if req.Headers.Get("X-Custom") != "value" {
			t.Errorf("expected X-Custom=value, got %q", req.Headers.Get("X-Custom"))
		}
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBodyRewriteRequestHeaderAdd(t *testing.T) {
	cfg := BodyRewriteConfig{
		RequestHeader: HeaderRewrite{
			Add: map[string]string{"X-Trace": "abc123"},
		},
	}

	mw := BodyRewrite(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		vals := req.Headers.Values("X-Trace")
		if len(vals) != 1 || vals[0] != "abc123" {
			t.Errorf("expected X-Trace=abc123, got %v", vals)
		}
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBodyRewriteRequestHeaderRemove(t *testing.T) {
	cfg := BodyRewriteConfig{
		RequestHeader: HeaderRewrite{
			Remove: []string{"X-Sensitive"},
		},
	}

	mw := BodyRewrite(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		if req.Headers.Get("X-Sensitive") != "" {
			t.Error("X-Sensitive should have been removed")
		}
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{Headers: http.Header{"X-Sensitive": {"secret"}}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBodyRewriteRequestBodyRegex(t *testing.T) {
	cfg := BodyRewriteConfig{
		RequestBody: []RewriteRule{
			{Pattern: "password=\\S+", Replacement: "password=***"},
		},
	}

	mw := BodyRewrite(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		body := string(req.Body)
		if body != "user=alice&password=***" {
			t.Errorf("expected rewritten body, got %q", body)
		}
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
		Body:    []byte("user=alice&password=secret123"),
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBodyRewriteResponseHeaderSet(t *testing.T) {
	cfg := BodyRewriteConfig{
		ResponseHeader: HeaderRewrite{
			Set: map[string]string{"X-Response-Custom": "resp-value"},
		},
	}

	mw := BodyRewrite(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("X-Response-Custom") != "resp-value" {
		t.Errorf("expected X-Response-Custom=resp-value, got %q", resp.Headers.Get("X-Response-Custom"))
	}
}

func TestBodyRewriteResponseBodyRegex(t *testing.T) {
	cfg := BodyRewriteConfig{
		ResponseBody: []RewriteRule{
			{Pattern: "token=\\S+", Replacement: "token=REDACTED"},
		},
	}

	mw := BodyRewrite(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{},
			Body:       []byte("user=bob&token=abc123xyz"),
		}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := string(resp.Body)
	if body != "user=bob&token=REDACTED" {
		t.Errorf("expected rewritten body, got %q", body)
	}
}

func TestBodyRewriteInvalidRegex(t *testing.T) {
	cfg := BodyRewriteConfig{
		RequestBody: []RewriteRule{
			{Pattern: "[invalid", Replacement: "replaced"},
		},
	}

	mw := BodyRewrite(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
		Body:    []byte("some body text"),
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("invalid regex should not panic, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestApplyHeaderRewriteNil(t *testing.T) {
	rw := HeaderRewrite{
		Set:   map[string]string{"X-Key": "val"},
		Add:   map[string]string{"X-Add": "val"},
		Remove: []string{"X-Remove"},
	}

	applyHeaderRewrite(nil, rw)
}
