package router

import (
	"hash/fnv"
)

type IPHash struct{}

func NewIPHash() *IPHash {
	return &IPHash{}
}

func (ih *IPHash) Select(key string, backends []*Backend) (*Backend, error) {
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}
	if len(backends) == 1 {
		return backends[0], nil
	}

	h := fnv.New32a()
	h.Write([]byte(key))
	idx := h.Sum32() % uint32(len(backends))
	return backends[idx], nil
}

func (ih *IPHash) Name() StrategyType {
	return StrategyIPHash
}
