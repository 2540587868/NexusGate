---
title: "NexusGate 架构设计与核心抽象"
slug: "nexusgate-02-architecture-design"
summary: "深入 NexusGate 的分层架构设计，详解核心类型抽象（Request/Response/GatewayError）、Handler/Middleware 函数式模型、分片引擎的并发模型，以及模块间的依赖关系与数据流。"
category: "NexusGate"
tags: ["Go", "架构设计", "分片引擎", "中间件模式", "函数式抽象"]
is_draft: false
---

# 02 | NexusGate 架构设计与核心抽象
> 「用 Go 构建网关」专栏第 2 篇。本文深入 NexusGate 的分层架构、核心类型体系和函数式抽象模型。

---

## 分层架构

NexusGate 采用经典的**洋葱模型**分层架构，每一层只依赖下一层，不跨层调用：

```
┌──────────────────────────────────────────────┐
│                   main.go                     │
│            启动编排 & 信号处理                  │
├──────────────────────────────────────────────┤
│              httparser.Parser                 │
│          TCP 流 → Request / Response          │
├──────────────────────────────────────────────┤
│              gateway.Gateway                  │
│         分片引擎 & 请求调度 & 背压              │
├──────────────────────────────────────────────┤
│           middleware.Chain                    │
│    Trace → AccessLog → RateLimit → CORS → CB │
├──────────────┬───────────────────────────────┤
│   router.    │          proxy.               │
│   Router     │          Proxy                │
│  路由匹配    │      代理转发+重试              │
├──────────────┴───────────────────────────────┤
│              config.Store                     │
│        双缓冲配置 + FileWatcher               │
├──────────────────────────────────────────────┤
│            lifecycle.Graceful                 │
│        HealthChecker  Recoverable             │
│           优雅关闭 & 健康管理                  │
└──────────────────────────────────────────────┘
```

**数据流方向**：

```
TCP连接 → Parser.ParseRequest() → Gateway.DispatchSync()
  → Shard.queue → Chain.Then(handler) → Router.Route()
  → Proxy.Forward() → httpClient.Do() → Parser.WriteResponse()
```

**依赖关系**：

- `main.go` 是唯一的编排层，组装所有模块
- `gateway` 依赖 `middleware` 和 `router`/`proxy`（通过 Handler 回调）
- `proxy` 依赖 `router`（获取 Backend 信息）
- `config` 和 `lifecycle` 是横切关注点，被 `main.go` 直接使用
- `httparser` 是纯 I/O 层，不依赖任何业务模块

## 核心类型体系

### Request — 请求抽象

```go
type Request struct {
    ID          string
    Method      string
    Path        string
    QueryString string
    Host        string
    Scheme      string
    Headers     http.Header
    Body        []byte
    RemoteAddr  string
    TenantID    string
    RespCh      chan *ResponseResult
    shardKey    uint32
}
```

关键设计决策：

| 字段 | 设计理由 |
|------|----------|
| `TenantID` | 多租户隔离的基石，决定分片路由和限流维度 |
| `RespCh` | 同步/异步双模式的桥梁——有值则同步等待，无值则 fire-and-forget |
| `shardKey` | 缓存 FNV 哈希结果，避免重复计算 |
| `Scheme` | 从 `X-Forwarded-Proto` 提取，支持反向代理后的原始协议 |

**ShardKey 计算**：

```go
func (r *Request) ShardKey() uint32 {
    if r.shardKey == 0 {
        h := fnv.New32a()
        h.Write([]byte(r.TenantID))
        r.shardKey = h.Sum32() % ShardCount
    }
    return r.shardKey
}
```

选择 FNV-1a 而非 MD5/SHA 的理由：
- **性能**：FNV-1a 是乘法哈希，比加密哈希快 10 倍以上
- **分布性**：对短字符串（TenantID 通常是 8-32 字节）分布足够均匀
- **零依赖**：标准库内置，无需引入第三方包

### Response — 响应抽象

```go
type Response struct {
    StatusCode int
    Headers    http.Header
    Body       []byte
}
```

极简设计——只保留 HTTP 语义必需的字段。不包含耗时、TraceID 等元数据，这些由中间件层通过日志和指标处理。

### GatewayError — 错误体系

```go
type GatewayError struct {
    Code    int
    Message string
    Detail  string
}

const (
    ErrBadRequest      = 10001
    ErrRouteNotFound   = 10002
    ErrBackendDown     = 10007
    ErrBackendTimeout  = 10008
    ErrRateLimited     = 10004
    ErrCircuitOpen     = 10005
    ErrInternal        = 10006
)
```

**错误码 → HTTP 状态码映射**：

```go
func (e *GatewayError) HTTPStatus() int {
    switch e.Code {
    case ErrBadRequest:    return 400
    case ErrRouteNotFound: return 404
    case ErrRateLimited:   return 429
    case ErrCircuitOpen:   return 503
    case ErrBackendDown:   return 502
    case ErrBackendTimeout:return 504
    default:               return 500
    }
}
```

设计考量：
- **语义化错误码**：5 位数字，1xxxx 表示网关层错误，2xxxx 预留给业务层
- **双格式输出**：内部传递 `GatewayError`，外部输出 JSON 格式错误响应
- **可重试标记**：`isRetryableError` 判断 `ErrBackendDown` 和 `ErrBackendTimeout` 可重试

## 函数式抽象模型

### Handler — 请求处理器

```go
type Handler func(req *Request) (*Response, error)
```

一个函数签名，零接口约束。这是 NexusGate 最核心的抽象——所有组件都围绕这个签名组合：

- **Gateway Shard**：`shard.worker` 就是 Handler
- **Middleware**：`func(Handler) Handler` 包装 Handler
- **Proxy.Forward**：最终被包装进 Handler
- **Chain.Then**：将 Middleware 链和最终 Handler 组合为单个 Handler

### Middleware — 中间件函数

```go
type Middleware func(Handler) Handler
```

这是经典的**装饰器模式**的函数式实现。一个中间件接收当前 Handler，返回增强后的 Handler：

```go
func RateLimit(rl *RateLimiter) Middleware {
    return func(next Handler) Handler {
        return func(req *Request) (*Response, error) {
            if !rl.Allow(req.TenantID) {
                return nil, NewGatewayError(ErrRateLimited, ...)
            }
            return next(req)
        }
    }
}
```

**为什么不用接口？**

```go
// 接口方式
type Middleware interface {
    Handle(req *Request, next Handler) (*Response, error)
}

// 函数方式（NexusGate 选择）
type Middleware func(Handler) Handler
```

| 维度 | 接口 | 函数 |
|------|------|------|
| 组合性 | 需要结构体包装 | 闭包天然支持 |
| 状态捕获 | 显式字段 | 隐式闭包捕获 |
| 类型数量 | 每个中间件一个类型 | 统一签名 |
| 可读性 | 分散在多个文件 | 内联定义 |

函数式模型让中间件的定义更紧凑，组合更自然。

## 分片引擎的并发模型

### 为什么分片？

网关的核心矛盾：**高并发请求 vs 共享状态**。

| 方案 | 优点 | 缺点 |
|------|------|------|
| 全局队列 | 简单 | 单锁瓶颈 |
| 无队列 | 零延迟 | 无背压，后端易过载 |
| 分片队列 | 锁粒度小，有背压 | 请求分布可能不均 |

NexusGate 选择**固定 8 分片**：

```go
const ShardCount = 8

type Gateway struct {
    shards    [ShardCount]*Shard
    handler   Handler
    queueSize int
}
```

为什么是 8？
- 2 的幂，取模运算可优化为位运算（`hash & 7`）
- 足够分散锁竞争（8 把独立锁）
- 不会过多浪费内存（8 个 channel + 8 个 goroutine）
- 与常见 CPU 核心数匹配（8 核机器每个分片一个核）

### Shard 内部结构

```go
type Shard struct {
    id      int
    queue   chan *Request     // 带缓冲队列
    worker  Handler           // 处理函数
    mu      sync.Mutex        // slowRecover 互斥
    pending atomic.Int64      // 实时队列深度
}
```

**三重保护机制**：

1. **Channel 缓冲**：`queueSize` 大小的缓冲区，吸收突发流量
2. **pending 计数**：原子计数器实时追踪队列深度，用于背压检测
3. **slowRecover**：队列利用率 >90% 时启动辅助处理

### 同步/异步双模式

```go
// 异步模式：fire-and-forget
func (gw *Gateway) Dispatch(req *Request) error

// 同步模式：等待响应
func (gw *Gateway) DispatchSync(req *Request) (*Response, error) {
    req.RespCh = make(chan *ResponseResult, 1)
    if err := gw.Dispatch(req); err != nil {
        return nil, err
    }
    select {
    case result := <-req.RespCh:
        return result.Resp, result.Err
    case <-time.After(gw.syncTimeout):
        return nil, NewGatewayError(ErrBackendTimeout, ...)
    }
}
```

**统一入口，按需模式**：
- `RespCh == nil`：异步模式，Shard worker 处理完直接丢弃结果
- `RespCh != nil`：同步模式，Shard worker 将结果写入 channel

这种设计避免了维护两套请求路径，一个入口覆盖两种语义。

## 模块间通信模式

### 1. 函数回调（Handler 链）

```
main.go 构建: chain.Then(buildHandler(rt, px))
  → Trace(AccessLog(RateLimit(CORS(CircuitBreaker(routeAndForward)))))
```

中间件链通过函数嵌套传递控制权，无需显式的 next() 调用。

### 2. Channel 通信（Shard 队列）

```
Dispatch() → shard.queue <- req → shard.run() → handler(req)
```

请求通过 channel 从调度 goroutine 传递到 Shard worker goroutine，天然串行化。

### 3. 原子状态（健康检查联动）

```
HealthChecker.onChange → Proxy.Pool().MarkUnhealthy/MarkHealthy
                       → Router.UpdateBackendHealth
```

健康检查结果通过回调函数同步更新 Proxy 和 Router 的后端状态，使用原子操作保证并发安全。

### 4. 原子替换（配置热更新）

```
FileWatcher.onChange → Router.SwapRoutes(newRt)
```

路由表通过 `sync.RWMutex` 保护的指针替换实现原子切换，读者无感知。

## 设计权衡

### 权衡 1：缓冲 channel vs 无锁队列

NexusGate 使用 Go 的 buffered channel 作为 Shard 队列，而非自研无锁队列。

| 维度 | buffered channel | 无锁队列 |
|------|-----------------|----------|
| 实现复杂度 | 零（标准库） | 高（CAS + 内存序） |
| 性能 | 足够（百万级/秒） | 极致（千万级/秒） |
| 背压支持 | 天然（满则阻塞/拒绝） | 需额外实现 |
| 公平性 | FIFO 保证 | 取决于实现 |

选择理由：Go channel 的性能对于网关场景已经足够，且天然提供背压和 FIFO 语义，不值得为 10% 的性能提升引入自研无锁队列的复杂度。

### 权衡 2：固定分片 vs 动态分片

NexusGate 使用编译期固定的 8 个分片，而非运行时动态调整。

选择理由：
- 分片数量影响哈希分布，动态调整会导致请求重新分布
- 8 分片已经足够分散锁竞争
- 固定数量简化了 `ShardKey` 计算和内存分配

### 权衡 3：全量路由替换 vs 增量更新

`SwapRoutes` 一次性替换整个路由表，而非增删改单条路由。

选择理由：
- 网关路由变更频率低（分钟级），全量替换的代价可接受
- 增量更新需要维护版本号和冲突解决逻辑
- 全量替换保证了路由表的一致性快照

## 小结

NexusGate 的架构可以用三个关键词概括：

1. **函数式**：Handler/Middleware 都是函数签名，组合优于继承
2. **分片化**：基于 TenantID 的 8 分片引擎，锁粒度最小化
3. **原子化**：配置双缓冲、路由原子替换、健康状态原子标记

这些设计决策共同实现了 NexusGate 的核心目标：**在 3000 行代码内，构建一个可理解、高性能、可扩展的网关内核**。
