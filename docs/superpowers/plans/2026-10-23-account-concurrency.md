# 同账号并发与凭据协调实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 普通对话与知识整理允许同账号并发，仅刷新、投射和回收凭据时协调。

**Architecture:** 执行持有账号共享租约，配置修改与登录仍独占。Codex 原生刷新请求通过回环端点交给宿主；同账号旧令牌刷新合并并先持久化，再返回 CLI。各调用的回写携带原始快照，拒绝覆盖其他调用已更新的凭据。

**Tech Stack:** Go、SQLite、Codex ACP、原生刷新端点覆盖。

**Spec:** 用户已确认的同账号并发要求及本会话故障分析。

## Global Constraints

- 保留现有配置、模型和权限隔离；不触发真实模型或修改运行中任务。
- 不提交、推送或重启应用；保留无关工作区修改。
- 所有新增 Go 字段有中文注释；不记录令牌。

## Review Focus

- 多个任务同时使用旧刷新令牌，只访问认证服务一次。
- 长时间模型生成不能阻塞同账号普通对话或知识调用。
- 旧任务晚结束不能覆盖最新令牌；失败回写不能伪装成功。
- 等待短凭据锁支持取消，不释放其他任务的租约。
- 刷新端点随原生进程关闭，不接受无能力路径或浏览器来源请求。

### Task 1: 账号共享与任务凭据回收

**Files:** `server/services/aiaccount/service.go`、`login.go`、对应测试。

- [x] 先写并运行同账号普通、后台并发及取消回归，确认旧实现失败。
- [x] 执行改为共享租约；准备、刷新与回收使用可取消短锁。
- [x] 各任务独立投射，使用原始凭据快照防止过期回写；兼容旧 LiveAuthPath 回收。

### Task 2: 原生刷新协调与文本调用并发

**Files:** `server/internal/codexoauth`、`server/internal/runtimes/{types,acp_binding,acp_backend,acp_binding_codex,text,text_codex}.go`、执行和知识账号装配入口、对应测试。

- [x] 编写阻塞模型调用期间第二个调用能开始、并发刷新合并、端点关闭测试，确认旧实现失败。
- [x] 注入账号刷新回调；系统账号使用原生认证文件权威；保留身份校验与原子回写。
- [x] 移除覆盖整个文本生成的 OAuth 租约；原生进程刷新通过受控回环端点协调。

### Task 3: 验证与审阅

- [x] gofmt、定向测试和 race 测试；完整 go vet ./... 与 go test ./...。
- [x] 更新现有账号架构说明并检查文档，审阅变更的生命周期、身份与取消边界。
- [x] 报告自动验证与真实平台验收的边界。

文档检查已执行，但被既有 `.superpowers/sdd/知识库错误与离线统一_badc6e87.plan/progress.md` 中的个人目录拦截；未修改该无关文件。

补充回归覆盖了已接收刷新结果后取消的持久化、原生适配器可选接口保留，以及关闭刷新入口时等待保存。真实 CLI 验证使用合成凭据与回环假服务，仅证明刷新协议、落盘和 Prompt 交付，不代表真实账号登录或线上模型验收。
