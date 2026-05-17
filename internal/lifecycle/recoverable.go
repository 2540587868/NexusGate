package lifecycle

import (
	"context"
	"log/slog"
	"math"
	"runtime/debug"
	"sync"
	"time"
)

type Recoverable struct {
	mu           sync.Mutex
	running      map[string]context.CancelFunc
	restarts     map[string]int
	maxRetry     int
	maxBackoff   time.Duration
}

func NewRecoverable(maxRetry int) *Recoverable {
	return &Recoverable{
		running:    make(map[string]context.CancelFunc),
		restarts:   make(map[string]int),
		maxRetry:   maxRetry,
		maxBackoff: 30 * time.Second,
	}
}

func NewRecoverableWithBackoff(maxRetry int, maxBackoff time.Duration) *Recoverable {
	r := NewRecoverable(maxRetry)
	if maxBackoff > 0 {
		r.maxBackoff = maxBackoff
	}
	return r
}

func (r *Recoverable) Go(name string, fn func(ctx context.Context) error) {
	r.mu.Lock()
	if oldCancel, exists := r.running[name]; exists {
		oldCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.running[name] = cancel
	r.mu.Unlock()

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("goroutine panicked, recovering",
					"name", name,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				r.mu.Lock()
				r.restarts[name]++
				count := r.restarts[name]
				r.mu.Unlock()

				if count <= r.maxRetry {
					delay := time.Duration(math.Min(math.Pow(2, float64(count-1)), float64(r.maxBackoff/time.Second))) * time.Second
					slog.Info("restarting goroutine", "name", name, "attempt", count, "delay", delay)
					select {
					case <-time.After(delay):
						r.Go(name, fn)
					case <-ctx.Done():
						slog.Info("goroutine restart cancelled", "name", name)
					}
				} else {
					slog.Error("goroutine exceeded max restart attempts", "name", name, "attempts", count)
					r.mu.Lock()
					delete(r.running, name)
					r.mu.Unlock()
				}
			} else {
				r.mu.Lock()
				delete(r.running, name)
				r.mu.Unlock()
			}
		}()

		if err := fn(ctx); err != nil && err != context.Canceled {
			slog.Error("goroutine exited with error", "name", name, "error", err)
		}
	}()
}

func (r *Recoverable) Stop(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cancel, ok := r.running[name]; ok {
		cancel()
		delete(r.running, name)
	}
}

func (r *Recoverable) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, cancel := range r.running {
		cancel()
		delete(r.running, name)
		slog.Info("stopped goroutine", "name", name)
	}
}

func (r *Recoverable) RestartCount(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restarts[name]
}
