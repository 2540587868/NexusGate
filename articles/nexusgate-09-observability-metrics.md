---
title: "NexusGate 可观测性：零依赖 Prometheus 指标系统"
slug: "nexusgate-09-observability-metrics"
summary: "深入 NexusGate 的零依赖指标系统实现，详解 atomic + sync.Map 的无锁指标收集、自研直方图分桶与分位数计算、后端维度延迟追踪以及 Prometheus exposition format 输出。"
category: "NexusGate"
tags: ["Go", "Prometheus", "可观测性", "直方图", "指标系统"]
is_draft: false
---

# 09 | 可观测性：零依赖 Prometheus 指标系统
> 「用 Go 构建网关」专栏第 9 篇。本文详解零依赖 Prometheus 指标系统的设计与实现。

---

## 为什么零依赖？

NexusGate 的设计红线是**最小外部依赖**。Prometheus client_golang 虽然功能强大，但引入了 15+ 个传递依赖。对于网关这种基础设施组件，每一个依赖都是潜在的安全漏洞和维护负担。

自研指标系统的代价：
- 不支持自定义标签（Label）
- 不支持 Histogram 分位数服务端计算
- 不支持 Summary 的 φ-quantile

但对于网关场景，这些限制可以接受——网关的指标维度固定（状态码、后端地址），分位数可以通过 Prometheus 的 `histogram_quantile` 函数在查询时计算。

## 指标类型实现

### Counter — 原子计数器

```go
var (
    requestTotal    atomic.Int64
    requestSuccess  atomic.Int64
    requestFailed   atomic.Int64
    activeRequests  atomic.Int64
    circuitBreakerOpen atomic.Int64
    rateLimitRejected atomic.Int64
)
```

`atomic.Int64` 提供无锁的原子递增/递减操作，性能远优于 `Mutex + int64`。

**写入点**：

| 指标 | 递增时机 | 递减时机 |
|------|----------|----------|
| requestTotal | 请求到达 | - |
| requestSuccess | 请求成功（<500） | - |
| requestFailed | 请求失败（≥500 或错误） | - |
| activeRequests | 请求开始 | 请求结束 |
| circuitBreakerOpen | 熔断器打开 | - |
| rateLimitRejected | 限流拒绝 | - |

### Histogram — 自研直方图

```go
var histogramBuckets = []time.Duration{
    0,
    5 * time.Millisecond,
    10 * time.Millisecond,
    25 * time.Millisecond,
    50 * time.Millisecond,
    100 * time.Millisecond,
    250 * time.Millisecond,
    500 * time.Millisecond,
    time.Second,
    math.MaxInt64,
}

var requestDurationCounts [len(histogramBuckets)]atomic.Int64
var requestDurationSum atomic.Int64
```

**分桶逻辑**：

```go
func recordDuration(duration time.Duration) {
    requestDurationSum.Add(int64(duration))

    seconds := duration.Seconds()
    for i, bucket := range histogramBuckets {
        if duration <= bucket {
            requestDurationCounts[i].Add(1)
            break
        }
    }
}
```

每个请求的耗时被归入第一个 `>= duration` 的桶。例如 12ms 的请求归入 25ms 桶。

**Prometheus 直方图语义**：桶是累积的——25ms 桶包含所有 ≤25ms 的请求，不仅包含 10-25ms 的请求。输出时需要做累积转换。

### 后端维度指标 — sync.Map

```go
type backendMetrics struct {
    requests atomic.Int64
    failures atomic.Int64
    latency  latencyTracker
}

var backendMetricsMap sync.Map // map[string]*backendMetrics
```

`sync.Map` 适用于读多写少的场景——后端地址在配置变更时才变化，但每个请求都需要读取。

### latencyTracker — 延迟追踪

```go
type latencyTracker struct {
    sum   atomic.Int64
    count atomic.Int64
    min   atomic.Int64
    max   atomic.Int64
}

func (lt *latencyTracker) Record(d time.Duration) {
    ns := d.Nanoseconds()
    lt.sum.Add(ns)
    lt.count.Add(1)

    for {
        currentMin := lt.min.Load()
        if ns >= currentMin || lt.min.CompareAndSwap(currentMin, ns) {
            break
        }
    }

    for {
        currentMax := lt.max.Load()
        if ns <= currentMax || lt.max.CompareAndSwap(currentMax, ns) {
            break
        }
    }
}
```

**CAS 更新 min/max**：使用 `CompareAndSwap` 实现无锁的最小/最大值更新。循环直到成功——如果当前值已经比新值更小（min）或更大（max），则不需要更新。

## Prometheus 格式输出

### MetricsHandler

```go
func MetricsHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var sb strings.Builder

        writeCounter(&sb, "nexusgate_request_total", requestTotal.Load())
        writeCounter(&sb, "nexusgate_request_success_total", requestSuccess.Load())
        writeCounter(&sb, "nexusgate_request_failed_total", requestFailed.Load())
        writeGauge(&sb, "nexusgate_active_requests", activeRequests.Load())
        writeCounter(&sb, "nexusgate_circuit_breaker_open_total", circuitBreakerOpen.Load())
        writeCounter(&sb, "nexusgate_rate_limit_rejected_total", rateLimitRejected.Load())

        writeHistogram(&sb, "nexusgate_request_duration_seconds")

        writeBackendMetrics(&sb)

        w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
        sb.WriteStringTo(w)
    }
}
```

### 直方图输出

```
# HELP nexusgate_request_duration_seconds Request duration in seconds
# TYPE nexusgate_request_duration_seconds histogram
nexusgate_request_duration_seconds_bucket{le="0"} 0
nexusgate_request_duration_seconds_bucket{le="0.005"} 8000
nexusgate_request_duration_seconds_bucket{le="0.01"} 9500
nexusgate_request_duration_seconds_bucket{le="0.025"} 11000
nexusgate_request_duration_seconds_bucket{le="0.05"} 11500
nexusgate_request_duration_seconds_bucket{le="0.1"} 11800
nexusgate_request_duration_seconds_bucket{le="0.25"} 12000
nexusgate_request_duration_seconds_bucket{le="0.5"} 12200
nexusgate_request_duration_seconds_bucket{le="1"} 12300
nexusgate_request_duration_seconds_bucket{le="+Inf"} 12345
nexusgate_request_duration_seconds_sum 45.2
nexusgate_request_duration_seconds_count 12345
```

**累积转换**：输出时将非累积的桶计数转换为累积计数——每个桶的值等于自身加上所有更小桶的值。

### 后端维度输出

```
# HELP nexusgate_backend_requests_total Total requests per backend
# TYPE nexusgate_backend_requests_total counter
nexusgate_backend_requests_total{backend="10.0.0.1:8080"} 5000
nexusgate_backend_requests_total{backend="10.0.0.2:8080"} 4800

# HELP nexusgate_backend_failures_total Total failures per backend
# TYPE nexusgate_backend_failures_total counter
nexusgate_backend_failures_total{backend="10.0.0.1:8080"} 50
nexusgate_backend_failures_total{backend="10.0.0.2:8080"} 120

# HELP nexusgate_backend_latency_seconds Backend latency stats
# TYPE nexusgate_backend_latency_seconds summary
nexusgate_backend_latency_seconds{backend="10.0.0.1:8080",quantile="avg"} 0.012
nexusgate_backend_latency_seconds{backend="10.0.0.1:8080",quantile="min"} 0.001
nexusgate_backend_latency_seconds{backend="10.0.0.1:8080",quantile="max"} 0.5
nexusgate_backend_latency_seconds_sum{backend="10.0.0.1:8080"} 60.0
nexusgate_backend_latency_seconds_count{backend="10.0.0.1:8080"} 5000
```

## 常用 PromQL 查询

### 请求成功率

```promql
sum(rate(nexusgate_request_success_total[5m]))
/
sum(rate(nexusgate_request_total[5m]))
```

### P99 延迟

```promql
histogram_quantile(0.99,
  rate(nexusgate_request_duration_seconds_bucket[5m])
)
```

### 后端错误率

```promql
sum(rate(nexusgate_backend_failures_total[5m])) by (backend)
/
sum(rate(nexusgate_backend_requests_total[5m])) by (backend)
```

### 熔断器触发次数

```promql
sum(increase(nexusgate_circuit_breaker_open_total[1h]))
```

### 限流拒绝率

```promql
sum(rate(nexusgate_rate_limit_rejected_total[5m]))
/
sum(rate(nexusgate_request_total[5m]))
```

## expvar 集成

除了 Prometheus 格式，NexusGate 还通过 `expvar` 发布指标：

```go
func init() {
    expvar.Publish("nexusgate", expvar.Func(func() interface{} {
        return map[string]interface{}{
            "request_total":     requestTotal.Load(),
            "request_success":   requestSuccess.Load(),
            "request_failed":    requestFailed.Load(),
            "active_requests":   activeRequests.Load(),
            "circuit_breaker_open": circuitBreakerOpen.Load(),
            "rate_limit_rejected":  rateLimitRejected.Load(),
        }
    }))
}
```

`expvar` 通过 `/debug/vars` 端点暴露 JSON 格式的运行时变量，方便开发调试。

## 性能影响

| 操作 | 耗时 | 频率 |
|------|------|------|
| `atomic.Int64.Add` | ~5ns | 每请求 4-6 次 |
| `sync.Map.Load` | ~10ns | 每请求 1 次 |
| `latencyTracker.Record` | ~50ns | 每请求 1 次 |
| **指标总开销** | **~100ns** | **每请求** |

指标收集对请求延迟的影响 <0.1μs，相对于网关总开销（15-30μs）可以忽略。

## 小结

NexusGate 的指标系统通过 `atomic` + `sync.Map` 实现了零依赖、无锁、低开销的指标收集：

1. **Counter**：`atomic.Int64` 原子递增
2. **Histogram**：10 桶直方图 + 累积计数
3. **Summary**：CAS 更新 min/max + sum/count 计算平均值
4. **输出**：标准 Prometheus exposition format

整个指标系统约 230 行代码，零外部依赖，每请求开销 <100ns。
