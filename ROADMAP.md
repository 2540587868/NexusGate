# Roadmap: NexusGate

> 最后更新: 2026-05-16 | 版本: v1.2

## 项目现状

- 代码文件: 57 个（43 源码 + 14 测试）
- 测试覆盖率: cmd 7.5% | config 23.3% | dashboard 34.9% | gateway 42.3% | httparser 58.6% | lifecycle 55.8% | middleware 35.5% | proxy 29.1% | router 61.6%
- 已知 Bug: 3 个（1 严重）
- 安全隐患: 5 个严重
- 技术债: 19 个新文件零测试覆盖
- 硬编码值: 15+ 处应改为可配置
- 长函数: 6 个超过 50 行
- 重复代码: 5 处
- CI/CD: ✅ GitHub Actions（CI + Deploy + Release）
- Docker: ✅ GHCR 镜像 + 多阶段构建 + 非 root 用户
- Dashboard: ✅ 可视化面板（14 Tab：Overview/Gateway/Topology/Routes/Backends/Logs/Config/Middleware/Metrics/API Docs/Schema/Discovery/Tenants/Rewrite）

## 🔴 P0 — 紧急（立即处理）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| ~~94~~ | 🐛 bug | ~~Dashboard Bearer token 使用 `==` 比较~~ | ✅ 已修复 | | internal/dashboard/api.go |

## 🟠 P1 — 高优先级（本版本）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| ~~99~~ | 🐛 bug | ~~CircuitBreaker Reset 空实现~~ | ✅ 已修复 | | internal/dashboard/api.go |

> **P0 #94-#98 和 P1 #99-#104, #109 已全部完成**，详见变更记录。剩余 P1 项目 #102（19个新文件零测试覆盖）工作量较大，单独执行。

## 🟡 P2 — 中优先级（下个版本）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| ~~105~~ | 🔧 techdebt | ~~doForward/doForwardStream 80% 代码重复~~ | ✅ 已修复 | | internal/proxy/proxy.go:294-490 |
| ~~106~~ | 🔧 techdebt | ~~handleDocs/handleConfigSchema 过长~~ | ✅ 已修复 | | internal/dashboard/api.go:640-931 |
| ~~107~~ | 🔧 techdebt | ~~regexCache 重复定义~~ | ✅ 已修复 | | internal/middleware/body_rewrite.go:29, internal/router/router.go:70 |
| ~~108~~ | 🔧 techdebt | ~~15+ 处硬编码超时值~~ | ✅ 已修复 | | internal/proxy/, internal/middleware/, internal/dashboard/ |
| ~~109~~ | 🐛 bug | ~~Dockerfile EXPOSE 端口与配置不一致~~ | ✅ 已修复 | | Dockerfile:34 |
| ~~110~~ | 🔧 techdebt | ~~DefaultConfig 与 handleConfigSchema 默认值不一致~~ | ✅ 已修复 | | internal/config/store.go:257, internal/dashboard/api.go:799 |
| ~~111~~ | 🐛 bug | ~~配置热加载不覆盖中间件~~ | ✅ 已修复 | | internal/dashboard/api.go:617-633, cmd/nexusgate/main.go |
| ~~112~~ | 🐛 bug | ~~Gateway Close 无超时~~ | ✅ 已修复 | | internal/gateway/gateway.go:194-202 |
| ~~113~~ | 🔧 techdebt | ~~Token/ID 生成函数重复~~ | ✅ 已修复 | | internal/dashboard/api.go, internal/middleware/, internal/gateway/ |
| ~~114~~ | 🔧 techdebt | ~~HTTP Transport 配置重复~~ | ✅ 已修复 | | internal/proxy/proxy.go, internal/lifecycle/health.go |

## 🔵 P3 — 低优先级（排期待定）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| ~~115~~ | 🔧 techdebt | ~~FileWatcher 使用轮询~~ | ✅ 已修复 | | internal/config/watcher.go |
| ~~116~~ | 🔧 techdebt | ~~validateSessionCookie TOCTOU~~ | ✅ 已修复 | | internal/dashboard/api.go:162-166 |
| ~~117~~ | 🐛 bug | ~~镜像请求结果被完全丢弃~~ | ✅ 已修复 | | internal/proxy/mirror.go:132 |
| ~~118~~ | 🔧 techdebt | ~~OTel ExportSpan 使用 context.Background()~~ | ✅ 已修复 | | internal/middleware/otel.go:108 |
| ~~119~~ | 🔧 techdebt | ~~parser.go parseHeaderLine 错误被 continue 跳过~~ | ✅ 已修复 | | internal/httparser/parser.go:204 |
| ~~120~~ | 🔧 techdebt | ~~etcd Password 明文存储~~ | ✅ 已修复 | | internal/config/etcd.go:84 |
| ~~121~~ | 🔧 techdebt | ~~applyEnvOverrides 仅在 Load 时执行~~ | ✅ 已修复 | | internal/config/store.go:374-453 |

## ⚪ P4 — 可选（有空再做）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 122 | ✨ feature | **Dashboard 配置 diff 对比**：配置编辑器保存前显示 diff 对比 | 误操作风险，无法预览变更 | 1-2天 | internal/dashboard/static/ |
| 123 | ✨ feature | **Dashboard 批量操作**：路由/后端/租户批量删除和导入导出 | 管理效率低 | 2-3天 | internal/dashboard/ |
| 124 | 📊 observability | **审计日志**：Dashboard 写操作（路由创建/删除/配置修改）记录审计日志 | 无法追溯谁在何时做了什么变更 | 1-2天 | internal/dashboard/api.go |
| 125 | ✨ feature | **Dashboard WebSocket 终端**：实时查看后端连接状态和流量 | 排查问题需登录服务器 | 2-3天 | internal/dashboard/ |
| 126 | 🔧 techdebt | **配置文件示例和模板**：提供完整的 nexusgate.yaml 示例配置 | 新用户上手成本高 | <1天 | configs/ |
| 127 | ✨ feature | **Dashboard 国际化**：前端支持中英文切换 | 国际用户使用不便 | 2-3天 | internal/dashboard/static/ |

---

## 版本规划

| 版本 | 目标 | 包含项目 | 状态 |
|------|------|----------|------|
| v0.3 | 安全加固 + 可观测性基础 | #1, #2, #3, #4, #6 | ✅ 已完成 |
| v0.4 | 安全加固 + 功能完善 + 可视化 + Bug 修复 | #7, #13, #14, #15, #31, #32, #33, #37, #38, #39, #40, #41, #42 | ✅ 已完成 |
| v0.5 | 运维就绪 + 生态集成 | #43, #44, #45, #47, #48 | ✅ 已完成 |
| v0.6 | 关键 Bug 修复 + WebSocket + 安全加固 | #55, #56, #57, #58, #59, #60, #9 | ✅ 已完成 |
| v0.7 | 生产特性 + 可观测性完善 | #8, #11, #51, #53, #61, #62, #63, #64, #65, #66 | ✅ 已完成 |
| v0.8 | 代码质量 + 功能补全 | #67, #68, #69, #70, #72, #73, #74 | ✅ 已完成 |
| v0.9 | Dashboard 完善 + 服务发现 | #10, #18, #22, #34, #35, #49, #50, #71 | ✅ 已完成 |
| v1.0 | 生产级稳定版 | #24, #26, #27, #28, #36, #46, #52, #54, #75, #76, #77, #78 | ✅ 已完成 |
| v1.1 | 前端配套优化 — Dashboard 与后端 API 对齐 | #79, #80, #81, #82, #83, #84, #85, #86, #87, #88, #89, #90, #91, #92, #93 | ✅ 已完成 |
| v1.2 | 安全加固 + 测试补全 + 功能修复 | #94, #95, #96, #97, #98, #99, #100, #101, #102, #103, #104, #105, #106, #107, #108, #109, #110, #111, #112, #113, #114, #115, #116, #117, #118, #119, #120, #121 | ✅ 已完成 |

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
| 2026-05-11 | 新增 #37-#54 |
| 2026-05-11 | ✅ 完成 P0 #37-#39, P1 #40-#42 |
| 2026-05-11 | 🎉 v0.4 版本完成！P0+P1 全部清零 |
| 2026-05-11 | ✅ 完成 P2 #43-#48 |
| 2026-05-12 | ✅ 完成 P3 #23 Radix Tree, P4 #29 sync.Pool, P4 #25 请求镜像, P3 #16 路由正则 |
| 2026-05-12 | 🎉 v0.5 版本完成 |
| 2026-05-15 | /roadmap-gen 全面重新扫描：五维分析模型深度审计 |
| 2026-05-15 | 新增 #55 etcd Watch 未 Commit（P0 严重） |
| 2026-05-15 | 新增 #56 mirror/health 硬编码 http://（P0） |
| 2026-05-15 | 新增 #57 重试未切换后端（P1） |
| 2026-05-15 | 新增 #58 镜像不发送 Body（P1） |
| 2026-05-15 | 新增 #59 Request ID 中间件（P1） |
| 2026-05-15 | 新增 #60 Dashboard 认证安全性（P1） |
| 2026-05-15 | 新增 #61 HeaderRoute 回退逻辑（P2） |
| 2026-05-15 | 新增 #62 Readiness 探针（P2） |
| 2026-05-15 | 新增 #63 per-route 中间件/超时/重试（P2） |
| 2026-05-15 | 新增 #64 请求/响应流式转发（P2） |
| 2026-05-15 | 新增 #65 Gateway Close 不等待（P2） |
| 2026-05-15 | 新增 #66 请求取消传播（P2） |
| 2026-05-15 | 新增 #67 配置验证不完整（P2） |
| 2026-05-15 | 新增 #68 Recoverable 无指数退避（P2） |
| 2026-05-15 | 新增 #69 match() 嵌套重构（P2） |
| 2026-05-15 | 新增 #70 IP Hash 策略（P2） |
| 2026-05-15 | 新增 #71 环境变量覆盖配置（P3） |
| 2026-05-15 | 新增 #72 parser 重复代码（P3） |
| 2026-05-15 | 新增 #73 findOrCreateNode 拆分（P3） |
| 2026-05-15 | 新增 #74 Makefile golangci-lint（P3） |
| 2026-05-15 | 新增 #75 FileWatcher fsnotify（P4） |
| 2026-05-15 | 新增 #76 Dashboard handler 重复（P4） |
| 2026-05-15 | 新增 #77 gRPC 代理（P4） |
| 2026-05-15 | 新增 #78 Dockerfile GOPROXY（P4） |
| 2026-05-15 | #9 WebSocket 从 P2 升级至 P1 |
| 2026-05-15 | #51 EtcdProvider 从 P3 升级至 P2 |
| 2026-05-15 | #53 SSRF 防护从 P4 升级至 P2 |
| 2026-05-15 | #10 请求/响应体修改从 P2 调整至 P3 |
| 2026-05-15 | #46 ProxyModeMmap 从 P2 降级至 P4 |
| 2026-05-15 | ✅ 完成 P0 #55 etcd Watch 未 Commit（添加 Commit + 验证 + 回滚） |
| 2026-05-15 | ✅ 完成 P0 #56 mirror/health 硬编码 http://（自动检测 scheme 支持 HTTPS） |
| 2026-05-15 | ✅ 完成 P1 #57 重试时未切换后端（Forward 接受 route 参数，重试选择不同后端 + 回归测试） |
| 2026-05-15 | ✅ 完成 P1 #58 镜像请求不发送 Body（添加 bytes.NewReader 转发请求体） |
| 2026-05-15 | ✅ 完成 P1 #59 Request ID 中间件（X-Request-ID 头 + 透传 + 响应回写） |
| 2026-05-15 | ✅ 完成 P1 #60 Dashboard 认证安全性（Session Cookie + Secure 标志 + 登录限速 + Session 过期） |
| 2026-05-15 | ✅ 完成 P1 #9 WebSocket 代理支持（升级检测 + TCP 隧道 + 双向转发 + 后端切换 + race 检测通过） |
| 2026-05-15 | ✅ 完成 P2 #61 HeaderRoute 回退逻辑（添加 WithFallback 控制，默认保留回退，可关闭） |
| 2026-05-15 | ✅ 完成 P2 #65 Gateway Close 不等待请求完成（添加 WaitGroup 等待所有 worker 完成） |
| 2026-05-15 | ✅ 完成 P2 #68 Recoverable 指数退避（1s→2s→4s→8s→16s→30s cap） |
| 2026-05-15 | ✅ 完成 P2 #69 match() 嵌套重构（提取 matchMethod 辅助函数，扁平化条件判断） |
| 2026-05-15 | ✅ 完成 P2 #62 Readiness 探针（/readyz 端点 + RouteCount 方法，503/200 状态码） |
| 2026-05-15 | ✅ 完成 P2 #67 配置验证不完整（新增 Server/TLS/Auth/Routes/Backend 验证，7→17 字段） |
| 2026-05-15 | ✅ 完成 P2 #70 IP Hash 负载均衡策略（FNV-1a 哈希 + RemoteAddr key + 策略注册） |
| 2026-05-15 | 🎉 /roadmap-run 完成！P0×2 + P1×5 + P2×8 = 15 项全部通过 go vet + go build + go test + go race |
| 2026-05-15 | ✅ 完成 P2 #51 EtcdProvider 接入 main.go（Connect + Load + Watch + graceful shutdown） |
| 2026-05-15 | ✅ 完成 P2 #53 SSRF 防护（IsPrivateAddress + ValidateNoSSRF + allow_private_backends 配置项） |
| 2026-05-15 | ✅ 完成 P2 #66 请求取消传播（Request.Ctx + NewRequestWithContext + context.WithCancel） |
| 2026-05-15 | ✅ 完成 P2 #8 灰度发布/金丝雀部署（CanaryStrategy + Header/Cookie/Weight 分流 + 配置支持） |
| 2026-05-15 | ✅ 完成 P2 #11 分布式限流（DistributedLimiter 接口 + RedisLimiter 骨架 + 本地 fallback + 配置） |
| 2026-05-15 | ✅ 完成 P2 #63 per-route 超时/重试配置（RouteTimeout/RouteRetry + routeClient + context.WithTimeout + per-route retryable status） |
| 2026-05-15 | ✅ 完成 P2 #64 请求/响应流式转发（ForwardStream + StreamBody + chunked transfer + WriteResponse 流式支持 + route.Streaming 配置） |
| 2026-05-15 | ✅ 完成 P3 #10 请求/响应体修改中间件（BodyRewrite + HeaderRewrite + 正则替换 + RouteRewrite 配置） |
| 2026-05-15 | ✅ 完成 P3 #49 Dashboard 写入 API（路由 CRUD + 后端增删 + 熔断器重置 + 限流调整 + reloadRouter） |
| 2026-05-15 | ✅ 完成 P3 #18 服务发现集成（ServiceDiscovery + etcd watch + 自动注册/注销 + RegisterService） |
| 2026-05-15 | ✅ 完成 P3 #71 环境变量覆盖配置（NEXUSGATE_* 前缀 + 20 个配置项覆盖 + 12-Factor App 合规） |
| 2026-05-15 | ✅ 完成 P3 #72 parser 重复代码提取（readBodyFromHeaders 公共函数 + readBody/readResponseBody 复用） |
| 2026-05-15 | ✅ 完成 P3 #73 findOrCreateNode 拆分（appendChild + splitNodeWithPrefix + splitNodePartial + replaceChild） |
| 2026-05-15 | ✅ 完成 P3 #22 cache.miss_threshold 实现（etcdCache + LoadFromCache + missCount 阈值触发重新加载） |
| 2026-05-15 | ✅ 完成 P3 #24 HTTP/2 上游代理（http2.ConfigureTransport + http2Client + WithHTTP2 + ForceAttemptHTTP2） |
| 2026-05-15 | ✅ 完成 P3 #74 Makefile golangci-lint（lint 目标改用 golangci-lint run --timeout 5m） |
| 2026-05-15 | ✅ 完成 P3 #27 API 文档（OpenAPI 3.0 /api/v1/docs 端点 + 全路径文档） |
| 2026-05-15 | ✅ 完成 P3 #28 配置项说明文档（/api/v1/config/schema + 类型/默认值/描述 + 环境变量覆盖列表） |
| 2026-05-15 | ✅ 完成 P3 #34 Dashboard 实时日志流（LogStream + SSE /api/v1/logs/stream + 过滤 + keepalive） |
| 2026-05-15 | ✅ 完成 P3 #35 Dashboard 配置编辑器（/api/v1/config/edit GET/PUT + YAML 验证 + reloadRouter） |
| 2026-05-15 | ✅ 完成 P3 #50 Dashboard Prometheus 指标图表（/api/v1/metrics/prometheus 代理端点） |
| 2026-05-15 | ✅ 完成 P4 #26 请求/响应压缩（Compression 中间件 + gzip + Content-Type 智能判断 + 压缩比检查） |
| 2026-05-15 | ✅ 完成 P4 #36 gRPC-HTTP 协议转码（GRPCTranscoder + Content-Type 转换 + X-GRPC-Transcoded 标记） |
| 2026-05-15 | ✅ 完成 P4 #46 OpenTelemetry 导出器（OTelMiddleware + OTelExporter 接口 + StdoutOTelExporter + Span 导出） |
| 2026-05-15 | ✅ 完成 P4 #52 插件系统框架（PluginManager + PluginHook + Plugin 接口 + Register/Execute） |
| 2026-05-15 | ✅ 完成 P4 #54 多租户隔离（TenantIsolation + 路径访问控制 + 独立限流 + X-Tenant-ID） |
| 2026-05-15 | ✅ 完成 P4 #75 access_log 输出格式配置（AccessLogWithConfig + $method/$path/$status 变量替换） |
| 2026-05-15 | ✅ 完成 P4 #76 健康检查自定义路径（WithCheckPath + health_check.path 配置已接入） |
| 2026-05-15 | ✅ 完成 P4 #77 TLS 客户端证书验证（NewTLSListenerWithClientAuth + tls_client_ca/tls_client_verify 配置） |
| 2026-05-15 | ✅ 完成 P4 #78 请求体大小限制配置（max_request_body_bytes + WithMaxBodyBytes 接入） |
| 2026-05-15 | /roadmap-gen 前端配套审计：发现 7 个新 API 端点和 7 个新中间件无 Dashboard UI 支持，新增 #79-#93 共 15 项前端配套优化 |
| 2026-05-15 | ✅ 完成 P1 #79 Dashboard 实时日志流页面（SSE EventSource + 终端风格 + Level/Source 过滤 + 暂停/恢复/清空） |
| 2026-05-15 | ✅ 完成 P1 #80 Dashboard 在线配置编辑器（YAML textarea + Load/Save + 校验提示 + 状态反馈） |
| 2026-05-15 | ✅ 完成 P1 #81 Dashboard 熔断器控制面板（状态指示器 Closed/Open/HalfOpen + Reset 按钮 + 二次确认弹窗） |
| 2026-05-15 | ✅ 完成 P1 #82 Dashboard 限流动态调整面板（RPS/Burst 输入框 + 实时校验 + PUT 更新 + 状态反馈） |
| 2026-05-15 | ✅ 完成 P2 #83 Dashboard Prometheus 指标可视化（Prometheus 文本解析 + 指标卡片 + 原始数据展示） |
| 2026-05-15 | ✅ 完成 P2 #84 Dashboard 路由管理增强（创建表单 + 删除按钮 + 策略选择 + 后端地址输入） |
| 2026-05-15 | ✅ 完成 P2 #85 Dashboard 配置 Schema 浏览器（树形展示 + 字段类型/默认值/描述 + 环境变量列表） |
| 2026-05-15 | ✅ 完成 P2 #86 Dashboard API 文档页（OpenAPI 解析 + 方法色标 + 路径列表 + 摘要） |
| 2026-05-15 | ✅ 完成 P2 #87 Dashboard 中间件统一配置页（熔断器 + 限流 + Compression/OTel/gRPC/Tenant 集成） |
| 2026-05-15 | ✅ 完成 P2 #88 Dashboard 服务发现状态页（etcd 连接状态 + 服务列表 + 注册状态） |
| 2026-05-15 | ✅ 完成 P3 #89 Dashboard 前端架构升级（CSS 提取为 dashboard.css + CSS 变量主题系统 + 外部样式表引用） |
| 2026-05-15 | ✅ 完成 P3 #90 Dashboard 深色/浅色主题切换（CSS 变量 + data-theme + localStorage 持久化 + 切换按钮） |
| 2026-05-15 | ✅ 完成 P3 #91 Dashboard 响应式布局优化（@media 移动端适配 + 卡片/表格/输入框自适应） |
| 2026-05-15 | ✅ 完成 P3 #92 Dashboard 多租户管理页（MiddlewareConfig.Tenant + main.go 接入 TenantIsolation + 租户列表/创建/删除 + 限流参数 + 路径 ACL） |
| 2026-05-15 | ✅ 完成 P3 #93 Dashboard 路由改写规则编辑器（路由选择 + 请求/响应头改写 + 请求/响应体正则替换 + 实时预览） |
| 2026-05-15 | 🎉 v1.1 前端配套优化版本完成！Dashboard 从 5 个 Tab 扩展至 14 个 Tab，全面对齐后端 API |
| 2026-05-15 | /roadmap-gen 全面重新扫描：五维分析模型深度审计 v1.2，发现 5 个严重安全问题、3 个 Bug、19 个零测试文件、15+ 硬编码值 |
| 2026-05-15 | 新增 #94 Bearer token 时序侧信道攻击（P0） |
| 2026-05-15 | 新增 #95 WebSocket 头部注入（P0） |
| 2026-05-15 | 新增 #96 自动生成 token 写入日志（P0） |
| 2026-05-15 | 新增 #97 PluginManager 无锁保护（P0） |
| 2026-05-15 | 新增 #98 Dockerfile Go 版本号错误（P0） |
| 2026-05-15 | 新增 #99 CircuitBreaker Reset 空实现（P1） |
| 2026-05-15 | 新增 #100 Dashboard 写入端点验证不足（P1） |
| 2026-05-15 | 新增 #101 rand.Read 错误未检查（P1） |
| 2026-05-15 | 新增 #102 19 个新文件零测试覆盖（P1） |
| 2026-05-15 | 新增 #103 SSRF 防护不完整（P1） |
| 2026-05-15 | 新增 #104 Dashboard config/edit 验证不足（P1） |
| 2026-05-15 | 新增 #105 doForward/doForwardStream 重复代码（P2） |
| 2026-05-15 | 新增 #106 handleDocs/handleConfigSchema 过长（P2） |
| 2026-05-15 | 新增 #107 regexCache 重复定义（P2） |
| 2026-05-15 | 新增 #108 15+ 处硬编码超时值（P2） |
| 2026-05-15 | 新增 #109 Dockerfile EXPOSE 端口不一致（P2） |
| 2026-05-15 | 新增 #110 DefaultConfig 与 Schema 默认值不一致（P2） |
| 2026-05-15 | 新增 #111 配置热加载不覆盖中间件（P2） |
| 2026-05-15 | 新增 #112 Gateway Close 无超时（P2） |
| 2026-05-15 | 新增 #113 Token/ID 生成函数重复（P2） |
| 2026-05-15 | 新增 #114 HTTP Transport 配置重复（P2） |
| 2026-05-15 | 新增 #115 FileWatcher 使用轮询（P3） |
| 2026-05-15 | 新增 #116 validateSessionCookie TOCTOU（P3） |
| 2026-05-15 | 新增 #117 镜像请求结果被完全丢弃（P3） |
| 2026-05-15 | 新增 #118 OTel ExportSpan 使用 context.Background()（P3） |
| 2026-05-15 | 新增 #119 parseHeaderLine 错误被 continue 跳过（P3） |
| 2026-05-15 | 新增 #120 etcd Password 明文存储（P3） |
| 2026-05-15 | 新增 #121 applyEnvOverrides 仅在 Load 时执行（P3） |
| 2026-05-15 | 新增 #122 Dashboard 配置 diff 对比（P4） |
| 2026-05-15 | 新增 #123 Dashboard 批量操作（P4） |
| 2026-05-15 | 新增 #124 审计日志（P4） |
| 2026-05-15 | 新增 #125 Dashboard WebSocket 终端（P4） |
| 2026-05-15 | 新增 #126 配置文件示例和模板（P4） |
| 2026-05-15 | 新增 #127 Dashboard 国际化（P4） |
| 2026-05-15 | ✅ 完成 P0 #94 Bearer token 时序侧信道攻击修复（`==` → `subtle.ConstantTimeCompare`） |
| 2026-05-15 | ✅ 完成 P0 #95 WebSocket 头部注入修复（CRLF 过滤 + 头部白名单 + sanitizeHeaderValue） |
| 2026-05-15 | ✅ 完成 P0 #96 自动生成 token 写入日志修复（移除日志中的 token 字段） |
| 2026-05-15 | ✅ 完成 P0 #97 PluginManager 无锁保护修复（添加 sync.RWMutex + Execute 快照复制） |
| 2026-05-15 | ✅ 完成 P0 #98 Dockerfile Go 版本号修复（1.26 → 1.24） |
| 2026-05-15 | ✅ 完成 P1 #99 CircuitBreaker Reset 空实现修复（添加 Reset 方法 + Dashboard 持有 cb 引用 + 实际调用） |
| 2026-05-15 | ✅ 完成 P1 #100 Dashboard 写入端点验证不足修复（updateRoute/deleteRoute/updateTenant/deleteTenant 越界返回 404） |
| 2026-05-15 | ✅ 完成 P1 #101 rand.Read 错误未检查修复（api.go panic on error + otel.go fallback） |
| 2026-05-15 | ✅ 完成 P1 #103 SSRF 防护不完整修复（createRoute/addBackend/handleConfigEdit 添加 SSRF 校验） |
| 2026-05-15 | ✅ 完成 P1 #104 Dashboard config/edit 验证不足修复（validateConfigFromDashboard 添加 backend address 非空 + SSRF 校验） |
| 2026-05-15 | ✅ 完成 P2 #109 Dockerfile EXPOSE 端口不一致修复（9091 → 9090） |
| 2026-05-16 | ✅ 完成 P1 #102 19个新文件零测试覆盖（新增 13 个测试文件，86+ 测试用例） |
| 2026-05-16 | ✅ 完成 P2 #107 regexCache 重复定义修复（提取到 internal/util 包共享） |
| 2026-05-16 | ✅ 完成 P2 #110 DefaultConfig 与 Schema 默认值不一致修复（Schema 默认值 4→1 对齐 DefaultConfig） |
| 2026-05-16 | ✅ 完成 P2 #112 Gateway Close 无超时修复（30s deadline + goroutine/channel/select 模式） |
| 2026-05-16 | ✅ 完成 P2 #113 Token/ID 生成函数重复修复（提取 GenerateRandomID/GenerateRandomHexID 到 util 包） |
| 2026-05-16 | ✅ 完成 P3 #117 镜像请求结果被完全丢弃修复（添加 slog.Debug 日志记录失败） |
| 2026-05-16 | ✅ 完成 P3 #118 OTel ExportSpan context.Background() 修复（优先使用 req.Ctx） |
| 2026-05-16 | ✅ 完成 P3 #119 parseHeaderLine 错误被 continue 跳过修复（添加 slog.Debug 日志） |
| 2026-05-16 | ✅ 完成 P2 #105 doForward/doForwardStream 代码重复修复（提取 buildUpstreamRequest 共享函数） |
| 2026-05-16 | ✅ 完成 P2 #106 handleDocs/handleConfigSchema 过长修复（提取 doc_data.go + buildOpenAPIDoc 函数） |
| 2026-05-16 | ✅ 完成 P2 #108 15+处硬编码超时值修复（新增 12 个配置字段 + With* 方法 + 配置驱动） |
| 2026-05-16 | ✅ 完成 P2 #111 配置热重载不覆盖中间件修复（RateLimiter.UpdateRate/Burst + CircuitBreaker.UpdateConfig） |
| 2026-05-16 | ✅ 完成 P2 #114 HTTP Transport 配置重复修复（提取 newBaseTransport + 统一 IdleConnTimeout） |
| 2026-05-16 | ✅ 完成 P3 #115 FileWatcher 使用轮询修复（fsnotify 事件驱动 + debounce + polling fallback） |
| 2026-05-16 | ✅ 完成 P3 #116 validateSessionCookie TOCTOU 修复（RLock→Lock 原子操作） |
| 2026-05-16 | ✅ 完成 P3 #120 etcd Password 明文存储修复（sanitizeConfig 遮蔽 Password） |
| 2026-05-16 | ✅ 完成 P3 #121 applyEnvOverrides 仅在 Load 时修复（调整执行顺序 + ReloadWithEnvOverrides API） |
| 2026-05-16 | ✅ 完成 util 包测试补全（regex_test.go 5 测试 + id_test.go 8 测试） |
| 2026-05-16 | 🎉 v1.2 版本完成！P0×5 + P1×6 + P2×10 + P3×7 = 28 项全部通过 go vet + go build + go test + go race |
