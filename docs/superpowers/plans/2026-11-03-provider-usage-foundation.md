# 原生用量正确性实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复多请求回合漏计、未知用量归零和错误终态丢失用量，建立后续统计所需的准确原生事实。

**Architecture:** acp-go 归一化各原生协议的用量，在标准 ACP 通知的命名空间元数据内传递可替换的本轮累计快照。NeuxBot 的现有 native/Session/Execution 链保留快照、完整性与上下文，并在原有终态归档；不新增执行循环。

**Tech Stack:** Go、acp-go、现有 ACP SDK 与 JSONL 事件日志。

**Spec:** `docs/superpowers/specs/2026-11-03-provider-runtime-parity-design.md`

## Global Constraints

- 子代理使用 GPT‑6.1 high；实现与审查分开，禁止子代理继续派生。
- 仅修改独立工作区；原工作区未提交修改保持不变。
- 每个新增或修改 Go 结构体的全部字段有准确中文注释。
- 不手工修改生成文件；不运行全量测试或全量检查。
- 不复制或翻译 OpenBot 源码；根据原生协议与审计结果独立实现。
- 不启动真实模型、打开或控制浏览器/桌面进行验收；需要真实资源时明确列出未验收范围。
- Token 未知不能当零；上下文不能当消费；推理输出不能重复加到输出。
- 不使用绝对路径 replace 提交依赖，两个仓库联调使用未提交的独立 Go workspace。

## Review Focus

- 首次 token 通知发生在恢复历史后：只能归属当前轮可证明的增量，不能收取整个历史。
- 同一 turn 多个模型请求并伴随重复累计通知：快照替换、幂等，既不漏计也不重计。
- 缓存输入与推理输出存在包含关系：总量按原生语义分解，负数/溢出不悄悄截断为可信零。
- 取消、网络故障或交付失败发生在已有消费后：已观测快照仍到终态，完整性明确。
- 正常返回但上游没报告 usage：保持 unknown；上下文通知不清空已经取得的 turn 用量。

## 后续分项目

本计划是总体规格的第一阶段。后续依次补齐：模型能力和结构错误/账号配额原生接口；NeuxBot 账号目录与配置贯通；受管安装；全局用量、费用与配额服务；服务商详情、组合模型选择及统计界面；跨仓库集成与真实验收。第一阶段完成不能宣称总体任务完成。

### Task 1: acp-go 用量快照与原生计数修复

**Files:**

- Create: acp-go `pkg/acpmeta/usage.go`、`pkg/acpmeta/usage_test.go`。
- Modify: acp-go `pkg/codex/event_handler.go`、`pkg/codex/session.go`、`pkg/codex/prompt.go` 及相应测试；`pkg/claude/usage.go`、`pkg/claude/events.go`、`pkg/claude/session.go` 及相应测试。
- Inspect/modify only when needed: acp-go `pkg/pi/usage.go`、`pkg/pi/events.go`、`pkg/kimi/usage.go`、`pkg/cursor/context_usage.go` 与对应原生 prompt 终态路径。

**Interfaces:**

- Produces: `acpmeta.TurnUsage`：`InputTokens`, `CacheReadTokens`, `CacheWriteTokens`, `OutputTokens`, `ReasoningTokens` 均为 `*int64`；`Source string` 标识原生来源；`Coverage string` 记录 native_turn、main_loop 或 observed_requests；`Complete bool` 明确覆盖整轮（main_loop不能冒充整轮完整）；`ContextKnown bool` 区分伴随通知是否有真实上下文；`Model string` 仅在原生明确报告时填入。
- Produces: `acpmeta.WithTurnUsage(meta map[string]any, usage TurnUsage) map[string]any` 和 `acpmeta.ReadTurnUsage(meta map[string]any) (TurnUsage, bool)`；命名空间 `acp-go/turn-usage`。
- 标准 `SessionUsageUpdate.Meta` 携带本轮累计快照；没有真实上下文时 Used/Size 为 0 且 ContextKnown=false，宿主不可把它展示为真实上下文零。已有真实上下文通知保留原义。普通 PromptResponse.Usage 继续兼容现有消费者，不能只修元数据而保留错误的最终计数。
- 快照属于当前 Prompt 的 session/generation；重复通知替换，不产生新消费。最终结果的 Meta 同时携带相同来源/完整性。SDK 生成结构不改动。

- [ ] **Step 1: 写失败用例。** Codex 一轮两请求 first input/output=100/10、second=150/20，累计结果须250/30；重复第二次累计仍250/30；恢复历史1000后本轮只计250/30。输入100含read20/write30时非缓存50。累计回退保持部分且不负数。Claude success缺usage保持nil，已有assistant用量后错误/取消保留部分。metadata JSON往返保留nil与0。
- [ ] **Step 2: 运行新用例记录 RED。** 使用 `go test ./pkg/codex ./pkg/claude ./pkg/acpmeta -run '<新增用例正则>' -count=1`，记录真实失败与原因。其余 provider 有具体修复时增加对应定向失败用例。
- [ ] **Step 3: 实现准确计数及快照。** Codex保留可靠session基线并在generation内做字段级差分；缺基线不能推断完整历史。Claude保留字段是否存在，累计最终回执优先于部分消息；最终未知不构造全零。错误路径的已有用量通过已到达的通知保留。对Pi/Kimi/Cursor只修审计可证实的不一致，不造原生未报告能力。
- [ ] **Step 4: gofmt并验证。** 运行涉及包测试：`go test ./pkg/acpmeta ./pkg/codex ./pkg/claude`，额外改动的 provider 包单独加入。校验现有上下文与PromptResponse契约回归。
- [ ] **Step 5: 自审并提交 acp-go。** 提交只含本任务文件；报告列 RED/GREEN、用量未知边界和实际commit。禁止推送、发布或启动真实账号调用。

### Task 2: NeuxBot 保存部分用量与完整性

**Files:**

- Modify: `server/internal/agent/native/contract.go`、`native/connection.go`、`native/connection_test.go`。
- Modify: `server/internal/agent/contract.go`、`session.go` 及相关测试。
- Modify: `server/services/execution/runtime_events.go`、`runtime_events_test.go`、必要的终态集成测试。

**Interfaces:**

- Consumes: Task 1 的 `acpmeta.TurnUsage` 与 `ReadTurnUsage`。
- Produces: native.Usage 保留现有数值字段并添加 `Reported *acpmeta.TurnUsage`，这是一份同次快照的来源/缺失性，不是另一个累加器。
- Produces: `agent.TurnUsageData` 保留现有JSON数值键以兼容历史；增加 `KnownFields []string`、`Complete *bool`、`Source string`、`Coverage string`、`Model string`。KnownFields使用现有蛇形Token键；nil Complete表示旧记录未声明，不能当完整。
- `UsageData.Turn` 与 `UsageData.Context` 分别覆盖更新，只有提供该分量时才覆盖，不以仅含context的新事件清掉turn。

- [ ] **Step 1: 写失败用例。** 用真实SDK pipe模拟部分usage后错误/取消，最终TurnResult保留Token和context；用量事件先turn后context，归档仍有turn；缺失cache分量与已知0往返可区分；最终PromptResponse替换部分快照，不把二者相加。
- [ ] **Step 2: 记录RED。** `go test ./internal/agent/native ./internal/agent ./services/execution -run '<新增用例正则>' -count=1`。
- [ ] **Step 3: 贯通与收敛。** 读取标准usage_update元数据，按ContextKnown解释上下文；同Turn保存最后已观测的累计快照，所有终态分支合并返回。通过现有observer与sink归档。保留既有错误/取消状态，不让“用量存在”变成功。未知JSONL历史不伪造KnownFields。
- [ ] **Step 4: 定向验证。** gofmt；运行native、agent及execution用量/终态相关用例；涉及消息回放时加 `./internal/chat/messageview` 对应用例，不全量。
- [ ] **Step 5: 自审并提交 NeuxBot。** 依赖通过独立Go workspace指向Task 1本地commit联调；绝对路径和workspace不进提交。明确记录发布依赖尚未固定的集成状态。

### Task 3: 跨原生边界用量回归与文档

**Files:**

- Modify: `server/internal/agent/native/connection_test.go`、`server/services/execution/runtime_events_test.go`、`server/services/chat/journal_test.go`（只在边界缺口需要时）。
- Modify: acp-go `pkg/acpmeta/usage.go`、`usage_test.go`，解决Task 1审查保留的JSON浮点整数精度边界。
- Modify: `docs/architecture/runtimes-and-accounts-cn.md`、`server/internal/agent/README.md`。
- Create: `docs/discussions/provider-runtime-parity/usage-foundation-acceptance-cn.md`。

**Interfaces:** Consumes Task 1/2 的快照合同与现有执行终态；不新增生产接口。

- [ ] **Step 1: 补跨层缺口用例。** 同一invocation部分更新→最终更新→重复终态只存一次；失败/取消已有消费仍可回放；fork继承记录不算新调用的约束为后续聚合保留身份。只在现有测试未覆盖时补测试。JSON默认解码将2^53+1舍入为2^53，先以真实往返取得RED，再使float64路径拒绝大于2^53-1的值；精确int64/json.Number路径继续按其真实范围校验。
- [ ] **Step 2: 执行最窄回归并记录输出。** 使用具体测试正则运行涉及的native/execution/chat包；不重复运行已经验证且未变化的包。
- [ ] **Step 3: 更新已实现架构与验收记录。** 明确新增元数据、旧记录未知、支持provider矩阵、实际基线和修复commit，真实平台/模型未验证状态单列。
- [ ] **Step 4: 检查文档与差异。** `node scripts/check-docs.mjs` 和 `git diff --check`；修复本次问题。
- [ ] **Step 5: 提交并整体审查本阶段。** 审查两仓库差异和相关测试证据，再进入总体规格下一分项目。不得把本阶段完成写成整体功能已完成。
