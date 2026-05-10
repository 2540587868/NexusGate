---
title: "NexusGate 网关引擎：分片调度与请求处理全流程"
slug: "nexusgate-03-gateway-engine"
summary: "深入 NexusGate 的分片引擎实现，详解请求从 TCP 连接到响应写回的完整生命周期、Shard 分片的并发模型、DispatchSync/Dispatch 双模式、slowRecover 背压机制以及错误处理链路。"
category: "NexusGate"
tags: ["Go", "分片引擎", "并发模型", "背压", "请求处理"]
is_draft: false
---

# 03 | 网关引擎：分片调度与请求处理全流程
> 「用 Go 构建网关」专栏第 3 篇。本文详解请求从 TCP 连接到响应写回的完整路径，以及分片引擎的内部实现。

---

## 请求处理全生命周期

一个 HTTP 请求从到达网关到获得响应，经历以下 8 个阶段：

```
 ① TCP Accept        ② Parse Request      ③ Dispatch
 ┌──────────┐      ┌──────────────┐     ┌───────────┐
 │ Listener │─────→│ httparser    │────→│ Gateway   │
 │ Accept() │      │ ParseRequest │     │ Dispatch  │
 └──────────┘      └──────────────┘     └─────┬─────┘
                                               │
 ④ Shard Queue     ⑤ Middleware Chain   ⑥ Route & Forward
 ┌──────────┐     ┌──────────────┐     ┌───────────────┐
 │ Shard    │────→│ Chain.Then   │────→│ Router.Route  │
 │ queue    │     │ handler      │     │ Proxy.Forward │
 └──────────┘     └──────────────┘     └───────┬───────┘
                                               │
 ⑦ Build Response   ⑧ Write Response
 ┌──────────────┐  ┌──────────────┐
 │ gateway      │  │ httparser    │
 │ Response     │←─│ WriteResponse│
 └──────────────┘  └──────────────┘
```

### 阶段 ①：TCP Accept

```go
func acceptConnections(listener net.Listener, parser *httparser.Parser, gw *gateway.Gateway) {
    for {
        conn, err := listener.Accept()
        if err != nil {
            if errors.Is(err, net.ErrClosed) {
                return
            }
            slog.Error("accept error", "error", err)
            continue
        }
        go handleConnection(conn, parser, gw)
    }
}
```

- 每个 TCP 连接分配一个独立 goroutine
- `net.ErrClosed` 表示监听器已关闭（优雅关闭场景），直接返回
- 其他错误（如 fd 耗尽）记录日志后继续

### 阶段 ②：Parse Request

```go
func handleConnection(conn net.Conn, parser *httparser.Parser, gw *gateway.Gateway) {
    defer conn.Close()

    req, err := parser.ParseRequest(conn)
    if err != nil {
        if gwErr, ok := err.(*gateway.GatewayError); ok {
            httparser.WriteErrorResponse(conn, gwErr)
        }
        return
    }
    // ...
}
```

- 解析失败时尝试写入错误响应（仅对 GatewayError 类型）
- `defer conn.Close()` 确保连接最终关闭
- **短连接模型**：每个请求处理完毕后关闭 TCP 连接，不复用

### 阶段 ③-⑥：Dispatch → Forward

```go
resp, err := gw.DispatchSync(req)
```

这一步是网关引擎的核心，后续章节详细展开。

### 阶段 ⑦-⑧：Write Response

```go
if err := httparser.WriteResponse(conn, resp); err != nil {
    slog.Debug("write response error", "error", err)
}
```

- 响应写入失败仅记录 Debug 级别日志（客户端可能已断开）
- 写入完成后 `defer conn.Close()` 关闭连接

## 分片引擎内部实现

### Shard 初始化

```go
func NewGateway(handler Handler, queueSize int) *Gateway {
    gw := &Gateway{
        handler:     handler,
        queueSize:   queueSize,
        syncTimeout: 30 * time.Second,
    }
    for i := 0; i < ShardCount; i++ {
        shard := &Shard{
            id:     i,
            queue:  make(chan *Request, queueSize),
            worker: handler,
        }
        gw.shards[i] = shard
        go shard.run()
    }
    return gw
}
```

初始化时：
1. 创建 `ShardCount`（8）个 Shard
2. 每个 Shard 分配 `queueSize` 大小的缓冲 channel
3. 每个 Shard 启动一个常驻 goroutine `run()`
4. 所有 Shard 共享同一个 `handler`（中间件链包装后的最终 Handler）

### Shard Worker 循环

```go
func (s *Shard) run() {
    for req := range s.queue {
        s.pending.Add(-1)
        resp, err := s.worker(req)
        if req.RespCh != nil {
            req.RespCh <- &ResponseResult{Resp: resp, Err: err}
        } else if err != nil {
            slog.Error("request handler error", "shard", s.id, "error", err)
        }
    }
}
```

**执行流程**：
1. 从 `queue` channel 阻塞读取请求
2. 取出后立即递减 `pending` 计数器
3. 执行 `worker`（中间件链 → 路由 → 代理）
4. 同步模式：将结果写入 `RespCh`
5. 异步模式：仅记录错误日志

**为什么取出后立即递减 pending？**

`pending` 的语义是"队列中等待处理的请求数"。取出后请求已经不在队列中，即使还没处理完，也不应计入队列深度。`pending` 用于背压检测，需要反映真实的队列积压情况。

### Dispatch 入队逻辑

```go
func (gw *Gateway) Dispatch(req *Request) error {
    shardIdx := req.ShardKey()
    shard := gw.shards[shardIdx]

    utilization := float64(shard.pending.Load()) / float64(gw.queueSize)
    if utilization > 0.9 {
        go shard.slowRecover()
    }

    shard.pending.Add(1)
    select {
    case shard.queue <- req:
        return nil
    default:
        shard.pending.Add(-1)
        return NewGatewayError(ErrRateLimited, "shard queue full", ...)
    }
}
```

**三步入队**：
1. **背压检测**：计算当前队列利用率，>90% 触发 slowRecover
2. **预增计数**：`pending.Add(1)` 在入队前执行，确保计数准确
3. **非阻塞入队**：`select + default` 模式，队列满时立即返回错误

**为什么先 Add(1) 再入队？**

如果先入队再 Add(1)，存在时间窗口：请求已在队列中但 pending 未递增，导致利用率被低估，背压检测失效。先 Add(1) 保证了 pending 的保守估计——宁可多算一个，不可少算一个。

**入队失败时为什么要 Add(-1)？**

`select + default` 入队失败时，请求没有进入队列，但 pending 已经递增了。必须回滚，否则 pending 会持续虚高，导致永久的背压误判。

## slowRecover 背压机制

### 触发条件

```go
utilization := float64(shard.pending.Load()) / float64(gw.queueSize)
if utilization > 0.9 {
    go shard.slowRecover()
}
```

当队列利用率超过 90% 时，以独立 goroutine 启动 slowRecover。注意：这里没有防重入检查，可能同时启动多个 slowRecover goroutine。

### 执行逻辑

```go
func (s *Shard) slowRecover() {
    s.mu.Lock()
    defer s.mu.Unlock()
    for i := 0; i < 16; i++ {
        select {
        case req := <-s.queue:
            s.pending.Add(-1)
            resp, err := s.worker(req)
            if req.RespCh != nil {
                req.RespCh <- &ResponseResult{Resp: resp, Err: err}
            } else if err != nil {
                slog.Error("slow recovery handler error", ...)
            }
        default:
            return
        }
    }
}
```

**关键设计**：
1. **互斥锁保护**：同一时刻只有一个 slowRecover 在执行，避免多个辅助 goroutine 竞争
2. **批量处理**：最多处理 16 个积压请求，防止长时间占用
3. **非阻塞读取**：`select + default`，队列空时立即返回
4. **直接处理**：不经过 Shard worker，直接调用 `worker` 处理请求

### 背压缓解效果

假设 `queueSize = 1000`，当前 pending = 950：

```
时间线：
  T0: pending=950, slowRecover 启动
  T1: slowRecover 处理 16 个请求, pending=934
  T2: slowRecover 释放锁
  T3: 如果 pending 仍 >900, 新的 slowRecover 可能启动
  ...
  Tn: pending 降至 900 以下, 不再触发
```

slowRecover 本质上是一种**自适应并发度调整**：正常情况下 1 个 worker 处理，过载时临时增加辅助 worker，负载降低后自动恢复。

## DispatchSync 同步模式

```go
func (gw *Gateway) DispatchSync(req *Request) (*Response, error) {
    req.RespCh = make(chan *ResponseResult, 1)
    if err := gw.Dispatch(req); err != nil {
        return nil, err
    }
    select {
    case result := <-req.RespCh:
        return result.Resp, result.Err
    case <-time.After(gw.syncTimeout):
        return nil, NewGatewayError(ErrBackendTimeout, "request timed out", ...)
    }
}
```

**超时保护**：
- 默认 30 秒超时（`syncTimeout`）
- 超时后返回 `ErrBackendTimeout`，但 Shard worker 可能仍在处理
- `RespCh` 容量为 1，worker 写入后不会阻塞（即使调用方已超时离开）

**潜在问题**：超时后 Shard worker 仍在执行，其结果写入 `RespCh` 后无人消费。由于 `RespCh` 容量为 1 且是局部变量，GC 会回收。但如果 worker 执行了有副作用的操作（如代理转发），则请求已经到达后端，但调用方认为已超时——这是分布式系统中常见的**超时歧义**问题。

## 错误处理链路

### 错误传播路径

```
Proxy.Forward() → GatewayError
  ↓
Shard.run() → RespCh / slog.Error
  ↓
DispatchSync() → (*Response, error) 返回给调用方
  ↓
handleConnection() → WriteErrorResponse() 写回客户端
```

### 错误分类

| 错误码 | 含义 | HTTP 状态 | 可重试 | 触发场景 |
|--------|------|-----------|--------|----------|
| 10001 | BadRequest | 400 | 否 | 请求格式错误 |
| 10002 | RouteNotFound | 404 | 否 | 无匹配路由 |
| 10004 | RateLimited | 429 | 否 | 队列满或令牌桶耗尽 |
| 10005 | CircuitOpen | 503 | 否 | 熔断器打开 |
| 10006 | Internal | 500 | 否 | 内部错误 |
| 10007 | BackendDown | 502 | 是 | 后端连接失败 |
| 10008 | BackendTimeout | 504 | 是 | 后端响应超时 |

### 错误响应格式

```json
{
  "code": 10004,
  "message": "rate limit exceeded",
  "detail": "too many requests for tenant: acme-corp"
}
```

统一的 JSON 错误格式，客户端可以解析 `code` 做程序化处理。

## 并发安全分析

### 数据竞争点

| 共享状态 | 保护机制 | 竞争场景 |
|----------|----------|----------|
| `Shard.queue` | channel 内部锁 | Dispatch 写入 vs run 读取 |
| `Shard.pending` | `atomic.Int64` | Dispatch 递增 vs run/slowRecover 递减 |
| `Shard.mu` | `sync.Mutex` | 多个 slowRecover 竞争 |
| `Request.RespCh` | channel | DispatchSync 等待 vs run 写入 |
| `Request.shardKey` | 惰性计算 + 缓存 | ShardKey() 可能被多 goroutine 调用 |

**`shardKey` 的竞争问题**：

```go
func (r *Request) ShardKey() uint32 {
    if r.shardKey == 0 {          // 读取
        h := fnv.New32a()
        h.Write([]byte(r.TenantID))
        r.shardKey = h.Sum32() % ShardCount  // 写入
    }
    return r.shardKey
}
```

这里存在数据竞争：如果两个 goroutine 同时调用 `ShardKey()` 且 `shardKey == 0`，可能同时计算并写入。但由于：
1. `Request` 在 `Dispatch` 之前只被一个 goroutine 持有
2. 计算结果是确定性的（相同 TenantID 总是产生相同 shardKey）
3. 写入的是 uint32，在大多数架构上是原子操作

因此这个竞争是**良性的**——即使发生，结果也是正确的。

## 性能特征

### 单请求延迟分解

| 阶段 | 典型耗时 | 说明 |
|------|----------|------|
| ParseRequest | ~10μs | 解析 HTTP 请求行 + 头部 + 体 |
| ShardKey 计算 | ~50ns | FNV-1a 哈希 + 取模 |
| Channel 入队 | ~100ns | buffered channel 写入 |
| 中间件链 | ~1-5μs | 取决于中间件数量和逻辑 |
| 路由匹配 | ~500ns | 遍历路由规则 + 策略选择 |
| 代理转发 | ~1-100ms | 取决于后端响应时间 |
| WriteResponse | ~10μs | 写入 HTTP 响应 |

**网关自身开销**：约 15-30μs（不含后端转发），主要来自 HTTP 解析和中间件执行。

### 吞吐量瓶颈

| 瓶颈点 | 原因 | 优化方向 |
|--------|------|----------|
| HTTP 解析 | 每个 TCP 连接只处理一个请求 | HTTP/1.1 Keep-Alive 或 HTTP/2 |
| Channel 开销 | 每个 Request 的 channel 通信 | sync.Pool 复用 Request 对象 |
| 响应体拷贝 | `io.ReadAll` 完整读取后端响应 | 流式转发（零拷贝） |
| 互斥锁 | slowRecover 和路由更新的锁 | 无锁数据结构 |

## 小结

NexusGate 的分片引擎通过三个核心机制实现了高性能请求处理：

1. **FNV 分片**：基于 TenantID 的确定性分片，消除全局锁竞争
2. **Channel 队列**：Go 原生并发原语，天然背压和 FIFO 语义
3. **slowRecover**：自适应并发度调整，过载时临时增加处理能力

这些机制共同构成了 NexusGate 的请求处理骨架——简单、高效、可预测。
