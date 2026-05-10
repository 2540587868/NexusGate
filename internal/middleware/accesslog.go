package middleware

import (
	"log/slog"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func AccessLog(next gateway.Handler) gateway.Handler {
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

		slog.Info("access",
			"method", req.Method,
			"path", req.Path,
			"host", req.Host,
			"tenant", req.TenantID,
			"remote", req.RemoteAddr,
			"status", status,
			"duration_us", duration.Microseconds(),
		)

		return resp, err
	}
}
