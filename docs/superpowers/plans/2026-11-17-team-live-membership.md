# 团队动态成员与负责人 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 仅在方案获确认并收到实施指令后执行；本计划不授权自动提交、推送或操作运行中的应用。

**Goal:** 去除会话成员及负责人资格快照，让全部已有团队对话使用当前团队关系，并保留原执行历史。

**Architecture:** Channel 拥有当前成员和负责人；线程只保存工作范围、成员 Chat 映射及明确配置覆盖。动态团队上下文经本轮输入交付，负责人历史通过执行归属引用回到原 Chat 日志，当前角色与历史作者分别管理。

**Tech Stack:** Go、GORM/SQLite、既有 Chat JSONL/Execution/capability、React/TanStack Query、生成 OpenAPI Client。

**Spec:** [团队动态成员与负责人改造方案](../specs/2026-11-17-team-live-membership-design.md)。状态为待评审；以下接口为拟定目标，不是当前能力。

## Global Constraints

- 团队成员及负责人只有一份当前事实；不按团队编辑向全部历史线程复制名单。
- 保留 NeuxBot 的负责人协调、成员执行与报告模式，不移植 OpenBot 多人路由。
- 团队目前最多 5 人；Context 复用既有名单裁剪常量。
- 变更不扩大线程原授权目录，不移动既有 cwd，不改写已接纳执行配置及历史作者。
- 所有生成文件通过现有命令更新；SQL 留在 Repository，默认单表与索引子查询。
- 不新增账号、额度、登录状态或模型目录前置探测；不新增轮询与第二条 WS。
- 保留无关工作区改动；只运行定向检查，未经明确授权不开启应用或浏览器。

## Review Focus

- 移除后立即重新加入：旧 grant、Attempt 和排队输入不能恢复权限，Task 2/3 验证。
- 成员准备发生在团队变更/删除旁边：不能重复创建 Chat、误删 shared 或扩大目录，Task 2 验证。
- 换负责人时新负责人仍在执行成员任务：旧报告仍有效，协调不并发，Task 4 验证。
- 负责人变化与分页/阅读/报告投递交错：历史不能换作者、重复或丢页，Task 4/5 验证。
- 历史表与 JSONL 不一致或转换中断：不能跳过损坏历史后开放新接纳，Task 6 验证。

## 文件与职责

| 位置                                                                                                                  | 改动职责                                                               |
| --------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `server/repositories/models/channel.go`、`channel_thread.go`、`channel_delivery.go`、`channel_task.go`                | 当前版本/代次、Chat 映射/覆盖、负责人执行与投递归属、公开引用/阅读位置 |
| `server/repositories/controllers/channel_controller.go`、`channel_thread_controller.go`、`channel_task_controller.go` | 资格、CAS、映射预留、历史来源、定向取消与转换                          |
| `server/services/channel/thread_context.go`（新增）                                                                   | 共享资格读取、成员 Chat 准备与动态上下文；不另起 Manager               |
| `server/services/channel/membership.go`（新增）                                                                       | 团队保存/伙伴退出/修复共用的关系差异与工作撤销机制                     |
| `server/services/channel` 原 thread/task/control/notification/lead_input/agent/team_md/recovery/operation 文件        | 将原编排入口接到当前资格及归属机制                                     |
| `server/services/execution/chat_group.go`、`intent.go`                                                                | 首发最小准备及增量成员准备，复用 Dormant Chat                          |
| `server/services/chat/chat.go`、`internal/chatstorage/layout.go`                                                      | 精确成员准备与回收，保留原目录归属                                     |
| `server/apis/channels`                                                                                                | 当前成员详情、版本输入、显式成员 Chat 准备入口和配置覆盖               |
| 阅读/看板 Repository 与 `server/services/im`                                                                          | 当前角色与历史公开来源拆开，保持原 Thread 结果归属                     |
| `packages/frontend/src/modules/team`、`modules/chat/pages/team-chat-*`、`shared/api/use-query-events*`                | 当前名单统一投影、角色切换与事件精确失效                               |
| `skills/bot/references/team-*.md`、`server/internal/agent/capability/catalog.go`                                      | 当前查询与角色授权说明                                                 |

新增文件围绕完整共享机制组织，不按命令或单个辅助函数拆文件；其余规则回到拥有该职责的原文件。

## Task 1: 当前资格与存储契约

**Files:** 上表 models、三个 channel Controller 与对应已有测试；新增模型仍归 `channel_thread.go`，不创建逐类型文件。

**Interfaces:**

- `ChannelController.GetMember(ctx context.Context, channelID, wakerID string) (models.ChannelMember, error)`：唯一当前资格查询。
- `ChannelController.UpdateAtRevision(ctx context.Context, channel models.Channel, members []models.ChannelMember, expectedRevision int64) error`：同事务完整更新，版本不符返回生成的团队配置冲突错误。
- `ChannelThreadController.GetContext(ctx context.Context, threadID, wakerID string) (models.ChannelThreadContext, bool, error)`，以及 `ListContexts(ctx context.Context, threadID string) ([]models.ChannelThreadContext, error)`：仅 Chat 映射。
- `ReserveContext(ctx context.Context, value models.ChannelThreadContext) (models.ChannelThreadContext, error)` 与 `MarkContextReady(ctx context.Context, threadID, wakerID, provisioningID string, readyAt int64) error`：唯一准备身份。
- 配置覆盖读写复用原成员执行配置 Controller 职责，改用 `ChannelThreadExecutionConfig`；覆盖缺失必须可与显式空模型、Fast nil 区分。
- Task 与 Attempt 增加 AssigneeGeneration，LeadTurn 增加 MemberGeneration/LeadGeneration；排队输入增加 TargetWakerID，Attempt 增加 ReportChatID；恢复 grant 从已接纳记录捕获代次，不重新套用当前值，对应职责见方案第 3.3 节。

- [ ] 在 `channel_controller_test.go` 加 `TestChannelMembershipIsCurrentAcrossExistingThreads`、`TestChannelUpdateRejectsStaleRevision`；断言旧 context 不授予资格、新成员无 context 仍获资格、旧版本保存不覆盖新名单；完整保存保留持续成员 Generation，重加成员和换回负责人使用新代次。
- [ ] 在 `channel_thread_controller_test.go` 加 `TestThreadContextReservationIsUnique` 和 `TestThreadExecutionOverridesPreserveEmptyValues`；真实临时 SQLite 验证唯一约束、事务回滚与 Fast 三态。
- [ ] 运行上述新测试并确认断言失败；按方案第 3 节增加目标模型和 Repository，不把投影字段再写回当前资格。此步保留旧类型/字段以便未迁移调用仍可编译，Task 6 再移除；过渡提交不作为可发布能力。
- [ ] 对修改 Go 文件执行 `gofmt -w`；运行 `cd server && go test ./repositories/controllers -run 'Test(ChannelMembershipIsCurrentAcrossExistingThreads|ChannelUpdateRejectsStaleRevision|ThreadContextReservationIsUnique|ThreadExecutionOverridesPreserveEmptyValues)$' -count=1`，预期全部通过。
- [ ] 定向检查新查询的索引计划，确保当前成员按主键查询，context 按联合主键/ChatID 查询；记录来源，不扩大到全部 SQL 基准。

## Task 2: 按需准备与成员退出

**Files:** `channel/thread_context.go`、`membership.go`、`thread.go`、`task.go`、`control.go`、`operation.go`、`waker_detach.go`、`recovery.go`、`service.go`；Execution/Chat/Storage 准备文件及其测试。

**Interfaces:**

- `(*service).ensureThreadContext(ctx context.Context, threadID, wakerID string) (models.ChannelThreadContext, error)`：当前成员校验、准备预留、复用与恢复共用入口。
- Execution 新增 `PrepareThreadMember(ctx context.Context, intent PrepareThreadMemberIntent) (models.Chat, error)`；intent 含 ThreadID、ProvisioningID、ChatID、WakerID、从线程 ScopeSourceChatID 明确读取的已批准目录及 ProjectSourceSnapshot 和已解析配置，不接收“按当前项目重算授权”的开关。
- `(*service).ApplyMembershipChange(ctx context.Context, previous models.Channel, next models.Channel, members []models.ChannelMember, expectedRevision int64) error`：只拥有关系更新和取消意图的共享原子提交，不接管 HTTP 表单、名称查重或响应。
- 所有 Task/`@`/直接成员 Chat 输入以 Task 1 当前资格验证；历史查询用 context，不混用两个接口。

- [ ] 新增 `thread_context_test.go`：`TestExistingThreadCanDispatchToNewMember`、`TestConcurrentContextPreparationReusesChat`、`TestContextPreparationKeepsOriginalScope`、`TestContextPreparationRecoveryDoesNotDeleteSharedFiles`；覆盖原授权来源伙伴退出/删除后，来源 Chat 仍由线程拥有且可核对。
- [ ] 扩展 `control_test.go`/`waker_detach_test.go`：`TestMemberRemovalCancelsOnlyOpenAssignedWork`、`TestLateReportAfterRemovalCannotCompleteTask`、`TestRejoinDoesNotReviveGrantOrTask`、`TestRemovedMemberChatRejectsNewInput`、`TestDeletingWakerDoesNotDeleteTeamHistory`。覆盖退出时的 pending/in_progress/in_review、其他成员未受影响和剩余 0/1 人团队的保留/补人恢复。
- [ ] 先运行新测试确认失败；实现首发最小准备、增量成员准备、定向取消与启动恢复。变更提交和实际取消分开，失败保留持久取消意图供重启重试；grant 必须验证持久 Generation，不能只靠进程内 Revoke。
- [ ] 更新新增结构体全部字段注释；定向运行 `cd server && go test ./services/channel -run 'Test(ExistingThreadCanDispatchToNewMember|ConcurrentContextPreparationReusesChat|ContextPreparation.*|MemberRemoval.*|LateReportAfterRemoval.*|Rejoin.*|RemovedMemberChat.*|DeletingWakerDoesNotDeleteTeamHistory)$' -count=1`。
- [ ] 分别运行 `go test ./services/execution -run 'TestPrepare(ThreadMember|ChatGroup)' -count=1` 与 `go test ./services/chat ./internal/chatstorage ./internal/workspace -run 'Test.*(ThreadMember|TeamProvision|TeamMember)' -count=1`，预期只回收明确属于本次准备的内容。

## Task 3: 动态上下文与 CLI 当前查询

**Files:** `channel/team_md.go`、`thread_context.go`、`agent.go`、`capability.go`、`lead_input.go`、`task.go`、`notification.go`、`control.go`；`capability/catalog.go`、系统技能源及其定向测试。

**Interfaces:**

- `(*service).buildTeamContext(ctx context.Context, threadID, selfWakerID, executionRole string) (workinput.Instruction, error)`：读取当前关系和资料，输出一次本轮动态段。
- 稳定 TEAM 指令不含具体成员、负责人 ID、当前角色或路径；各执行入口把动态段加入 `WorkInput.ContextInstructions`。
- `team.members.list` 保留 `items`，增加 `revision`，同时授予负责人和成员只读能力；有效性仍检查真实当前执行、撤销状态和 Attempt。

- [ ] 将 `TestTeamDispatchReadsCurrentProfilesWithoutChangingAdmittedMembership` 改为动态成员回归，补 `TestTeamContextRefreshDoesNotChangeStableInstructions`、`TestEveryTeamEntryReceivesCurrentContext`、`TestMemberCanReadCurrentRosterWithoutMutationRights`。
- [ ] 使用参数化用例覆盖首发、负责人续聊、通知、成员派发、补充及支持的 steering；断言当前名单、执行角色、裁剪与一次注入，原始用户文本未被改写。
- [ ] 确认新断言失败后实施。删掉“不许核实名单”和缺失才查询的指引；按需查询是合法业务读取，不触发账号诊断。
- [ ] 定向执行 `cd server && go test ./services/channel -run 'Test(Team.*|EveryTeamEntry.*|MemberCanReadCurrentRoster.*)$' -count=1`；使用既有真实协议子进程替身加连续两轮测试，断言成员变化不改变 DeveloperInstructions、稳定工具身份或原生 Session，仅第二轮业务上下文变化。
- [ ] 运行 `pnpm skills:generate`、`pnpm skills:check` 和修改到的 capability/CLI 定向测试；生成产物只能由命令更新。

## Task 4: 负责人交接与公开来源

**Files:** `channel/lead_input.go`、`member_report.go`、`notification.go`、`timeline.go`、`capability.go`、`agent.go`、`recovery.go`；model delivery/lead turn 与 thread/task Controller；阅读/看板、IM 来源与自动运行定向测试。

**Interfaces:**

- `(*service).currentLeadContext(ctx context.Context, threadID string) (models.Channel, models.ChannelThreadContext, error)`：派发边界按当前负责人解析，不从 thread 冻结字段读取。
- `RecordLeadTurn(ctx context.Context, turn models.ChannelLeadTurn) error`：仅在真实接纳提交边界记录幂等归属；同一 ExecutionID 不能被另一伙伴占用。
- `AppendTimelineEntry(ctx context.Context, entry models.ChannelTimelineEntry) (models.ChannelTimelineEntry, error)`：与既有 ChannelEvent 来源引用同事务幂等提交，线程内分配 Position，不存正文。
- `ListTimelineEntries(ctx context.Context, threadID string, throughPosition, beforePosition int64, limit int) ([]models.ChannelTimelineEntry, error)`：按 Position keyset 有界读取，再按来源回读原日志，禁止比较不同 Chat 的 CreatedSequence。
- 线程公开阅读使用 `ChannelThreadReadCursor`，沿用现有 ReaderID、UnreadGeneration、HoldUnread 规则；SourceSequence 与公开 Position 不混用。
- 一份协调席位由已有持久 delivering 输入/通知与实际活跃 LeadTurn 推导，不复制 Execution 生命周期；报告首次投影目标 ReportChatID 保持不变。

- [ ] 新增 `lead_handoff_test.go`：`TestLeadChangeAppliesToExistingThread`、`TestLeadHandoffWaitsForAcceptedTurn`、`TestPromotedMemberMayFinishExistingAttemptReport`、`TestPromotedMemberPendingTaskDoesNotStallHandoff`、`TestExplicitMentionKeepsOriginalRecipient`、`TestRestartDuringHandoffDoesNotRedeliverBatch`；已持久 pending 任务保留原成员工作身份，新建给负责人的任务仍拒绝。明确覆盖晋升者 pending 任务依赖 in_review 上游时，空闲 Chat 仍能接纳用户协调输入；重启后旧 grant 仍用原持久代次验证。
- [ ] 在 `timeline_test.go` 增加 `TestTimelineKeepsAuthorsAcrossLeadChanges`、`TestTimelinePaginationAcrossLeadChanges`、`TestReportProjectedOnceAcrossLeadChanges`、`TestTimelineProjectionRecoversAfterSourceCommit`；断言来源 Chat 内部序号交错时公开 Position 仍稳定、分页旧区间、直达结果、私有过程隔离及新旧来源去重。
- [ ] 先运行新测试确认失败；实施当前负责人解析、接纳归属、旧 grant 降权、目标明确的队列路由和有界五轮交接。取消和完成回调必须捕获原 Chat/执行归属，不用变化后的负责人替换。
- [ ] 修正 `work_board_controller.go`、`work_board_candidates.go`、`work_attention_changes.go`、`chat_reading` 与 IM 的初始 LeadChatID 假设；自动运行仍由原 ThreadID 和封闭事实决定，不因交接重新触发运行。
- [ ] 分组运行以上新用例，以及阅读/看板/IM 的新增 `LeadChange` 定向回归，预期负责人变化不重置阅读位置、漏结果或重复唤醒。检查来源 SQL 的限量与索引计划，不在每页枚举无限历史 Chat。

## Task 5: API、事件与前端当前名单

**Files:** `apis/channels/channel_input.go`、`update_channel.go`、`get_channel_thread.go`、`update_channel_thread_member_configs.go`、`router.go`；新增同包成员 Chat 显式准备 Operator；Team 页面/投影/上下文、Query 事件与定向测试。

**Interfaces:**

- 保存团队携带期望 revision；冲突保持用户草稿并重新读取当前数据。
- 线程详情 `members` 为当前有效成员，`contexts` 为已存在的 Chat 历史映射；当前成员 ChatID 可以为空。
- 新增 `POST /channels/:channelId/threads/:threadId/members/:wakerId/chat`，只为当前成员准备或返回已有 ChatID；该返回值用于随后打开对话。
- 时间线响应以 `latestPosition` 替换单 Chat 窗口序号；新增线程路径的 `GET reading`、`POST read`、`POST unread`，复用现有阅读命令的观察代次/hold 规则，使用 ThroughPosition。
- 团队资源版本变化失效活动线程的当前资料与角色，不重取无变化的全部消息，不发每个历史线程一份成员事件。

- [ ] 先补 `TestThreadDetailUsesCurrentMembersAndHistoricalContexts`、`TestPrepareMemberChatIsIdempotent` API 契约测试；前端投影测试断言无 Chat 的新成员可选，历史退出者只能从原消息/历史入口查看。
- [ ] 在团队聊天集成测试覆盖当前页面保存加人、`@` 新成员、移除当前查看成员、更换负责人、断线后重新对账。断言输入草稿、滚动区和历史加载页不被重置。
- [ ] 失败后实现统一投影，移除 `roster: navigation/execution` 分叉；配置编辑明确保存线程覆盖。复用共享控件，不添加快照或同步说明层。
- [ ] 用 `pnpm api:generate` 更新 OpenAPI 与 Client，再运行 `pnpm api:check`；枚举/错误按对应 `go generate` 声明生成，不手改生成文件。
- [ ] 运行 `pnpm --dir packages/frontend exec vitest run src/modules/team/model/team-member-projection.test.ts src/modules/chat/pages/chat-page.integration.test.tsx` 及实际修改到的团队事件/页面测试文件；对修改的 TS/TSX 定向 ESLint，运行 `pnpm --dir packages/frontend typecheck`。

## Task 6: 历史转换与一致性验收

**Files:** 新增 `server/repositories/controllers/channel_thread_migration.go` 与 `_test.go`，职责仅为本次旧表转换；`channel/service.go` 启动顺序和恢复测试；相关架构/指南文档。

**Interfaces:**

- Repository 提供本次专用的可重入转换准备/提交操作，由 Channel.Start 在开放接纳前编排；事务外日志读取通过 Chat/ConversationStore 端口完成，不由 Repository 读取文件。
- 准备记录按线程分批保存转换进度和来源引用，不新建通用迁移框架；最终结构切换有明确完成标记，成功后不再读取旧快照。
- 历史旧表仅是转换输入；新能力查询、调度和授权不能通过“新表缺失则读旧快照”回退。

- [ ] 用真实旧 Schema/JSONL 脱敏夹具补 `TestLegacyThreadConversionPreservesPublicIdentity`、`TestLegacyConversionKeepsOnlyCurrentMembership`、`TestLegacyExecutionConfigConversionPreservesEffectiveValues`、`TestInterruptedConversionResumesBeforeDispatch`、`TestCorruptLegacySourceBlocksConversionWithoutDataLoss`；公开基线必须包含旧版已显示但缺少 LeadInput/Notification 记录的消息，保留原顺序和 ID。
- [ ] 覆盖旧负责人已退出、新人无旧行、全部旧资料清空、旧负责人 Chat 内同时有成员任务、重复迁移和读游标；核对消息/执行/报告/Chat ID、原目录及公开排序未改变。
- [ ] 先确认新断言失败后实现转换；只在来源及引用完整性验证通过后切换并移除旧表/字段。启动恢复先转换、再成员准备/取消、再任务与负责人队列。复制旧负责人 Chat 阅读边界到公开 Position 并保留未读代次/hold，移除过渡旧类型/字段及所有旧资格调用。
- [ ] 运行 `cd server && go test ./repositories/controllers -run 'Test(Legacy.*|InterruptedConversion.*|CorruptLegacySource.*)$' -count=1`，再运行 `./services/channel` 对应启动/恢复用例；拒绝用全量测试替代这些边界断言。
- [ ] 实现完成后更新 `CONTEXT.md`、`docs/architecture/teams-cn.md`、`chat-and-execution-cn.md`、`workspace-and-storage-cn.md`、`notifications-and-work-board-cn.md` 和 `docs/guide/teams-and-workflows-cn.md` 中对应原章节；当前仍是旧实现，本轮不提前修改这些能力说明。
- [ ] 运行 `pnpm docs:check`，复核受影响 Go 包的定向 vet/测试及前端契约检查。经用户授权后执行 Claude/Codex 连续会话与 UI 验收；逐项记录证据，不将自动化通过写成真实平台已验收。

## 交付界限

本次完成标准是用户场景与六个任务的边界共同成立，不是更换函数名、让 CLI 多返回一个人或只在 UI 看见新头像。实施前审阅负责人交接、历史来源和旧数据转换三个风险最高的部分；完成实现后再评审实际 diff，未经明确指令不推送或部署。
