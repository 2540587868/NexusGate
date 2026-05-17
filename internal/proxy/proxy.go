package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
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
	"golang.org/x/net/http2"
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
	tracker                 *BackendTracker
	retryPolicy             *RetryPolicy
	httpClient              *http.Client
	http2Client             *http.Client
	maxResponseBodyBytes    int64
	enableHTTP2             bool
	idleConnTimeout         time.Duration
	keepAlive               time.Duration
	mirrorTimeout           time.Duration
	webSocketConnectTimeout time.Duration
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

	idleTimeout := 90 * time.Second
	keepAlive := 30 * time.Second

	transport := &http.Transport{
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdlePerHost,
		IdleConnTimeout:     idleTimeout,
		DisableKeepAlives:   false,
	}

	http2Transport := &http.Transport{
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdlePerHost,
		IdleConnTimeout:     idleTimeout,
		DisableKeepAlives:   false,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		ForceAttemptHTTP2: true,
	}
	http2.ConfigureTransport(http2Transport)

	return &Proxy{
		tracker:                 NewBackendTracker(),
		retryPolicy:             DefaultRetryPolicy(),
		maxResponseBodyBytes:    10 << 20,
		idleConnTimeout:         idleTimeout,
		keepAlive:               keepAlive,
		mirrorTimeout:           5 * time.Second,
		webSocketConnectTimeout: 10 * time.Second,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		http2Client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: http2Transport,
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

func (p *Proxy) WithMaxResponseBodyBytes(n int64) *Proxy {
	if n > 0 {
		p.maxResponseBodyBytes = n
	}
	return p
}

func (p *Proxy) WithIdleConnTimeout(d time.Duration) *Proxy {
	if d > 0 {
		p.idleConnTimeout = d
		if tr, ok := p.httpClient.Transport.(*http.Transport); ok {
			tr.IdleConnTimeout = d
		}
		if tr, ok := p.http2Client.Transport.(*http.Transport); ok {
			tr.IdleConnTimeout = d
		}
	}
	return p
}

func (p *Proxy) WithKeepAlive(d time.Duration) *Proxy {
	if d > 0 {
		p.keepAlive = d
	}
	return p
}

func (p *Proxy) WithMirrorTimeout(d time.Duration) *Proxy {
	if d > 0 {
		p.mirrorTimeout = d
	}
	return p
}

func (p *Proxy) WithWebSocketConnectTimeout(d time.Duration) *Proxy {
	if d > 0 {
		p.webSocketConnectTimeout = d
	}
	return p
}

func (p *Proxy) WithHTTP2(enabled bool) *Proxy {
	p.enableHTTP2 = enabled
	return p
}

func (p *Proxy) WithTimeouts(connect, read, write time.Duration) *Proxy {
	p.httpClient.Timeout = read
	if tr, ok := p.httpClient.Transport.(*http.Transport); ok {
		dialer := &net.Dialer{
			Timeout:   connect,
			KeepAlive: p.keepAlive,
		}
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}
		tr.IdleConnTimeout = p.idleConnTimeout
		tr.ResponseHeaderTimeout = write
	}
	return p
}

func (p *Proxy) Pool() *BackendTracker {
	return p.tracker
}

func (p *Proxy) Forward(req *gateway.Request, backend *router.Backend, route *router.Route) (*gateway.Response, error) {
	maxRetries := p.retryPolicy.MaxRetries
	retryableStatus := p.retryPolicy.RetryableStatus
	if route != nil && route.Retry.MaxRetries > 0 {
		maxRetries = route.Retry.MaxRetries
		retryableStatus = route.Retry.RetryableStatus
		if len(retryableStatus) == 0 {
			retryableStatus = p.retryPolicy.RetryableStatus
		}
	}

	var resp *gateway.Response
	var err error
	tried := map[string]bool{backend.Address: true}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := p.retryPolicy.Backoff.Next(attempt - 1)
			slog.Debug("retrying request", "attempt", attempt, "delay", delay, "backend", backend.Address)
			time.Sleep(delay)

			if route != nil {
				if nextBackend := p.nextBackend(route, tried); nextBackend != nil {
					backend = nextBackend
					tried[backend.Address] = true
					slog.Info("retrying with different backend", "backend", backend.Address)
				}
			}
		}

		resp, err = p.doForward(req, backend, route)
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
		return nil, fmt.Errorf("forward to %s failed after %d attempts: %w", backend.Address, maxRetries+1, err)
	}
	return resp, nil
}

func (p *Proxy) nextBackend(route *router.Route, tried map[string]bool) *router.Backend {
	for _, b := range route.Backends {
		if !tried[b.Address] && b.Healthy {
			return b
		}
	}
	return nil
}

func (p *Proxy) ForwardStream(req *gateway.Request, backend *router.Backend, route *router.Route) (*gateway.Response, error) {
	resp, err := p.doForwardStream(req, backend, route)
	if err == nil {
		return resp, nil
	}

	if gwErr, ok := err.(*gateway.GatewayError); ok {
		if !isRetryableError(gwErr) {
			return nil, err
		}
	}

	if route == nil || route.Retry.MaxRetries <= 0 {
		return nil, err
	}

	maxRetries := route.Retry.MaxRetries
	tried := map[string]bool{backend.Address: true}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		delay := p.retryPolicy.Backoff.Next(attempt - 1)
		slog.Debug("retrying stream request", "attempt", attempt, "delay", delay, "backend", backend.Address)
		time.Sleep(delay)

		if nextBackend := p.nextBackend(route, tried); nextBackend != nil {
			backend = nextBackend
			tried[backend.Address] = true
			slog.Info("retrying stream with different backend", "backend", backend.Address)
		}

		resp, err = p.doForwardStream(req, backend, route)
		if err == nil {
			return resp, nil
		}

		if gwErr, ok := err.(*gateway.GatewayError); ok {
			if !isRetryableError(gwErr) {
				return nil, err
			}
		}
	}

	return nil, fmt.Errorf("stream forward to %s failed after %d attempts: %w", backend.Address, maxRetries+1, err)
}

func (p *Proxy) buildUpstreamRequest(req *gateway.Request, backend *router.Backend, route *router.Route) (*http.Request, *http.Client, error) {
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

	ctx := context.Background()
	if req.Ctx != nil {
		ctx = req.Ctx
	}

	if route != nil && route.Timeout.Total > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, route.Timeout.Total)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL.String(), bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("build request to %s: %w", targetURL.String(), err)
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

	client := p.httpClient
	if p.enableHTTP2 && scheme == "https" {
		client = p.http2Client
	}
	if route != nil && (route.Timeout.Connect > 0 || route.Timeout.Read > 0 || route.Timeout.Write > 0) {
		client = p.routeClient(route)
	}

	return httpReq, client, nil
}

func (p *Proxy) doForwardStream(req *gateway.Request, backend *router.Backend, route *router.Route) (*gateway.Response, error) {
	httpReq, client, err := p.buildUpstreamRequest(req, backend, route)
	if err != nil {
		return nil, err
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		p.tracker.MarkUnhealthy(backend.Address)
		return nil, gateway.NewGatewayErrorWithCause(gateway.ErrBackendDown,
			"backend request failed", err)
	}

	if httpResp.StatusCode >= 500 {
		p.tracker.MarkUnhealthy(backend.Address)
		httpResp.Body.Close()

		retryableStatus := p.retryPolicy.RetryableStatus
		if route != nil && route.Retry.MaxRetries > 0 && len(route.Retry.RetryableStatus) > 0 {
			retryableStatus = route.Retry.RetryableStatus
		}
		if isRetryableStatusList(httpResp.StatusCode, retryableStatus) {
			return nil, gateway.NewGatewayError(gateway.ErrBackendDown,
				fmt.Sprintf("backend returned retryable status %d", httpResp.StatusCode), "")
		}

		return &gateway.Response{
			StatusCode: httpResp.StatusCode,
			Headers:    httpResp.Header,
		}, nil
	}

	return &gateway.Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		StreamBody: httpResp.Body,
	}, nil
}

func (p *Proxy) doForward(req *gateway.Request, backend *router.Backend, route *router.Route) (*gateway.Response, error) {
	httpReq, client, err := p.buildUpstreamRequest(req, backend, route)
	if err != nil {
		return nil, err
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		p.tracker.MarkUnhealthy(backend.Address)
		return nil, gateway.NewGatewayErrorWithCause(gateway.ErrBackendDown,
			"backend request failed", err)
	}
	defer httpResp.Body.Close()

	var body []byte
	if httpResp.Body != nil {
		body, err = io.ReadAll(io.LimitReader(httpResp.Body, p.maxResponseBodyBytes))
		if err != nil {
			return nil, gateway.NewGatewayErrorWithCause(gateway.ErrBackendTimeout,
				"read backend response failed", err)
		}
	}

	retryableStatus := p.retryPolicy.RetryableStatus
	if route != nil && route.Retry.MaxRetries > 0 && len(route.Retry.RetryableStatus) > 0 {
		retryableStatus = route.Retry.RetryableStatus
	}

	if httpResp.StatusCode >= 500 {
		p.tracker.MarkUnhealthy(backend.Address)
		if isRetryableStatusList(httpResp.StatusCode, retryableStatus) {
			return nil, gateway.NewGatewayError(gateway.ErrBackendDown,
				fmt.Sprintf("backend returned retryable status %d", httpResp.StatusCode), "")
		}
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

func isRetryableStatusList(statusCode int, statusList []int) bool {
	for _, code := range statusList {
		if statusCode == code {
			return true
		}
	}
	return false
}

func (p *Proxy) newBaseTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        p.httpClient.Transport.(*http.Transport).MaxIdleConns,
		MaxIdleConnsPerHost: p.httpClient.Transport.(*http.Transport).MaxIdleConnsPerHost,
		IdleConnTimeout:     p.idleConnTimeout,
		DisableKeepAlives:   false,
	}
}

func (p *Proxy) routeClient(route *router.Route) *http.Client {
	transport := p.newBaseTransport()

	if route.Timeout.Connect > 0 {
		dialer := &net.Dialer{
			Timeout:   route.Timeout.Connect,
			KeepAlive: p.keepAlive,
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}
	}

	if route.Timeout.Write > 0 {
		transport.ResponseHeaderTimeout = route.Timeout.Write
	}

	timeout := p.httpClient.Timeout
	if route.Timeout.Read > 0 {
		timeout = route.Timeout.Read
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
