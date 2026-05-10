package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type HealthChecker struct {
	mu        sync.RWMutex
	backends  map[string]*BackendHealth
	interval  time.Duration
	timeout   time.Duration
	threshold int
	onChange  func(address string, healthy bool)
	client    *http.Client
}

type BackendHealth struct {
	Address         string
	Healthy         bool
	ConsecutiveFails int
	LastCheck       time.Time
}

func NewHealthChecker(interval, timeout time.Duration, threshold int) *HealthChecker {
	return &HealthChecker{
		backends:  make(map[string]*BackendHealth),
		interval:  interval,
		timeout:   timeout,
		threshold: threshold,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableKeepAlives:   false,
			},
		},
	}
}

func (hc *HealthChecker) OnChange(fn func(address string, healthy bool)) {
	hc.onChange = fn
}

func (hc *HealthChecker) Register(address string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if _, ok := hc.backends[address]; !ok {
		hc.backends[address] = &BackendHealth{
			Address: address,
			Healthy: true,
		}
	}
}

func (hc *HealthChecker) Unregister(address string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	delete(hc.backends, address)
}

func (hc *HealthChecker) StopAll() {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.backends = make(map[string]*BackendHealth)
}

func (hc *HealthChecker) Run(ctx context.Context) error {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			hc.checkAll()
		}
	}
}

func (hc *HealthChecker) checkAll() {
	hc.mu.RLock()
	backends := make([]*BackendHealth, 0, len(hc.backends))
	for _, bh := range hc.backends {
		backends = append(backends, bh)
	}
	hc.mu.RUnlock()

	var notifications []struct {
		address string
		healthy bool
	}

	for _, bh := range backends {
		err := hc.check(bh.Address)
		hc.mu.Lock()
		if err != nil {
			bh.ConsecutiveFails++
			if bh.ConsecutiveFails >= hc.threshold && bh.Healthy {
				bh.Healthy = false
				slog.Warn("backend marked unhealthy",
					"address", bh.Address,
					"consecutive_fails", bh.ConsecutiveFails,
				)
				notifications = append(notifications, struct {
					address string
					healthy bool
				}{bh.Address, false})
			}
		} else {
			if !bh.Healthy {
				slog.Info("backend recovered",
					"address", bh.Address,
				)
				notifications = append(notifications, struct {
					address string
					healthy bool
				}{bh.Address, true})
			}
			bh.ConsecutiveFails = 0
			bh.Healthy = true
		}
		bh.LastCheck = time.Now()
		hc.mu.Unlock()
	}

	hc.mu.RLock()
	onChange := hc.onChange
	hc.mu.RUnlock()

	if onChange != nil {
		for _, n := range notifications {
			onChange(n.address, n.healthy)
		}
	}
}

func (hc *HealthChecker) check(address string) error {
	url := fmt.Sprintf("http://%s/health", address)
	resp, err := hc.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("unhealthy status: %d", resp.StatusCode)
	}
	return nil
}

func (hc *HealthChecker) IsHealthy(address string) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if bh, ok := hc.backends[address]; ok {
		return bh.Healthy
	}
	return false
}

func (hc *HealthChecker) Status() map[string]BackendHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]BackendHealth)
	for addr, bh := range hc.backends {
		result[addr] = *bh
	}
	return result
}
