package router

import (
	"errors"
	"sync"
)

var ErrNoBackends = errors.New("no available backends")

type WeightedRR struct {
	mu             sync.Mutex
	currentWeights []int64
}

func NewWeightedRR() *WeightedRR {
	return &WeightedRR{}
}

func (w *WeightedRR) Select(key string, backends []*Backend) (*Backend, error) {
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.currentWeights) != len(backends) {
		old := w.currentWeights
		w.currentWeights = make([]int64, len(backends))
		copy(w.currentWeights, old)
	}

	totalWeight := 0
	for _, b := range backends {
		totalWeight += b.Weight
	}
	if totalWeight == 0 {
		return backends[0], nil
	}

	var bestIdx int
	var bestWeight int64 = -1

	for i, b := range backends {
		w.currentWeights[i] += int64(b.Weight)
		if w.currentWeights[i] > bestWeight {
			bestWeight = w.currentWeights[i]
			bestIdx = i
		}
	}

	w.currentWeights[bestIdx] -= int64(totalWeight)

	return backends[bestIdx], nil
}

func (w *WeightedRR) Name() StrategyType {
	return StrategyWeightedRR
}
