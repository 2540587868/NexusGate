# Go Optimize Plan: NexusGate

## 项目信息
- 模块路径: github.com/nexusgate/nexusgate
- Go 版本: 1.22+
- 文件数: 44
- 测试覆盖率: ~55%
- 代码规模: ~5000 行

## 阶段 1: 安全审计
| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 1 | /go-security-audit | cmd/nexusgate/main.go (pprof 无 token 时暴露) | ⬜ 待执行 | S-1: tokenAuthMiddleware 空 token 时放行 |
| 2 | /go-security-audit | cmd/nexusgate/main.go + internal/dashboard/api.go (时序攻击) | ⬜ 待执行 | S-2/S-3: token 比较用 == 而非 subtle.ConstantTimeCompare |
| 3 | /go-security-audit | internal/dashboard/api.go (token 明文日志) | ⬜ 待执行 | S-4: 自动生成 token 打印到 slog.Warn |
| 4 | /go-security-audit | internal/proxy/mirror.go (HTTP 硬编码 + 敏感头泄露) | ⬜ 待执行 | S-7/S-8: 镜像请求硬编码 http:// 且复制所有 headers |
| 5 | /go-security-audit | internal/proxy/proxy.go (X-Forwarded-For 伪造) | ⬜ 待执行 | S-9: 未清洗客户端传入的 XFF 头 |

## 阶段 2: 并发安全
| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 6 | /go-concurrency-audit | internal/router/router.go (rebuildTree 数据竞争) | ⬜ 待执行 | C-1: RLock 下修改 tree 指针 + dirty 标志无锁保护 |
| 7 | /go-concurrency-audit | internal/lifecycle/health.go (checkAll 锁问题) | ⬜ 待执行 | C-3: RLock 和 Lock 之间可能访问已删除对象 |
| 8 | /go-concurrency-audit | cmd/nexusgate/main.go (context.Background 泄漏) | ⬜ 待执行 | C-8/C-9: recoverable=nil 时 goroutine 永不停止 |
| 9 | /go-concurrency-audit | internal/lifecycle/recoverable.go (递归 Go 竞争) | ⬜ 待执行 | C-4: 快速连续调用 Go 可能导致多 goroutine 竞争 |
| 10 | /go-concurrency-audit | internal/router/router.go (SwapRoutes 状态不一致) | ⬜ 待执行 | C-5: 替换 selectors 时影响进行中请求 |

## 阶段 3: 错误处理
| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 11 | /go-error-handling | internal/dashboard/api.go (rand.Read 忽略错误) | ⬜ 待执行 | E-1: crypto/rand.Read 返回值未检查 |
| 12 | /go-error-handling | internal/proxy/mirror.go (io.Copy 忽略错误) | ⬜ 待执行 | E-2: body 读取失败可能影响连接回收 |
| 13 | /go-error-handling | internal/config/etcd.go (Close 错误忽略) | ⬜ 待执行 | E-4: etcd 客户端关闭失败可能泄漏连接 |
| 14 | /go-error-handling | internal/httparser/parser.go (格式错误头静默忽略) | ⬜ 待执行 | E-11: 恶意请求头部被 continue 跳过 |
| 15 | /go-error-handling | internal/httparser/parser.go (json.Marshal 忽略错误) | ⬜ 待执行 | E-6: WriteErrorResponse 中序列化失败写空 body |

## 阶段 4: 性能优化
| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 16 | /go-optimize | internal/router/consistent_hash.go (字符串拼接) | ⬜ 待执行 | P-1: backendsSignature 循环中 += 拼接，应用 strings.Builder |
| 17 | /go-optimize | internal/middleware/metrics.go (sync.Map 开销) | ⬜ 待执行 | P-3/P-4: 每请求 13 次 sync.Map 操作 + fmt.Fprintf 输出 |
| 18 | /go-optimize | internal/config/store.go (deepCopy YAML 序列化) | ⬜ 待执行 | P-6: yaml.Marshal+Unmarshal 深拷贝开销大 |
| 19 | /go-optimize | internal/gateway/types.go (DispatchSync channel 浪费) | ⬜ 待执行 | P-7/Q-8: pool 预创建 channel 被 DispatchSync 覆盖 |
| 20 | /go-optimize | internal/proxy/proxy.go (重试 context 取消) | ⬜ 待执行 | Q-6: time.Sleep 阻塞不支持 context 取消 |

## 阶段 5: 代码质量 + 测试
| # | 命令 | 目标 | 状态 | 备注 |
|---|------|------|------|------|
| 21 | /go-review | internal/router/radix.go (findOrCreateNode 过长) | ⬜ 待执行 | Q-4: 86 行 + 4 层嵌套，需拆分 |
| 22 | /go-review | internal/middleware/metrics.go (MetricsHandler 过长) | ⬜ 待执行 | Q-1: 70+ 行重复 fmt.Fprintf |
| 23 | /go-review | internal/httparser/parser.go (readBody 重复代码) | ⬜ 待执行 | Q-3: readBody 和 readResponseBody 大量重复 |
| 24 | /go-test-gen | internal/proxy/mirror.go | ⬜ 待执行 | Q-10: 零测试覆盖 |
| 25 | /go-test-gen | internal/middleware/trace.go | ⬜ 待执行 | Q-10: 零测试覆盖 |
| 26 | /go-test-gen | internal/config/watcher.go | ⬜ 待执行 | Q-10: 零测试覆盖 |

## 执行摘要
- 总步骤: 26
- ✅ 已完成: 0
- ⬜ 待执行: 26
- ❌ 失败: 0
- ⏭️ 跳过: 0
- 最后更新: 2026-05-12
