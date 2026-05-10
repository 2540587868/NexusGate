package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoverableGo(t *testing.T) {
	r := NewRecoverable(3)
	var ran atomic.Int32

	r.Go("test", func(ctx context.Context) error {
		ran.Add(1)
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	if ran.Load() != 1 {
		t.Errorf("expected 1 run, got %d", ran.Load())
	}

	r.StopAll()
}

func TestRecoverablePanicRestart(t *testing.T) {
	r := NewRecoverable(5)
	var runs atomic.Int32

	r.Go("panicker", func(ctx context.Context) error {
		count := runs.Add(1)
		if count <= 2 {
			panic("test panic")
		}
		return nil
	})

	time.Sleep(5 * time.Second)

	count := runs.Load()
	if count < 3 {
		t.Errorf("expected at least 3 runs after panics, got %d", count)
	}

	r.StopAll()
}

func TestRecoverableStop(t *testing.T) {
	r := NewRecoverable(3)
	var ran atomic.Int32

	r.Go("stoppable", func(ctx context.Context) error {
		ran.Add(1)
		<-ctx.Done()
		return ctx.Err()
	})

	time.Sleep(50 * time.Millisecond)
	if ran.Load() != 1 {
		t.Errorf("expected 1 run, got %d", ran.Load())
	}

	r.Stop("stoppable")
	time.Sleep(50 * time.Millisecond)
}

func TestRecoverableMaxRetryExceeded(t *testing.T) {
	r := NewRecoverable(2)
	var runs atomic.Int32

	r.Go("max-retry", func(ctx context.Context) error {
		runs.Add(1)
		panic("always panics")
	})

	time.Sleep(5 * time.Second)

	count := runs.Load()
	if count != 3 {
		t.Errorf("expected 3 runs (1 initial + 2 retries), got %d", count)
	}
}

func TestRecoverableRestartCount(t *testing.T) {
	r := NewRecoverable(5)
	var runs atomic.Int32

	r.Go("counter", func(ctx context.Context) error {
		count := runs.Add(1)
		if count <= 3 {
			panic("restart")
		}
		return nil
	})

	time.Sleep(8 * time.Second)

	if r.RestartCount("counter") < 3 {
		t.Errorf("expected at least 3 restarts, got %d", r.RestartCount("counter"))
	}

	r.StopAll()
}

func TestRecoverableStopAll(t *testing.T) {
	r := NewRecoverable(3)
	var ran atomic.Int32

	r.Go("svc1", func(ctx context.Context) error {
		ran.Add(1)
		<-ctx.Done()
		return ctx.Err()
	})
	r.Go("svc2", func(ctx context.Context) error {
		ran.Add(1)
		<-ctx.Done()
		return ctx.Err()
	})

	time.Sleep(50 * time.Millisecond)
	if ran.Load() != 2 {
		t.Errorf("expected 2 runs, got %d", ran.Load())
	}

	r.StopAll()
	time.Sleep(50 * time.Millisecond)
}

func TestRecoverableContextCancellation(t *testing.T) {
	r := NewRecoverable(3)
	var ran atomic.Int32

	r.Go("ctx-test", func(ctx context.Context) error {
		ran.Add(1)
		<-ctx.Done()
		return ctx.Err()
	})

	time.Sleep(50 * time.Millisecond)

	r.Stop("ctx-test")
	time.Sleep(50 * time.Millisecond)

	r.mu.Lock()
	_, exists := r.running["ctx-test"]
	r.mu.Unlock()
	if exists {
		t.Error("stopped goroutine should be removed from running map")
	}
}

func TestHealthCheckerRegister(t *testing.T) {
	hc := NewHealthChecker(10*time.Second, 5*time.Second, 3)

	hc.Register("10.0.0.1:8080")
	hc.Register("10.0.0.2:8080")

	status := hc.Status()
	if len(status) != 2 {
		t.Errorf("expected 2 backends, got %d", len(status))
	}

	if !hc.IsHealthy("10.0.0.1:8080") {
		t.Error("newly registered backend should be healthy")
	}
}

func TestHealthCheckerUnregister(t *testing.T) {
	hc := NewHealthChecker(10*time.Second, 5*time.Second, 3)

	hc.Register("10.0.0.1:8080")
	hc.Unregister("10.0.0.1:8080")

	status := hc.Status()
	if len(status) != 0 {
		t.Errorf("expected 0 backends after unregister, got %d", len(status))
	}
}

func TestHealthCheckerUnknownBackend(t *testing.T) {
	hc := NewHealthChecker(10*time.Second, 5*time.Second, 3)

	if hc.IsHealthy("unknown:8080") {
		t.Error("unknown backend should not be healthy")
	}
}

func TestGracefulShutdown(t *testing.T) {
	g := NewGraceful(5 * time.Second)

	var shutdownCalled atomic.Int32
	g.OnShutdown(func() error {
		shutdownCalled.Add(1)
		return nil
	})

	g.OnShutdown(func() error {
		shutdownCalled.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	g.WaitContext(ctx)

	if shutdownCalled.Load() != 2 {
		t.Errorf("expected 2 shutdown handlers called, got %d", shutdownCalled.Load())
	}
}
