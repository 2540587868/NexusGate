package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/util"
)

type OTelExporterConfig struct {
	Endpoint string `yaml:"endpoint"`
	Insecure bool   `yaml:"insecure"`
	Service  string `yaml:"service"`
}

type OTelSpan struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Operation  string            `json:"operation"`
	StartTime  time.Time         `json:"start_time"`
	Duration   time.Duration     `json:"duration"`
	Attributes map[string]string `json:"attributes"`
	Status     string            `json:"status"`
}

type OTelExporter interface {
	ExportSpans(ctx context.Context, spans []OTelSpan) error
	Shutdown(ctx context.Context) error
}

type NoopOTelExporter struct{}

func (e *NoopOTelExporter) ExportSpans(ctx context.Context, spans []OTelSpan) error {
	return nil
}

func (e *NoopOTelExporter) Shutdown(ctx context.Context) error {
	return nil
}

type StdoutOTelExporter struct{}

func (e *StdoutOTelExporter) ExportSpans(ctx context.Context, spans []OTelSpan) error {
	for _, span := range spans {
		slog.Info("otel span",
			"trace_id", span.TraceID,
			"span_id", span.SpanID,
			"operation", span.Operation,
			"duration", span.Duration,
			"status", span.Status,
		)
	}
	return nil
}

func (e *StdoutOTelExporter) Shutdown(ctx context.Context) error {
	return nil
}

func OTelMiddleware(cfg OTelExporterConfig, exporter OTelExporter) gateway.Middleware {
	if exporter == nil {
		exporter = &NoopOTelExporter{}
	}

	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			traceID := req.Headers.Get("Traceparent")
			if traceID == "" {
				traceID = req.Headers.Get("X-Request-ID")
			}

			spanID := generateOTelSpanID()
			start := time.Now()

			resp, err := next(req)

			duration := time.Since(start)
			status := "ok"
			if err != nil {
				status = "error"
			}

			span := OTelSpan{
				TraceID:   traceID,
				SpanID:    spanID,
				Operation: fmt.Sprintf("%s %s", req.Method, req.Path),
				StartTime: start,
				Duration:  duration,
				Attributes: map[string]string{
					"http.method":      req.Method,
					"http.url":         req.Path,
					"http.host":        req.Host,
					"http.scheme":      req.Scheme,
					"net.peer.ip":      req.RemoteAddr,
					"service.name":     cfg.Service,
				},
				Status: status,
			}

			if resp != nil {
				span.Attributes["http.status_code"] = fmt.Sprintf("%d", resp.StatusCode)
			}

			exportCtx := context.Background()
			if req.Ctx != nil {
				exportCtx = req.Ctx
			}
			_ = exporter.ExportSpans(exportCtx, []OTelSpan{span})

			return resp, err
		}
	}
}

func generateOTelSpanID() string {
	return util.GenerateRandomHexID(8)
}
