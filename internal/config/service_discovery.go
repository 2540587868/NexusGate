package config

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.etcd.io/etcd/client/v3"
)

type ServiceDiscoveryConfig struct {
	Prefix      string        `yaml:"prefix"`
	PollInterval time.Duration `yaml:"poll_interval"`
}

type ServiceDiscovery struct {
	config    ServiceDiscoveryConfig
	client    *clientv3.Client
	store     *Store
	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
}

func NewServiceDiscovery(cfg ServiceDiscoveryConfig, client *clientv3.Client, store *Store) *ServiceDiscovery {
	return &ServiceDiscovery{
		config: cfg,
		client: client,
		store:  store,
	}
}

func (sd *ServiceDiscovery) Start(ctx context.Context) error {
	sd.mu.Lock()
	if sd.running {
		sd.mu.Unlock()
		return nil
	}
	sdCtx, cancel := context.WithCancel(ctx)
	sd.cancel = cancel
	sd.running = true
	sd.mu.Unlock()

	prefix := sd.config.Prefix
	if prefix == "" {
		prefix = "/nexusgate/services/"
	}

	pollInterval := sd.config.PollInterval
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}

	go func() {
		defer func() {
			sd.mu.Lock()
			sd.running = false
			sd.mu.Unlock()
		}()

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		sd.syncServices(sdCtx, prefix)

		for {
			select {
			case <-sdCtx.Done():
				slog.Info("service discovery stopped")
				return
			case <-ticker.C:
				sd.syncServices(sdCtx, prefix)
			}
		}
	}()

	watchCh := sd.client.Watch(sdCtx, prefix, clientv3.WithPrefix())
	go func() {
		for {
			select {
			case <-sdCtx.Done():
				return
			case watchResp, ok := <-watchCh:
				if !ok {
					return
				}
				if err := watchResp.Err(); err != nil {
					slog.Error("service discovery watch error", "error", err)
					return
				}
				for _, event := range watchResp.Events {
					slog.Info("service change detected",
						"type", event.Type.String(),
						"key", string(event.Kv.Key),
					)
				}
				sd.syncServices(sdCtx, prefix)
			}
		}
	}()

	slog.Info("service discovery started", "prefix", prefix, "poll_interval", pollInterval)
	return nil
}

func (sd *ServiceDiscovery) Stop() {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if sd.cancel != nil {
		sd.cancel()
		sd.cancel = nil
	}
}

func (sd *ServiceDiscovery) syncServices(ctx context.Context, prefix string) {
	resp, err := sd.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		slog.Error("service discovery: failed to get services", "error", err)
		return
	}

	services := make(map[string][]string)
	for _, kv := range resp.Kvs {
		key := strings.TrimPrefix(string(kv.Key), prefix)
		parts := strings.SplitN(key, "/", 2)
		if len(parts) < 2 {
			continue
		}
		serviceName := parts[0]
		address := strings.TrimSpace(string(kv.Value))
		if address != "" {
			services[serviceName] = append(services[serviceName], address)
		}
	}

	if len(services) == 0 {
		return
	}

	sd.store.Update(func(cfg *Config) {
		for i := range cfg.Routes {
			serviceTag := cfg.Routes[i].Match.Headers["X-Service-Name"]
			if serviceTag == "" {
				continue
			}
			addresses, ok := services[serviceTag]
			if !ok {
				continue
			}

			existingAddrs := make(map[string]bool)
			for _, b := range cfg.Routes[i].Backend {
				existingAddrs[b.Address] = true
			}

			for _, addr := range addresses {
				if !existingAddrs[addr] {
					cfg.Routes[i].Backend = append(cfg.Routes[i].Backend, BackendConfig{
						Address: addr,
						Weight:  1,
					})
					slog.Info("service discovery: registered backend",
						"service", serviceTag,
						"address", addr,
					)
				}
			}

			var activeBackends []BackendConfig
			addrSet := make(map[string]bool)
			for _, addr := range addresses {
				addrSet[addr] = true
			}
			for _, b := range cfg.Routes[i].Backend {
				if addrSet[b.Address] {
					activeBackends = append(activeBackends, b)
				}
			}
			if len(activeBackends) > 0 {
				cfg.Routes[i].Backend = activeBackends
			}
		}
	})
	sd.store.Commit()

	slog.Debug("service discovery: synced", "services", len(services), "keys", len(resp.Kvs))
}

func (sd *ServiceDiscovery) RegisterService(ctx context.Context, serviceName, address string, ttl time.Duration) error {
	if sd.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	prefix := sd.config.Prefix
	if prefix == "" {
		prefix = "/nexusgate/services/"
	}

	key := fmt.Sprintf("%s%s/%s", prefix, serviceName, strings.ReplaceAll(address, ":", "_"))
	lease, err := sd.client.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		return fmt.Errorf("grant lease: %w", err)
	}

	_, err = sd.client.Put(ctx, key, address, clientv3.WithLease(lease.ID))
	if err != nil {
		return fmt.Errorf("register service: %w", err)
	}

	ch, err := sd.client.KeepAlive(ctx, lease.ID)
	if err != nil {
		return fmt.Errorf("keepalive: %w", err)
	}

	go func() {
		for range ch {
		}
	}()

	slog.Info("service registered", "service", serviceName, "address", address, "ttl", ttl)
	return nil
}
