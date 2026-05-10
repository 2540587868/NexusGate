package lifecycle

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Graceful struct {
	mu       sync.Mutex
	shutdown []func() error
	timeout  time.Duration
}

func NewGraceful(timeout time.Duration) *Graceful {
	return &Graceful{
		timeout: timeout,
	}
}

func (g *Graceful) OnShutdown(fn func() error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.shutdown = append(g.shutdown, fn)
}

func (g *Graceful) Wait() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	sig := <-sigCh
	slog.Info("received shutdown signal", "signal", sig.String())

	g.executeShutdown()
}

func (g *Graceful) WaitContext(ctx context.Context) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	select {
	case sig := <-sigCh:
		slog.Info("received shutdown signal", "signal", sig.String())
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down")
	}

	g.executeShutdown()
}

func (g *Graceful) executeShutdown() {
	done := make(chan struct{})
	go func() {
		g.mu.Lock()
		handlers := make([]func() error, len(g.shutdown))
		copy(handlers, g.shutdown)
		g.mu.Unlock()

		var errs []error
		for i := len(handlers) - 1; i >= 0; i-- {
			slog.Info("executing shutdown handler", "step", len(handlers)-i, "total", len(handlers))
			if err := handlers[i](); err != nil {
				slog.Error("shutdown handler error", "error", err)
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			slog.Error("shutdown completed with errors", "error_count", len(errs))
		}
		close(done)
	}()

	timer := time.NewTimer(g.timeout)
	defer timer.Stop()

	select {
	case <-done:
		slog.Info("graceful shutdown completed")
	case <-timer.C:
		slog.Warn("graceful shutdown timed out, forcing exit")
	}
}
