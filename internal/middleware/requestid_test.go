package middleware

import (
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestRequestIDGeneratesNewID(t *testing.T) {
	handler := RequestID(func(req *gateway.Request) (*gateway.Response, error) {
		if req.ID == "" {
			t.Error("expected req.ID to be generated")
		}
		if req.Headers.Get(RequestIDHeader) == "" {
			t.Error("expected X-Request-ID header to be set")
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

func TestRequestIDPreservesExisting(t *testing.T) {
	existingID := "existing-id-12345"

	handler := RequestID(func(req *gateway.Request) (*gateway.Response, error) {
		if req.ID != existingID {
			t.Errorf("expected req.ID=%q, got %q", existingID, req.ID)
		}
		if req.Headers.Get(RequestIDHeader) != existingID {
			t.Errorf("expected X-Request-ID=%q, got %q", existingID, req.Headers.Get(RequestIDHeader))
		}
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
	}
	req.Headers.Set(RequestIDHeader, existingID)
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRequestIDSetsResponseHeader(t *testing.T) {
	handler := RequestID(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	respID := resp.Headers.Get(RequestIDHeader)
	if respID == "" {
		t.Error("expected X-Request-ID in response headers")
	}
	if respID != req.ID {
		t.Errorf("response X-Request-ID=%q does not match req.ID=%q", respID, req.ID)
	}
}

func TestRequestIDSetsReqID(t *testing.T) {
	handler := RequestID(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{Headers: http.Header{}}
	_, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.ID == "" {
		t.Error("expected req.ID to be set")
	}
	if len(req.ID) != 32 {
		t.Errorf("expected 32-char hex ID, got %d chars: %q", len(req.ID), req.ID)
	}
}
