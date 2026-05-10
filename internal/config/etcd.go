package config

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.etcd.io/etcd/client/v3"
	"gopkg.in/yaml.v3"
)

type EtcdProviderConfig struct {
	Endpoints   []string      `yaml:"endpoints"`
	Prefix      string        `yaml:"prefix"`
	Watch       bool          `yaml:"watch"`
	DialTimeout time.Duration `yaml:"dial_timeout"`
	Username    string        `yaml:"username"`
	Password    string        `yaml:"password"`
}

type EtcdProvider struct {
	config  EtcdProviderConfig
	client  *clientv3.Client
	store   *Store
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

func NewEtcdProvider(cfg EtcdProviderConfig, store *Store) *EtcdProvider {
	return &EtcdProvider{
		config: cfg,
		store:  store,
	}
}

func (ep *EtcdProvider) Connect(ctx context.Context) error {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if ep.client != nil {
		return nil
	}

	dialTimeout := ep.config.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 5 * time.Second
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   ep.config.Endpoints,
		DialTimeout: dialTimeout,
		Username:    ep.config.Username,
		Password:    ep.config.Password,
	})
	if err != nil {
		return fmt.Errorf("connect to etcd: %w", err)
	}

	ep.client = cli
	slog.Info("connected to etcd", "endpoints", ep.config.Endpoints)
	return nil
}

func (ep *EtcdProvider) Load(ctx context.Context) error {
	if ep.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	prefix := ep.config.Prefix
	if prefix == "" {
		prefix = "/nexusgate/config/"
	}

	resp, err := ep.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("read config from etcd: %w", err)
	}

	if len(resp.Kvs) == 0 {
		slog.Warn("no config found in etcd", "prefix", prefix)
		return nil
	}

	var configYAML strings.Builder
	for _, kv := range resp.Kvs {
		key := strings.TrimPrefix(string(kv.Key), prefix)
		if key == "nexusgate.yaml" || key == "config" {
			configYAML.Write(kv.Value)
			break
		}
	}

	if configYAML.Len() == 0 {
		for _, kv := range resp.Kvs {
			if configYAML.Len() > 0 {
				configYAML.WriteByte('\n')
			}
			configYAML.Write(kv.Value)
		}
	}

	cfg, err := parseYAMLConfig([]byte(configYAML.String()))
	if err != nil {
		return fmt.Errorf("parse etcd config: %w", err)
	}

	ep.store.Update(func(current *Config) {
		*current = *cfg
	})

	slog.Info("loaded config from etcd", "keys", len(resp.Kvs))
	return nil
}

func (ep *EtcdProvider) StartWatch(ctx context.Context) error {
	if !ep.config.Watch {
		return nil
	}

	ep.mu.Lock()
	if ep.running {
		ep.mu.Unlock()
		return nil
	}

	if ep.client == nil {
		ep.mu.Unlock()
		return fmt.Errorf("etcd client not connected")
	}

	watchCtx, cancel := context.WithCancel(ctx)
	ep.cancel = cancel
	ep.running = true
	ep.mu.Unlock()

	prefix := ep.config.Prefix
	if prefix == "" {
		prefix = "/nexusgate/config/"
	}

	go func() {
		defer func() {
			ep.mu.Lock()
			ep.running = false
			ep.mu.Unlock()
		}()

		watchCh := ep.client.Watch(watchCtx, prefix, clientv3.WithPrefix())
		slog.Info("watching etcd for config changes", "prefix", prefix)

		for {
			select {
			case <-watchCtx.Done():
				slog.Info("etcd watch stopped")
				return
			case watchResp, ok := <-watchCh:
				if !ok {
					slog.Warn("etcd watch channel closed")
					return
				}
				if err := watchResp.Err(); err != nil {
					slog.Error("etcd watch error", "error", err)
					return
				}
				for _, event := range watchResp.Events {
					slog.Info("etcd config change detected",
						"type", event.Type.String(),
						"key", string(event.Kv.Key),
					)
				}
				if err := ep.Load(watchCtx); err != nil {
					slog.Error("failed to reload config from etcd", "error", err)
				}
			}
		}
	}()

	return nil
}

func (ep *EtcdProvider) Stop() {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if ep.cancel != nil {
		ep.cancel()
		ep.cancel = nil
	}

	if ep.client != nil {
		ep.client.Close()
		ep.client = nil
	}
}

func parseYAMLConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	return &cfg, nil
}
