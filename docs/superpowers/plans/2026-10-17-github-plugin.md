# GitHub 插件与授权入口 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 通过官方 CLI 共享登录与官方 MCP，让 GitHub 插件安装后可供伙伴使用，并为两个提供者提供二维码弹窗和网页授权入口。

**Architecture:** 从飞书抽取公共授权状态机；飞书和 GitHub 各自适配官方进程。资源、安装和执行按 provider 查询唯一授权组件，GitHub token 只进入精确官方 MCP 的派生请求头。保持标准插件包和固定本地 Git bundle。

**Tech Stack:** Go、官方 GitHub CLI 2.102.0、GitHub remote MCP、React/TanStack Query、现有 Dialog 与 qrcode.react。

**Spec:** `docs/superpowers/specs/2026-10-17-github-plugin-design.md`

## Global Constraints

- 用户已授权持续开发、最终独立 Review、提交、push、更新面向 main 的 PR #2；继续 `codex/plugin-resources` 独立 worktree。
- 不做历史迁移，不访问外部账号凭据，不自动打开浏览器或运行中的应用。
- 使用中文文档、注释与详细提交说明；生成协议只能由命令更新。
- 沿用原资源/版本/配置/占用/执行快照与统一事件；不新增持久化账号或前端轮询。
- 固定官方资产及许可证；远端 MCP 实现不声称固定版本。包体预算不随意放宽。

## Review Focus

- GitHub 环境变量或钥匙串覆盖私有身份：真实子进程边界隔离，并拒绝无私有 profile 的状态/令牌读取。
- 取消后官方进程还未退出：沿用 deviceRunning 保护，安装和新执行必须继续受阻。
- 伪称 GitHub 的本地包窃取 Bearer：仅精确 streamable HTTP 官方 MCP 允许凭据注入。
- 多提供者同次执行/列表：分别持有与缓存身份；飞书 CLI 环境和 GitHub MCP 凭据按各自边界投射。
- 设备流程有 URL 但遗漏验证码、远端失败或用户关闭弹窗：验证码可读可复制、不能误就绪，关闭弹窗不取消后台等待。

### Task 1: 公共授权状态机与 GitHub 官方适配

**Files:** 新增 `server/internal/plugins/authorization/`、`server/internal/plugins/github/`；调整 `server/internal/plugins/feishu/authorization.go` 及其测试；`server/constants/common.go`。

**Interfaces:** `authorization.Authorizer` 提供 Open/Close、Acquire(...Usage)、Usages、Status(ctx,verify)、Configure、Login(ctx,...scope)、Cancel、Logout、Snapshot、Binary、Environment、AuthorizeServer(ctx,url)；Account/Session/Usage 为中立类型，Session 增加 userCode。Driver 提供官方进程操作；Login 的回调报告 Device{VerificationURL,UserCode,ExpiresAt}，持续运行到官方流程结束。provider 构造返回共同 Authorizer。

- [x] 写失败测试：GitHub 无应用配置即可开始；固定非交互参数；流式解析官方 URL/验证码；状态不返回 token；无私有 profile 不查询外部身份；真实子进程清理 GH/GITHUB 覆盖；官方地址才允许 token。
- [x] 在 server 运行 `go test ./internal/plugins/... -count=1`，确认新能力缺失导致失败。
- [x] 实现公共状态机与两个适配；保留飞书所有已有取消/共享占用/race 行为；登录初始化 30 秒，GitHub 本地等待 15 分钟；调用官方私有文件 profile 并限制权限。
- [x] 再运行相同测试，预期 PASS；提交按功能分组留到最终 Review 后统一落地。

### Task 2: 资源、安装和执行多提供者准入

**Files:** `server/global/config.go`、`server/apis/plugins/{authorization,resources,install_preparation,get_authorization,start_authorization,types}.go`；`server/services/execution/{service,plugins,work}.go` 与对应测试。

**Interfaces:** global/执行 Config 使用 provider -> *authorization.Authorizer 的唯一装配表；列表和安装以 provider 缓存状态；Response 增加 provider；required scopes 来自各适配的最小权限与扩展。执行在同一占用内验证账号并注入官方 MCP header，快照只记录账号公开 ID。

- [x] 写失败测试：GitHub required scopes 没有 offline_access；列表同时查询飞书和 GitHub，各一次；未授权安装失败；执行官方 MCP 有 Bearer，恶意 URL/stdio 被拒绝；身份变化重试失败。
- [x] 运行 `go test ./apis/plugins ./services/execution -count=1`，预期新测试 FAIL。
- [x] 实现上述准入，保留显式引用/自动跳过语义；GitHub 不向 Runtime 暴露 CLI profile 或程序目录，飞书 profile 继续在 Runtime 覆盖合并后隔离；令牌不进入模型指令或快照。
- [x] 运行相同命令，预期 PASS。

### Task 3: 固定 GitHub 插件源与平台程序

**Files:** `.scratch/neux-plugins` 独立源；跟踪 `plugin-sources/*`、`scripts/plugin-resources.mjs` 及测试；构建调用；`THIRD_PARTY_NOTICES.md`。

**Interfaces:** source.lock.providers 是多提供者数组，每个提供者含 name/version/upstreamCommit/binary 及固定输入；飞书使用发布 assets，GitHub 使用 source/build（工具链、隔离补丁及回归摘要）；prepareProvider 和 verifyPrepared 遍历当次目标全部提供者。catalog 新增 github，标准 mcp.json 使用官方地址及限制工具组，所需组件为官方 MCP。

- [x] 写失败脚本测试：双提供者固定资产选择、缺项拒绝、两个包经过真实 Go 解析；ZIP/tar 提取正确程序与 LICENSE；离线缓存摘要不符失败。
- [x] 运行 `node --test scripts/plugin-resources.test.mjs`，预期 FAIL。
- [x] 实现多提供者；校验固定 GitHub CLI 源码与补丁，按六种目标编译应用隔离版本，更新独立源 commit/bundle/digest；不得添加运行时下载。
- [x] 相同脚本测试及 `node scripts/plugin-resources.mjs --check` 预期 PASS；本机准备两个固定程序。

### Task 4: 授权界面、生成协议与完整验收

**Files:** `desktop/src/modules/plugin/components/plugin-auth-panel.tsx`、授权弹窗、安装/详情 provider 条件、i18n 与行为测试；生成 API；架构/验收文档。

**Interfaces:** 公共授权 panel 根据 API provider 决定是否显示飞书应用配置；waiting 的原始 URL 驱动二维码 Dialog 和外链；userCode 可见并调用共享剪贴板；弹窗关闭不调用 cancel API。

- [x] 生成协议 `pnpm api:generate`；写失败行为测试：GitHub 不出现应用配置，显示验证码；点击二维码打开 Dialog；关闭再开；打开页面只使用原 URL；成功/取消关闭弹窗。
- [x] 运行相关 Vitest，预期 FAIL。
- [x] 实现界面并通过相关测试。更新架构和增量验收记录，明确真实账号/平台/视觉未运行。
- [x] 执行 gofmt、`go vet ./...`、`go test ./...`、授权包 `go test -race`、`NODE_OPTIONS=--no-experimental-webstorage pnpm check`，预期全部 PASS；构建本机包并核实预算。
- [x] 发起全新独立子代理 Review（最终代码与 Spec）；确认问题先失败测试再修复，复查通过后提交中文说明、push、更新 PR #2。不 merge。
