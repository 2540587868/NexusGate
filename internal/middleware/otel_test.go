package middleware

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type mockOTelExporter struct {
	spans []OTelSpan
}

func (e *mockOTelExporter) ExportSpans(ctx context.Context, spans []OTelSpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *mockOTelExporter) Shutdown(ctx context.Context) error { return nil }

func TestOTelMiddlewareCreatesSpan(t *testing.T) {
	exporter := &mockOTelExporter{}
	cfg := OTelExporterConfig{Service: "test-svc"}

	mw := OTelMiddleware(cfg, exporter)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Method:     "GET",
		Path:       "/api/test",
		Host:       "example.com",
		Scheme:     "https",
		RemoteAddr: "192.168.1.100",
		Headers:    http.Header{},
	}

	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}

	span := exporter.spans[0]
	if span.Operation != "GET /api/test" {
		t.Errorf("expected operation 'GET /api/test', got %q", span.Operation)
	}
	if span.Attributes["http.method"] != "GET" {
		t.Errorf("expected http.method=GET, got %q", span.Attributes["http.method"])
	}
	if span.Attributes["http.url"] != "/api/test" {
		t.Errorf("expected http.url=/api/test, got %q", span.Attributes["http.url"])
	}
	if span.Attributes["http.host"] != "example.com" {
		t.Errorf("expected http.host=example.com, got %q", span.Attributes["http.host"])
	}
	if span.Attributes["http.scheme"] != "https" {
		t.Errorf("expected http.scheme=https, got %q", span.Attributes["http.scheme"])
	}
	if span.Attributes["net.peer.ip"] != "192.168.1.100" {
		t.Errorf("expected net.peer.ip=192.168.1.100, got %q", span.Attributes["net.peer.ip"])
	}
	if span.Attributes["service.name"] != "test-svc" {
		t.Errorf("expected service.name=test-svc, got %q", span.Attributes["service.name"])
	}
	if span.Attributes["http.status_code"] != "200" {
		t.Errorf("expected http.status_code=200, got %q", span.Attributes["http.status_code"])
	}
}

func TestOTelMiddlewareErrorStatus(t *testing.T) {
	exporter := &mockOTelExporter{}
	cfg := OTelExporterConfig{Service: "test-svc"}

	mw := OTelMiddleware(cfg, exporter)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return nil, errors.New("something went wrong")
	})

	req := &gateway.Request{
		Method:  "POST",
		Path:    "/fail",
		Headers: http.Header{},
	}

	_, err := handler(req)
	if err == nil {
		t.Fatal("expected error from handler")
	}

	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}

	span := exporter.spans[0]
	if span.Status != "error" {
		t.Errorf("expected span status 'error', got %q", span.Status)
	}
}

func TestOTelMiddlewareSuccessStatus(t *testing.T) {
	exporter := &mockOTelExporter{}
	cfg := OTelExporterConfig{Service: "test-svc"}

	mw := OTelMiddleware(cfg, exporter)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Path:    "/ok",
		Headers: http.Header{},
	}

	_, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}

	span := exporter.spans[0]
	if span.Status != "ok" {
		t.Errorf("expected span status 'ok', got %q", span.Status)
	}
}

func TestOTelMiddlewareNoopExporter(t *testing.T) {
	cfg := OTelExporterConfig{Service: "test-svc"}

	mw := OTelMiddleware(cfg, nil)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Path:    "/noop",
		Headers: http.Header{},
	}

	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error with nil exporter: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOTelMiddlewareTraceIDFromTraceparent(t *testing.T) {
	exporter := &mockOTelExporter{}
	cfg := OTelExporterConfig{Service: "test-svc"}

	mw := OTelMiddleware(cfg, exporter)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Path:    "/trace",
		Headers: http.Header{},
	}
	req.Headers.Set("Traceparent", "00-abc123def456-789-01")
	req.Headers.Set("X-Request-ID", "req-should-be-ignored")

	_, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}

	span := exporter.spans[0]
	if span.TraceID != "00-abc123def456-789-01" {
		t.Errorf("expected TraceID from Traceparent header, got %q", span.TraceID)
	}
}

func TestOTelMiddlewareTraceIDFromRequestID(t *testing.T) {
	exporter := &mockOTelExporter{}
	cfg := OTelExporterConfig{Service: "test-svc"}

	mw := OTelMiddleware(cfg, exporter)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Method:  "GET",
		Path:    "/trace",
		Headers: http.Header{},
	}
	req.Headers.Set("X-Request-ID", "req-12345")

	_, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}

	span := exporter.spans[0]
	if span.TraceID != "req-12345" {
		t.Errorf("expected TraceID from X-Request-ID header, got %q", span.TraceID)
	}
}

func TestOTelSpanIDGeneration(t *testing.T) {
	spanID := generateOTelSpanID()

	if len(spanID) != 16 {
		t.Errorf("expected 16-char hex string, got %d chars: %q", len(spanID), spanID)
	}

	for _, c := range spanID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("expected hex character, got %c", c)
			break
		}
	}

	spanID2 := generateOTelSpanID()
	if spanID == spanID2 {
		t.Error("two generated span IDs should not be equal (extremely unlikely)")
	}
}

func TestNoopOTelExporter(t *testing.T) {
	exporter := &NoopOTelExporter{}

	err := exporter.ExportSpans(context.Background(), []OTelSpan{
		{TraceID: "test", SpanID: "span1", Operation: "op"},
	})
	if err != nil {
		t.Errorf("ExportSpans should return nil, got %v", err)
	}

	err = exporter.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown should return nil, got %v", err)
	}
}

func TestStdoutOTelExporter(t *testing.T) {
	exporter := &StdoutOTelExporter{}

	err := exporter.ExportSpans(context.Background(), []OTelSpan{
		{TraceID: "test", SpanID: "span1", Operation: "op"},
	})
	if err != nil {
		t.Errorf("ExportSpans should return nil, got %v", err)
	}

	err = exporter.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown should return nil, got %v", err)
	}
}
