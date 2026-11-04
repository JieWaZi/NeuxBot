# AgentRuntime 重建：Claude 原生接入模块

> **For agentic workers:** 使用 subagent-driven-development 执行完整模块，并在模块提交后做独立聚焦审查。

**Goal:** 完整重构 acp-go Claude 原生接入，从直接 stream-json/control 到会话、配置、认证、用量与故障事实形成一条可验证路径。

**Architecture:** Agent 是 ACP 组合根；原生连接拥有传输与进程，会话拥有配置代次及输入与后台工作的生命周期，原生回合拥有身份、消息、完成和计数。技术差异留在 Adapter，公开聊天、资源复制、账号持久化和计价留在后续内部运行底座。

**Tech Stack:** Go、acp-go SDK、Claude stream-json/control、独立 fake CLI；不依赖 TypeScript Agent SDK 执行。

**Spec:** [云端重建方案](../specs/2026-11-04-agentruntime-rebuild-design.md)，特别是第 3、5、8.2 节。[P0 矩阵](../../discussions/agentruntime-rebuild/p0-provider-matrix-cn.md)提供初始源码差异。

## Global Constraints

- 本任务在 Codex 完整模块提交及聚焦审查关闭后实施；沿用其真实共享合同，不预建三家同义接口。
- 实现者必须实际阅读 OpenBot `f44dbe116c89a944f8d7d085e092c967b3fce0df` 对应 owning 源码与测试，不能只读摘要。以原生声明交叉验证其兼容判断。
- 用户指定 GPT-6.1 high；实现者不派生子代理，根协调安排独立 review。
- 整个模块重构、验证、立即提交后再审查；不把字段、helper 或注释翻译单独当作重构交付。
- 文件 snake_case；类型、Options、私有状态、转换和专属 helper 就近内聚；不因行数机械拆分，不增加无消费者接口或转发层。
- 新写与重写代码、测试使用英文注释，公开标识符注释以标识符开头。文档正文中文，许可与生成来源准确保留。
- 不以旧 Agent/Session/Store 层数和命名作为约束；保留有依据的行为及回归，删除被替代路径。
- OpenBot 与官方 SDK 用作协议和行为参照，独立 Go 实现和 synthetic 测试，不复制源码、测试或夹具。
- 不访问真实账号、登录或付费模型，不控制 UI；只做定向检查，不推送、合并或发布。

## 文件与职责

具体对象与命名在编码前写入实施报告；下表明确拆分依据，不要求保留旧对象或为每行创建 struct。

| 归属 | 完整职责 | 状态与边界 |
| --- | --- | --- |
| `pkg/claude/agent.go` | ACP 组合、能力协商、连接注入和根关闭接纳 | 根关闭状态与会话登记；不继续堆放配置、认证、事件细节 |
| `pkg/claude/session.go` | new/load/resume/fork 的准备、原生会话身份、安装与释放 | 同一 session 代次、打开串行化、部分资源和可重试释放；取消/关闭失败不得先移除所有者 |
| `pkg/claude/turn.go`、`turn_events.go`、`turn_usage.go` | 输入/steer、当前原生回合、原生后台回合、完成/取消、消息/工具归属、计数 | 只有一份回合身份与终态；状态和转换就近，旧 session/events/usage 主流程按此归属重组 |
| `pkg/claude/session_configuration.go` | 模型目录、setter 值映射、逐模型选项、请求/回执/观测 | 替代旧 config 中的混合推断；不按 model/effort/Fast 拆控制器 |
| `pkg/claude/launch.go`、`transport.go`、`process*.go` | 原生启动交付、control 配对、读写/背压、进程退出 | 各有真实协议或进程生命周期；专属 Options 和 helper 同文件 |
| `pkg/claude/auth.go` | 原生认证命令与无 Prompt 的模型/配额管理探测 | 完整管理操作；无账号数据库、租约调度或缓存策略 |
| 现有权限、表单、历史、工具与内容映射文件 | 真实外部交互和协议转换边界 | 继续保持消息/工具稳定身份，整合只做转发或单 helper 的拆分 |
| `pkg/claude/protocol/protocol.go` | 有来源的原生声明与 presence | 新增字段留原生层；公共事实经 acpmeta 转换后消费 |
| `pkg/acpmeta` | 三家真正共用且有消费者的技术事实 | 复用 Codex 已交付合同；不能放 Claude 任意 payload 或通用业务对象 |

## Review Focus

- 恢复/分叉后的首个累计统计、失败全零结果和 clear 重置，不能制造本轮用量或抹去已观察消费；见 Task 1 用量用例。
- 已返回 result 后仍有后台工作，或没有新 Prompt 而出现原生回合，不能误杀进程、丢消息或算到下一输入；见身份与后台用例。
- control 写入背压与取消/关闭竞争，不能阻塞无界、重复提交、遗失未释放资源；见生命周期用例。
- 空 MCP、权限 allow 规则及项目配置继承，不能伪称严格资源隔离；见启动与权限用例。
- 登录状态读取失败、账号根切换及旧结果迟到，不能报告为已登出或沿用另一个身份；见管理与代次用例。

### Task 1: 完整重构 Claude 原生接入模块

**Read first:** OpenBot `docs/architecture/providers.md`、`src/backend/claude-client.ts`、`claude-workspace-sandbox.ts`、`provider-drivers.ts` 的 Claude 段、`cli.ts` 的版本 gate、`agent/thread-lifecycle.ts`、`agent/usage-collection.ts`、`provider-history.ts` 的 Claude 路径，以及 `scripts/claude-usage-resume-e2e.ts` 和对应 conformance 测试。读 acp-go 当前 Claude owning 主流程与直接生产消费者；已存在的研究笔记只是导航。

**Scope:** `pkg/claude` 全部手写生产职责及必要回归、模块 README/UPSTREAM，`pkg/acpmeta` 必须共享的窄合同及直接调用编译适配。不修改 NeuxBot 产品模块，不重写其他 CLI。

**Interfaces:** 消费 Codex 模块已交付的原生指令入口/能力声明、模型选项事实与管理合同；其中配额使用 `acpmeta.AccountUsageMethod` 的 `_account/usage`，窗口单位明确为秒。对外仍提供实际 ACP 构造与会话入口；额外认证/后台/计数事实通过协商的技术扩展或明确返回合同，宿主不解析 Claude 原始 JSON。新增共享声明需给出消费者及真实 wire 证据。

**Architecture and behavior requirements:**

1. 写清核心类型保留/替换的理由、唯一状态、接纳与释放顺序及 OpenBot 参照。不要只把旧方法拆文件；取消、完成、后台、输出和用量必须围绕相同原生身份组织。临时管理连接与运行连接分别释放，不让管理探测借用正在执行的会话。
2. new/load/resume/fork 交付明确的原生 system append；缺省不覆盖、明确空可清除。恢复时处理旧 prompt snapshot；OpenBot 已验证 `--system-prompt-snapshot off` 的版本 gate 为 2.1.263，不能推断所有版本都支持或仅设置 SDK 同名选项就生效。版本不支持必须如实限制，而非偷偷落回生成 CLAUDE.md。
3. cwd 与额外目录是真实路径；设置源、严格 MCP、CLI 内置工具、原生 plugin 目录和权限各自表达。显式空 MCP 仍用空配置及 strict，不能继承任意服务器。资源复制由 runtime 完成；Adapter 只传真实入口及加载诊断。必要约束无法执行时拒绝，不用提示词声称隔离。管理探测不发 Prompt、不持久化会话、不启动用户 MCP。
4. 模型以原生 resolved ID 对外、实际 setter value 对内，支持缺省/未知/已知空；不因 OpenBot 有 medium/high 产品 fallback 就虚构能力。模型/effort/Fast 的请求、ACK、观测分别处理；换模型失效 effort 通过原生 null 清除或解释拒绝。已有已证实 Fast 不因固定 OpenBot 未暴露而删除。失败配置不能写成生效，部分应用必须保留实际事实。
5. 认证 status/login/logout/cancel 用真实命令和账号环境，API key 与订阅身份分开；`CLAUDE_CONFIG_DIR` 与 macOS Keychain 对应关系留清晰技术边界。登录状态的 timeout/spawn/malformed 不当作 signed out；退出 1 加可解析 `loggedIn:false` 与未知区别。管理 quota 采用官方 `get_usage`、`skip_behaviors:true`，将 ISO reset 转为明确单位，保留全部窗口与未知/零，余额与套餐限额分开，不把 session 累计当账号消耗。
6. 原生输入、stream、assistant、result、idle、背景任务与 steer 有明确身份；使用 `user_message_uuid` 及存在时的已消费 UUID 列表。前台结束不等于所有后台工作已结束，无新宿主 Prompt 的后续原生回合仍保留身份、消息与用量。旧 producer 缺字段不能猜关联；进程 `result_index` 不能冒充跨进程稳定 ID。主代理与子代理内容不能互相去重或重复拼接。
7. result 是原生完成依据；`subtype:success` 加 `is_error:true` 仍为失败，明确 `terminal_reason` 支持真实取消。`api_retry` 仅为原生重试事实；rate-limit rejected 可能继续 overage，不能仅此提前结束/重发。压缩使用已支持原生机制并等待真实结束；post_tokens 与完整 Context 不混同。
8. 用量分别覆盖 request 消息替换去重、本轮 main_loop、query pipeline 模型累计和 Context。modelUsage 包括子任务等且在 resume/fork 带历史，clear 重置、crash 全零须有有效基线判断；可靠差分与部分本轮请求不能重叠计费。native total_cost_usd 是累计**估算**而非真实账单，保留 scope/source/valuation。失败取消仍交付已观察用量；原生最终事实不能只靠可能失败的 observer 更新保存。
9. 传输保留顺序、反向 control 上限、末帧/坏帧与协议健康语义；短写、部分提交、等待写锁取消、pending 取消和退出不得无界阻塞。取消不猜安全重放，关闭失败同一所有者保留可重试资源；根关闭封住新打开且排空或取消进行中的准备。stderr 有界脱敏，不泄漏 prompt、认证或 MCP 凭据。
10. 错误限定 phase、原生证据、接纳强度、已见输出/副作用；会话缺失必须匹配当前资源 ID，不以通用 missing 文本触发恢复。未知原生能力明确 unsupported/unknown，能力声明由实际实现和版本证据支持。

**Steps:**

- [ ] **Step 1:** 实际读源码与官方声明，报告 owning 表与入口至终态的调用链；必要新共享合同以真实消费者解释。不得直接照搬 SDK 功能名为 Go 能力。
- [ ] **Step 2:** 建立独立 fake CLI 行为回归并记录实际 RED：指令 new/resume/fork、空 MCP/严格探测、模型切换与部分失败、get_usage 无 Prompt/窗口 presence、主/后台/steer 身份、恢复累计/失败零/重置、取消背压/关闭重试、精确缺失与反例。已有正确用例保留，不为制造 RED 人为破坏实现。
- [ ] **Step 3:** 按所有者完成整个模块替换，将配置与回合状态转换集中，删除被替代文件和转发路径。现有正确权限、工具、内容、历史、输入交互必须继续可用。
- [ ] **Step 4:** `gofmt -w` 修改文件；`GOWORK=off go test ./pkg/claude ./pkg/claude/protocol ./pkg/acpmeta` 及直接受影响消费者定向检查；对真实并发改动运行命名用例 `-race`，报告覆盖对象。只运行必要生成命令，不手改生成声明。
- [ ] **Step 5:** 更新模块真实能力与上游映射；自审文件/类型命名、英文注释、唯一状态和主要阅读路径，完成即提交。报告 base/head、命令与结果、公共合同迁移、来源和未验收边界，交独立聚焦 review。

fake CLI 通过仅证明 Adapter 的状态与交付，不能证明真实 CLI 接受所有 flags、实际账号隔离或模型费用。真实 CLI 加本机假服务、真实账号/模型、平台与 UI 验收分开记录，后者不会因本计划自动执行。
