package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type TraceExporter interface {
	ExportSpan(span *Span)
}

type Span struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Operation  string
	StartTime  time.Time
	Duration   time.Duration
	StatusCode int
	Attributes map[string]string
}

type NoopExporter struct{}

func (n *NoopExporter) ExportSpan(_ *Span) {}

type OTelHTTPExporter struct {
	endpoint  string
	serviceName string
}

func NewOTelHTTPExporter(endpoint, serviceName string) *OTelHTTPExporter {
	return &OTelHTTPExporter{
		endpoint:    endpoint,
		serviceName: serviceName,
	}
}

func (e *OTelHTTPExporter) ExportSpan(span *Span) {
	slog.Debug("trace span exported",
		"trace_id", span.TraceID,
		"span_id", span.SpanID,
		"operation", span.Operation,
		"duration_ms", span.Duration.Milliseconds(),
		"endpoint", e.endpoint,
		"service", e.serviceName,
	)
}

type TraceConfig struct {
	ServiceName string
	Exporter    TraceExporter
}

func TraceWithConfig(cfg TraceConfig) gateway.Middleware {
	exporter := cfg.Exporter
	if exporter == nil {
		exporter = &NoopExporter{}
	}

	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			start := time.Now()

			traceID, parentSpanID := parseTraceParent(req.Headers.Get("traceparent"))
			if traceID == "" {
				traceID = generateTraceID()
			}

			spanID := generateSpanID()

			req.ID = traceID

			req.Headers.Set("traceparent", fmt.Sprintf("00-%s-%s-01", traceID, spanID))
			if req.Headers.Get("tracestate") == "" {
				req.Headers.Set("tracestate", fmt.Sprintf("nexusgate=%s", spanID))
			}

			slog.Debug("request started",
				"trace_id", traceID,
				"span_id", spanID,
				"method", req.Method,
				"path", req.Path,
				"tenant", req.TenantID,
			)

			resp, err := next(req)

			duration := time.Since(start)
			statusCode := 0
			if resp != nil {
				statusCode = resp.StatusCode
			}

			exporter.ExportSpan(&Span{
				TraceID:   traceID,
				SpanID:    spanID,
				ParentID:  parentSpanID,
				Operation: req.Method + " " + req.Path,
				StartTime: start,
				Duration:  duration,
				StatusCode: statusCode,
				Attributes: map[string]string{
					"service.name": cfg.ServiceName,
					"tenant.id":    req.TenantID,
					"remote.addr": req.RemoteAddr,
				},
			})

			if err != nil {
				slog.Error("request completed with error",
					"trace_id", traceID,
					"span_id", spanID,
					"method", req.Method,
					"path", req.Path,
					"duration_ms", duration.Milliseconds(),
					"error", err,
				)
			} else {
				slog.Info("request completed",
					"trace_id", traceID,
					"span_id", spanID,
					"method", req.Method,
					"path", req.Path,
					"status", statusCode,
					"duration_ms", duration.Milliseconds(),
				)
			}

			return resp, err
		}
	}
}

func Trace(next gateway.Handler) gateway.Handler {
	return TraceWithConfig(TraceConfig{
		ServiceName: "nexusgate",
		Exporter:    &NoopExporter{},
	})(next)
}

func parseTraceParent(header string) (traceID, parentSpanID string) {
	if header == "" {
		return "", ""
	}

	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return "", ""
	}

	if parts[0] != "00" {
		return "", ""
	}

	traceID = parts[1]
	parentSpanID = parts[2]
	return traceID, parentSpanID
}

func generateTraceID() string {
	return randomHex(16)
}

func generateSpanID() string {
	return randomHex(8)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
