package gateway

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type Shard struct {
	id        int
	queue     chan *Request
	worker    Handler
	mu        sync.Mutex
	pending   atomic.Int64
	recovering atomic.Bool
	gateway   *Gateway
	wg        sync.WaitGroup
}

type Gateway struct {
	shards               []*Shard
	handler              Handler
	queueSize            int
	syncTimeout          time.Duration
	slowRecoveryThreshold float64
	shardCount           int
	workerPerShard       int
	slowRecoveryBatchSize int
}

func NewGateway(handler Handler, shardCount, queueSize int) *Gateway {
	if shardCount <= 0 {
		shardCount = 8
	}
	if queueSize <= 0 {
		queueSize = 4096
	}
	gw := &Gateway{
		handler:               handler,
		queueSize:             queueSize,
		syncTimeout:           30 * time.Second,
		slowRecoveryThreshold: 0.9,
		shardCount:            shardCount,
		workerPerShard:        1,
		slowRecoveryBatchSize: 16,
	}
	gw.shards = make([]*Shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shard := &Shard{
			id:      i,
			queue:   make(chan *Request, queueSize),
			worker:  handler,
			gateway: gw,
		}
		gw.shards[i] = shard
		go shard.run()
	}
	slog.Info("gateway initialized", "shards", shardCount, "queue_size", queueSize, "workers_per_shard", 1)
	return gw
}

func (gw *Gateway) WithWorkerPerShard(n int) *Gateway {
	if n <= 0 {
		n = 1
	}
	gw.workerPerShard = n
	for _, shard := range gw.shards {
		for i := 1; i < n; i++ {
			go shard.run()
		}
	}
	slog.Info("gateway workers configured", "workers_per_shard", n)
	return gw
}

func (gw *Gateway) WithSyncTimeout(d time.Duration) *Gateway {
	if d > 0 {
		gw.syncTimeout = d
	}
	return gw
}

func (gw *Gateway) WithSlowRecoveryBatchSize(n int) *Gateway {
	if n > 0 {
		gw.slowRecoveryBatchSize = n
	}
	return gw
}

func (gw *Gateway) WithSlowRecoveryThreshold(t float64) *Gateway {
	if t > 0 && t <= 1.0 {
		gw.slowRecoveryThreshold = t
	}
	return gw
}

func (gw *Gateway) Dispatch(req *Request) error {
	shardIdx := req.ShardKey() % uint32(gw.shardCount)
	shard := gw.shards[shardIdx]

	utilization := float64(shard.pending.Load()) / float64(gw.queueSize)
	if utilization > gw.slowRecoveryThreshold && shard.recovering.CompareAndSwap(false, true) {
		slog.Warn("shard queue near capacity, slow recovery active",
			"shard", shard.id, "pending", shard.pending.Load(), "capacity", gw.queueSize)
		go shard.slowRecover()
	}

	shard.wg.Add(1)
	shard.pending.Add(1)
	select {
	case shard.queue <- req:
		return nil
	default:
		shard.pending.Add(-1)
		shard.wg.Done()
		return NewGatewayError(ErrRateLimited, "shard queue full",
			"shard queue is at capacity, request rejected")
	}
}

func (gw *Gateway) DispatchSync(req *Request) (*Response, error) {
	req.RespCh = make(chan *ResponseResult, 1)

	if err := gw.Dispatch(req); err != nil {
		ReleaseRequest(req)
		return nil, err
	}

	timer := time.NewTimer(gw.syncTimeout)
	defer timer.Stop()

	select {
	case result := <-req.RespCh:
		ReleaseRequest(req)
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Resp, nil
	case <-timer.C:
		ReleaseRequest(req)
		return nil, NewGatewayError(ErrBackendTimeout, "sync dispatch timeout",
			"request timed out waiting for response")
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
		s.wg.Done()
	}
}

func (s *Shard) slowRecover() {
	defer s.recovering.Store(false)

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < s.gateway.slowRecoveryBatchSize; i++ {
		select {
		case req := <-s.queue:
			if req == nil {
				return
			}
			s.pending.Add(-1)
			resp, err := s.worker(req)
			if req.RespCh != nil {
				req.RespCh <- &ResponseResult{Resp: resp, Err: err}
			} else if err != nil {
				slog.Error("slow recovery handler error", "shard", s.id, "error", err)
			}
			s.wg.Done()
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

	done := make(chan struct{})
	go func() {
		for _, shard := range gw.shards {
			shard.wg.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		slog.Info("gateway closed")
	case <-time.After(30 * time.Second):
		slog.Warn("gateway close timed out after 30s, some workers may still be running")
	}
}

type Config struct {
	ShardCount int
	QueueSize  int
}

func DefaultConfig() *Config {
	return &Config{
		ShardCount: 8,
		QueueSize:  4096,
	}
}
