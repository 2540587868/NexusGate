---
title: "NexusGate 中间件链：流量治理的六把利器"
slug: "nexusgate-06-middleware-chain"
summary: "深入 NexusGate 的链式中间件实现，详解 Chain 不可变组合模式、CORS 跨域处理、令牌桶限流、三态熔断器、Prometheus 指标收集和请求追踪的完整设计与实现。"
category: "NexusGate"
tags: ["Go", "中间件", "熔断器", "限流", "CORS", "可观测性"]
is_draft: false
---

# 06 | 中间件链：流量治理的六把利器
> 「用 Go 构建网关」专栏第 6 篇。本文详解链式中间件的组合模式和六种中间件的实现细节。

---

## Chain 不可变组合模式

### 核心实现

```go
type Chain struct {
    middlewares []gateway.Middleware
}

func (c *Chain) Use(mw gateway.Middleware) *Chain {
    newMiddlewares := make([]gateway.Middleware, len(c.middlewares)+1)
    copy(newMiddlewares, c.middlewares)
    newMiddlewares[len(c.middlewares)] = mw
    return &Chain{middlewares: newMiddlewares}
}

func (c *Chain) Then(handler gateway.Handler) gateway.Handler {
    if len(c.middlewares) == 0 {
        return handler
    }
    result := handler
    for i := len(c.middlewares) - 1; i >= 0; i-- {
        result = c.middlewares[i](result)
    }
    return result
}
```

### 反向包装原理

注册顺序 `[Trace, AccessLog, RateLimit, CORS, CircuitBreaker]`，`Then` 反向遍历：

```
result = handler
result = CircuitBreaker(handler)                    // i=4
result = CORS(CircuitBreaker(handler))              // i=3
result = RateLimit(CORS(CircuitBreaker(handler)))   // i=2
result = AccessLog(RateLimit(CORS(CircuitBreaker(handler))))  // i=1
result = Trace(AccessLog(RateLimit(CORS(CircuitBreaker(handler)))))  // i=0
```

执行顺序：Trace → AccessLog → RateLimit → CORS → CircuitBreaker → handler

**为什么反向遍历？**

中间件 `func(Handler) Handler` 的语义是"包装下一个 Handler"。最后注册的中间件最靠近 handler，最先注册的中间件最外层。反向遍历确保了**注册顺序 = 执行顺序**。

### 不可变性

`Use` 返回新 Chain，不修改原 Chain：

```go
chain := middleware.NewChain()
chain = chain.Use(middleware.Trace)          // 返回新 Chain
chain = chain.Use(middleware.AccessLog)      // 返回新 Chain
chain = chain.Use(middleware.RateLimit(rl))  // 返回新 Chain
```

好处：
- 可以基于同一个基础 Chain 创建不同的中间件组合
- 避免并发修改问题
- 符合函数式编程的不可变数据原则

## Trace — 请求追踪

```go
func Trace(next gateway.Handler) gateway.Handler {
    return func(req *gateway.Request) (*gateway.Response, error) {
        start := time.Now()
        traceID := req.ID
        if traceID == "" {
            traceID = generateTraceID()
            req.ID = traceID
        }
        slog.Debug("request started", "trace_id", traceID, "method", req.Method, "path", req.Path)
        resp, err := next(req)
        duration := time.Since(start)
        if err != nil {
            slog.Error("request failed", "trace_id", traceID, "duration_ms", duration.Milliseconds(), "error", err)
        } else {
            slog.Info("request completed", "trace_id", traceID, "duration_ms", duration.Milliseconds(), "status", resp.StatusCode)
        }
        return resp, err
    }
}
```

**TraceID 生成**：`时间戳-随机16位hex`，确保全局唯一。

**为什么放在最外层？**

Trace 需要记录整个请求的完整耗时，包括所有中间件的执行时间。放在最外层可以捕获最完整的请求生命周期。

## AccessLog — 访问日志 + 指标记录

```go
func AccessLog(next gateway.Handler) gateway.Handler {
    return func(req *gateway.Request) (*gateway.Response, error) {
        RecordRequestStart()
        start := time.Now()
        resp, err := next(req)
        duration := time.Since(start)
        success := err == nil && (resp == nil || resp.StatusCode < 500)
        RecordRequestEnd(success, duration)
        slog.Info("access", "method", req.Method, "path", req.Path,
            "status", status, "duration_us", duration.Microseconds(),
            "remote_addr", req.RemoteAddr, "tenant_id", req.TenantID)
        return resp, err
    }
}
```

**双重职责**：
1. 记录结构化访问日志（方法、路径、状态码、耗时微秒）
2. 更新 Metrics 指标（requestTotal、activeRequests、success/failed）

**耗时为什么用微秒？**

网关自身处理时间通常在 10-100μs 级别，毫秒精度不够。微秒级日志可以帮助发现性能退化。

## RateLimit — 令牌桶限流

### 令牌桶算法

```
          令牌生成速率 = rate/s
              ↓
    ┌─────────────────────┐
    │    Token Bucket     │ ← 容量 = burst
    │  ████████░░░░░░░░░  │
    └─────────┬───────────┘
              │
         tokens >= 1?
         ├── 是 → 消耗 1 个令牌，放行
         └── 否 → 拒绝（429 Too Many Requests）
```

### 实现

```go
type RateLimiter struct {
    mu             sync.Mutex
    buckets        map[string]*tokenBucket
    rate           float64         // 每秒生成令牌数
    burst          int             // 桶容量
    maxBuckets     int             // 最大桶数（100000）
    lastCleanup    time.Time
    cleanupInterval time.Duration  // 5 分钟
}

type tokenBucket struct {
    tokens   float64
    lastTime time.Time
}

func (rl *RateLimiter) Allow(key string) bool {
    rl.mu.Lock()
    defer rl.mu.Unlock()

    now := time.Now()
    if now.Sub(rl.lastCleanup) > rl.cleanupInterval {
        rl.cleanup(now)
    }

    bucket, ok := rl.buckets[key]
    if !ok {
        if len(rl.buckets) >= rl.maxBuckets {
            rl.cleanup(now)
        }
        bucket = &tokenBucket{tokens: float64(rl.burst), lastTime: now}
        rl.buckets[key] = bucket
    }

    elapsed := now.Sub(bucket.lastTime).Seconds()
    bucket.tokens += elapsed * rl.rate
    if bucket.tokens > float64(rl.burst) {
        bucket.tokens = float64(rl.burst)
    }
    bucket.lastTime = now

    if bucket.tokens < 1 {
        return false
    }
    bucket.tokens--
    return true
}
```

**关键设计**：

| 设计点 | 实现 | 理由 |
|--------|------|------|
| 限流维度 | TenantID | 多租户隔离，不同租户独立限流 |
| 新桶初始令牌 | burst（满桶） | 新租户首次请求不应被拒绝 |
| 桶数上限 | 100000 | 防止 DDoS 攻击创建无限桶导致 OOM |
| 清理间隔 | 5 分钟 | 平衡内存回收和 CPU 开销 |
| 过期判定 | 10 分钟无活跃 | 超过 10 分钟未访问的桶被清理 |

### 中间件集成

```go
func RateLimit(rl *RateLimiter) gateway.Middleware {
    return func(next gateway.Handler) gateway.Handler {
        return func(req *gateway.Request) (*gateway.Response, error) {
            if !rl.Allow(req.TenantID) {
                RecordRateLimitRejected()
                return nil, gateway.NewGatewayError(gateway.ErrRateLimited,
                    "rate limit exceeded", "too many requests")
            }
            return next(req)
        }
    }
}
```

限流失败时记录指标并返回 429 错误，不进入后续中间件。

## CORS — 跨域资源共享

### 配置

```go
type CORSOptions struct {
    AllowOrigins     []string
    AllowMethods     []string
    AllowHeaders     []string
    ExposeHeaders    []string
    AllowCredentials bool
    MaxAge           int
}
```

### 核心逻辑

```go
func CORS(opts CORSOptions) gateway.Middleware {
    return func(next gateway.Handler) gateway.Handler {
        return func(req *gateway.Request) (*gateway.Response, error) {
            if req.Method == "OPTIONS" {
                resp := &gateway.Response{StatusCode: 204, Headers: http.Header{}}
                applyCORSHeaders(resp, req, opts)
                return resp, nil
            }
            resp, err := next(req)
            if resp != nil {
                applyCORSHeaders(resp, req, opts)
            }
            return resp, err
        }
    }
}
```

**OPTIONS 预检短路**：OPTIONS 请求不进入后续中间件和 handler，直接返回 204 + CORS 头。这是浏览器跨域预检的标准处理方式。

### Origin 匹配

```go
func originMatches(origin string, allowOrigins []string) bool {
    if len(allowOrigins) == 0 {
        return true
    }
    for _, allowed := range allowOrigins {
        if allowed == "*" || allowed == origin {
            return true
        }
    }
    return false
}
```

**Vary: Origin 头**：无论匹配结果如何，都设置 `Vary: Origin`，防止 CDN 缓存错误的 CORS 响应。

## CircuitBreaker — 三态熔断器

### 状态机

```
         失败数 ≥ 阈值
  Closed ──────────────→ Open
    ↑                      │
    │    探测成功数 ≥ 阈值   │ 超时
    │                      ↓
    └──────────────── HalfOpen
                 ↑      │
                 │ 失败  │
                 └──────┘
```

### 数据结构

```go
type CircuitBreaker struct {
    state            State          // Closed/Open/HalfOpen
    failureCount     int
    successCount     int
    failureThreshold int            // 失败阈值，默认 5
    successThreshold int            // 成功阈值，默认 3
    timeout          time.Duration  // Open→HalfOpen 超时，默认 30s
    lastFailure      time.Time
    halfOpenActive   int            // 半开态活跃请求数
}
```

### Allow 判定

```go
func (cb *CircuitBreaker) Allow() error {
    switch cb.state {
    case StateClosed:
        return nil
    case StateOpen:
        if time.Since(cb.lastFailure) > cb.timeout {
            cb.state = StateHalfOpen
            cb.halfOpenActive = 0
            return nil
        }
        return ErrCircuitOpen
    case StateHalfOpen:
        if cb.halfOpenActive >= cb.successThreshold {
            return ErrCircuitOpen
        }
        cb.halfOpenActive++
        return nil
    }
    return nil
}
```

**半开态限流**：`halfOpenActive >= successThreshold` 时拒绝新请求。这确保了半开态最多有 `successThreshold` 个试探请求同时执行，防止试探请求过多导致后端再次过载。

### RecordSuccess / RecordFailure

```go
func (cb *CircuitBreaker) RecordSuccess() {
    switch cb.state {
    case StateHalfOpen:
        cb.successCount++
        cb.halfOpenActive--
        if cb.successCount >= cb.successThreshold {
            cb.state = StateClosed
            cb.failureCount = 0
            cb.successCount = 0
        }
    case StateClosed:
        cb.failureCount = 0
    }
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.failureCount++
    cb.lastFailure = time.Now()
    switch cb.state {
    case StateClosed:
        if cb.failureCount >= cb.failureThreshold {
            cb.state = StateOpen
        }
    case StateHalfOpen:
        cb.state = StateOpen
        cb.halfOpenActive--
        cb.successCount = 0
    }
}
```

**Closed 态成功重置**：任何一次成功都重置 `failureCount`，避免历史失败累积导致误触发。

### 中间件集成

```go
func CircuitBreakerMiddleware(cb *CircuitBreaker) gateway.Middleware {
    return func(next gateway.Handler) gateway.Handler {
        return func(req *gateway.Request) (*gateway.Response, error) {
            if err := cb.Allow(); err != nil {
                RecordCircuitBreakerOpen()
                return nil, gateway.NewGatewayError(gateway.ErrCircuitOpen,
                    "circuit breaker is open", "service temporarily unavailable")
            }
            resp, err := next(req)
            if err != nil || (resp != nil && resp.StatusCode >= 500) {
                cb.RecordFailure()
            } else {
                cb.RecordSuccess()
            }
            return resp, err
        }
    }
}
```

**5xx 也算失败**：即使请求成功到达后端，如果后端返回 5xx，也视为失败——后端可能在过载。

## Metrics — 零依赖指标系统

### 指标类型

| 指标名 | 类型 | 实现 |
|--------|------|------|
| request_total | Counter | `atomic.Int64` |
| request_success / request_failed | Counter | `atomic.Int64` |
| active_requests | Gauge | `atomic.Int64` |
| circuit_breaker_open | Counter | `atomic.Int64` |
| rate_limit_rejected | Counter | `atomic.Int64` |
| request_duration | Histogram | 10 桶 + `atomic.Int64` |
| backend_requests / backend_failures | Counter per backend | `sync.Map` |
| backend_latency | Summary per backend | `sync.Map` + `latencyTracker` |

### 自研直方图

```go
var histogramBuckets = []time.Duration{
    0, 5 * time.Millisecond, 10 * time.Millisecond, 25 * time.Millisecond,
    50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond,
    500 * time.Millisecond, time.Second, math.MaxInt64,
}

func RecordRequestEnd(success bool, duration time.Duration) {
    // 更新 success/failed 计数器
    // 更新 active_requests（递减）
    // 更新直方图
    for i, bucket := range histogramBuckets {
        if duration <= bucket {
            requestDurationCounts[i].Add(1)
            break
        }
    }
}
```

### Prometheus 格式输出

```
# HELP nexusgate_request_total Total number of requests
# TYPE nexusgate_request_total counter
nexusgate_request_total 12345

# HELP nexusgate_request_duration_seconds Request duration histogram
# TYPE nexusgate_request_duration_seconds histogram
nexusgate_request_duration_seconds_bucket{le="0.005"} 8000
nexusgate_request_duration_seconds_bucket{le="0.01"} 9500
nexusgate_request_duration_seconds_bucket{le="0.025"} 11000
nexusgate_request_duration_seconds_bucket{le="+Inf"} 12345
nexusgate_request_duration_seconds_sum 45.2
nexusgate_request_duration_seconds_count 12345
```

**零依赖实现**：不使用 Prometheus client 库，直接拼接 Prometheus exposition format 字符串。代价是无法支持复杂的标签和 Histogram 分位数计算，但对于网关场景足够。

## 中间件执行时序

```
请求进入
  │
  ▼
Trace ──── 记录开始时间，生成 TraceID
  │
  ▼
AccessLog ── 递增 requestTotal + activeRequests
  │
  ▼
RateLimit ── 检查令牌桶，不足则返回 429
  │
  ▼
CORS ────── OPTIONS 短路返回 204，其他添加 CORS 头
  │
  ▼
CircuitBreaker ── 检查熔断状态，Open 则返回 503
  │
  ▼
Handler ─── 路由匹配 + 代理转发
  │
  ▼
CircuitBreaker ── 记录成功/失败
  │
  ▼
CORS ────── 为响应添加 CORS 头
  │
  ▼
AccessLog ── 递减 activeRequests，记录日志
  │
  ▼
Trace ──── 记录结束时间，输出日志
  │
  ▼
响应返回
```

## 小结

NexusGate 的六种中间件覆盖了流量治理的核心需求：

1. **Trace**：请求级追踪，TraceID 贯穿全链路
2. **AccessLog**：结构化访问日志 + 指标记录
3. **RateLimit**：令牌桶限流，多租户隔离
4. **CORS**：跨域处理，OPTIONS 预检短路
5. **CircuitBreaker**：三态熔断，半开态限流
6. **Metrics**：零依赖 Prometheus 指标

所有中间件都遵循 `func(Handler) Handler` 签名，通过 Chain 不可变组合，注册顺序即执行顺序。
