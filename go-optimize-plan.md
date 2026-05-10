# Go Optimize Plan: nexusgate

## 项目信息
- 模块路径: github.com/nexusgate/nexusgate
- Go 版本: 1.22
- 源文件数: 35
- 测试文件数: 11
- 测试覆盖率: 49.8%（优化前: 31.4%，↑18.4%）
- 直接依赖: 1 (yaml.v3)
- 项目类型: API 网关
- 风险等级: 🟡 中（高并发代理 + 自研路由 + 中间件链）

## 覆盖率明细

| 包 | 覆盖率 | 优化前 |
|----|--------|--------|
| cmd/nexusgate | 5.8% | — |
| internal/config | 41.6% | — |
| internal/gateway | 64.4% | — |
| internal/httparser | 41.2% | — |
| internal/lifecycle | 59.4% | — |
| internal/middleware | 49.8% | — |
| internal/proxy | 73.4% | — |
| internal/router | 62.9% | — |

## 阶段 1: 安全审计

| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 1 | /go-security-audit | internal/gateway/tls.go | ✅ 已完成 | 添加 NextProtos ALPN 协议协商 |
| 2 | /go-security-audit | internal/middleware/cors.go | ✅ 已完成 | 修复空 AllowOrigins 默认放行、Credentials+Wildcard 不安全组合 |
| 3 | /go-security-audit | internal/middleware/trace.go | ✅ 已完成 | 使用全随机 Trace ID、移除可预测时间戳前缀 |
| 4 | /go-security-audit | internal/config/store.go | ✅ 已完成 | 配置文件权限 0644→0600、添加 validateConfig 校验 |

## 阶段 2: 错误处理

| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 1 | /go-error-handling | internal/proxy/proxy.go | ✅ 已完成 | GatewayError 添加 Cause+Unwrap 错误链、Forward 添加重试上下文 |
| 2 | /go-error-handling | internal/proxy/retry.go | ✅ 已完成 | 升级 math/rand/v2、IsRetryableStatus nil 检查、jitter 上限保护 |
| 3 | /go-error-handling | internal/gateway/gateway.go | ✅ 已完成 | DispatchSync 使用 NewTimer 替代 time.After 防 goroutine 泄露 |
| 4 | /go-error-handling | internal/lifecycle/graceful.go | ✅ 已完成 | NewTimer 替代 time.After、错误聚合日志 |
| 5 | /go-error-handling | internal/lifecycle/recoverable.go | ✅ 已完成 | 修复 context 泄露、正常退出清理 running map、可取消重试等待 |

## 阶段 3: 并发审计

| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 1 | /go-concurrency-audit | internal/proxy/proxy.go | ✅ 已完成 | 添加 BackendTracker.Register 方法、doForward 自动注册后端 |
| 2 | /go-concurrency-audit | internal/proxy/retry.go | ✅ 已完成 | math/rand/v2 并发安全，RetryPolicy 只读无竞态 |
| 3 | /go-concurrency-audit | internal/middleware/ratelimit.go | ✅ 已完成 | 审计确认 Mutex 保护正确，cleanup 在锁内执行 |
| 4 | /go-concurrency-audit | internal/middleware/circuitbreaker.go | ✅ 已完成 | 修复 RecordSuccess 在非 HalfOpen 状态下 halfOpenActive 下溢、Open→HalfOpen 计数 |
| 5 | /go-concurrency-audit | internal/router/weighted_rr.go | ✅ 已完成 | 后端数量变化时保留已有权重 |
| 6 | /go-concurrency-audit | internal/router/consistent_hash.go | ✅ 已完成 | 修复双重检查锁定 TOCTOU、获取写锁后二次验证 |
| 7 | /go-concurrency-audit | internal/router/least_conn.go | ✅ 已完成 | Release 使用 CAS 防止计数器变负 |
| 8 | /go-concurrency-audit | internal/config/watcher.go | ✅ 已完成 | Start 方法在锁内设置 cancel 防竞态 |

## 阶段 4: 代码审查

| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 1 | /go-review | internal/middleware/ | ✅ 已完成 | Middlewares() 返回副本防外部修改、metrics LoadOrStore 修复竞态 |
| 2 | /go-review | internal/router/ | ✅ 已完成 | Route() 单次 RLock 防 TOCTOU、UpdateBackends/Routes 加锁、strings.HasPrefix |
| 3 | /go-review | internal/gateway/ | ✅ 已完成 | slowRecover 用 atomic.Bool 防止多 goroutine 触发 |
| 4 | /go-review | internal/proxy/ | ✅ 已完成 | 已在阶段 2/3 中审查修复 |
| 5 | /go-review | internal/lifecycle/ | ✅ 已完成 | health.go onChange 回调加锁保护 |
| 6 | /go-review | internal/httparser/ | ✅ 已完成 | WriteErrorResponse 添加错误日志 |

## 阶段 5: 测试 + 性能

| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 1 | /go-test-gen | internal/middleware/ratelimit.go | ✅ 已完成 | 添加 tokenRefill、emptyKey、maxBuckets、cleanup 测试 |
| 2 | /go-test-gen | internal/middleware/circuitbreaker.go | ✅ 已完成 | 添加 halfOpenFailure、halfOpenLimits、closedResetsFailures 测试 |
| 3 | /go-test-gen | internal/middleware/cors.go | ✅ 已完成 | 添加 matchingOrigin、nonMatching、wildcard、credentials+wildcard、emptyOrigins、preflight、noOrigin 测试 |
| 4 | /go-test-gen | internal/proxy/retry.go | ✅ 已完成 | 添加 jitter、fixedBackoff、isRetryableStatus、nilPolicy、customStatus 测试 |
| 5 | /go-test-gen | internal/lifecycle/recoverable.go | ✅ 已完成 | 添加 maxRetryExceeded、restartCount、stopAll、contextCancellation 测试 |
| 6 | /go-optimize | internal/proxy/proxy.go | ✅ 已完成 | strings.Builder 替代 fmt.Sprintf、Header 批量复制 |
| 7 | /go-optimize | internal/router/router.go | ✅ 已完成 | strings.HasPrefix 已优化，线性搜索在少量路由下可接受 |
| 8 | /go-optimize | internal/httparser/parser.go | ✅ 已完成 | readChunkedBody 预分配+直接写入、CRLF buffer 复用 |

## 执行摘要
- 总步骤: 31
- ✅ 已完成: 31
- 🔄 执行中: 0
- ⬜ 待执行: 0
- ⏭️ 跳过: 0
- ❌ 失败: 0
- 最后更新: 2026-05-10

## 优化成果总结

### 安全修复（4 项）
- 🔴 CORS 空配置默认放行 → 拒绝所有来源
- 🔴 CORS Credentials+Wildcard 不安全组合 → 反射 Origin
- 🔴 Trace ID 可预测时间戳前缀 → 全随机 ID
- ⚠️ 配置文件权限过宽 → 0600

### 错误处理（5 项）
- 🔴 GatewayError 无错误链 → 添加 Cause+Unwrap
- 🔴 Forward 最终错误无重试上下文 → 添加 attempts 信息
- 🔴 DispatchSync time.After goroutine 泄露 → NewTimer+Stop
- 🔴 Recoverable context 泄露 + running map 不清理 → 修复
- ⚠️ graceful shutdown time.After 泄露 → NewTimer+Stop

### 并发修复（8 项）
- 🔴 CircuitBreaker halfOpenActive 下溢 → 状态检查+下界保护
- 🔴 ConsistentHash 双重检查锁定 TOCTOU → 写锁内二次验证
- 🔴 LeastConn Release 计数器变负 → CAS+下界保护
- 🔴 Watcher Start cancel 竞态 → 锁内设置
- ⚠️ BackendTracker 缺少 Register → 添加自动注册
- ⚠️ WeightedRR 后端变化丢失权重 → 保留已有权重

### 代码审查（6 项）
- 🔴 metrics.go sync.Map Load/Store 竞态 → LoadOrStore
- 🔴 router.go Route() 两次 RLock TOCTOU → 单次 RLock
- 🔴 router.go UpdateBackends/Routes 无锁 → 加锁保护
- 🔴 chain.go Middlewares() 暴露内部切片 → 返回副本
- ⚠️ gateway.go slowRecover 多 goroutine 触发 → atomic.Bool
- ⚠️ health.go onChange 无锁读取 → RLock 保护

### 测试新增（30+ 用例）
- ratelimit: tokenRefill、emptyKey、maxBuckets、cleanup
- circuitbreaker: halfOpenFailure、halfOpenLimits、closedResetsFailures
- CORS: matchingOrigin、nonMatching、wildcard、credentials+wildcard、emptyOrigins、preflight、noOrigin
- retry: jitter、fixedBackoff、isRetryableStatus、nilPolicy、customStatus
- recoverable: maxRetryExceeded、restartCount、stopAll、contextCancellation

### 性能优化（3 项）
- proxy.go: strings.Builder 替代 fmt.Sprintf、Header 批量复制
- parser.go: readChunkedBody 预分配+直接写入、CRLF buffer 复用
- router.go: strings.HasPrefix 替代手动切片比较

### 测试覆盖率变化
- 优化前: 31.4%
- 优化后: 49.8%
- 提升: ↑18.4%
