package gateway

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type Shard struct {
	id         int
	queue      chan *Request
	worker     Handler
	mu         sync.Mutex
	pending    atomic.Int64
	recovering atomic.Bool
}

type Gateway struct {
	shards      [ShardCount]*Shard
	handler     Handler
	queueSize   int
	syncTimeout time.Duration
}

func NewGateway(handler Handler, queueSize int) *Gateway {
	gw := &Gateway{
		handler:     handler,
		queueSize:   queueSize,
		syncTimeout: 30 * time.Second,
	}
	for i := 0; i < ShardCount; i++ {
		shard := &Shard{
			id:     i,
			queue:  make(chan *Request, queueSize),
			worker: handler,
		}
		gw.shards[i] = shard
		go shard.run()
	}
	slog.Info("gateway initialized", "shards", ShardCount, "queue_size", queueSize)
	return gw
}

func (gw *Gateway) WithSyncTimeout(d time.Duration) *Gateway {
	gw.syncTimeout = d
	return gw
}

func (gw *Gateway) Dispatch(req *Request) error {
	shardIdx := req.ShardKey()
	shard := gw.shards[shardIdx]

	utilization := float64(shard.pending.Load()) / float64(gw.queueSize)
	if utilization > 0.9 && shard.recovering.CompareAndSwap(false, true) {
		slog.Warn("shard queue near capacity, slow recovery active",
			"shard", shard.id, "pending", shard.pending.Load(), "capacity", gw.queueSize)
		go shard.slowRecover()
	}

	shard.pending.Add(1)
	select {
	case shard.queue <- req:
		return nil
	default:
		shard.pending.Add(-1)
		return NewGatewayError(ErrRateLimited, "shard queue full",
			"shard queue is at capacity, request rejected")
	}
}

func (gw *Gateway) DispatchSync(req *Request) (*Response, error) {
	req.RespCh = make(chan *ResponseResult, 1)

	if err := gw.Dispatch(req); err != nil {
		return nil, err
	}

	timer := time.NewTimer(gw.syncTimeout)
	defer timer.Stop()

	select {
	case result := <-req.RespCh:
		return result.Resp, result.Err
	case <-timer.C:
		return nil, NewGatewayError(ErrBackendTimeout, "request timed out",
			"gateway dispatch sync timeout")
	}
}

func (s *Shard) run() {
	for req := range s.queue {
		s.pending.Add(-1)
		resp, err := s.worker(req)
		if req.RespCh != nil {
			req.RespCh <- &ResponseResult{Resp: resp, Err: err}
		} else if err != nil {
			slog.Error("request handler error", "shard", s.id, "path", req.Path, "error", err)
		}
	}
}

func (s *Shard) slowRecover() {
	defer s.recovering.Store(false)

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < 16; i++ {
		select {
		case req := <-s.queue:
			s.pending.Add(-1)
			resp, err := s.worker(req)
			if req.RespCh != nil {
				req.RespCh <- &ResponseResult{Resp: resp, Err: err}
			} else if err != nil {
				slog.Error("slow recovery handler error", "shard", s.id, "error", err)
			}
		default:
			return
		}
	}
}

func (gw *Gateway) Stats() map[int]int64 {
	stats := make(map[int]int64)
	for i, shard := range gw.shards {
		stats[i] = shard.pending.Load()
	}
	return stats
}

func (gw *Gateway) Close() {
	for _, shard := range gw.shards {
		close(shard.queue)
	}
	slog.Info("gateway closed")
}

type Config struct {
	ShardCount int
	QueueSize  int
}

func DefaultConfig() *Config {
	return &Config{
		ShardCount: ShardCount,
		QueueSize:  4096,
	}
}
