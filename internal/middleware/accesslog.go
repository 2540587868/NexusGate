package middleware

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type AccessLogConfig struct {
	Format string `yaml:"format"`
}

func AccessLogWithConfig(cfg AccessLogConfig) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			RecordRequestStart()
			start := time.Now()

			resp, err := next(req)

			duration := time.Since(start)
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}

			success := err == nil && status < 500
			RecordRequestEnd(success, duration)

			if cfg.Format != "" {
				msg := formatAccessLog(cfg.Format, req, resp, duration, err)
				slog.Info(msg)
			} else {
				slog.Info("access",
					"method", req.Method,
					"path", req.Path,
					"host", req.Host,
					"tenant", req.TenantID,
					"remote", req.RemoteAddr,
					"status", status,
					"duration_us", duration.Microseconds(),
				)
			}

			return resp, err
		}
	}
}

func AccessLog(next gateway.Handler) gateway.Handler {
	return AccessLogWithConfig(AccessLogConfig{})(next)
}

func formatAccessLog(format string, req *gateway.Request, resp *gateway.Response, duration time.Duration, err error) string {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}

	replacer := strings.NewReplacer(
		"$method", req.Method,
		"$path", req.Path,
		"$host", req.Host,
		"$tenant", req.TenantID,
		"$remote", req.RemoteAddr,
		"$status", fmt.Sprintf("%d", status),
		"$duration", duration.String(),
		"$duration_us", fmt.Sprintf("%d", duration.Microseconds()),
		"$duration_ms", fmt.Sprintf("%.3f", float64(duration.Microseconds())/1000.0),
		"$scheme", req.Scheme,
		"$request_id", req.Headers.Get("X-Request-ID"),
	)

	return replacer.Replace(format)
}
