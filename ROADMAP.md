# Roadmap: NexusGate

> 最后更新: 2026-05-11 | 版本: v0.4

## 项目现状

- 代码文件: 38 个（含 2 个 dashboard 新增）
- 测试文件: 12 个
- 测试函数: 82 个
- 测试覆盖率: ~50%（dashboard 零覆盖）
- 已知 Bug: 3 个（1 个严重）
- 配置字段: 40 个（12 个未使用，30% 浪费）
- 硬编码值: 23 处应改为可配置
- 长函数: 7 个超过 50 行（最长 main() 219 行）
- CI/CD: ✅ GitHub Actions（CI + Deploy + Release）
- Docker: ✅ GHCR 镜像 + 服务器部署
- Dashboard: ✅ 可视化面板（gate.747575.xyz）

## 🔴 P0 — 紧急（立即处理）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 1 | 🔒 security | ✅ 认证/鉴权中间件（API Key + JWT HMAC） | 系统完全暴露 | 2-3天 | internal/middleware/auth.go |
| 2 | 🔒 security | ✅ CORS 默认配置安全修复 | 跨域攻击风险 | <1小时 | configs/nexusgate.yaml |
| 37 | 🐛 bug | **API Key 验证形同虚设**：auth.go:110 `subtle.ConstantTimeCompare([]byte(key), []byte(key))` 将 key 与自身比较，永远返回 true | 任何非空 API Key 都能通过认证，等于没有认证 | <1小时 | internal/middleware/auth.go:110 |
| 38 | 🐛 bug | **LeastConn 连接计数泄漏**：`Release()` 从未被调用，连接计数只增不减 | 长时间运行后所有后端连接计数无限增长，路由完全失效 | <1天 | internal/router/least_conn.go, internal/proxy/proxy.go |
| 39 | 🔒 security | **Dashboard 无认证**：/api/v1/config 暴露完整配置（含 API Key、JWT Secret） | 任何人可访问 gate.747575.xyz 查看所有密钥 | 1-2天 | internal/dashboard/api.go |

## 🟠 P1 — 高优先级（本版本）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 3 | 📊 observability | ✅ 分布式追踪 W3C TraceContext + OTel Exporter | 无法跨服务追踪 | 2-3天 | internal/middleware/trace.go |
| 4 | 📊 observability | ✅ Prometheus 标准 histogram 格式 | 无法与 Prometheus 集成 | 1天 | internal/middleware/metrics.go |
| 5 | ✨ feature | ✅ etcd 配置集成 | 仅支持文件配置 | 2-3天 | internal/config/etcd.go |
| 6 | 🔧 techdebt | ✅ cmd/nexusgate 测试补全 | 启动流程无回归测试 | 1天 | cmd/nexusgate/main.go |
| 7 | 🔧 techdebt | ✅ 核心包覆盖率提升 | 核心逻辑变更风险高 | 2-3天 | internal/gateway/, internal/httparser/ |
| 31 | ✨ feature | ✅ Admin Dashboard 可视化面板 | 网关状态不可见 | 3-5天 | internal/dashboard/ |
| 13 | 🔧 techdebt | ✅ logging.level/format 应用到 slog | 日志配置无效 | <1天 | cmd/nexusgate/main.go |
| 14 | 📊 observability | ✅ pprof 调试端点 | 性能问题难排查 | <1小时 | cmd/nexusgate/main.go |
| 15 | 📊 observability | ✅ /healthz 自健康检查端点 | K8s 无法探测健康 | <1天 | cmd/nexusgate/main.go |
| 32 | ✨ feature | ✅ Admin API RESTful 接口 | 无管理接口 | 2-3天 | internal/dashboard/api.go |
| 33 | 📊 observability | ✅ 实时流量拓扑可视化 | 路由关系不直观 | 2-3天 | internal/dashboard/static/ |
| 40 | 🔧 techdebt | **12 个配置字段定义但未使用**（gateway.shard_count, worker_per_shard, slow_recovery_threshold, proxy.default_mode, router.consistent_hash.virtual_nodes, header_route.header, middleware.order, plugin.*, config.cache.miss_threshold, lifecycle.recoverable, logging.access_log, routes.middleware） | 30% 配置项是空壳，用户配置了不生效且无提示 | 2-3天 | internal/config/store.go, cmd/nexusgate/main.go |
| 41 | 🔧 techdebt | **main() 函数 219 行**，承担配置加载、路由构建、中间件组装、健康检查、配置热加载、优雅关闭、TCP 监听等全部职责 | 难以维护和测试，任何修改都可能引入回归 | 1-2天 | cmd/nexusgate/main.go |
| 42 | 🐛 bug | **路由 Headers 匹配未实现**：`MatchRule.Headers` 字段被 matchRule 完全忽略 | 配置了 headers 匹配规则的路由永远不会命中 | 1天 | internal/router/router.go:165-173 |

## 🟡 P2 — 中优先级（下个版本）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 8 | ✨ feature | 灰度发布/金丝雀部署支持（按权重/头部/cookie 分流） | 无法安全上线新版本后端 | 2-3天 | internal/router/, internal/middleware/ |
| 9 | ✨ feature | WebSocket 代理支持 | 无法代理长连接场景 | 2-3天 | internal/proxy/, internal/gateway/ |
| 10 | ✨ feature | 请求/响应体修改中间件 | 无法做协议适配或数据脱敏 | 1-2天 | internal/middleware/ |
| 11 | ✨ feature | 分布式限流（Redis 后端） | 多实例限流不生效 | 2-3天 | internal/middleware/ratelimit.go |
| 43 | 🔧 techdebt | **23 处硬编码超时/阈值**应改为可配置（ShardCount=8, syncTimeout=30s, 慢恢复阈值=0.9, 响应体限制=10MB, 健康检查URL, 虚拟节点数=150 等） | 无法根据生产环境调优，修改需重新编译 | 1-2天 | internal/gateway/, internal/proxy/, internal/lifecycle/, internal/httparser/ |
| 44 | 🔧 techdebt | **Dashboard 零测试覆盖** + FileWatcher/EtcdProvider 零测试 | 关键组件无回归保护 | 1-2天 | internal/dashboard/, internal/config/ |
| 45 | 🐛 bug | **IsRetryableStatus 死代码**：已定义但在 Forward 中从未调用，重试只检查 isRetryableError 不检查状态码 | 5xx 响应不会触发重试 | <1天 | internal/proxy/retry.go, proxy.go |
| 46 | 🔧 techdebt | **ProxyModeMmap 未实现**：枚举已定义但 DetermineProxyMode 只返回 Splice/Buffer | mmap 零拷贝模式永远无法使用 | 2-3天 | internal/gateway/types.go, internal/proxy/ |
| 47 | 🔒 security | **Dashboard CORS 硬编码 `Access-Control-Allow-Origin: *`** | 生产环境应限制允许的来源 | <1小时 | internal/dashboard/api.go |
| 48 | 🔒 security | **pprof 端点无认证**暴露在 metrics 端口 | 可被外部访问获取运行时敏感信息 | <1天 | cmd/nexusgate/main.go |

## 🔵 P3 — 低优先级（排期待定）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 16 | ✨ feature | 路由正则匹配支持 | 复杂路由规则无法表达 | 1天 | internal/router/router.go |
| 18 | ✨ feature | 服务发现集成（Consul/etcd 后端自动注册） | 后端上下线需手动改配置 | 2-3天 | internal/router/, internal/config/ |
| 19 | 🚀 ops | ✅ Dockerfile + docker-compose 部署 | 部署效率低 | <1天 | 项目根目录 |
| 20 | 🚀 ops | ✅ CI/CD 流水线 | 无自动化测试和发布 | 1天 | .github/workflows/ |
| 21 | 🚀 ops | ✅ 版本号管理 | 无法确认运行版本 | <1小时 | cmd/nexusgate/main.go |
| 22 | 🔧 techdebt | 配置中 cache.miss_threshold 未实现 | 缓存配置项无功能对应 | 1天 | internal/config/ |
| 23 | 🔧 techdebt | 路由匹配线性搜索 O(n) | 100+ 路由时性能差 | 2-3天 | internal/router/router.go |
| 34 | ✨ feature | Dashboard 实时日志流（WebSocket 推送） | 排查需登录服务器 | 1-2天 | internal/dashboard/ |
| 35 | ✨ feature | Dashboard 配置编辑器（在线编辑+热更新） | 修改需 SSH 到服务器 | 2-3天 | internal/dashboard/ |
| 49 | ✨ feature | Dashboard 写入 API（路由/后端 CRUD、熔断器重置、限流调整） | Dashboard 只读无法管理 | 2-3天 | internal/dashboard/api.go |
| 50 | ✨ feature | Dashboard Prometheus 指标图表展示 | 指标只能看原始文本 | 1-2天 | internal/dashboard/static/ |
| 51 | 🔧 techdebt | EtcdProvider 已实现但 main.go 未接入 | etcd 配置加载代码是死代码 | 1天 | cmd/nexusgate/main.go, internal/config/etcd.go |
| 52 | 🔧 techdebt | 旧版 EtcdConfig 结构体未清理 | 与新版 EtcdProviderConfig 不一致 | <1小时 | internal/config/store.go |

## ⚪ P4 — 可选（有空再做）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 24 | ✨ feature | HTTP/2 上游代理支持 | 无法利用 HTTP/2 多路复用 | 2-3天 | internal/proxy/proxy.go |
| 25 | ✨ feature | 请求镜像/影子流量 | 无法安全验证新后端 | 1-2天 | internal/proxy/ |
| 26 | ✨ feature | 自定义插件系统（Go plugin 或 WASM） | 无法动态扩展中间件 | 1周+ | internal/middleware/, internal/config/ |
| 27 | 📝 docs | API 文档（OpenAPI/Swagger） | 新用户上手成本高 | 1-2天 | docs/ |
| 28 | 📝 docs | 配置项完整说明文档 | 配置含义不明确 | <1天 | configs/ |
| 29 | ⚡ perf | sync.Pool 复用 Request/Response 对象 | 高 QPS 下 GC 压力大 | 1天 | internal/gateway/types.go |
| 30 | ⚡ perf | 连接池指标暴露和调优 | 无法监控连接池利用率 | <1天 | internal/proxy/proxy.go |
| 36 | 📊 observability | Grafana Dashboard 模板 | 需从零搭建 Grafana | 1天 | deploy/grafana/ |
| 53 | 🔒 security | 后端地址 SSRF 防护（验证不允许访问内网地址） | 恶意配置可访问内网服务 | 1天 | internal/proxy/proxy.go |
| 54 | 🔧 techdebt | HTTP Parser 100-Continue 和 Trailer 处理 | 部分场景请求处理不完整 | 1天 | internal/httparser/parser.go |

## 版本规划

| 版本 | 目标 | 包含项目 | 状态 |
|------|------|----------|------|
| v0.3 | 安全加固 + 可观测性基础 | #1, #2, #3, #4, #6 | ✅ 已完成 |
| v0.4 | 功能完善 + 可视化 + Bug 修复 | #7, #13, #14, #15, #31, #32, #33, #37, #38, #39, #40, #41, #42 | 🔄 进行中 |
| v0.5 | 运维就绪 + 生态集成 | #8, #9, #10, #11, #43, #44, #45, #46, #47, #48 | ⬜ 计划中 |
| v0.6 | Dashboard 完善 + 服务发现 | #16, #18, #34, #35, #49, #50, #51, #52 | ⬜ 计划中 |
| v1.0 | 生产级稳定版 | #22, #23, #24, #25, #26, #27, #28, #29, #30, #36, #53, #54 | ⬜ 远期 |

## 变更记录

| 日期 | 变更 |
|------|------|
| 2026-05-10 | 初始创建，基于五维分析模型生成 30 项路线图 |
| 2026-05-10 | ✅ 完成 P0 #1 认证/鉴权中间件 |
| 2026-05-10 | ✅ 完成 P0 #2 CORS 默认配置安全修复 |
| 2026-05-10 | ✅ 完成 P1 #3 分布式追踪 |
| 2026-05-10 | ✅ 完成 P1 #4 Prometheus histogram |
| 2026-05-10 | ✅ 完成 P1 #5 etcd 配置集成 |
| 2026-05-10 | ✅ 完成 P1 #6 cmd/nexusgate 测试补全 |
| 2026-05-10 | ✅ 完成 P1 #7 核心包覆盖率提升 |
| 2026-05-10 | v0.3 版本完成，进入 v0.4 |
| 2026-05-11 | ✅ 完成 P3 #19 Dockerfile + docker-compose 部署 |
| 2026-05-11 | ✅ 完成 P3 #20 CI/CD 流水线 |
| 2026-05-11 | ✅ 完成 P3 #21 版本号管理 |
| 2026-05-11 | ✅ 完成 P1 #31 Admin Dashboard 可视化面板 |
| 2026-05-11 | ✅ 完成 P2 #32 Admin API RESTful 接口 |
| 2026-05-11 | ✅ 完成 P2 #33 实时流量拓扑可视化 |
| 2026-05-11 | ✅ 完成 P2 #13 logging.level/format 应用到 slog |
| 2026-05-11 | ✅ 完成 P2 #14 pprof 调试端点 |
| 2026-05-11 | ✅ 完成 P2 #15 /healthz 自健康检查端点 |
| 2026-05-11 | /roadmap-gen 重新扫描：发现 3 个 Bug（1 严重）、12 个未使用配置、23 处硬编码 |
| 2026-05-11 | 新增 #37 API Key 验证 Bug（P0 严重） |
| 2026-05-11 | 新增 #38 LeastConn 连接计数泄漏（P0） |
| 2026-05-11 | 新增 #39 Dashboard 无认证（P0） |
| 2026-05-11 | 新增 #40 12 个配置字段未使用（P1） |
| 2026-05-11 | 新增 #41 main() 函数过长（P1） |
| 2026-05-11 | 新增 #42 路由 Headers 匹配未实现（P1） |
| 2026-05-11 | 新增 #43 23 处硬编码值（P2） |
| 2026-05-11 | 新增 #44 Dashboard/FileWatcher/Etcd 零测试（P2） |
| 2026-05-11 | 新增 #45 IsRetryableStatus 死代码（P2） |
| 2026-05-11 | 新增 #46 ProxyModeMmap 未实现（P2） |
| 2026-05-11 | 新增 #47 Dashboard CORS 硬编码（P2） |
| 2026-05-11 | 新增 #48 pprof 无认证（P2） |
| 2026-05-11 | 新增 #49 Dashboard 写入 API（P3） |
| 2026-05-11 | 新增 #50 Dashboard 指标图表（P3） |
| 2026-05-11 | 新增 #51 EtcdProvider 未接入（P3） |
| 2026-05-11 | 新增 #52 旧版 EtcdConfig 未清理（P3） |
| 2026-05-11 | 新增 #53 SSRF 防护（P4） |
| 2026-05-11 | 新增 #54 HTTP Parser 完善（P4） |
| 2026-05-11 | #17 Admin API 升级合并为 #31+#32 |
