# 私聊与团队协作的源码依据

配套[设计正文](2026-11-20-unified-partner-collaboration-design.md)。本次核对了 NeuxBot 的 `main@11bc6d4bff93bd9c765d662672c0b0e8da03c0e2` 加工作区现有修改，以及 OpenSuperAgent 的 `c217b8e0dcaaa40bb865febad2deeba483d0e1cd`。主线提交号不能独立还原工作区修改，以下同时给出文件和业务符号定位。

## 1. OpenSuperAgent 如何把调用写简单

作者在 README 中直接标明 DeepSeek Super-agent 提供了 Router、Worker、Watchdog、Inline & Spill 的架构思路，并在架构文档中给出每个 Bot 一个 Router、后台 Worker 和群发言队列的总图。下面区分作者自述的来源、程序里的实际对象，以及 NeuxBot 设计所借用的概念。[致谢](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/README.md#L169-L176) · [原项目架构总览](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/docs/architecture.md#L3-L34)

| 哪层架构 | 源码说明 |
| --- | --- |
| 每个 Bot 的 Router 与 Worker | `runtime(bot_id)` 按伙伴缓存 Router，`respond(chat_id,turn)` 进入该 Bot 的前台回合。Worker 以任务目标和自己的工具循环工作。groups 只决定谁获得这一轮发言。[Router 身份](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L271-L302) · [Worker 工具循环](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/worker.py#L248-L296) |
| Task 与 Ticket | OpenSuperAgent 的 `Task` 含目标、bot、chat、运行状态、reply、handoff。DeepSeek 公开原型显式区分持续委托 Ticket 和一次执行分配 Assignment，主会话可接受新工作或把输入挂到旧工作。这里引用原型只为核对术语。NeuxBot 的任务与尝试仍从自己的主线演进。[上游 Task](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/worker.py#L26-L59) · [原型 Ticket](https://github.com/deepseek-ai/dsh-superagent-prototype/blob/8c32a9dc5d72c033816c6a8110b37f40cf94fba4/src/core/model.ts#L271-L307) · [原型 Assignment](https://github.com/deepseek-ai/dsh-superagent-prototype/blob/8c32a9dc5d72c033816c6a8110b37f40cf94fba4/src/core/model.ts#L211-L233) · [原型路由动作](https://github.com/deepseek-ai/dsh-superagent-prototype/blob/8c32a9dc5d72c033816c6a8110b37f40cf94fba4/src/app/tools/main.ts#L88-L128) |
| 独立监督与上下文 | Watchdog 遍历活动任务，检查时限、进展、调用重复和预算，跳过正在等人批准的任务。Inline & Spill 用最近正文及较早消息的有限索引构造 Router 上下文，`recall` 按消息和会话核验原文。两者承担不同职责。[Watchdog](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/watchdog.py#L32-L59) · [上下文](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/memory.py#L160-L180) · [原文取回](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/memory.py#L103-L108) |

| 要回答的问题 | 实际实现与源码 |
| --- | --- |
| 用户跟哪个 Bot 说话？ | 私聊在当前 Bot 上调用 `respond(chat_id)`。群聊先从成员和点名顺序得到发言人，再调那位 Bot 的同一应答方法。Router 收到的 `turn` 只在需要回信或解释被谁唤醒时带额外信息。[私聊应答](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L318-L350) · [群选人](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/groups.py#L106-L130) |
| 为什么没有单独的派工号？ | `dispatch` 先向来源 Chat 追加任务消息，把返回的消息 ID 直接放入 `Task.id`，启动后台 Worker。结果是另一条消息，通过 task_id 回指原工作。Rust 实现使用相同做法。[Python 派工](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L719-L739) · [结果写回](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L765-L805) · [Rust 派工](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/router.rs#L856-L886) |
| 收到私信怎么回？ | `send` 保存发起 Bot、原 Chat、深度和上一跳回信地址。对方可以直接回答，也可以把回信地址传给自己的 Worker。`answer` 按原地址写回并唤醒发起方，无须模型重新输出一组路由 ID。上游找不到合适群地址时可退回伙伴私聊。[Python 私信链](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/dm.py#L40-L80) · [Rust 私信链](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/dm.rs#L54-L120) |
| 交棒本身需要任务吗？ | `handoff` 写群里的交接说明，改变发言队列。`dispatch(handoff_to)` 才创建后台任务，成功时按该任务的结果再交接。已等结果的成员会跳过本轮重复点名。群发言上限为 8，Bot 私信深度上限为 5。[发言队列](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/groups.py#L23-L56) · [完成后交接](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/groups.py#L121-L156) |
| 哪些边界不同？ | 上游群成员间用 handoff，允许向群外 Bot 发私信。回信等待和部分调度状态在内存，启动时将原进行中任务标为中断。NeuxBot 的目标要求团队只在内部协作，也要求重启后沿已接纳回执判断结果。两处都需要 Core 的明确规则。[工具分流](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L569-L594) · [启动处理](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/tasklog.py#L156-L165) |

上游冒烟用例分别覆盖直接聊天派工、派工后群内交接、任务极快完成时只唤醒一次，以及私信回到发起处。它们适合当作可观察行为样例，测试使用了预设模型响应。[Python 测试](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/tests/test_smoke.py#L10-L101) · [服务契约测试](https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/contract/test_smoke.py#L16-L92)

## 2. 主线路径与职责

按团队、私聊、执行、客户端四条路径核对了核心服务、模型/控制器、工具、入口与恢复调用方。团队定域包括 `services/channel`、`apis/channels`、`internal/teamcapability`、`internal/teamwork` 及 Channel 模型/控制器。相邻领域沿调用链读取。以下是落实每项设计时应当回看的代码位置。

| 路径 | 已有事实 | 源码 |
| --- | --- | --- |
| 普通首发、续发 | 首发以稳定 clientRequestId 关联接纳，续发带实际 Chat 与 submissionId。Execution 用 WorkIntent 处理输入、附件、配置、准入和原生执行。界面队列中未提交的草稿仍是本地状态 | [CreateChat](../../../server/apis/chats/create_chat.go) · [SendMessage](../../../server/apis/chats/send_message.go) · [Start](../../../server/services/execution/intent.go) · [startMessage](../../../server/services/execution/work.go) · [聊天架构](../../architecture/chat-and-execution-cn.md) |
| 每 Chat 一次执行及精确停止 | 执行登记按实际 Chat 预留活动位置，ThreadLifecycle 持有绑定，Turn 持有输入，ProviderRuntime 管理进程和实际租约。可按 chatId、executionId 精确取消 | [执行登记](../../../server/services/execution/lifecycle.go) · [精确取消](../../../server/apis/chats/cancel_chat_execution.go) · [ThreadLifecycle](../../../server/internal/agent/thread_lifecycle.go) · [Turn](../../../server/internal/agent/turn_lifecycle.go) · [原生所有者](../../../server/internal/agent/provider_runtime.go) |
| 团队首发、成员身份、单人点名 | 首发准备一人一个 Chat，负责人 Chat 承载公开主线。原负责人 Chat 接纳后才确认线程就绪。续发 @ 普通成员时，现有活跃任务收补充，否则创建可直接交付的任务。当前接口用单个 MentionWakerID | [线程和路由](../../../server/services/channel/thread.go) · [首发补偿](../../../server/services/channel/recovery.go) · [成员 Chat 准备](../../../server/services/execution/chat_group.go) · [成员和负责人映射](../../../server/repositories/controllers/channel_thread_controller.go) |
| 领取和尝试 | `schedule` 从线程组织候选，`dispatchTask` 取实际成员 Chat。`ClaimForDispatch` 按任务旧状态、fence、线程限额、成员占用和线程停止状态条件更新，尝试创建及本批补充标记在同一事务。外部原生调用在事务外 | [调度](../../../server/services/channel/task.go) · [原子领取与回收](../../../server/repositories/controllers/channel_task_controller.go) · [任务模型](../../../server/repositories/models/channel_task.go) |
| 协作工具与权限 | 现有原生能力按 execution、角色、线程、尝试和期限绑定。`team.tasks.create` 用于负责人指派非负责人成员，`team.reports.submit` 是成员的具名报告入口。通用 registry 已提供 requestId 与业务提交回执 | [命令目录](../../../server/internal/agent/capability/catalog.go) · [能力回执](../../../server/internal/agent/capability/capability.go) · [团队角色绑定](../../../server/services/channel/capability.go) · [任务工具调用](../../../server/services/channel/agent.go) |
| 报告、人工验收与结果汇总 | 成员报告验证当前尝试。报告候选、终态、用户批准、主 Chat 发布有各自事实。直接交付核对 DirectDelivery、UserReady、当前 attempt、完整内容等。人工验收校验 expectedFence | [报告](../../../server/services/channel/member_report.go) · [终态结算](../../../server/services/channel/lifecycle.go) · [通知与直达](../../../server/services/channel/notification.go) · [验收](../../../server/apis/channels/review_channel_task.go) |
| 追加结果与发言 | `AppendParticipantMessages` 接受稳定消息 ID、实际作者和来源 Chat。目标忙会返回 ChatHasActiveExecution，成功只写权威日志，不自动发起模型回合。`ReadTimeline` 按负责人 Chat 与待投输入合并 | [参与者消息](../../../server/services/chat/participant.go) · [团队时间线](../../../server/services/channel/timeline.go) · [用户提问与审批](../../../server/services/channel/interaction.go) |
| 重启、停止、删除与外部来源 | Channel.Start 会全库取 pending/in-progress 团队任务。StopChannelWork 保留待验收。工作流关联的团队停止及 IM 回复有各自归属。启用私聊写入之前必须按来源调整扫描与清理 | [启动与恢复](../../../server/services/channel/service.go) · [停止与补充](../../../server/services/channel/control.go) · [停止整个线程](../../../server/apis/channels/stop_channel_work.go) · [删除](../../../server/services/channel/deletion.go) · [工作流关联](../../../server/services/workflow/team_stop.go) |
| 客户端 | 普通聊天和团队主线各有草稿/队列语义。主线已有右侧 workbench、成员页、团队 timeline 和共用事件通道。私聊的协作摘要与任务详情需在这些现有界面内新接入 | [普通 Chat](../../../packages/frontend/src/modules/chat/pages/chat-page.tsx) · [团队 Chat](../../../packages/frontend/src/modules/chat/pages/team-chat-page.tsx) · [右侧工作区](../../../packages/frontend/src/modules/chat/workbench/right-workbench.tsx) · [Core 事件](../../../server/apis/events/stream.go) · [团队架构](../../architecture/teams-cn.md) |

## 3. ID 简化的核验切口

1. `ChannelTaskAttempt.ID` 是主键，`(TaskID,Fence)` 有唯一约束，非空 `ExecutionID` 也有唯一约束。目前 `dispatchTask` 预先生成两次随机 ID，一次调用 `Execution.Start`。新行可先生成一个值填入两列。待已有 API 和消息消费者都迁移后再评估物理列是否有删减价值。[模型](../../../server/repositories/models/channel_task.go) · [派发及接纳](../../../server/services/channel/task.go)
2. `AttemptID` 已被补充领取、重试回收、报告、委派详情和公开消息引用。旧值不能用 `executionId` 重算。验收通过还会用 taskId、fence 和固定用途派生验收通知的去重值，和本轮成员报告分别记账。应减少新 ID 的生成，保留旧引用和不同事件之间的区分。[补充与回收](../../../server/repositories/controllers/channel_task_controller.go) · [批准通知](../../../server/apis/channels/review_channel_task.go) · [通知数据](../../../server/repositories/models/channel_delivery.go)
3. 已有 `FindByChatID` 可反查团队线程。真实来源、真实作者和运行配置可由 Core 从原 Chat/绑定恢复，模型报自己的 actor、thread 或 fence 不增加可信度。一个普通伙伴可以有多个 Chat，`wakerId` 无法单独充当回复地址。[线程仓储](../../../server/repositories/controllers/channel_thread_controller.go) · [团队绑定检查](../../../server/services/channel/capability.go)

## 4. 实施时复用的定向用例

| 改动 | 现有锚点 | 需追加的用例 |
| --- | --- | --- |
| 私聊首发、同键重放、精确取消 | [提交回执](../../../server/services/execution/submission_receipt_test.go) · [首发请求登记](../../../server/services/execution/create_request_registry_test.go) | 自办长工作期间普通用户还能发新问题。同一条工具请求重传得到原 taskId，两次独立求助得到两个 taskId |
| 提取派工、尝试与回报 | [任务仓储](../../../server/repositories/controllers/channel_task_controller_test.go) · [团队派工](../../../server/services/channel/task_test.go) · [通知](../../../server/services/channel/notification_test.go) | 同批旧数据包含 attemptId 等于及不等于 executionId，均可取消和读详情。报告/用户验收不重复，一方失败不会吞另一方结果 |
| 群内接力与回信 | [团队负责人回合](../../../server/services/channel/leader_turn_test.go) · [恢复](../../../server/services/channel/thread_recovery_test.go) · [验收](../../../server/apis/channels/review_channel_task_test.go) | 私聊回信和群公开接手分别验证。极快完成、目标被同时点名、来源被删、越团队、签发后界面关闭，分别核对真实消息数及唤醒次数 |
| 前端与实时恢复 | [聊天输入集成](../../../packages/frontend/src/modules/chat/runtime/chat-composer.integration.test.tsx) · [团队首发集成](../../../packages/frontend/src/app/pages/team-thread-page.integration.test.tsx) · [右栏](../../../packages/frontend/src/modules/chat/workbench/right-workbench.test.tsx) | 工作摘要能回到准确运行。两个 Chat 同时收同一伙伴的回报仍各自正确。主时间线真实作者、断线后去重和已读边界有效 |

以上是静态源码核对和未来的验收入口。生成 SDK、第三方 CLI 内部实现和所有测试正文未逐行审计。本次仅修改设计文件，尚无新运行时、数据库迁移或真实模型的验收成绩。
