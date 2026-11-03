# 服务商模型与错误能力实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 模型、逐模型强度、Fast 从原生目录到账号查询、配置保存和实际执行一致，并准确区分余额、额度和会话错误。

**Architecture:** 复用 acp-go 的模型 metadata 与配置 setter；标准化 Fast 选项，原生 service tier ID 留在适配器。NeuxBot 沿现有 Catalog、账号租约、SessionPlan 和 Execution 冻结配置贯通，不增加平行的模型目录或执行管理器。

**Tech Stack:** Go、ACP 配置目录、现有 OpenAPI 生成器、React/TanStack Query。

**Spec:** `docs/superpowers/specs/2026-11-03-provider-runtime-parity-design.md`

## Global Constraints

- 必须在用量基础阶段完成后实施；两仓库都使用同一独立功能分支。
- 子代理 GPT‑6.1 high，禁止继续派生；实现和审查分开。
- 模型 ID、原生 option value、显示 label 各司其职；不以更换模型 ID 模拟 Fast。
- 已知空配置不能继承其他模型选项；未知能力不能宣称支持。
- Fast 使用 `*bool` 表达未指定/明确关闭/明确开启；关闭不能因 omitempty 丢失。
- 不改既有账号凭据权威、Session owner 和重试调度；不静默重放结果未知的请求。
- 所有修改的 Go 结构体字段有中文注释，生成文件只通过项目命令更新。
- 仅定向验证；不打开/控制浏览器、桌面或运行真实模型；不推送发布。

## Review Focus

- 新旧 Codex 目录的 tier ID 与显示名不同，关闭必须清除原生 tier。
- 模型切换或 setter 回执更新目录后，之前合法的强度/Fast 可能已不合法。
- 账号 A/B 的模型权限不同，异步返回及陈旧目录不得跨账号复用。
- 默认值更改不能改变已接纳的执行；恢复旧 session 必须考虑 Fast 的兼容性。
- 资源不存在错误可能指文件/其他 session，余额不足也不能显示成有重置时间的套餐耗尽。

### Task 1: acp-go Codex Fast 与结构错误语义

**Files:** acp-go `pkg/codex/config.go`、`agent.go`、`session.go`、`appserver_client.go` 和对应配置/请求/假服务测试；`pkg/claude/config.go` 仅补统一语义所需的 metadata；`pkg/acpmeta/model.go` 与模型metadata测试；`pkg/kimi/outcome.go`、`pkg/pi/events.go` 及对应错误测试。

**Interfaces:**
- Produces: Codex 和 Claude 的逐模型配置目录通过已存在的 `WithModelConfigOptions` 声明 `fast` select，值为 `on`/`off`；仅模型确实支持时提供。Codex在适配器内部保存真实 service tier ID，读取新 `serviceTiers`、兼容原生旧 `additionalSpeedTiers`，名称不冒充ID。
- Produces: Codex新建/恢复/每轮原生请求准确传递当前tier；`off`明确发送原生清除值，`nil`不强行覆盖原生默认。需要显式JSON null时使用非生成封装，或修生成器表达可空语义，不手改生成文件。
- Consumes/produces: 保留现有 `errorKind`，Kimi原生402映射billing_error；Pi只在原生有结构字段时映射，未知文本不强归类。session missing必须携带resource与resourceId。

- [ ] **Step 1: 新增RED。** fake model/list返回tier ID `priority-v2`/Name `Fast`，选择on后的turn/start保持model且tier为priority-v2；off发送null；不支持模型不提供Fast；模型切换不带旧tier。Kimi402与429区分；未知Pi错误保持unknown。
- [ ] **Step 2: 运行新增定向测试并记录失败。** `go test ./pkg/codex ./pkg/claude ./pkg/acpmeta ./pkg/kimi ./pkg/pi -run '<新增测试正则>' -count=1`。
- [ ] **Step 3: 实现原生目录、配置和错误映射。** 使用同一session配置锁，setter回执记录真实值；不新建配置存储或Prompt循环。
- [ ] **Step 4: gofmt并运行受影响包测试。** 只包含实际修改的上述包，验证现有模型/强度/权限回归。
- [ ] **Step 5: 自审并提交acp-go，保存报告。** 记录实际接口及关闭null的wire证据。

### Task 2: NeuxBot 模型能力、Fast 与执行冻结贯通

**Files:** `server/internal/agent/config.go`、`catalog.go`、`contract.go`、`native/contract.go`、`native/registry.go`、`native/connection.go`、`session.go`；`server/repositories/models/runtime_environment.go`、`execution.go`、`chat.go`；`server/services/execution` 中选择、接纳、重试和fork配置相关文件；`server/apis/runtime_options`、`server/apis/chats` 和实际共享执行选项DTO；相关测试与生成契约。

**Interfaces:**
- Produces: `ModelOption.ConfigurationKnown bool`、`ModelOption.FastSupported bool`、`ModelOption.DefaultFast *bool`，从模型自己的目录读取；对应 RuntimeModelOption JSON为 `configurationKnown`、`fastSupported`、`defaultFast`。
- Produces: `Fast *bool` 沿 Runtime默认值、执行选择、SessionPlan的Profile、native.Profile及冻结Execution/Chat选择传递；JSON `fast`，默认设置 `defaultFast`。`native.Configuration.ActualFast *bool` 和现有 InvocationFacts.ServiceTier 反映实际原生回执（tier不能由宿主猜值）。
- Consumes: ACP `fast` select on/off；先设置模型，再强度，再Fast，并在最终回执重新核对所有请求值。未知/不支持的显式Fast请求返回Unsupported。
- 兼容摘要、chat续接比较、retry/fork与工作流/自动任务共用的配置序列化全部保留Fast三态。没有显式Fast的旧数据仍按原生默认处理。

- [ ] **Step 1: RED覆盖目录三态和真实配置。** A有high、B明确空、C未知，B/C不借用A；on/off/nil假原生请求及回执不同；后续setter回退模型/强度失败；Fast变化不能热复用旧配置；冻结后更改默认不影响执行。
- [ ] **Step 2: 运行对应agent/native/runtime_options/execution测试正则并记录失败。**
- [ ] **Step 3: 实现字段与映射。** 追踪每个现有Reasoning传播点，按职责加入Fast，不能漏retry/fork；未知与空保留ConfigurationKnown，删除按len回退到全局强度的逻辑。
- [ ] **Step 4: 生成契约并定向验证。** gofmt；运行受影响Go包的相应用例，`pnpm api:generate`；检查生成文件无非预期契约漂移。依赖使用两个worktree的临时Go workspace。
- [ ] **Step 5: 自审并提交NeuxBot，记录具体验证与未覆盖平台。**

### Task 3: 账号模型目录身份与错误恢复信息

**Files:** `server/apis/ai_accounts/native_models.go`、`native_models_test.go`、`server/services/aiaccount/capabilities.go` 及相关测试；`server/internal/agent/diagnostic.go`、`server/constants/runtime_failure_code.go`、错误定义及生成输出；`packages/frontend/src/modules/chat/model/runtime-failure-presentation.ts`、`shared/api/runtime-queries.ts` 和相关测试。

**Interfaces:**
- Consumes: 既有 `AIAccountService.DetectModel`，所有账号目录查询使用请求账号的共享租约与原生环境，不能返回Runtime默认账号的Detection冒充结果。
- Produces: `GetAIAccountNativeModelsResponse` 保留models/defaultModel，并增加 `Options []RuntimeModelOption`、`DetectedAt int64`、`AccountID string`、`RuntimeID string`；系统账号查询使用Runtime原有明确作用域。前端Query key包含runtime/account/config revision，旧主体响应不能写新主体缓存。
- Produces: `BILLING_ERROR`独立失败码，UI提示余额/计费入口；quota仅有真实reset证据才显示时间。`-32002`只有resource=session及匹配当前session时才映射SessionNotFound，其他资源保持原错误。
- 提交证据不充足时不增加可重试结论；原有PromptAccepted表示宿主接纳，文档不能把它当CLI已执行证明。

- [ ] **Step 1: RED覆盖账号与错误。** A/B fake账号不同目录，B查询从B启动资源取得；A迟到响应不覆盖B；目录失败不跨主体回退；billing/quota不同文案；文件与别的session的-32002不退休当前session。
- [ ] **Step 2: 运行具体API/native/diagnostic和前端Query测试记录失败。**
- [ ] **Step 3: 复用租约实现目录，修错误分类与作用域。** 不添加永久轮询或额外全局连接，不读取真实凭据进行验证。
- [ ] **Step 4: 生成API/枚举并运行定向回归、前端类型检查。** 生成命令遵循项目脚本；只修本任务产生的失败。
- [ ] **Step 5: 自审、提交并做阶段审查。** 页面整合在总体后续UI计划完成，本阶段不宣称完整用户体验已完成。
