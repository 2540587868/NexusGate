---
title: "NexusGate 代理转发：重试、超时与连接管理"
slug: "nexusgate-05-proxy-forwarding"
summary: "深入 NexusGate 的代理转发实现，详解 Forward 重试流程、指数退避策略、BackendTracker 健康追踪、httpClient Transport 配置以及 WithTimeouts 的完整实现。"
category: "NexusGate"
tags: ["Go", "代理转发", "重试策略", "指数退避", "连接池"]
is_draft: false
---

# 05 | 代理转发：重试、超时与连接管理
> 「用 Go 构建网关」专栏第 5 篇。本文详解代理转发的重试机制、超时控制和连接管理。

---

## 代理转发架构

```
Proxy.Forward()
  ├── 重试循环 (最多 MaxRetries+1 次)
  │   ├── 指数退避等待
  │   └── doForward()
  │       ├── 构建目标 URL
  │       ├── 构建 HTTP 请求
  │       ├── 设置转发头 (X-Forwarded-*)
  │       ├── httpClient.Do()
  │       ├── 读取响应体 (≤10MB)
  │       └── 5xx → MarkUnhealthy
  └── 返回最终结果或错误
```

## Forward 重试流程

### 重试策略配置

```go
type RetryPolicy struct {
    MaxRetries      int              // 最大重试次数，默认 2
    RetryableStatus []int            // 可重试的 HTTP 状态码，默认 [502, 503, 504]
    Backoff         BackoffStrategy  // 退避策略
}
```

### 重试循环

```go
func (p *Proxy) Forward(req *gateway.Request, backend *router.Backend) (*gateway.Response, error) {
    var resp *gateway.Response
    var err error

    for attempt := 0; attempt <= p.retryPolicy.MaxRetries; attempt++ {
        if attempt > 0 {
            delay := p.retryPolicy.Backoff.Next(attempt - 1)
            slog.Debug("retrying request", "attempt", attempt, "delay", delay, "backend", backend.Address)
            time.Sleep(delay)
        }

        resp, err = p.doForward(req, backend)
        if err == nil {
            return resp, nil
        }

        if gwErr, ok := err.(*gateway.GatewayError); ok {
            if !isRetryableError(gwErr) {
                return nil, err
            }
        }

        slog.Warn("forward attempt failed", "attempt", attempt, "backend", backend.Address, "error", err)
    }

    if err != nil {
        return nil, err
    }
    return resp, nil
}
```

**重试决策链**：

```
doForward 返回 err == nil → 成功，立即返回
doForward 返回 err != nil → 检查错误类型
  ├── isRetryableError → 可重试，继续循环
  └── !isRetryableError → 不可重试，立即返回
```

**可重试条件**：仅 `ErrBackendDown`（连接失败）和 `ErrBackendTimeout`（响应超时）可重试。其他错误（如 `ErrBadRequest`、`ErrCircuitOpen`）不可重试——重试不会改变结果。

### 指数退避

```go
type ExponentialBackoff struct {
    Base   time.Duration   // 基础延迟，默认 100ms
    Max    time.Duration   // 最大延迟，默认 5s
    Jitter bool            // 是否添加抖动
}

func (eb *ExponentialBackoff) Next(attempt int) time.Duration {
    delay := eb.Base
    for i := 0; i < attempt; i++ {
        delay *= 2
        if delay > eb.Max {
            delay = eb.Max
            break
        }
    }

    if eb.Jitter {
        jitter := time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
        delay = jitter
    }

    return delay
}
```

**退避序列**（默认配置）：

| 重试次数 | 基础延迟 | 抖动范围 |
|----------|----------|----------|
| 1 | 100ms | 80ms - 120ms |
| 2 | 200ms | 160ms - 240ms |
| 3 | 400ms | 320ms - 480ms |
| 4 | 800ms | 640ms - 960ms |
| 5 | 1600ms | 1280ms - 1920ms |
| 6+ | 5000ms | 4000ms - 6000ms |

**Jitter 的必要性**：当多个请求同时失败并同时重试时，没有 Jitter 会导致所有重试请求在同一时刻到达后端——**惊群效应**。Jitter 将重试时间随机分散，避免后端承受瞬时重试风暴。

**为什么用 `rand.Float64()` 而非 `crypto/rand`？**

退避抖动不需要密码学安全性，`math/rand` 的性能足够且无额外依赖。

## doForward 转发细节

### 请求构建

```go
func (p *Proxy) doForward(req *gateway.Request, backend *router.Backend) (*gateway.Response, error) {
    scheme := "http"
    if req.Scheme == "https" {
        scheme = "https"
    }

    targetURL := fmt.Sprintf("%s://%s%s", scheme, backend.Address, req.Path)
    if req.QueryString != "" {
        targetURL += "?" + req.QueryString
    }
    // ...
}
```

**URL 构建**：`scheme://backend.Address + path + querystring`。注意 `backend.Address` 已包含端口（如 `10.0.0.1:8080`），无需额外拼接。

### 转发头设置

```go
// Host 头
if httpReq.Host == "" {
    httpReq.Host = req.Host
}

// X-Forwarded-For：追加客户端地址
existingXFF := httpReq.Header.Get("X-Forwarded-For")
if existingXFF != "" {
    httpReq.Header.Set("X-Forwarded-For", existingXFF+", "+req.RemoteAddr)
} else {
    httpReq.Header.Set("X-Forwarded-For", req.RemoteAddr)
}

// X-Forwarded-Proto：原始协议
httpReq.Header.Set("X-Forwarded-Proto", req.Scheme)
```

**X-Forwarded-For 追加语义**：如果请求已经携带 XFF 头（说明经过上游代理），则追加而非替换，保留完整的代理链路。

### 响应处理

```go
httpResp, err := p.httpClient.Do(httpReq)
if err != nil {
    p.tracker.MarkUnhealthy(backend.Address)
    return nil, gateway.NewGatewayError(gateway.ErrBackendDown,
        "backend request failed", err.Error())
}
defer httpResp.Body.Close()

var body []byte
if httpResp.Body != nil {
    body, err = io.ReadAll(io.LimitReader(httpResp.Body, 10<<20))
    if err != nil {
        return nil, gateway.NewGatewayError(gateway.ErrBackendTimeout,
            "read backend response failed", err.Error())
    }
}

if httpResp.StatusCode >= 500 {
    p.tracker.MarkUnhealthy(backend.Address)
}
```

**响应体限制**：`io.LimitReader` 限制最大 10MB，防止后端返回超大响应导致网关 OOM。

**5xx 标记不健康**：后端返回 5xx 时标记不健康，但仍然将响应返回给客户端——客户端需要知道后端出错了。标记不健康只是让 HealthChecker 和后续请求感知到后端异常。

## BackendTracker 健康追踪

### 数据结构

```go
type BackendTracker struct {
    mu       sync.Mutex
    backends map[string]*backendState
}

type backendState struct {
    healthy atomic.Bool
    inUse   atomic.Int64
}
```

### 核心方法

```go
func (bt *BackendTracker) MarkUnhealthy(address string) {
    bt.mu.Lock()
    defer bt.mu.Unlock()
    if st, ok := bt.backends[address]; ok {
        st.healthy.Store(false)
    }
}

func (bt *BackendTracker) MarkHealthy(address string) {
    bt.mu.Lock()
    defer bt.mu.Unlock()
    if st, ok := bt.backends[address]; ok {
        st.healthy.Store(true)
    }
}

func (bt *BackendTracker) IsHealthy(address string) bool {
    bt.mu.Lock()
    st, ok := bt.backends[address]
    bt.mu.Unlock()
    if !ok {
        return true  // 未知后端默认健康
    }
    return st.healthy.Load()
}
```

**设计决策**：
- `Mutex` 仅保护 map 的增删，不保护 `healthy` 的读写（由 `atomic.Bool` 保护）
- 未知后端默认健康——避免新加入的后端因未注册而被误判
- `inUse` 字段预留给未来的连接数追踪

### 与 HealthChecker 的联动

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

健康检查结果同时更新 BackendTracker 和 Router 的 Backend.Healthy，形成双重保障：
- **主动探测**：HealthChecker 定期 GET `/health`
- **被动感知**：Proxy 在转发失败时标记不健康

## httpClient Transport 配置

### 默认配置

```go
func NewProxy(poolSize int, maxIdle int) *Proxy {
    maxIdleConns := poolSize       // 全局最大空闲连接
    maxIdlePerHost := maxIdle      // 每个 Host 最大空闲连接

    transport := &http.Transport{
        MaxIdleConns:        maxIdleConns,
        MaxIdleConnsPerHost: maxIdlePerHost,
        IdleConnTimeout:     90 * time.Second,
        DisableKeepAlives:   false,
    }

    return &Proxy{
        httpClient: &http.Client{
            Timeout:   30 * time.Second,
            Transport: transport,
            CheckRedirect: func(req *http.Request, via []*http.Request) error {
                return http.ErrUseLastResponse
            },
        },
    }
}
```

**关键配置解读**：

| 参数 | 默认值 | 含义 |
|------|--------|------|
| `MaxIdleConns` | poolSize | 全局最大空闲连接数 |
| `MaxIdleConnsPerHost` | maxIdle | 每个 Host 最大空闲连接数 |
| `IdleConnTimeout` | 90s | 空闲连接超时，超时后关闭 |
| `DisableKeepAlives` | false | 启用 Keep-Alive，复用 TCP 连接 |
| `Timeout` | 30s | 整体请求超时（含连接+TLS+读写） |
| `CheckRedirect` | 不跟随 | 返回 3xx 响应，不自动跟随重定向 |

### WithTimeouts 完整实现

```go
func (p *Proxy) WithTimeouts(connect, read, write time.Duration) *Proxy {
    p.httpClient.Timeout = read
    if tr, ok := p.httpClient.Transport.(*http.Transport); ok {
        dialer := &net.Dialer{
            Timeout:   connect,
            KeepAlive: 30 * time.Second,
        }
        tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
            return dialer.DialContext(ctx, network, addr)
        }
        tr.IdleConnTimeout = 90 * time.Second
        tr.ResponseHeaderTimeout = write
    }
    return p
}
```

**三级超时体系**：

| 超时 | 配置位置 | 含义 |
|------|----------|------|
| connect | `Dialer.Timeout` | TCP 连接建立超时 |
| read | `httpClient.Timeout` | 整体请求超时（含所有阶段） |
| write | `ResponseHeaderTimeout` | 等待后端响应头的超时 |

**为什么 `httpClient.Timeout = read` 而非单独设置？**

Go 的 `http.Client.Timeout` 是整体超时，包含连接建立、TLS 握手、发送请求、等待响应头、读取响应体的全部时间。它是最后一道防线——即使其他超时未生效，整体超时也能保证请求不会无限等待。

## 连接复用与 Keep-Alive

### 连接生命周期

```
请求1 → 建立 TCP 连接 → 发送请求 → 读取响应 → 连接放入空闲池
请求2 → 从空闲池取出连接 → 发送请求 → 读取响应 → 连接放回空闲池
...
空闲 90s → 连接超时关闭
```

**Keep-Alive 的性能收益**：

| 指标 | 无 Keep-Alive | 有 Keep-Alive |
|------|---------------|---------------|
| TCP 连接数 | 每请求一个 | 复用，1 个连接可服务 100+ 请求 |
| 连接建立延迟 | ~1ms（本地） | 0ms（复用） |
| TLS 握手 | 每次握手 | 一次握手，后续复用 |

### 连接池参数调优

```yaml
proxy:
  pool_size: 256        # 全局最大空闲连接
  pool_max_idle: 64     # 每 Host 最大空闲连接
  connect_timeout: 5s
  read_timeout: 30s
  write_timeout: 30s
```

**调优建议**：

| 场景 | pool_size | pool_max_idle | 理由 |
|------|-----------|---------------|------|
| 少量后端 | 64 | 32 | 后端少，每后端连接多 |
| 大量后端 | 512 | 16 | 后端多，每后端连接少 |
| 高延迟后端 | 256 | 64 | 连接占用时间长，需要更多空闲连接 |

## 小结

NexusGate 的代理转发通过三个层次保障可靠性：

1. **重试机制**：指数退避 + Jitter 抖动，最多 2 次重试，仅对可恢复错误重试
2. **超时控制**：三级超时（connect/read/write），确保请求不会无限等待
3. **健康追踪**：BackendTracker 被动感知 + HealthChecker 主动探测，双重保障

这些机制共同构成了 NexusGate 的**弹性代理层**——在后端故障时自动重试和摘除，在后端恢复时自动加回。
