# Roadmap: NexusGate

> 最后更新: 2026-05-10 | 版本: v0.3

## 项目现状

- 代码文件: 35 个
- 测试文件: 11 个
- 测试覆盖率: 49.8%
- 已知问题: 0 个
- 技术债: 6 项
- 优化计划: 31/31 已完成（见 go-optimize-plan.md）

## 🔴 P0 — 紧急（立即处理）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 1 | 🔒 security | ✅ 认证/鉴权中间件（API Key + JWT HMAC） | 系统完全暴露，任何请求可直接到达后端 | 2-3天 | internal/middleware/auth.go |
| 2 | 🔒 security | ✅ CORS 默认配置从 ["*"] 改为空列表+凭据安全 | 跨域攻击风险 | <1小时 | configs/nexusgate.yaml, internal/config/store.go |

## 🟠 P1 — 高优先级（本版本）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 3 | 📊 observability | ✅ 分布式追踪：W3C TraceContext 传播 + OTel Exporter 接口 | 无法跨服务追踪请求链路 | 2-3天 | internal/middleware/trace.go |
| 4 | 📊 observability | ✅ Prometheus 标准格式 histogram（累积桶+秒单位+le标签） | 无法与标准 Prometheus 生态集成 | 1天 | internal/middleware/metrics.go |
| 5 | ✨ feature | ✅ etcd 配置集成（加载+Watch+动态更新） | 无法从 etcd 动态加载配置，仅支持文件 | 2-3天 | internal/config/etcd.go |
| 6 | 🔧 techdebt | ✅ cmd/nexusgate 测试补全（buildAuthConfig + buildHandler + 集成测试） | 启动流程变更无法自动检测回归 | 1天 | cmd/nexusgate/main.go |
| 7 | 🔧 techdebt | ✅ 核心包覆盖率提升（gateway +7 测试、httparser +6 测试） | 核心逻辑变更风险高 | 2-3天 | internal/gateway/, internal/httparser/, internal/config/ |

## 🟡 P2 — 中优先级（下个版本）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 8 | ✨ feature | 灰度发布/金丝雀部署支持（按权重/头部/cookie 分流） | 无法安全上线新版本后端 | 2-3天 | internal/router/, internal/middleware/ |
| 9 | ✨ feature | WebSocket 代理支持 | 无法代理长连接场景（实时通信、流式推送） | 2-3天 | internal/proxy/, internal/gateway/ |
| 10 | ✨ feature | 请求/响应体修改中间件（请求改写、响应转换） | 无法在网关层做协议适配或数据脱敏 | 1-2天 | internal/middleware/ |
| 11 | ✨ feature | 分布式限流（Redis 后端） | 多实例部署时限流不生效 | 2-3天 | internal/middleware/ratelimit.go |
| 12 | 🔧 techdebt | 配置中 plugin.dir/hot_reload 未实现 | 插件配置项存在但无功能对应 | 2-3天 | internal/config/store.go, cmd/nexusgate/main.go |
| 13 | 🔧 techdebt | 配置中 logging.level/format 未应用到 slog | 日志级别和格式配置无效 | <1天 | cmd/nexusgate/main.go |
| 14 | 📊 observability | 缺少 pprof 调试端点 | 生产环境性能问题难以排查 | <1小时 | cmd/nexusgate/main.go |
| 15 | 📊 observability | 缺少自健康检查 HTTP 端点（/healthz） | K8s/负载均衡器无法探测网关自身健康 | <1天 | cmd/nexusgate/main.go |

## 🔵 P3 — 低优先级（排期待定）

| # | 类别 | 描述 | 影响 | 工作量 | 关联文件 |
|---|------|------|------|--------|----------|
| 16 | ✨ feature | 路由正则匹配支持 | 复杂路由规则无法表达 | 1天 | internal/router/router.go |
| 17 | ✨ feature | Admin API（路由管理、后端管理、配置查看） | 无法运行时动态管理 | 2-3天 | cmd/nexusgate/main.go, internal/ |
| 18 | ✨ feature | 服务发现集成（Consul/etcd 后端自动注册） | 后端上下线需手动改配置 | 2-3天 | internal/router/, internal/config/ |
| 19 | 🚀 ops | Dockerfile + docker-compose 多阶段构建 | 部署效率低 | <1天 | 项目根目录 |
| 20 | 🚀 ops | CI/CD 流水线（GitHub Actions） | 无自动化测试和发布 | 1天 | .github/workflows/ |
| 21 | 🚀 ops | 版本号管理（ldflags 注入） | 无法确认运行版本 | <1小时 | cmd/nexusgate/main.go, Makefile |
| 22 | 🔧 techdebt | 配置中 cache.miss_threshold 未实现 | 缓存配置项无功能对应 | 1天 | internal/config/ |
| 23 | 🔧 techdebt | 路由匹配线性搜索，大规模路由场景性能差 | 100+ 路由时匹配延迟增加 | 2-3天 | internal/router/router.go |

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

## 版本规划

| 版本 | 目标 | 包含项目 | 状态 |
|------|------|----------|------|
| v0.3 | 安全加固 + 可观测性基础 | #1, #2, #3, #4, #6 | ✅ 已完成 |
| v0.4 | 功能完善 + 测试补全 | #5, #7, #8, #9, #10, #12, #13, #14, #15 | 🔄 进行中 |
| v0.5 | 运维就绪 + 生态集成 | #11, #16, #17, #18, #19, #20, #21, #22 | ⬜ 计划中 |
| v1.0 | 生产级稳定版 | #23, #24, #25, #26, #27, #28, #29, #30 | ⬜ 远期 |

## 变更记录

| 日期 | 变更 |
|------|------|
| 2026-05-10 | 初始创建，基于五维分析模型生成 30 项路线图 |
| 2026-05-10 | ✅ 完成 P0 #1 认证/鉴权中间件（API Key + JWT HMAC） |
| 2026-05-10 | ✅ 完成 P0 #2 CORS 默认配置安全修复 |
| 2026-05-10 | ✅ 完成 P1 #3 分布式追踪 W3C TraceContext + OTel Exporter |
| 2026-05-10 | ✅ 完成 P1 #4 Prometheus 标准 histogram 格式 |
| 2026-05-10 | ✅ 完成 P1 #5 etcd 配置集成 |
| 2026-05-10 | ✅ 完成 P1 #6 cmd/nexusgate 测试补全 |
| 2026-05-10 | ✅ 完成 P1 #7 核心包覆盖率提升 |
| 2026-05-10 | v0.3 版本完成，进入 v0.4 |
