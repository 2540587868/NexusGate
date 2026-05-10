package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/router"
)

type BackendTracker struct {
	mu       sync.Mutex
	backends map[string]*backendState
}

type backendState struct {
	healthy atomic.Bool
	inUse   atomic.Int64
}

func NewBackendTracker() *BackendTracker {
	return &BackendTracker{
		backends: make(map[string]*backendState),
	}
}

func (bt *BackendTracker) Register(address string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	if _, ok := bt.backends[address]; !ok {
		bt.backends[address] = &backendState{}
	}
}

func (bt *BackendTracker) MarkUnhealthy(address string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	if st, ok := bt.backends[address]; ok {
		st.healthy.Store(false)
	}
}

func (bt *BackendTracker) MarkHealthy(address string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	if st, ok := bt.backends[address]; ok {
		st.healthy.Store(true)
	}
}

func (bt *BackendTracker) IsHealthy(address string) bool {
	bt.mu.Lock()
	st, ok := bt.backends[address]
	bt.mu.Unlock()
	if !ok {
		return true
	}
	return st.healthy.Load()
}

func (bt *BackendTracker) Stats() map[string]PoolStats {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	stats := make(map[string]PoolStats)
	for addr, st := range bt.backends {
		stats[addr] = PoolStats{
			InUse:   st.inUse.Load(),
			Healthy: st.healthy.Load(),
		}
	}
	return stats
}

type PoolStats struct {
	InUse   int64 `json:"inUse"`
	Healthy bool  `json:"healthy"`
}

type Proxy struct {
	tracker     *BackendTracker
	retryPolicy *RetryPolicy
	httpClient  *http.Client
}

func NewProxy(poolSize int, maxIdle int) *Proxy {
	maxIdleConns := poolSize
	if maxIdleConns <= 0 {
		maxIdleConns = 10
	}
	maxIdlePerHost := maxIdle
	if maxIdlePerHost <= 0 {
		maxIdlePerHost = 5
	}

	transport := &http.Transport{
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdlePerHost,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	return &Proxy{
		tracker:     NewBackendTracker(),
		retryPolicy: DefaultRetryPolicy(),
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *Proxy) WithRetryPolicy(policy *RetryPolicy) *Proxy {
	p.retryPolicy = policy
	return p
}

func (p *Proxy) WithTimeouts(connect, read, write time.Duration) *Proxy {
	p.httpClient.Timeout = read
	if tr, ok := p.httpClient.Transport.(*http.Transport); ok {
		dialer := &net.Dialer{
			Timeout:   connect,
			KeepAlive: 30 * time.Second,
		}
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}
		tr.IdleConnTimeout = 90 * time.Second
		tr.ResponseHeaderTimeout = write
	}
	return p
}

func (p *Proxy) Pool() *BackendTracker {
	return p.tracker
}

func (p *Proxy) Forward(req *gateway.Request, backend *router.Backend) (*gateway.Response, error) {
	var resp *gateway.Response
	var err error

	for attempt := 0; attempt <= p.retryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := p.retryPolicy.Backoff.Next(attempt - 1)
			slog.Debug("retrying request", "attempt", attempt, "delay", delay, "backend", backend.Address)
			time.Sleep(delay)
		}

		resp, err = p.doForward(req, backend)
		if err == nil {
			return resp, nil
		}

		if gwErr, ok := err.(*gateway.GatewayError); ok {
			if !isRetryableError(gwErr) {
				return nil, err
			}
		}

		slog.Warn("forward attempt failed", "attempt", attempt, "backend", backend.Address, "error", err)
	}

	if err != nil {
		return nil, fmt.Errorf("forward to %s failed after %d attempts: %w", backend.Address, p.retryPolicy.MaxRetries+1, err)
	}
	return resp, nil
}

func (p *Proxy) doForward(req *gateway.Request, backend *router.Backend) (*gateway.Response, error) {
	p.tracker.Register(backend.Address)

	scheme := "http"
	if req.Scheme == "https" {
		scheme = "https"
	}

	var targetURL strings.Builder
	targetURL.Grow(len(scheme) + 3 + len(backend.Address) + len(req.Path) + 1 + len(req.QueryString))
	targetURL.WriteString(scheme)
	targetURL.WriteString("://")
	targetURL.WriteString(backend.Address)
	targetURL.WriteString(req.Path)
	if req.QueryString != "" {
		targetURL.WriteByte('?')
		targetURL.WriteString(req.QueryString)
	}

	var bodyReader io.Reader
	if req.Body != nil && len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequest(req.Method, targetURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request to %s: %w", targetURL.String(), err)
	}

	for key, values := range req.Headers {
		httpReq.Header[key] = append(httpReq.Header[key], values...)
	}

	if httpReq.Host == "" {
		httpReq.Host = req.Host
	}

	existingXFF := httpReq.Header.Get("X-Forwarded-For")
	if existingXFF != "" {
		httpReq.Header.Set("X-Forwarded-For", existingXFF+", "+req.RemoteAddr)
	} else {
		httpReq.Header.Set("X-Forwarded-For", req.RemoteAddr)
	}
	httpReq.Header.Set("X-Forwarded-Proto", req.Scheme)

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		p.tracker.MarkUnhealthy(backend.Address)
		return nil, gateway.NewGatewayErrorWithCause(gateway.ErrBackendDown,
			"backend request failed", err)
	}
	defer httpResp.Body.Close()

	var body []byte
	if httpResp.Body != nil {
		body, err = io.ReadAll(io.LimitReader(httpResp.Body, 10<<20))
		if err != nil {
			return nil, gateway.NewGatewayErrorWithCause(gateway.ErrBackendTimeout,
				"read backend response failed", err)
		}
	}

	if httpResp.StatusCode >= 500 {
		p.tracker.MarkUnhealthy(backend.Address)
	}

	return &gateway.Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       body,
	}, nil
}

func isRetryableError(gwErr *gateway.GatewayError) bool {
	return gwErr.Code == gateway.ErrBackendDown || gwErr.Code == gateway.ErrBackendTimeout
}
