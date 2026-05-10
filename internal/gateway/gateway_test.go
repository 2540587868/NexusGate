package gateway

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestNewGateway(t *testing.T) {
	var processed atomic.Int64
	handler := func(req *Request) (*Response, error) {
		processed.Add(1)
		return &Response{StatusCode: 200, Body: []byte("ok")}, nil
	}

	gw := NewGateway(handler, 1024)
	defer gw.Close()

	if gw == nil {
		t.Fatal("NewGateway returned nil")
	}

	stats := gw.Stats()
	if len(stats) != ShardCount {
		t.Errorf("expected %d shards, got %d", ShardCount, len(stats))
	}
}

func TestGatewayDispatch(t *testing.T) {
	var processed atomic.Int64
	handler := func(req *Request) (*Response, error) {
		processed.Add(1)
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 1024)
	defer gw.Close()

	req := &Request{
		TenantID: "test-tenant",
		Method:   "GET",
		Path:     "/api/test",
		Headers:  map[string][]string{},
	}

	if err := gw.Dispatch(req); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if processed.Load() != 1 {
		t.Errorf("expected 1 processed request, got %d", processed.Load())
	}
}

func TestGatewayDispatchMultiple(t *testing.T) {
	var processed atomic.Int64
	handler := func(req *Request) (*Response, error) {
		processed.Add(1)
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 1024)
	defer gw.Close()

	for i := 0; i < 100; i++ {
		req := &Request{
			TenantID: "test-tenant",
			Method:   "GET",
			Path:     "/api/test",
			Headers:  map[string][]string{},
		}
		if err := gw.Dispatch(req); err != nil {
			t.Fatalf("Dispatch() error = %v", err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	if processed.Load() != 100 {
		t.Errorf("expected 100 processed requests, got %d", processed.Load())
	}
}

func TestGatewayDispatchSameTenantSameShard(t *testing.T) {
	var shardIDs []int
	var mu struct{}

	handler := func(req *Request) (*Response, error) {
		_ = mu
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 1024)
	defer gw.Close()

	req1 := &Request{TenantID: "tenant-A", Method: "GET", Path: "/", Headers: map[string][]string{}}
	req2 := &Request{TenantID: "tenant-A", Method: "GET", Path: "/", Headers: map[string][]string{}}

	shard1 := req1.ShardKey()
	shard2 := req2.ShardKey()

	_ = shardIDs

	if shard1 != shard2 {
		t.Errorf("same tenant should go to same shard, got %d and %d", shard1, shard2)
	}
}

func TestGatewayQueueFull(t *testing.T) {
	var processed atomic.Int64
	handler := func(req *Request) (*Response, error) {
		processed.Add(1)
		time.Sleep(100 * time.Millisecond)
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 2)
	defer gw.Close()

	for i := 0; i < 2; i++ {
		req := &Request{TenantID: "blocking", Method: "GET", Path: "/", Headers: map[string][]string{}}
		if err := gw.Dispatch(req); err != nil {
			t.Fatalf("Dispatch(%d) error = %v", i, err)
		}
	}

	time.Sleep(20 * time.Millisecond)

	overflowCount := 0
	for i := 0; i < 20; i++ {
		req := &Request{TenantID: "blocking", Method: "GET", Path: "/", Headers: map[string][]string{}}
		if err := gw.Dispatch(req); err != nil {
			overflowCount++
		}
	}

	if overflowCount == 0 {
		t.Error("expected some requests to be rejected when queue is full")
	}

	time.Sleep(500 * time.Millisecond)
}

func TestGatewayDispatchSync(t *testing.T) {
	handler := func(req *Request) (*Response, error) {
		return &Response{StatusCode: 200, Body: []byte("sync-ok")}, nil
	}

	gw := NewGateway(handler, 1024)
	defer gw.Close()

	req := &Request{
		TenantID: "sync-tenant",
		Method:   "GET",
		Path:     "/api/sync",
		Headers:  map[string][]string{},
	}

	resp, err := gw.DispatchSync(req)
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != "sync-ok" {
		t.Errorf("expected 'sync-ok', got %q", string(resp.Body))
	}
}

func TestGatewayDispatchSyncTimeout(t *testing.T) {
	handler := func(req *Request) (*Response, error) {
		time.Sleep(5 * time.Second)
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 1024)
	gw.syncTimeout = 50 * time.Millisecond
	defer gw.Close()

	req := &Request{
		TenantID: "timeout-tenant",
		Method:   "GET",
		Path:     "/api/slow",
		Headers:  map[string][]string{},
	}

	_, err := gw.DispatchSync(req)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestGatewayClose(t *testing.T) {
	handler := func(req *Request) (*Response, error) {
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 1024)
	gw.Close()

	_ = gw
}

func TestGatewayStats(t *testing.T) {
	handler := func(req *Request) (*Response, error) {
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 1024)
	defer gw.Close()

	stats := gw.Stats()
	if len(stats) != ShardCount {
		t.Errorf("expected %d shard stats, got %d", ShardCount, len(stats))
	}
}

func TestGatewaySlowRecover(t *testing.T) {
	var processed atomic.Int64
	handler := func(req *Request) (*Response, error) {
		processed.Add(1)
		time.Sleep(200 * time.Millisecond)
		return &Response{StatusCode: 200}, nil
	}

	gw := NewGateway(handler, 4)
	defer gw.Close()

	for i := 0; i < 4; i++ {
		req := &Request{
			TenantID: "slow-tenant",
			Method:   "GET",
			Path:     "/api/slow",
			Headers:  map[string][]string{},
		}
		gw.Dispatch(req)
	}

	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 20; i++ {
		req := &Request{
			TenantID: "slow-tenant",
			Method:   "GET",
			Path:     "/api/slow",
			Headers:  map[string][]string{},
		}
		gw.Dispatch(req)
	}

	time.Sleep(500 * time.Millisecond)
}

func TestGatewayErrorInHandler(t *testing.T) {
	handler := func(req *Request) (*Response, error) {
		return nil, NewGatewayError(ErrBackendDown, "backend error", "test backend")
	}

	gw := NewGateway(handler, 1024)
	defer gw.Close()

	req := &Request{
		TenantID: "error-tenant",
		Method:   "GET",
		Path:     "/api/error",
		Headers:  map[string][]string{},
		RespCh:   make(chan *ResponseResult, 1),
	}

	gw.Dispatch(req)

	select {
	case result := <-req.RespCh:
		if result.Err == nil {
			t.Error("expected error from handler")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for error response")
	}
}
