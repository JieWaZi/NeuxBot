# 伙伴记忆与技能学习实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将记忆范围收敛到伙伴，交付前端双分组、核心记忆注入、一次性会话自动复盘、对话内技能学习与智能体设置。

**Architecture:** `memory.db` 继续拥有伙伴记忆、索引、历史和复盘处理边界。Execution 经 ThreadLifecycle 向合格的普通伙伴聊天交付核心快照。后台从 Chat 已提交事实提取增量，使用 ProviderRuntime 上的临时无工具会话生成候选，由 Go Core 提交记忆和可选技能建议。按技能学习模式在聊天确认后或自动生成正文，复用现有技能链路新建和修订。

**Tech Stack:** Go、SQLite FTS5/sqlite-vec、Chat JSONL、acp-go、生成 OpenAPI Client、React、TanStack Query、Vitest。

**Spec:** [伙伴记忆功能规格](../specs/2026-11-18-partner-memory-spec.md) · [伙伴记忆架构方案](../specs/2026-11-18-partner-memory-design.md)。以下接口为实施目标。

## Global Constraints

- 记忆、索引和命令回执归属伙伴，按新数据结构交付。
- 核心记忆和普通记忆共用一条记录。核心快照按最终序列化的 UTF-8 字节数限制在 **8 KiB**。
- 自动记忆开关和技能学习三档按伙伴独立配置，默认关闭。共用 Chat 进度，累计 **20 个合格成功轮次**，或至少 **3 个合格成功轮次且空闲 15 分钟** 后触发。单批最多 **20 轮、24 KiB 证据**。
- 仅有自动记忆资格时只使用脱敏用户原文及有界的相关已有记忆。技能学习可加入经白名单筛选和脱敏的最终助手正文及工具步骤。两项功能都使用单轮、无工具且无会话正文持久化的生成，并保留规范用量元数据。先验证适配器的临时模式，再开放对应 CLI。
- 关键路径保持有界本地读取，遵守[执行关键路径约定](../../development/backend-cn.md#82-执行关键路径禁止诊断探测)。后台复用原账号及进程租约。事件复用现有全局连接。
- schema、文档和内嵌技能通过既有生成命令更新。仅执行定向测试。真实应用与浏览器须经用户明确同意后操作。按工作区约定保留无关改动。

## Review Focus

| 输入或故障                                 | 验证任务 |
| ------------------------------------------ | -------- |
| 伙伴尝试读取另一伙伴的记忆       | 1        |
| 并发设核心或双组刷新顺序相反               | 2        |
| 一次原生提交失败，旧绑定曾收到其他核心版本 | 3        |
| 生成正常返回但退出、取消或重启留下文件     | 4        |
| 20 轮共用触发上界，文本预算只剩一轮尾批    | 5        |
| 禁用或切换复盘 CLI 配置后，原代次返回 JSON  | 5、6、7  |
| 采纳事务完成后响应丢失，用户再次提交       | 7        |

## 文件与职责

| 位置                                                                                                                                                                                  | 改动职责                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `server/internal/memory/{store,service,memory,agent,keyword,vecstore,scope_resolver}.go`                                                                                              | 伙伴归属、授权、既有读写和索引。核心快照与复盘分别收在同包职责文件。                          |
| `server/apis/memory`、`server/apis/{projects,wakers}/delete_*.go`                                                                                                                     | 伙伴记忆契约、核心切换、复盘设置和归属删除。                                                      |
| `server/apis/chats/{message_input,create_chat,send_message}.go`、`server/services/execution/{intent,work,native_turn,lifecycle,service}.go`、`server/internal/chat/eventlog/types.go` | 可信接纳来源、冻结资格、终态提示、动态核心记忆。                                                  |
| `server/internal/agent`、`server/internal/chat/conversation`                                                                                                                          | 原生绑定交付、独立调用存储策略、用途归属和用量证据。Claude 通过独立 acp-go 依赖交付原生临时能力。 |
| `server/services/chat/{journal,live_projection,recovery,chat}.go`、`server/global/config.go`                                                                                          | 复盘事实读取、恢复对账、删除边界和生命周期装配。                                                  |
| `packages/frontend/src/modules/memory`、`packages/frontend/src/shared/i18n/{workspace-resources,base-en}.ts`                                                                          | 伙伴单归属、两组、切换动作、自动记忆设置与错误反馈。                                              |
| `server/internal/agent/capability/catalog.go`、`server/internal/agent/cli`、`skills/bot/references/memory.md`                                                                         | 伙伴命令参数、回执、操作说明与内嵌技能。                                                          |

## Task 1 伙伴归属与存储

落实 PM-01，并交付可用的伙伴单范围记忆页。

**Files**

`server/internal/memory/{store,service,memory,agent,keyword,vecstore,scope_resolver}.go`，`server/apis/memory/{list_memories,list_timeline,mutate_memory,types}.go`，两个归属删除用例，`server/global/config.go`，能力目录、内置记忆指南、botcli README 和对应测试。前端修改 `waker-memory-page.tsx`、两个列表和表单组件 `memory-list.tsx`、`memory-form-dialog.tsx`、`api/memory-queries.ts`、`model/types.ts`。

**Interfaces**

- `memory.Context{RunID, ChatID, PartnerID string}`。作用域由 `PartnerID` 唯一确定。将现有服务签名收口成 `Write(ctx context.Context, mc Context, content string) (WriteResult, error)` 和 `Search(ctx context.Context, mc Context, query string, topK int) (SearchResult, error)`。修改和遗忘继续用现有条目 ID。
- 管理 API 沿用 `/memory/wakers/{wakerId}/memories`、`/{memoryId}` 和伙伴 `/timeline`。删除 `scope`、`scopeId` 和 `projectId` 的对外属性。统一服务端、前端和生成契约的伙伴归属。
- CLI 保留 `memory.entries.search/get/create/update/forget` 命令名，去除范围输入。新回执键使用当前授权的 Chat、伙伴、操作和请求标识。

- [ ] 在 `service_test.go`、`agent_test.go` 和 `memory_api_test.go` 验证新数据的记忆 CRUD、变更历史、跨伙伴访问拒绝及命令回执重放。
- [ ] 在 `waker-memory-page.test.tsx` 编写新建直接打开、页面不请求项目列表以及创建／修改／遗忘使用伙伴端点的用例。运行本任务 Go 和前端命令，确认旧行为触发新断言失败。
- [ ] 实现伙伴范围的存储、索引、API 和授权。删除项目退出记忆清理，删除伙伴保留记忆清理，复用同一归属校验。
- [ ] 运行 `pnpm api:generate`、`pnpm skills:generate`，随后运行 `pnpm api:check`、`pnpm skills:check`。使用新生成契约完成前端改动，按发布行为更新 [CONTEXT](../../../CONTEXT.md) 和[记忆架构](../../architecture/resources-and-memory-cn.md#记忆)。
- [ ] 定向运行 `cd server && go test ./internal/memory ./apis/memory ./internal/agent/capability ./internal/agent/cli ./apis/projects ./apis/wakers -run 'Test.*(Memory|DeleteProject|DeleteWaker)' -count=1`。运行 `pnpm --dir packages/frontend test src/modules/memory/pages/waker-memory-page.test.tsx src/modules/memory/components/memory-form-dialog.test.tsx`。预期权限、历史及记忆 CRUD 通过。

## Task 2 核心分类、预算和前端分组

落实 PM-02。管理端交付分类、移动和预算反馈，供 Task 3 消费。

**Files**

新增 `server/internal/memory/core_memory.go` 与 `core_memory_test.go`，新增 `server/apis/memory/set_memory_core.go`。修改现有 `store.go`、`service.go`、`memory.go`、`list_memories.go`、`types.go`、`router.go`。前端修改 `waker-memory-page.tsx`、`memory-list.tsx`、相关测试、查询和词典。

**Interfaces**

- `(*memory.Service).SetCore(ctx context.Context, partnerID, memoryID string, core bool) error`。同值调用幂等。记忆资源带 `core: boolean`，列表接收可选 `core` 查询值，先过滤后分页，保留共同的查询与时间范围。切换使用 `PUT /memory/wakers/{wakerId}/memories/{memoryId}/core` 和 `{core: boolean}`，区分缺字段与显式 `false`。
- `(*memory.Service).CoreSnapshot(ctx context.Context, partnerID string) (memory.CoreSnapshot, error)`。结果包含 `Revision uint64` 和确定性序列化后的 `JSON []byte`。内容为 `{"items":[{"id":"...","text":"..."}]}`，按 ID 稳定排序，以 `len(JSON) <= 8192` 做预算。版本元数据独立于内容载荷。

- [ ] 编写 `TestCoreMemoryBudgetUsesSerializedUTF8`、`TestConcurrentCoreSelectionHonorsBudget`、`TestCoreRevisionAdvancesOnlyForSnapshotChanges` 和 `TestMemoryAPIListsCoreBeforePagination`。用中文、转义、多字节和恰好 8192 字节验证预算，超限时断言正文、分类和版本均保持原值。
- [ ] 在页面和列表测试编写「核心在上，普通在下」「两个分组查询使用相同搜索和日期」「目标组刷新先返回也不出现重复 ID」「键盘移动与分组焦点回退」「预算拒绝保持原行」用例。运行本任务的定向命令，确认旧实现缺少行为。
- [ ] 新建条目默认为 `core=false`。写入、切换和删除在同一事务中校验预算及推进快照版本。前端复用现有列表行与共享控件，分别分页、显示骨架和空状态。切换成功后取消该伙伴两组过期读取，从源分组缓存移除该 ID，再精确刷新两组查询。渲染按 ID 去重。目标条目可见时恢复到其操作入口，其他情况聚焦目标组标题。
- [ ] 重新生成并校验 API。运行 `cd server && go test ./internal/memory ./apis/memory -run 'Test.*(Core|PartnerMemory)' -count=1` 与 `pnpm --dir packages/frontend test src/modules/memory/pages/waker-memory-page.test.tsx src/modules/memory/components/memory-list.test.tsx src/shared/i18n/translations.test.ts`。预期预算、分页、键盘及词典通过。

## Task 3 核心快照进入正常聊天

落实 PM-03，也给 Task 5 提供可恢复的工作台入口来源。

**Files**

`server/apis/chats/{create_chat,send_message,message_input}.go`，`server/services/execution/{intent,work,native_turn,service}.go`，`server/internal/chat/eventlog/types.go`，`server/internal/agent/{thread_lifecycle,turn_lifecycle}.go` 及对应集成测试。装配位于 `server/global/config.go`。

**Interfaces**

- 在接纳入口用内部 `WorkOptions.EntryPoint` 传递工作台来源，保存到 `ExecutionCreatedData.EntryPoint`。只从明确拥有普通聊天的入口赋值，非工作台、归属不明和独立运行都使用原路径。按入口与已冻结的 Chat 归属共同判断资格。
- Execution 的本地快照端口通过 Task 2 的 `CoreSnapshot` 获取数据。给 `agent.TurnRequest` 加 `CoreMemory *agent.CoreMemoryContext`，含版本号与 JSON 字节。ThreadLifecycle 在解析原生绑定后决定是否附入。交付记录按 Chat 和具体原生绑定持有，Core 重启后未知则重新发送。

- [ ] 在 `native_turn_test.go` 扩展真实协议子进程记录，编写 `TestCoreMemoryDeliveryFollowsNativeBinding`、`TestCoreMemoryDeliveryRetriesAfterUnknownOutcome`、`TestCoreMemoryInjectionUsesTrustedWorkbenchOrigin`。依次断言首轮、同版本热续、版本变化、空快照、替换／分叉、取消或失败后再次发送。覆盖有项目和临时目录的普通聊天，以及也使用 `user` Actor 的 IM。
- [ ] 用原路径运行测试并看见缺少快照或入口区分的失败。实现本地读取与本轮数据注入。只有真实原生成功终态提交后确认该绑定的版本。测试断言公开消息保留原始用户正文，核心数据附在模型输入，改变核心不修改静态配置摘要。复用或交接原生绑定时核对其既有归属，隔离不同入口。
- [ ] 运行 `cd server && go test ./services/execution ./internal/agent -run 'Test.*(CoreMemory|ThreadTurn.*Resume|ThreadTurn.*Replace|ThreadFork)' -count=1`，并定向运行 `./apis/chats` 和改及的 IM 入口用例。用协议记录确认发送、续聊、恢复、分叉和支持的 steering 仍走既有调用，无额外账号或模型诊断。

## Task 4 临时生成和用量证据

落实 PM-05。完成提交前可测试的临时生成通道，开放功能还需真实 CLI 验收。

**Files**

`server/internal/agent/{detached_task,thread_lifecycle,turn_lifecycle,provider_runtime,agent_client,native_outcomes}.go`，`server/internal/chat/conversation/{native_stream,usage_scan}.go`。复用现有 `thread_lifecycle_test.go` 和 `usage_scan_test.go`。原生 Claude 参数及协议测试在独立 acp-go 的 `pkg/claude`。仅在验证原生能力后调整 `server/go.mod` 和 `server/go.sum`。

**Interfaces**

- `agent.TextRequest` 增加 `Ephemeral bool` 和 `UsageWakerID string`。调用仍用 `(*ThreadLifecycle).GenerateText(ctx context.Context, request TextRequest) (TextResult, error)`。开关为真时要求无工具和原生历史不落盘。现有调用沿用原默认路径。
- `(*ThreadLifecycle).SupportsEphemeralText(provider string) bool` 从实现已核验的适配器策略读取本地能力。复盘使用 `UsageSource: "memory_review"`，只允许规范终态的无正文用量元数据进入统计链路。

- [ ] 以唯一提示与响应作为哨兵，编写 `TestEphemeralTextLeavesNoConversationBody`、`TestEphemeralTextRecordsUsageWithoutDetachedHistory`、`TestEphemeralTextCleansAfterFailureOrCancellation`、`TestEphemeralTextRecoveryOwnsOnlyItsScratch`。扫描测试的共享原生文件、Chat 目录与临时路径，并核对 `ReadUsageTerminals` 的实际结果。先运行以确认现有独立调用仍走耐久原生写入。
- [ ] 复用租约和原生客户端，用临时存储策略隔离接纳、过程、最终候选与错误正文。统计终态只载白名单元数据，按原生调用去重。Claude 需要由适配器传递原生不持久化参数及临时工作目录，拒绝已存在的会话身份或额外挂载。新版 acp-go 未具备此行为时，先在该项目实现并验证，使用固定依赖版本。
- [ ] 正常收尾、失败、取消均从同一 Runtime 所有者回收。启动回收只消费有可靠归属标记、无活动租约的复盘临时目录和状态。清理错误保留可重试记录，诊断只公开稳定错误。
- [ ] 运行 `cd server && go test ./internal/agent ./internal/chat/conversation -run 'Test.*(EphemeralText|GenerateText|UsageTerminal)' -count=1`，并定向验证现有知识及工作流独立生成。真实 CLI 验收用虚构哨兵，比较调用前后的 CLI 历史、公开 Chat、共享原生分片与受管临时目录，覆盖成功、失败、取消、重启及用量查询。四种路径无正文遗留后才允许 Task 6 启用。

## Task 5 增量复盘、提交与恢复

落实 PM-04 的共用后台流程，将 Chat 终态来源、Task 4 临时调用和 Task 1 记忆写入接通。支持附带可空的短技能建议输出，Task 7 接入技能取证及确认。

**Files**

新增 `server/internal/memory/review.go`、`review_store.go` 及对应测试，职责分别为批次编排和 SQLite 状态。修改 `server/services/chat/{journal,live_projection,recovery,chat}.go`、`server/internal/chat/eventlog/types.go`、`server/services/execution/{native_turn,lifecycle,service}.go`、`server/apis/wakers/delete_waker.go`、`server/global/config.go`。

**Interfaces**

- `memory.ReviewTerminal{ChatID string; Sequence int64}` 与 `(*memory.Reviewer).ObserveTerminal(event ReviewTerminal)`。Chat 提交后合并唤醒。读取端口 `ReadReviewTurns(ctx context.Context, chatID string, after, through int64, limit int) ([]memory.ReviewTurn, error)` 按权威顺序返回合格终态、两项资格及用户原文，技能证据按资格加载。协调器在当前配置代次内触发，恢复使用启动补齐路径。
- 伙伴级 `ReviewSettings` 保存记忆开关、技能学习三档及各自代次。全局仅保存一套复盘 CLI 来源和专用配置。轮次接纳事实保存各功能的资格及代次和有效 CLI 配置版本。CLI 切换后过滤旧配置接纳的轮次，同一配置的暂时故障可续跑。按 Chat 使用一套游标、终态上界和批次恢复。领取、外发和提交均核对作用范围、功能代次及有效 CLI 配置版本。
- 复盘模型返回有引用的记忆新增、更新、遗忘列表，以及至多一条新建或修订技能的建议。批次身份为伙伴、Chat、序号区间与冻结的 CLI 配置和功能代次。Chat 删除和伙伴删除复用短保护边界，挡住迟到提交。

- [ ] 编写 `TestMemoryReviewThresholdAndIdle`、`TestMemoryReviewSplitsTriggeredRangeWithoutStrandingTail` 和 `TestMemoryReviewRecoversAfterCommitBeforeWakeup`。用可控时钟验证 19/20 轮、2/3 轮、15 分钟边界、活动轮次延迟、24 KiB 导致只剩一轮的尾批、首次运行没有游标时的重启恢复。另测不同 Chat 计数独立。
- [ ] 编写 `TestMemoryReviewDoesNotSendDerivedContent`、`TestMemoryReviewEpochRejectsLateResults`、`TestMemoryReviewCommitsCursorWithEntries` 和 `TestMemoryReviewForgetSuppressesSameContent`。对截取数据注入不同哨兵，分别断言两项资格决定发送字段，只有自动记忆时仅发送脱敏用户原文、标识和有界记忆快照。注入写入故障、向量变化、重复批次、删除 Chat／伙伴、切换来源和更换实际调用配置，核对外发、数据库与游标结果。先运行并检查失败原因。
- [ ] 实现接纳代次、成功终态资格和逐 Chat 游标。投影补齐只读游标后的权威事实，记录触发时的终态上界，按有界批处理到尾批。单后台作业有序领取，前台占用租约时退避。
- [ ] 接入 Task 4 的临时生成。先检索有界相关快照，校验新增、更新、遗忘的证据和目标。增加手工条目、已被接管、并发变化、核心预算、同批先遗忘后新增核心及删除索引测试。沿用既有向量配置，在同库事务提交合法变更、来源、历史、索引、核心版本及进度。空候选提交进度，判定或索引失败整批重试。短临界区重验仍有效，过期或删除丢弃结果。
- [ ] 运行 `cd server && go test ./internal/memory ./services/chat ./services/execution ./apis/wakers -run 'Test.*(MemoryReview|CoreMemory|DeleteChat|DeleteWaker)' -count=1`。预期失败重试不影响前台 Chat，投影未更新时仍能从权威日志补齐。

## Task 6 智能体设置与自动记忆整体验收

交付 PM-04、PM-06 的共用 CLI 配置、可用状态、重试和后台结果可见性，完成记忆链路联调。技能学习三档随 Task 7 接入。

**Files**

新增 `server/apis/memory/{get_review_settings,update_review_settings,retry_memory_review}.go`，对应路由和测试。修改 `server/apis/memory/types.go`、`server/constants/memory_timeline_source.go`、设置模块的智能体页、记忆页及 `api/memory-queries.ts`、两份词典。现有语义检索表单从智能体设置打开。复用现有事件发布和 `useQueryEvents`。

**Interfaces**

- 伙伴复盘设置接口读取并更新记忆开关和技能学习三档，返回实际 CLI、能力和状态。全局设置读写一套来源和专用 CLI、账号、模型。重试接口只处理当前配置范围内的可重试批次。
- 使用既有全局资源事件注册 `partner.memory` 主题，以伙伴 ID 订阅。提交后通知该伙伴的列表、时间线或设置状态变更，按实际变化精确失效。

- [ ] 编写 `TestMemoryReviewSettingsRejectUnverifiedRuntime`、`TestMemoryReviewSettingsRetryUsesCurrentGeneration` 和前端用例，覆盖默认关闭、两种 CLI 来源、按伙伴解析、全局专用配置、配置变更仅影响对应伙伴、外发说明、不可用、持续失败和重试。添加事件用例，断言只更新当前伙伴的对应查询且不因一条自动记忆再次请求项目。
- [ ] 运行定向测试确认新行为缺失。实现查询与设置、理由和重试动作。在智能体设置顶层列表新增「记忆」组，接入合格运行时的能力开关和后端事件。在现有来源位置显示自动复盘来源。现有查询失败保留列表与可执行的本地操作。
- [ ] 重新生成并校验 API 和技能。运行 `cd server && go test ./apis/memory ./internal/memory -run 'Test.*(PartnerMemory|Core|Review)' -count=1`。运行 `pnpm --dir packages/frontend test src/modules/memory/pages/waker-memory-page.test.tsx src/modules/memory/components/memory-list.test.tsx src/modules/memory/components/memory-settings-dialog.test.tsx src/shared/i18n/translations.test.ts`，对变更文件运行 ESLint 与格式检查。
- [ ] 更新实现后的 [CONTEXT](../../../CONTEXT.md)、[资源与记忆架构](../../architecture/resources-and-memory-cn.md#记忆) 及 botcli 指南，运行 `pnpm docs:check`。按规格表记录真实 CLI 的落盘检查和 Web／桌面的浅暗主题、窄容器及键盘结果。实际运行环境尚未验收的项单独记录。

## Task 7 技能学习、自动安装与版本修订

落实 SL-01、SL-02 和 PM-06 的三档设置。详细流程见[技能学习设计](../specs/2026-11-18-partner-memory-design.md#技能学习)。

- [ ] 扩展 Task 5 同批复盘的证据投影和结构化输出，区分技能新建或修订。验证只开一项、项目聊天、同类建议去重、已有技能覆盖、外发边界和重启恢复。
- [ ] 从来源聊天读取建议，复用共享聊天卡片。测试询问档确认前无正文生成，自动档自动派发，关闭或切换模式时撤销未发布任务，失败重试和重复操作保持幂等。
- [ ] 复用 Task 4 生成技能正文，以及技能库的文件准备、期望版本保护、版本发布和安装关系。测试首次安装、ZIP 和学习来源修订、仅来源伙伴使用时自动修订、共享技能提示影响并确认、共享安装关系并发变化、保留原资源、回滚、来源删除及恢复回执。
- [ ] 生成契约并运行相关前后端测试。真实 CLI 验收用纠正、有效技巧和已有技能过时等样例，核对模型输入、输出质量、落盘、用量及后续会话加载。运行 `pnpm docs:check`。
