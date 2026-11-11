# 会话阅读与待处理重建 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按 OpenBot 的阅读/请求/提醒职责重建 Go 会话阅读，保留铃铛小点和任务看板，删除旧通知业务，无兼容。

**Architecture:** 三张功能表保存消息阅读索引、Chat 游标、原运行查看记录；一个标记只负责首次基线初始化。索引加入现有 Chat.LatestSequence 投影事务，原有效请求保持原所有者；共享查询提供完整聚合，原 AfterCommit/Hub 发失效，客户端重读。Electron 仅对 live 事件提醒。

**Tech Stack:** 现有 Go、ginx/toolx、GORM/SQLite 默认模式、ConversationStore、认证 WS、React/TanStack Query、Electron；不新增事件存储或任务框架。

**Spec:** [收敛后的完整设计](../specs/2026-11-10-conversation-reading-and-attention-design.md)。当前只修改文档，全部步骤均未实施。

## Global Constraints

- 旧通知业务彻底删除，无兼容接口、双写、状态转换或运行期开关；原请求、日志、文件成果和配置保留。
- 铃铛只有小点，hasAction||hasUnread，不显示总数，打开铃铛/看板不清状态。
- 看板保留“需要操作”“待我查看”、工作记录及原筛选；两类可共存。
- Bot 多会话完整聚合放 Go，不依赖前端缓存；成员/worker/joined 不重复计数。
- 不新增 ChatReadingProjection、WorkspaceState、WorkspaceChange、ViewSnapshot 等表，不向原 Run 增加 ReviewVersion/ReviewAvailableAt。
- 只有 ChatReadCursor、ChatReadableMessage、RunViewMark 三张功能表和 ReadingInitialization 标记；ReaderID 为服务端固定本机 UUID 常量。
- 同一公开 MessageID 只计一条；顺序取 Message.CreatedSequence，不取终态到达顺序；有合格持久正文可计数，不必等待整轮结束。
- 复用 Chat.LatestSequence 唯一覆盖游标；读索引与原投影同事务提交，不建立第二份消息权威。
- 工作流重跑创建新 RunID，原终态不改写；按原 RunID 记录已看。
- 保留纯 Hub/AfterCommit/既有连接及目录，不因名字移动全项目事件导入。不迁移 OpenBot 整套事件溯源数据库。
- 无全局持久 Revision；订阅失效、在途 dirty、原资源版本及新 epoch 后重读负责实时恢复，不补弹历史。
- 默认只做定向验证；不运行 go test ./...、pnpm check、desktop check、全量前端测试。
- 不打开/控制/截图应用，真实 CLI/系统通知验收另获授权；不改真实数据、不提交或推送。
- 实施前核对分支/工作树并保留无关修改；本计划不能视为已经收到实施指令。

## Review Focus

1. explicit 请求在途后另一窗口标未读，旧打开意图不能刷新代次重试：任务 1/3/6。
2. Chat 索引与原 LatestSequence 不得分开推进，活进程投影失败也必须可恢复：任务 2/5。
3. 基线完成后 Runtime 恢复再崩溃，不能重做历史基线吞掉新失败：任务 8。
4. 工作流重试新 Run、joined/detached 和自动机会归属不能重复：任务 3/4。
5. 批量阅读跨批允许部分完成，但不升级冻结边界、不跨重启继续、不清新消息：任务 4/6。

## OpenBot 源码对照与验收

参考版本为 OpenBot 0.66.0，固定提交 `22cd028e7c99c41e49bbab434dbda63ff9e56304`。[设计第 2 节](../specs/2026-11-10-conversation-reading-and-attention-design.md#2-openbot-实现依据与适配边界)说明原行为及 NeuxBot 的差异。各任务的「OpenBot 对照」列出实现前要读的源码入口和要验证的行为。本机有源码时可用 `git show <提交>:<仓库相对路径>` 查看固定版本。

- [ ] 开始每项任务前，阅读该任务列出的 OpenBot 代码和对应的 NeuxBot 调用链。
- [ ] 任务验收时记录 OpenBot 函数与行号、NeuxBot 实现位置、适配差异和对应的定向用例。审阅者根据这四项核对。

## 文件与职责

| 文件                                                          | 职责                                                                           |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| server/repositories/models/chat_reading.go                    | ChatReadableMessage、ChatReadCursor、ReadingInitialization；字段按设计完整注释 |
| server/repositories/models/run_viewing.go                     | RunViewMark，不增加 Run 结果版本                                               |
| server/repositories/controllers/chat_reading_controller.go    | 索引、边界/代次条件更新、分批 COUNT 与初始化读写                               |
| server/repositories/controllers/run_viewing_controller.go     | 原运行最终结果资格与查看记录                                                   |
| server/services/chat/reading.go                               | 原公开 messageview 资格与索引接入，复用原投影恢复                              |
| server/services/workspaceattention/query.go                   | 无后台生命周期的共享归属/请求/阅读查询，仅多个真实入口复用                     |
| server/apis/chat_reading/、run_viewing/、workspace_attention/ | 原权限下的阅读命令与摘要 Operator                                              |
| server/apis/work_board/                                       | 保留看板，替换查询和无耐久任务的批量已查看                                     |
| packages/frontend/src/modules/workspaceattention/             | Query、铃铛小点和原事件失效装配，不保存第二份状态                              |
| packages/frontend/src/shared/api/chat-reading.tsx             | 共用阅读资格、一次打开意图与同 Chat 写队列                                     |
| desktop/electron/main/workspace-attention.ts                  | 主进程实时提醒策略、内存去重/撤回及定位                                        |

所有新增路径是拟定位置。HTTP DTO 留在 API 包；原业务结构放原所有者；不为每个辅助函数新建 Service。下面方法名由任务 1 固定后贯穿后续，不复制两份同义类型。

## Task 1：最小数据模型与阅读命令契约

**OpenBot 对照**

对照已读游标单调前进、手动未读独立回退，以及同一边界如何计算未读。跨窗口代次和 Run 查看记录按 NeuxBot 契约验证。 [已读和标未读](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/conversation-read-store.ts#L129-L173) · [未读计算](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/conversation-read-store.ts#L280-L302)

**Files:** 新增上述两个 Model、两个 Controller 及对应测试；修改 `server/repositories/models/chat.go` 增加可重建 LatestReadableSequence 提示；原 constants 枚举/错误及注册处按规范生成。

**Interfaces:**

- `ChatReadingController.Get(ctx context.Context, readerID, chatID string) (models.ChatReadingState, error)`。
- `MarkRead(ctx context.Context, readerID, chatID, throughMessageID string, observedGeneration int64, intent constants.ReadingIntent) error`。
- `MarkUnread(ctx context.Context, readerID, chatID, fromMessageID string, observedGeneration int64) error`。
- `RunViewingController.Get(ctx context.Context, readerID string, owner models.RunOwnerRef) (models.RunViewingState, error)`；`MarkViewed(ctx context.Context, readerID string, owner models.RunOwnerRef) error`。
- 共享 Model 的字段与零值逐项采用设计第 4 节；RunViewingState=原 OwnerRef、是否有可见最终结果、是否已看，不能带新 ReviewVersion。

- [ ] 写 `TestChatReadingGenerationAndHold`：普通 read 的 max 边界不后退、普通 read 不增加 UnreadGeneration；手动未读 Generation+1；automatic 在 hold 下失败；相同代次的 explicit 才能解除；旧代次不重试。
- [ ] 写 `TestRunViewMarkImmutableRun`：同 Run 多次查看幂等，普通技术更新不重新待看，新 RunID 仍未看；运行未终态/不可见拒绝，主动 cancelled/无公开结果的空终态不进入待查看。
- [ ] 写 `TestChatReadingDeletedBoundary`：删边界消息不清 Cursor，不重置历史未读；同 MessageID 索引 upsert 一行。
- [ ] `cd server` 后运行 `go test ./repositories/controllers -run 'TestChatReading|TestRunViewMark'`，先确认新增契约缺失失败，再实现模型/接口与事务条件更新并通过。
- [ ] 按文件中的 go:generate 更新枚举/稳定错误，gofmt 修改 Go 文件；检查只增加设计列出的表，没有普通读取全局锁或新全局状态版本。

## Task 2：接入唯一 Chat 投影与恢复

**OpenBot 对照**

对照消息分页、阅读边界共用的排序键，以及按作者和消息类型筛选未读。团队分页与实时合并须使用同一顺序。索引原子性和恢复按 NeuxBot 的 JSONL/SQLite 边界验证。 [消息分页](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/database/conversation-queries.ts#L571-L615) · [阅读排序键](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/conversation-read-store.ts#L213-L235) · [未读资格](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/packages/contracts/src/conversation-read.ts#L28-L64)

**Files:** 新增 `server/services/chat/reading.go`、`reading_test.go`；修改 `live_projection.go`、`recovery.go`、`event_pipeline.go`、`journal.go`、`service.go` 及其他实际推进覆盖位置的路径。团队持久消息顺序修改 `server/services/channel/timeline.go` 及前端实际实时合并处。不可修改 acp-go 或原生模型探测。

**Interfaces:**

- `projectReadableMessages(ctx context.Context, chatID string, messages []messageview.Message) error`：由原投影事务调用，保存事务外按冻结日志范围解析的窄索引增删，与原覆盖位置一起提交。逐条提交、批量创建、分叉、启动追平及完整重建都使用同一不变量。
- `CatchUpReading(ctx context.Context, chatID string) error`：委托现有唯一 Chat catch-up；固定此次日志末尾，不无限追赶。
- 原日志成功、DB 失败时，原投影所有者登记最多 256 个 Chat，以一个可取消计时器按 1/2/4 秒至最多 30 秒定向重试。第 257 个失败触发全局 incomplete 与分批核对原日志目录，核对期间的再次溢出安排下一轮。

- [ ] 写 `TestReadingPublicMessageQualification`：持久公开正文首次可读即计数；Token/commentary/用户本人不计；同消息流式、终态、迟到文件不重计；主对话成员报告按 actor 计一次。
- [ ] 写 `TestReadingShownOrder`：消息页排序与 OrderSequence 一致；终态交错不改序；读旧消息不清展示在后面的消息；覆盖 steering、继承以及同毫秒先 Z 后 A 的团队正式报告，验证历史分页、实时合并与读到/从某条标未读一致。
- [ ] 写 `TestReadingProjectionAtomicRecovery`：原日志成功/DB失败；索引与 Chat.LatestSequence 同事务回滚；重放一行，验证原逐条提交与分叉路径不能越过未更新的索引。对投影替换 Delete/Create 保留 Cursor、代次和 HoldUnread，永久删除才清理。
- [ ] 写 `TestReadingProjectionLiveRepair`：运行中失败即通知恢复中，定向重试成功后可查询；仅发失效、不伪造 live 新答复；退出取消重试，重启由原日志恢复缺口。注入 257 个不同 Chat 投影失败和核对中再次溢出，验证恢复覆盖后才解除 incomplete。
- [ ] 运行 `go test ./services/chat -run 'TestReading'`，先失败后实现并通过；对实际受影响原投影用例定向重跑。
- [ ] 核对原生发送/续聊不等待阅读健康、没有第二个 AppliedSequence，没有全局业务表扫描。gofmt 并检查改动范围。

## Task 3：阅读 API、原请求与原运行生命周期

**OpenBot 对照**

对照读写命令只影响指定会话，以及完整、部分快照对缺席请求的处理。验收分别覆盖跨会话边界、请求结束和部分结果保留有效请求。 [会话读写入口](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/agent/conversation-reader.ts#L137-L185) · [提问对账](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/agents/agent-runtime-snapshot.ts#L82-L113) · [审批对账](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/agents/agent-runtime-snapshot.ts#L147-L158)

**Files:** 新增 `server/apis/chat_reading/{router,get,read,unread}.go`、`reading_test.go` 和 `server/apis/run_viewing/{router,get,view}.go`、`viewing_test.go`；修改实际 API 总注册、原请求提交发布处。工作流 `retry_workflow_run.go`、`schedule/runs.go` 只核对/补窄测试，不更改现有新 Run 和接纳生命周期。

**Interfaces:** 设计第 6 节 Chat/Run API。身份由 Core 固定提供；写成功通用响应，读/未读写后原事务 AfterCommit 发 reading 主题安全失效。请求操作仍走原接口。

- [ ] 写 `TestReadingAPIIdentityAndGeneration`：跨 Chat 消息、非法边界、客户端 readerID、旧 explicit 代次和 automatic 清 hold 均拒绝；正确命令成功。
- [ ] 写 `TestViewingAPINewRunAndAdmission`：只查看原 RunID；重试生成新 ID，旧查看不影响新运行；只有无目标的 failed/interrupted 自动机会可独立待看，pending/skipped 排除。
- [ ] 核对并定向执行已有 `TestRetryCreatesNewRunWithOriginalVersionAndEvidence`，不得为了新阅读重写原重试行为。
- [ ] 为原请求补 `TestReadingRequestOwnershipAndRecovery`：旧执行 ID/旧验收 fence 不可操作；Runtime 失联请求结束，持久工作流等待保留；完整快照移除缺席项、部分只合并。
- [ ] 各自在对应 API/领域包先运行上述新增用例确认失败，再实现/接入并通过；审批不在 SQL 事务内等待原生响应。
- [ ] 不新增 Run ReviewVersion、请求状态表或通知通用处理接口；生成枚举/错误并 gofmt。

## Task 4：完整聚合、看板与无耐久批量阅读

**OpenBot 对照**

对照主会话和开放子线程求和，以及逐线程捕获消息边界再标已读。NeuxBot 由 Go 汇总完整范围并冻结批量目标。验收覆盖未访问会话、独立计数和操作期间到达的新消息。 [主会话与子线程聚合](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/conversation/conversation-context.tsx#L171-L183) · [开放子线程计数](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/agent-tasks/agent-tasks-state.ts#L421-L455) · [全部已读](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/conversation/conversation-context.tsx#L1128-L1155) · [子线程全部已读](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/agent-tasks/agent-tasks-state.ts#L708-L717)

**Files:** 新增 `server/services/workspaceattention/query.go`、`query_test.go` 和 `server/apis/workspace_attention/{router,snapshot}.go`；修改 `server/repositories/controllers/work_board_*.go`、`server/apis/work_board/{router,list,summary}.go`；新建 `view_targets.go`、`mark_viewed.go` 及定向测试；删除旧 result/clear_results 接口。

**Interfaces:**

- `WorkspaceAttentionQuery.Snapshot(ctx context.Context, readerID string) (models.WorkspaceAttentionSnapshot, error)`。
- `ListPending(ctx context.Context, readerID string, filter models.WorkspaceAttentionFilter) (models.PendingPage, error)`。
- `ListAwaitingView(ctx context.Context, readerID string, filter models.WorkspaceAttentionFilter) (models.AwaitingViewPage, error)`。
- `ExecutorSummaries(ctx context.Context, readerID string, refs []models.ExecutorRef) ([]models.ExecutorAttentionSummary, error)`。
- API 用例 `prepareViewTargets(ctx, readerID, filter)` 固定设计第 5.3 节的准确目标，至多 10000 个/4 MiB；`markViewedBatch(ctx, readerID, targets)` 至多 128 项，本批事务返回 applied/skippedConflict/skippedDeleted；无 snapshotID 或任务进度。

- [ ] 写 `TestAttentionMultipleChats`：设计例中 5 条/2 Chat/1 等待工作/2 请求/1 运行；未访问 Chat 冷启动计入；局部操作独立。
- [ ] 写 `TestAttentionUniqueOwnership`：团队公开报告、真实成员请求、joined/detached、自动目标不重复；看板两类可共存。
- [ ] 写 `TestAttentionQueryPlanAndScale`：128 Chat 一批、准确分组 COUNT、EXPLAIN 使用范围索引；100/1000/10000 Chat，0/稀疏/大量未读，统计整个请求的 SQL 和日志 stat，确认平时只读 SQLite，零日志 stat、无正文加载和逐 Chat 查询。
- [ ] 写 `TestAttentionBellAndPagination`：铃铛只有精确布尔值；不完整用 complete=false、不能假零；列表/总数在同短事务，预览5不是总数，limit50/最大100。
- [ ] 写 `TestMarkViewedFrozenBatch`：捕获后新消息/新 Run 不加入，旧代次显式跳过；重试原批边界不扩大；第3批断线不虚报后续完成；DB故障不伪装目标删除。
- [ ] 写 `TestMarkViewedTargetBounds`：10001 目标/4 MiB 超限明确拒绝，单批129拒绝；每项映射/权限重验；不清审批；无后台跨重启继续。
- [ ] 对 `services/workspaceattention`、`apis/work_board`、`apis/workspace_attention` 和实际 Repository 用例按上述名称定向红绿验证；记录查询计划、语句数和耗时。
- [ ] 完成新 API 后执行 `pnpm api:generate`、`pnpm api:check`；只通过命令更新 OpenAPI/SDK。

## Task 5：既有事件的失效与重读闭环

**OpenBot 对照**

对照游标提交后发失效，再从权威读取。NeuxBot 复用 work-board/global，按自身连接契约验证先订阅后查询、未打开会话提交和 GET 失败后的补读。 [提交后 conversation-invalidated](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/agent/conversation-reader.ts#L144-L160)

**Files:** 修改 `server/apis/events/{sources,protocol,stream}.go`、实际领域 AfterCommit 发布点、`packages/frontend/src/shared/api/global-events.ts`/`use-query-events.ts` 的主题及调度装配和其定向测试；保留纯 `internal/notification/events/hub.go` 与事务目录。

**Interfaces:** 保留并从旧通知逻辑拆出 work-board/global 的原提交后失效。全局铃铛、看板和桌面复用同一主题。桌面实时帧区分于 changed 和 ready，携带原消息/请求身份，无历史补读。chat/team/workflow 资源主题继续供局部页面。reading 详情变化同时发布工作看板和原实体失效。先登记订阅再 ready，ready/resync 后完整 GET。

- [ ] 写 `TestReadingSubscribeReadyRace`：登记前后提交、GET在途变化、新加入共享消费者均最终补读；不按事件次数加减计数。
- [ ] 写 `TestReadingInvalidationFailure`：事务回滚不发、busy重试只发成功一次；超量资源折叠 global，消费者满/发布异常要求明确 resync，不能只等下次业务变化。
- [ ] 写 `TestReadingEpochAndStaleResponse`：新握手取消旧请求、拒绝旧 epoch；同查询 dirty在途保留，旧结果不能覆盖；隐藏Scene恢复对账。
- [ ] 写 `TestAttentionColdChatGlobalEvents`，超过 128 个 Chat 时，只打开 A 或没有 Chat Renderer，B 新消息和请求仍驱动小点与桌面 live，恢复 ready 不补弹。
- [ ] 写 `TestReadingDirtySurvivesQueryFailure`，WS 持续健康、无后续领域事件，首轮 GET 耗尽后恢复仍能补读。只有成功完整结果确认 dirty，隔离导致的 incomplete 等待修复或用户重试。
- [ ] 写 `TestReadingCatchUpDoesNotPresent`：重建/ready/断线快照只刷新业务状态，不补弹历史提醒。
- [ ] 定向执行上述 Go 事件用例及前端 global-events/use-query-events 文件，先失败后接入通过；确认无 workspace-state/全局Revision/outbox/新WS。

## Task 6：铃铛小点、会话阅读意图与看板

**OpenBot 对照**

对照显式重开解除 hold、前台可读条件、同会话读写串行与侧栏优先级。验收覆盖启动恢复不误读、旧打开意图不清新未读、待办展示期间保留未读。铃铛与看板按 NeuxBot 产品契约实现。 [显式重开](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/conversation/conversation-context.tsx#L362-L376) · [前台可读资格](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/conversation/conversation-context.tsx#L417-L433) · [同会话串行读写](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/conversation/conversation-context.tsx#L991-L1096) · [侧栏状态优先级](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/packages/ui/src/features/sidebar/sidebar-agent-states.ts#L69-L94) · [需要你分组计数](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/packages/ui/src/features/sidebar/SidebarWaitingGroup.tsx#L16-L35) · [主线程请求归属](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/renderer/src/features/agents/agent-thread-attention.ts#L13-L38)

**Files:** 新增 `packages/frontend/src/modules/workspaceattention/{index.ts,api/queries.ts,components/attention-bell.tsx}` 和测试；新增 `shared/api/chat-reading.tsx` 及测试；修改 `app/layout/app-shell.tsx`、`entity-navigation.tsx`、会话列表/Tab、ChatThread、团队成员定位、work-board、工作流/自动机会原结果区域和 i18n。

**Interfaces:**

- `AttentionBell` 仅消费准确的 hasAction/hasUnread；点击操作优先进入原看板，点状态不因导航清除。
- `useChatReading` 接收 chatId、Scene 活跃/焦点、前台实际呈现的公开消息边界、一次性 openIntent。同 Chat 串行写操作，显式打开只绑定动作发生时已知的代次，离开目标 Scene 后失效。主动标已读在操作时冻结代次和边界。
- Run 查看使用原 RunID，原结果区域呈现才提交。批量确认前冻结 view-targets 数组，仅重试原批次，不刷新更大边界。

- [ ] 写铃铛用例：布尔四组合、无数字、离线保留状态、Tooltip/访问名称/键盘焦点；打开看板不读。
- [ ] 写 `TestExplicitOpenCannotReplayAfterMarkUnread`：explicit 在途遇到另一窗标未读，冲突刷新后旧动作不升级代次；动作发生时代次未知，随后查询返回新代次也不能绑定旧动作；新用户动作才可解除 hold。
- [ ] 写阅读用例：启动恢复、聚焦和刷新不生成 explicit；重启后保留 hold，真实重开可解除；自动读须实际呈现持久公开正文，后台、预取和未进入可视区的正文不推进边界，流式同消息不重复。
- [ ] 看板集成用例：原筛选/工作记录保留，两类共存；预览不读，真实内容才读；多个请求会话先选择准确目标；部分批次失败显示实际结果。
- [ ] 使用 frontend 目录 `pnpm exec vitest run <上述具体文件>` 定向红绿验证；Node原生webstorage干扰jsdom时使用 `NODE_OPTIONS=--no-experimental-webstorage`，不改产品逻辑迁就环境。
- [ ] 修改文件定向 ESLint/Prettier、UI策略检查；API变化运行前端 typecheck，不跑全量前端测试，不启动真实界面。

## Task 7：Electron 实时提醒

**OpenBot 对照**

对照主进程统一分派、窗口聚焦抑制、原请求身份、请求结束时撤回和点击定位。验收覆盖多窗口、异步构造期间请求结束，以及重连不补弹历史。 [应用统一分派](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/main/renderer-forwarders.ts#L134-L149) · [过滤、撤回与导航](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/main/renderer-forwarders.ts#L325-L441) · [事件资格与请求键](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/packages/team-client/src/agent-notifications.ts#L92-L131)

**Files:** 新增 `desktop/electron/main/workspace-attention.ts` 和对应 node 测试；修改 `core-events.ts`、`desktop-services.ts`、`index.ts`、实际提醒设置/导航桥接。仅确有 Bridge 变化时更新协议/白名单定向验证。

**Interfaces:** 一个主进程 consumer 接 work-board/global 的独立 live 完成/失败/请求帧，参考 OpenBot.renderer-forwarders；内存去重、异步查摘要期间请求结束防护、request_closed撤回。系统通知不是阅读事实。

- [ ] 写测试：多个Renderer只提醒一次，任何有效窗口聚焦抑制；all/needs-me/off，安全预览；同消息/请求身份去重，原执行代次不同不混淆。
- [ ] 写测试：异步查摘要期间resolved不弹、断线关闭现有请求提示/在途构造、重连快照不重弹；点击校验真实目标，失效进入看板；关闭不调用阅读/审批。
- [ ] 定向 node 测试先失败后实现通过；内存句柄有界，系统权限拒绝不影响看板/铃铛。
- [ ] 无用户授权不做原生截图/点击和真实CLI审批，单独列待验收边界。

## Task 8：首次基线、删除旧业务与收口

**OpenBot 对照**

对照首次启用时固定旧线程阅读边界，以及随后新消息仍计为未读。NeuxBot 在自身启动顺序中先记录基线，再恢复执行。验收覆盖隔离坏日志和中途崩溃。 [历史基线初始化](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/openbot-database-schema.ts#L1042-L1061) · [读取基线和初始化游标](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/conversation-read-store.ts#L40-L62) · [历史基线回归测试](https://github.com/nightly-labs/openbot/blob/22cd028e7c99c41e49bbab434dbda63ff9e56304/src/backend/conversation-read-migration.test.ts#L21-L65)

**Files:** 新增 `server/services/workspaceattention/initialize.go`、`initialize_test.go`，但只提供首次启动用例、无常驻派发器；修改 `server/global/config.go` 与真实启动顺序、相关Repository初始化/删表方法；删除设计第9节旧生产链；实施完成后更新权威架构说明及生成契约。

**Interfaces:** `InitializeReading(ctx)` 沿用 Core 启动屏障。从 ConversationStore 枚举历史 Chat，复用投影重放并写入初始阅读事实，持久记录 BaselineCompletedAt 后才运行原中断执行恢复。清理旧表后标记 CompletedAt 并开放接纳。

- [ ] 写 `TestReadingInitializationInterruptedBaseline`，通过实际启动入口覆盖 SQLite 投影缺失的 Chat。逐 Chat 提交索引与初始 Cursor，崩溃后补齐缺失对象并保留已有 Cursor 和有效请求。
- [ ] 写 `TestReadingInitializationIsolatesBrokenChat`，一个历史 Chat 损坏时仍开放健康 Chat，隔离对象不写虚假已读。以后修复但无可信旧边界时从 0 计数。
- [ ] 写 `TestReadingInitializationCrashDuringRuntimeRecovery`：基线完成→原恢复新增失败消息/运行→崩溃→重启只继续恢复，不再标新失败已看。
- [ ] 写 `TestReadingInitializationPreservesBusiness`：原日志/配置/Run/文件/分叉保留，旧通知/成果跟踪表drop-if-exists可重试；完成后新Chat默认未读边界为0。
- [ ] 写 `TestReadingNoLegacyCompatibility`：旧通知路由、结果view/clear、旧主题和旧装配不可用；纯Hub、原请求与文件成果仍可用。
- [ ] 在临时数据目录/SQLite中定向红绿验证；不得对真实数据运行drop或重置。
- [ ] `rg` 清点 NotificationService/NotificationItem/WorkResultReceipt/WorkResultTracking/attention_notice_acks/notifications.snapshot/notifications.updated/work-results 的生产引用；无别名/fallback/双写。纯Hub目录名允许保留，不把保留纯传输误报为旧通知业务残留。
- [ ] 通过命令生成契约，更新 `docs/architecture/notifications-and-work-board-cn.md`、存储/桌面说明及相关工程约定中的已实现契约；设计阶段不提前改这些权威现状文档。
- [ ] 执行定向受影响测试、`pnpm docs:check`、`git diff --check`；交付实测与剩余真实平台验收。不自行提交/发布/执行真实数据切换。

## 覆盖与当前边界

| 设计重点                        | 任务    |
| ------------------------------- | ------- |
| OpenBot证据、三类事实、最少数据 | 1、2、3 |
| 单覆盖投影与活进程定向修复      | 2、5    |
| 原运行身份、原请求恢复          | 3、4    |
| 多会话准确聚合、看板/批量       | 4、6    |
| 先订阅后查询、dirty/epoch       | 5       |
| 一次打开意图、跨窗口未读        | 1、3、6 |
| 仅live系统通知、无耐久提醒      | 7       |
| 基线与恢复分阶段、无兼容删除    | 8       |

当前所有方法、路径和测试为实施约束，不表示已创建或执行。先审阅设计；未获实施指令前不改产品代码、不清真实数据、不启动界面。
