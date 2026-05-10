package router

import (
	"fmt"
	"hash/crc32"
	"sort"
	"sync"
)

type ConsistentHash struct {
	mu           sync.RWMutex
	virtualNodes int
	ring         []uint32
	nodeMap      map[uint32]*Backend
	lastBackends string
}

func NewConsistentHash(virtualNodes int) *ConsistentHash {
	return &ConsistentHash{
		virtualNodes: virtualNodes,
		ring:         make([]uint32, 0),
		nodeMap:      make(map[uint32]*Backend),
	}
}

func (ch *ConsistentHash) Select(key string, backends []*Backend) (*Backend, error) {
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}
	if len(backends) == 1 {
		return backends[0], nil
	}

	ch.mu.RLock()
	needsRebuild := len(ch.ring) == 0 || ch.backendsChanged(backends)
	ch.mu.RUnlock()

	if needsRebuild {
		ch.mu.Lock()
		if len(ch.ring) == 0 || ch.backendsChanged(backends) {
			ch.rebuild(backends)
		}
		ch.mu.Unlock()
	}

	ch.mu.RLock()
	defer ch.mu.RUnlock()

	hash := crc32.ChecksumIEEE([]byte(key))

	idx := sort.Search(len(ch.ring), func(i int) bool {
		return ch.ring[i] >= hash
	})

	if idx >= len(ch.ring) {
		idx = 0
	}

	node, ok := ch.nodeMap[ch.ring[idx]]
	if !ok {
		return backends[0], nil
	}

	return node, nil
}

func (ch *ConsistentHash) Name() StrategyType {
	return StrategyConsistentHash
}

func (ch *ConsistentHash) backendsChanged(backends []*Backend) bool {
	sig := backendsSignature(backends)
	return sig != ch.lastBackends
}

func backendsSignature(backends []*Backend) string {
	sig := ""
	for _, b := range backends {
		sig += b.Address + ","
	}
	return sig
}

func (ch *ConsistentHash) rebuild(backends []*Backend) {
	ch.ring = ch.ring[:0]
	for k := range ch.nodeMap {
		delete(ch.nodeMap, k)
	}

	for _, b := range backends {
		for i := 0; i < ch.virtualNodes; i++ {
			vnKey := fmt.Sprintf("%s#%d", b.Address, i)
			hash := crc32.ChecksumIEEE([]byte(vnKey))
			ch.ring = append(ch.ring, hash)
			ch.nodeMap[hash] = b
		}
	}

	sort.Slice(ch.ring, func(i, j int) bool {
		return ch.ring[i] < ch.ring[j]
	})

	ch.lastBackends = backendsSignature(backends)
}
