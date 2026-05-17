package middleware

import (
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestGRPCTranscoderRequest(t *testing.T) {
	cfg := GRPCTranscoderConfig{Enable: true}

	mw := GRPCTranscoder(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		if req.Headers.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type=application/json, got %q", req.Headers.Get("Content-Type"))
		}
		if req.Headers.Get("X-GRPC-Transcoded") != "true" {
			t.Errorf("expected X-GRPC-Transcoded=true, got %q", req.Headers.Get("X-GRPC-Transcoded"))
		}
		if req.Headers.Get("X-Original-Content-Type") != "application/grpc" {
			t.Errorf("expected X-Original-Content-Type=application/grpc, got %q", req.Headers.Get("X-Original-Content-Type"))
		}
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Content-Type": {"application/grpc"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGRPCTranscoderResponse(t *testing.T) {
	cfg := GRPCTranscoderConfig{Enable: true}

	mw := GRPCTranscoder(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers: http.Header{
				"Grpc-Status":  {"0"},
				"Grpc-Message": {"OK"},
			},
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Content-Type": {"application/grpc"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Grpc-Status") != "" {
		t.Error("Grpc-Status should be removed from response")
	}
	if resp.Headers.Get("Grpc-Message") != "" {
		t.Error("Grpc-Message should be removed from response")
	}
	if resp.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", resp.Headers.Get("Content-Type"))
	}
}

func TestGRPCTranscoderNonGRPC(t *testing.T) {
	cfg := GRPCTranscoderConfig{Enable: true}

	mw := GRPCTranscoder(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		if req.Headers.Get("Content-Type") != "application/json" {
			t.Errorf("non-gRPC content type should not be changed, got %q", req.Headers.Get("Content-Type"))
		}
		if req.Headers.Get("X-GRPC-Transcoded") != "" {
			t.Error("X-GRPC-Transcoded should not be set for non-gRPC requests")
		}
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"application/json"}},
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Content-Type": {"application/json"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("response Content-Type should be unchanged, got %q", resp.Headers.Get("Content-Type"))
	}
}

func TestGRPCTranscoderGRPCProto(t *testing.T) {
	cfg := GRPCTranscoderConfig{Enable: true}

	mw := GRPCTranscoder(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		if req.Headers.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type=application/json, got %q", req.Headers.Get("Content-Type"))
		}
		if req.Headers.Get("X-GRPC-Transcoded") != "true" {
			t.Errorf("expected X-GRPC-Transcoded=true, got %q", req.Headers.Get("X-GRPC-Transcoded"))
		}
		if req.Headers.Get("X-Original-Content-Type") != "application/grpc+proto" {
			t.Errorf("expected X-Original-Content-Type=application/grpc+proto, got %q", req.Headers.Get("X-Original-Content-Type"))
		}
		return &gateway.Response{StatusCode: 200, Headers: http.Header{}}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Content-Type": {"application/grpc+proto"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
