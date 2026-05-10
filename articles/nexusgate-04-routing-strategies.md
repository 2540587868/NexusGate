---
title: "NexusGate 路由策略：四种负载均衡算法详解"
slug: "nexusgate-04-routing-strategies"
summary: "深入 NexusGate 的四种路由策略实现——一致性哈希（150 虚拟节点）、平滑加权轮询（Nginx SWRR）、最少连接（原子计数器）、Header 路由（金丝雀发布），详解算法原理、数据结构和并发安全设计。"
category: "NexusGate"
tags: ["Go", "一致性哈希", "加权轮询", "负载均衡", "金丝雀发布"]
is_draft: false
---

# 04 | 路由策略：四种负载均衡算法详解
> 「用 Go 构建网关」专栏第 4 篇。本文详解 NexusGate 的四种路由策略的算法原理、数据结构和实现细节。

---

## 路由架构总览

### 核心数据结构

```go
type Backend struct {
    Address string            // 后端地址，如 "10.0.0.1:8080"
    Weight  int               // 权重，用于加权轮询
    Healthy bool              // 健康状态
    Meta    map[string]string // 元数据，用于 Header 路由
}

type Route struct {
    ID          string
    Match       MatchRule       // 匹配规则
    Backends    []*Backend      // 后端列表
    Strategy    StrategyType    // 路由策略
    Middlewares []string        // 适用的中间件
}

type MatchRule struct {
    PathPrefix string            // 前缀匹配
    PathExact  string            // 精确匹配
    Methods    []string          // HTTP 方法
    Headers    map[string]string // Header 匹配
}
```

### 路由匹配流程

```go
func (rt *Router) Route(req *gateway.Request) (*Route, *Backend, error) {
    rt.mu.RLock()
    defer rt.mu.RUnlock()

    route := rt.match(req)
    if route == nil {
        return nil, nil, ErrRouteNotFound
    }

    healthyBackends := filterHealthy(route.Backends)
    if len(healthyBackends) == 0 {
        return nil, nil, ErrBackendDown
    }

    selector := rt.selectors[route.Strategy]
    return route, selector.Select(selectKey, healthyBackends), nil
}
```

**三步路由**：
1. **匹配**：遍历路由表，找到第一个匹配的路由规则
2. **过滤**：剔除不健康的后端
3. **选择**：根据策略从健康后端中选择一个

### 匹配规则优先级

```go
func (rt *Router) match(req *gateway.Request) *Route {
    for _, route := range rt.routes {
        rule := route.Match

        // 精确匹配优先
        if rule.PathExact != "" {
            if req.Path != rule.PathExact { continue }
        }

        // 前缀匹配
        if rule.PathPrefix != "" {
            if !strings.HasPrefix(req.Path, rule.PathPrefix) { continue }
        }

        // HTTP 方法校验
        if len(rule.Methods) > 0 {
            if !contains(rule.Methods, req.Method) { continue }
        }

        // Header 匹配
        for key, value := range rule.Headers {
            if req.Headers.Get(key) != value { continue }
        }

        return route  // 第一个匹配即返回
    }
    return nil
}
```

**设计决策**：路由规则按配置顺序匹配，**先匹配先赢**（First Match Wins）。这与 Nginx 的最长前缀匹配不同，但更简单直观。

## 策略一：一致性哈希

### 算法原理

一致性哈希解决的核心问题：**当后端节点增减时，最小化请求的重新映射**。

传统取模哈希（`hash(key) % N`）在节点数 N 变化时，几乎所有请求都会重新分配。一致性哈希通过**哈希环**将这个比例降低到 `1/N`。

### 数据结构

```go
type ConsistentHash struct {
    mu           sync.RWMutex
    virtualNodes int              // 虚拟节点数，默认 150
    ring         []uint32         // 排序的哈希环
    nodeMap      map[uint32]*Backend
    lastBackends string           // 后端签名，变更检测
}
```

### 哈希环构建

```go
func (ch *ConsistentHash) rebuild(backends []*Backend) {
    ch.ring = ch.ring[:0]
    ch.nodeMap = make(map[uint32]*Backend)

    for _, b := range backends {
        for i := 0; i < ch.virtualNodes; i++ {
            key := fmt.Sprintf("%s#%d", b.Address, i)
            hash := crc32.ChecksumIEEE([]byte(key))
            ch.ring = append(ch.ring, hash)
            ch.nodeMap[hash] = b
        }
    }

    sort.Slice(ch.ring, func(i, j int) bool {
        return ch.ring[i] < ch.ring[j]
    })
}
```

**虚拟节点**：每个真实后端对应 150 个虚拟节点，均匀分布在哈希环上。虚拟节点越多，分布越均匀，但内存占用越大。150 是经验值，在 10 个后端时标准差约 3%。

**为什么用 CRC32 而非 MD5？**

- CRC32 计算速度约 500 MB/s，MD5 约 300 MB/s
- CRC32 输出 32 位，直接用于排序环，MD5 输出 128 位需要截断
- 一致性哈希不需要加密安全性，只需要分布均匀

### 请求路由

```go
func (ch *ConsistentHash) Select(key string, backends []*Backend) (*Backend, error) {
    ch.mu.RLock()
    if ch.needsRebuild(backends) {
        ch.mu.RUnlock()
        ch.mu.Lock()
        if ch.needsRebuild(backends) {
            ch.rebuild(backends)
        }
        ch.mu.Unlock()
        ch.mu.RLock()
    }
    defer ch.mu.RUnlock()

    hash := crc32.ChecksumIEEE([]byte(key))
    idx := sort.Search(len(ch.ring), func(i int) bool {
        return ch.ring[i] >= hash
    })
    if idx == len(ch.ring) {
        idx = 0  // 回绕
    }
    return ch.nodeMap[ch.ring[idx]], nil
}
```

**双重检查锁定**（Double-Checked Locking）：
1. 先读锁检查是否需要重建
2. 需要时释放读锁，获取写锁重建
3. 重建后释放写锁，重新获取读锁继续

**惰性重建**：不主动监听后端变化，而是在每次 Select 时通过 `backendsSignature` 检测。签名是所有后端地址的拼接字符串，变更时触发重建。

### 顺时针查找

```
哈希环（0 → 2^32-1）：

     B#0(100)  B#1(500)
        \       /
  C#2(800)---A#0(900)
    /              \
A#1(1200)    C#0(1500)
    \              /
  B#2(2000)---A#2(2500)

请求 key hash = 700 → 顺时针找到 C#2(800) → 后端 C
请求 key hash = 950 → 顺时针找到 A#1(1200) → 后端 A
请求 key hash = 3000 → 超出最大值，回绕到 B#0(100) → 后端 B
```

## 策略二：平滑加权轮询

### 算法原理

这是 Nginx 的 SWRR（Smooth Weighted Round-Robin）算法，解决的核心问题：**按权重比例分配请求，且分配序列均匀分散**。

朴素加权轮询（权重 5:3:2）的序列：`A A A A A B B B C C`——5 个 A 连续分配，不均匀。

SWRR 的序列：`A B A C A B A C A B`——均匀分散，无连续聚集。

### 数据结构

```go
type WeightedRR struct {
    mu             sync.Mutex
    currentWeights []int64
}
```

仅维护一个 `currentWeights` 切片，极其简洁。

### 算法实现

```go
func (w *WeightedRR) Select(key string, backends []*Backend) (*Backend, error) {
    w.mu.Lock()
    defer w.mu.Unlock()

    if len(w.currentWeights) != len(backends) {
        w.currentWeights = make([]int64, len(backends))
    }

    var totalWeight int64
    var bestWeight int64 = math.MinInt64
    var bestIdx int

    for i, b := range backends {
        totalWeight += int64(b.Weight)
        w.currentWeights[i] += int64(b.Weight)
        if w.currentWeights[i] > bestWeight {
            bestWeight = w.currentWeights[i]
            bestIdx = i
        }
    }

    w.currentWeights[bestIdx] -= totalWeight
    return backends[bestIdx], nil
}
```

**三步算法**：
1. **累加**：每个后端的 `currentWeight` 加上自身 `Weight`
2. **选取**：选择 `currentWeight` 最大的后端
3. **扣减**：被选中后端的 `currentWeight` 减去总权重

### 推演示例

权重 A=5, B=3, C=2，总权重=10：

| 轮次 | 累加后 | 选中 | 扣减后 |
|------|--------|------|--------|
| 1 | [5, 3, 2] | A | [-5, 3, 2] |
| 2 | [0, 6, 4] | B | [0, -4, 4] |
| 3 | [5, -1, 6] | C | [5, -1, -4] |
| 4 | [10, 2, -2] | A | [0, 2, -2] |
| 5 | [5, 5, 0] | A（先到先赢）| [-5, 5, 0] |
| 6 | [0, 8, 2] | B | [0, -2, 2] |
| 7 | [5, 1, 4] | A | [-5, 1, 4] |
| 8 | [0, 4, 6] | C | [0, 4, -4] |
| 9 | [5, 7, -2] | B | [5, -3, -2] |
| 10 | [10, 0, 0] | A | [0, 0, 0] |

10 轮分配：A=5, B=3, C=2，完全符合权重比例，且序列均匀分散。

## 策略三：最少连接

### 算法原理

最少连接策略解决的核心问题：**将请求发送到当前负载最低的后端**，适用于请求处理时间差异较大的场景。

### 数据结构

```go
type LeastConn struct {
    mu          sync.Mutex
    connections map[string]*atomic.Int64
}
```

每个后端地址对应一个原子计数器，记录当前活跃连接数。

### 算法实现

```go
func (lc *LeastConn) Select(key string, backends []*Backend) (*Backend, error) {
    lc.mu.Lock()
    defer lc.mu.Unlock()

    var best *Backend
    var bestCount int64 = math.MaxInt64

    for _, b := range backends {
        counter := lc.getCounter(b.Address)
        count := counter.Load()
        if count < bestCount {
            bestCount = count
            best = b
        }
    }

    lc.getCounter(best.Address).Add(1)
    return best, nil
}

func (lc *LeastConn) Release(address string) {
    lc.mu.Lock()
    defer lc.mu.Unlock()
    if counter, ok := lc.connections[address]; ok {
        counter.Add(-1)
    }
}
```

**Select + Release 配对**：
- `Select` 时递增计数器
- 请求完成后调用 `Release` 递减计数器
- 如果忘记 Release，计数器会持续增长，导致该后端永远不被选中

**为什么用 `atomic.Int64` 而非 `int64 + Mutex`？**

计数器的读写频率极高（每个请求至少一次读+一次写），用原子操作避免了锁竞争。`Mutex` 仅保护 `connections` map 的增删，不保护计数器的读写。

### 懒初始化

```go
func (lc *LeastConn) getCounter(address string) *atomic.Int64 {
    if counter, ok := lc.connections[address]; ok {
        return counter
    }
    counter := &atomic.Int64{}
    lc.connections[address] = counter
    return counter
}
```

首次访问时创建计数器，避免启动时为所有可能的后端预分配。

## 策略四：Header 路由

### 算法原理

Header 路由解决的核心问题：**根据请求头将流量路由到特定版本的后端**，这是金丝雀发布和 A/B 测试的基础。

### 数据结构

```go
type HeaderRoute struct {
    headerName string  // 默认 "X-Service-Version"
}
```

### 算法实现

```go
func (hr *HeaderRoute) Select(key string, backends []*Backend) (*Backend, error) {
    for _, b := range backends {
        if b.Meta["version"] == key {
            return b, nil
        }
    }
    return backends[0], nil  // fallback 到第一个后端
}
```

**使用场景**：

```yaml
routes:
  - match:
      path_prefix: "/api"
    strategy: header
    backends:
      - address: "v1-service:8080"
        weight: 1
        meta:
          version: "v1"
      - address: "v2-service:8080"
        weight: 1
        meta:
          version: "v2"
```

请求头 `X-Service-Version: v2` → 路由到 v2-service，其他 → fallback 到 v1-service。

### selectKey 的特殊处理

在 `Router.Route` 中，Header 路由的 `selectKey` 取自请求头而非 `RouteKey`：

```go
if route.Strategy == StrategyHeader {
    selectKey = req.Headers.Get(selector.headerName)
}
```

这使得同一个路由规则可以根据不同的 Header 值动态选择后端。

## 策略选择指南

| 场景 | 推荐策略 | 理由 |
|------|----------|------|
| 无状态 API 服务 | 加权轮询 | 简单高效，按权重分配 |
| 有状态会话服务 | 一致性哈希 | 同一用户始终路由到同一后端 |
| 长连接/WebSocket | 最少连接 | 按实际负载分配 |
| 金丝雀发布 | Header 路由 | 按 Header 精确控制流量 |
| 多版本共存 | Header 路由 | 按版本号路由到对应后端 |

## 健康后端过滤

```go
func filterHealthy(backends []*Backend) []*Backend {
    healthy := make([]*Backend, 0, len(backends))
    for _, b := range backends {
        if b.Healthy {
            healthy = append(healthy, b)
        }
    }
    return healthy
}
```

**关键设计**：健康过滤在策略选择之前执行。这意味着：
- 一致性哈希：不健康后端的虚拟节点不参与选择
- 加权轮询：不健康后端不参与权重计算
- 最少连接：不健康后端不参与计数比较

当所有后端都不健康时，返回 `ErrBackendDown`，由上层返回 502 给客户端。

## SwapRoutes 原子切换

```go
func (rt *Router) SwapRoutes(newRouter *Router) {
    rt.mu.Lock()
    defer rt.mu.Unlock()
    rt.routes = newRouter.routes
    rt.selectors = newRouter.selectors
}
```

配置热更新时，先构建完整的 Router，然后一次性替换路由表和选择器映射。这保证了：
- 不会出现"一半旧路由一半新路由"的不一致状态
- 正在处理的请求使用旧路由表（已持有读锁或已复制引用）
- 新请求使用新路由表

## 小结

NexusGate 的四种路由策略各有侧重：

1. **一致性哈希**：虚拟节点 + CRC32 + 惰性重建，适合有状态服务
2. **加权轮询**：Nginx SWRR 算法，3 行核心逻辑，适合无状态服务
3. **最少连接**：原子计数器 + 懒初始化，适合长连接服务
4. **Header 路由**：Meta 字段匹配 + fallback，适合金丝雀发布

所有策略都遵循 `Selector` 接口，新增策略只需实现 `Select` 方法，零侵入扩展。
