package proxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/router"
)

type MirrorConfig struct {
	Backends   []string
	Timeout    time.Duration
	SampleRate float64
	Async      bool
}

type MirrorResult struct {
	Backend    string
	StatusCode int
	Latency    time.Duration
	Err        error
}

type MirrorStats struct {
	TotalSent   atomic.Int64
	TotalFailed atomic.Int64
}

type RequestMirror struct {
	proxy   *Proxy
	config  MirrorConfig
	stats   MirrorStats
	counter atomic.Uint64
	client  *http.Client
}

func NewRequestMirror(proxy *Proxy, cfg MirrorConfig) *RequestMirror {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 1.0
	}
	if cfg.SampleRate > 1.0 {
		cfg.SampleRate = 1.0
	}

	rm := &RequestMirror{
		proxy:  proxy,
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	return rm
}

func (rm *RequestMirror) ShouldMirror() bool {
	if len(rm.config.Backends) == 0 {
		return false
	}
	if rm.config.SampleRate >= 1.0 {
		return true
	}
	v := rm.counter.Add(1)
	threshold := uint64(rm.config.SampleRate * 10000)
	return (v % 10000) < threshold
}

func (rm *RequestMirror) Mirror(req *gateway.Request, mainResp *gateway.Response) {
	if !rm.ShouldMirror() {
		return
	}

	if rm.config.Async {
		go rm.mirrorAsync(req)
	} else {
		rm.mirrorSync(req, mainResp)
	}
}

func (rm *RequestMirror) mirrorAsync(req *gateway.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), rm.config.Timeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, addr := range rm.config.Backends {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			result := rm.sendMirror(ctx, req, address)
			rm.stats.TotalSent.Add(1)
			if result.Err != nil {
				rm.stats.TotalFailed.Add(1)
				slog.Debug("mirror request failed",
					"backend", address,
					"error", result.Err,
					"latency_ms", result.Latency.Milliseconds())
			} else {
				slog.Debug("mirror request completed",
					"backend", address,
					"status", result.StatusCode,
					"latency_ms", result.Latency.Milliseconds())
			}
		}(addr)
	}
	wg.Wait()
}

func (rm *RequestMirror) mirrorSync(req *gateway.Request, mainResp *gateway.Response) {
	ctx, cancel := context.WithTimeout(context.Background(), rm.config.Timeout)
	defer cancel()

	for _, addr := range rm.config.Backends {
		result := rm.sendMirror(ctx, req, addr)
		rm.stats.TotalSent.Add(1)
		if result.Err != nil {
			rm.stats.TotalFailed.Add(1)
			slog.Debug("sync mirror request failed",
				"backend", addr,
				"error", result.Err,
				"latency_ms", result.Latency.Milliseconds())
		}
	}
}

func (rm *RequestMirror) sendMirror(ctx context.Context, req *gateway.Request, address string) *MirrorResult {
	start := time.Now()
	result := &MirrorResult{Backend: address}

	target := address + req.Path
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		target = "http://" + target
	}

	var bodyReader io.Reader
	if req.Body != nil && len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, target, bodyReader)
	if err != nil {
		result.Err = err
		result.Latency = time.Since(start)
		return result
	}

	for k, vv := range req.Headers {
		httpReq.Header[k] = vv
	}
	httpReq.Header.Set("X-Mirror", "true")
	httpReq.Header.Set("X-Mirror-Request-ID", req.ID)

	httpResp, err := rm.client.Do(httpReq)
	if err != nil {
		result.Err = err
		result.Latency = time.Since(start)
		return result
	}
	defer httpResp.Body.Close()

	io.Copy(io.Discard, httpResp.Body)

	result.StatusCode = httpResp.StatusCode
	result.Latency = time.Since(start)

	return result
}

func (rm *RequestMirror) Stats() (sent, failed int64) {
	return rm.stats.TotalSent.Load(), rm.stats.TotalFailed.Load()
}

func BuildMirrorConfig(route *router.Route, timeout time.Duration) MirrorConfig {
	var mirrorAddrs []string
	for _, b := range route.Backends {
		if b.Meta != nil {
			if _, ok := b.Meta["mirror"]; ok {
				mirrorAddrs = append(mirrorAddrs, b.Address)
			}
		}
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return MirrorConfig{
		Backends:   mirrorAddrs,
		Timeout:    timeout,
		SampleRate: 1.0,
		Async:      true,
	}
}
