package router

import (
	"sync"
	"sync/atomic"
)

type LeastConn struct {
	mu          sync.Mutex
	connections map[string]*atomic.Int64
}

func NewLeastConn() *LeastConn {
	return &LeastConn{
		connections: make(map[string]*atomic.Int64),
	}
}

func (lc *LeastConn) Select(key string, backends []*Backend) (*Backend, error) {
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	var best *Backend
	var bestCount int64 = 1<<63 - 1

	for _, b := range backends {
		counter := lc.getCounter(b.Address)
		count := counter.Load()
		if count < bestCount {
			bestCount = count
			best = b
		}
	}

	if best != nil {
		counter := lc.getCounter(best.Address)
		counter.Add(1)
	}

	return best, nil
}

func (lc *LeastConn) Name() StrategyType {
	return StrategyLeastConn
}

func (lc *LeastConn) Release(address string) {
	lc.mu.Lock()
	counter, ok := lc.connections[address]
	lc.mu.Unlock()
	if ok {
		for {
			v := counter.Load()
			if v <= 0 {
				return
			}
			if counter.CompareAndSwap(v, v-1) {
				return
			}
		}
	}
}

func (lc *LeastConn) getCounter(address string) *atomic.Int64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if counter, ok := lc.connections[address]; ok {
		return counter
	}

	counter := &atomic.Int64{}
	lc.connections[address] = counter
	return counter
}

func (lc *LeastConn) GetConnectionCount(address string) int64 {
	lc.mu.Lock()
	counter, ok := lc.connections[address]
	lc.mu.Unlock()
	if ok {
		return counter.Load()
	}
	return 0
}
