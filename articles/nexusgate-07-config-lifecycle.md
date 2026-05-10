---
title: "NexusGate 配置热更新与生命周期管理"
slug: "nexusgate-07-config-lifecycle"
summary: "深入 NexusGate 的配置双缓冲机制、FileWatcher 热重载流程、Graceful 优雅关闭（LIFO 逆序执行）、HealthChecker 健康检查联动以及 Recoverable 可恢复 Goroutine 的设计与实现。"
category: "NexusGate"
tags: ["Go", "配置热更新", "双缓冲", "优雅关闭", "健康检查"]
is_draft: false
---

# 07 | 配置热更新与生命周期管理
> 「用 Go 枻建网关」专栏第 7 篇。本文详解配置双缓冲、文件监控热重载、优雅关闭、健康检查和可恢复 Goroutine 的实现。

---

## 配置双缓冲机制

### Store 架构

```go
type Store struct {
    mu    sync.RWMutex
    dirty atomic.Value   // 脏数据（可修改）
    clean atomic.Value   // 干净数据（已提交，对外可见）
    path  string
}
```

**双缓冲设计**：`dirty` 和 `clean` 各持一份配置副本。

| 操作 | 目标 | 说明 |
|------|------|------|
| `Get()` | clean | 读者永远看到已提交的稳定配置 |
| `Update()` | dirty | 修改脏数据，不影响对外可见的配置 |
| `Commit()` | dirty → clean | 深拷贝 dirty 到 clean，配置生效 |
| `Rollback()` | clean → dirty | 放弃修改，回滚到已提交版本 |

### 深拷贝实现

```go
func deepCopy(cfg *Config) *Config {
    data, err := yaml.Marshal(cfg)
    if err != nil {
        return shallowCopy(cfg)
    }
    var copy Config
    if err := yaml.Unmarshal(data, &copy); err != nil {
        return shallowCopy(cfg)
    }
    return &copy
}
```

通过 `yaml.Marshal → yaml.Unmarshal` 实现深拷贝。这是最简洁的深拷贝方案——不需要反射库，不需要手写每个字段的拷贝逻辑。代价是性能不如 `encoding/gob` 或手写拷贝，但配置变更频率极低（分钟级），性能不是瓶颈。

### Load 流程

```go
func (s *Store) Load() error {
    data, err := os.ReadFile(s.path)
    if err != nil {
        return err
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return err
    }
    s.mu.Lock()
    s.dirty.Store(&cfg)
    s.clean.Store(&cfg)
    s.mu.Unlock()
    return nil
}
```

`Load` 同时更新 dirty 和 clean，确保首次加载后两者一致。

## FileWatcher 热重载

### 轮询检测

```go
type FileWatcher struct {
    store     *Store
    interval  time.Duration     // 默认 5 秒
    cancel    context.CancelFunc
    callbacks []ConfigChangeCallback
}
```

**为什么选择轮询而非 fsnotify？**

| 维度 | 轮询 | fsnotify |
|------|------|----------|
| 依赖 | 零 | 需要 `golang.org/x/exp` 或第三方库 |
| 跨平台 | 完全一致 | 不同 OS 行为差异 |
| 延迟 | 5 秒 | 毫秒级 |
| 可靠性 | 极高 | 某些编辑器保存不触发事件 |
| 复杂度 | 极低 | 需要处理事件合并、溢出 |

5 秒延迟对于网关配置变更完全可以接受——配置变更不是高频操作。

### watchLoop 流程

```go
func (fw *FileWatcher) watchLoop(ctx context.Context) {
    ticker := time.NewTicker(fw.interval)
    var lastModTime time.Time

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            modTime, changed := fw.checkFileChange(lastModTime)
            if changed {
                oldCfg := fw.store.Get()
                fw.store.Load()
                newCfg := fw.store.Get()
                lastModTime = modTime
                fw.notifyCallbacks(oldCfg, newCfg)
            }
        }
    }
}
```

**变更检测**：比较文件的 `ModTime`，首次运行仅记录不触发回调。

**回调通知**：先保存旧配置快照 → 重新加载文件 → 获取新配置 → 通知回调。这确保回调能拿到变更前后的配置对比。

### 回调保护

```go
func (fw *FileWatcher) notifyCallbacks(oldCfg, newCfg *Config) {
    for _, cb := range fw.callbacks {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    slog.Error("config change callback panicked", "error", r)
                }
            }()
            cb(oldCfg, newCfg)
        }()
    }
}
```

每个回调在独立函数中执行，带 `recover()` 防止 panic 传播。一个回调崩溃不影响其他回调的执行。

### 热重载应用

```go
watcher.OnChange(func(oldCfg, newCfg *config.Config) {
    slog.Info("config changed, applying hot reload")

    newRoutes := config.BuildRoutes(newCfg)
    newRt := router.NewRouter()
    for _, route := range newRoutes {
        newRt.AddRoute(route)
    }
    rt.SwapRoutes(newRt)

    hc.StopAll()
    for _, route := range newRoutes {
        for _, b := range route.Backends {
            hc.Register(b.Address)
        }
    }
})
```

**原子路由切换**：先构建完整的新 Router，再通过 `SwapRoutes` 一次性替换。这避免了"一半旧路由一半新路由"的不一致状态。

**健康检查重置**：`StopAll()` 清除旧后端列表，重新注册新后端。旧后端的健康状态被丢弃，新后端默认健康。

## Graceful 优雅关闭

### 信号监听

```go
func (g *Graceful) Wait() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
    sig := <-sigCh
    slog.Info("shutting down", "signal", sig)
    g.executeShutdown()
}
```

### LIFO 逆序执行

```go
func (g *Graceful) executeShutdown() {
    done := make(chan struct{})
    go func() {
        g.mu.Lock()
        handlers := make([]func() error, len(g.shutdown))
        copy(handlers, g.shutdown)
        g.mu.Unlock()

        for i := len(handlers) - 1; i >= 0; i-- {
            if err := handlers[i](); err != nil {
                slog.Error("shutdown handler error", "error", err)
            }
        }
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(g.timeout):
        slog.Warn("graceful shutdown timed out, forcing exit")
    }
}
```

**注册顺序与执行顺序**：

```
注册（FIFO）：
  1. watcher.Stop()
  2. gw.Close()
  3. recoverable.StopAll()
  4. listener.Close()

执行（LIFO）：
  4. listener.Close()      ← 先停止接收新连接
  3. recoverable.StopAll()  ← 停止后台 goroutine
  2. gw.Close()             ← 等待在途请求完成
  1. watcher.Stop()         ← 最后停止配置监控
```

LIFO 确保了**先停入口，后停出口**——先关闭监听器停止接收新请求，再等待在途请求处理完毕，最后清理后台任务。

**超时保护**：默认 30 秒超时，超时后不再等待直接返回。这防止了某个关闭函数卡死导致进程无法退出。

## HealthChecker 健康检查

### 检查逻辑

```go
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
                notifications = append(notifications, struct {
                    address string
                    healthy bool
                }{bh.Address, false})
            }
        } else {
            if !bh.Healthy {
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

    if hc.onChange != nil {
        for _, n := range notifications {
            hc.onChange(n.address, n.healthy)
        }
    }
}
```

**回调延迟调用**：状态变更通知收集在 `notifications` 切片中，在所有锁释放后才调用 `onChange`。这避免了回调中获取同一把锁导致的死锁。

### 健康检查端点

```go
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
```

**HTTP 客户端复用**：HealthChecker 使用独立的 `http.Client`，配置了自定义 `Transport`：

```go
client: &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:      10,
        IdleConnTimeout:   30 * time.Second,
        DisableKeepAlives: false,
    },
}
```

独立的客户端避免与 Proxy 的 httpClient 共享连接池，健康检查请求不会影响业务请求的连接管理。

### 联动机制

```go
hc.OnChange(func(address string, healthy bool) {
    if healthy {
        px.Pool().MarkHealthy(address)
    } else {
        px.Pool().MarkUnhealthy(address)
    }
    rt.UpdateBackendHealth(address, healthy)
})
```

**双重更新**：
1. `BackendTracker.MarkUnhealthy/MarkHealthy`：影响 Proxy 的转发决策
2. `Router.UpdateBackendHealth`：影响路由的健康后端过滤

## Recoverable 可恢复 Goroutine

### 设计动机

网关有多个长期运行的后台 goroutine（健康检查、配置监控等），它们可能因为意外错误或 panic 而退出。Recoverable 确保这些 goroutine 在异常退出后能自动恢复。

### 实现

```go
func (r *Recoverable) Go(name string, fn func(ctx context.Context) error) {
    r.mu.Lock()
    ctx, cancel := context.WithCancel(context.Background())
    r.running[name] = cancel
    r.mu.Unlock()

    go func() {
        defer func() {
            if rec := recover(); rec != nil {
                slog.Error("goroutine panicked", "name", name, "stack", debug.Stack())
                r.mu.Lock()
                r.restarts[name]++
                count := r.restarts[name]
                r.mu.Unlock()

                if count <= r.maxRetry {
                    slog.Info("restarting goroutine", "name", name, "attempt", count)
                    time.Sleep(time.Duration(count) * time.Second)
                    r.Go(name, fn)
                }
            }
        }()

        if err := fn(ctx); err != nil {
            slog.Error("goroutine exited with error", "name", name, "error", err)
        }
    }()
}
```

**三层保护**：

1. **panic 恢复**：`recover()` 捕获 panic，记录完整堆栈（`debug.Stack()`）
2. **退避重启**：第 N 次重启等待 N 秒，避免快速重启循环
3. **最大重试**：超过 `maxRetry`（5 次）后放弃重启

**独立 context**：每个 goroutine 持有独立的 context，`Stop(name)` 通过取消 context 来停止指定 goroutine。

### 使用方式

```go
recoverable.Go("health-checker", func(ctx context.Context) error {
    return hc.Run(ctx)
})

recoverable.Go("config-watcher", func(ctx context.Context) error {
    return watcher.Start(ctx)
})
```

后台任务只需关注 `ctx.Done()` 信号，不需要自己处理 panic 和重启逻辑。

## 完整启动流程

```
1. 加载配置文件 → Store.Load()
2. 构建路由表 → BuildRoutes() → Router.AddRoute()
3. 创建代理 → NewProxy(poolSize, maxIdle)
4. 创建限流器 → NewRateLimiter(rate, burst)
5. 创建熔断器 → NewCircuitBreaker(thresholds...)
6. 组装中间件链 → Chain.Use(Trace).Use(AccessLog)...
7. 创建网关 → NewGateway(chain.Then(handler), queueSize)
8. 注册健康检查 → HealthChecker.Register(backends...)
9. 启动后台任务 → Recoverable.Go("health-checker", ...)
10. 启动配置监控 → Recoverable.Go("config-watcher", ...)
11. 启动指标端点 → http.ListenAndServe(metricsListen, mux)
12. 监听端口 → net.Listen("tcp", listen)
13. 等待关闭信号 → Graceful.Wait()
```

## 小结

NexusGate 的生命周期管理通过四个组件实现：

1. **Store 双缓冲**：dirty/clean 分离，读者无锁访问已提交配置
2. **FileWatcher**：5 秒轮询 + ModTime 检测 + 回调保护
3. **Graceful**：LIFO 逆序关闭 + 超时保护
4. **HealthChecker**：主动探测 + 回调延迟调用 + 双重联动
5. **Recoverable**：panic 恢复 + 退避重启 + 最大重试限制

这些组件共同保障了网关的**运行时稳定性**——配置可以热更新、后端故障可以自动摘除、后台任务可以自动恢复、关闭过程可以优雅完成。
