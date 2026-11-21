# open-super-agent 与 NeuxBot Superagent 源码对照

open-super-agent 已经实现了常驻协调者、后台 Worker、群内接力、跨 Bot 私信回传、事件派工和任务过程。它的执行单元以一次后台运行及其聊天回执为中心。DeepSeek DSH 原型以可持续负责的 Ticket 为中心，任务跨越多次执行，输入归属、订阅、执行尝试和回传各有身份。NeuxBot 的目标是让用户留在同一个主对话中交错发任务、补充或收结果。这三者的源码可以回答不同层次的问题。 [open-super-agent Router][osa-python] · [DSH 收件与归属][dsh-intake] · [NeuxBot 原型对照会话][neux-thread]

## 核对基线

- open-super-agent 固定在 `c217b8e0dcaaa40bb865febad2deeba483d0e1cd`。默认 Compose 使用 Python 服务，macOS 桌面包构建 Rust `osa-server`。下文按两套后端的共同行为判断核心能力，具体实现引用对应入口。 [Compose][osa-compose] · [Python 启动入口][osa-docker] · [Rust 桌面构建][osa-desktop]
- DeepSeek DSH 固定在 `8c32a9dc5d72c033816c6a8110b37f40cf94fba4`。NeuxBot 核对的是承载预览的 `codex/superagent-integration` 工作树，含基线 `f46d4cce` 之后的本地修改。`codex/superagent-native` 的 `6181de32` 保存早期设计、验收脚本和场景。 [DSH 源码][dsh-rev] · [NeuxBot 原型对照会话][neux-thread]
- 本文证据来自静态源码及既有验收记录。上游交接和回传测试使用脚本化模型输出。README 的 DeepSeek 演示是作者披露的运行记录。 [Python 冒烟测试][osa-smoke] · [Rust 契约测试][osa-rust-tests] · [上游演示说明][osa-demo]

## open-super-agent 实现到了哪一层

| 能力 | 实际调用与持久边界 |
| --- | --- |
| 主对话与后台派工 | Router 写入用户消息，加载当前对话及任务状态，决定直接回答或调用 `dispatch`。派工入库时用派发消息 ID 作任务 ID，后台启动 Worker，结果、附件沿同一聊天 ID 回传。Worker 从目标、角色指引和相关长期记忆开始自己的工具循环。 [Python Router][osa-intake] · [Rust 派工][osa-dispatch] · [Worker 循环][osa-worker] · [Python 回传][osa-result] |
| 多 Bot 协作 | 群内由明确提及的 Bot 或协调者发言。`handoff` 安排另一 Bot 下一轮接手。`dispatch(handoff_to)` 先登记等待，成功后持任务 ID 写交接消息并唤醒接手者，执行失败会反馈并结束该交接。群外 `message_bot` 写私信，回信投回发起对话。群轮次及私信链深有上限。 [群调度][osa-group] · [完成后交接][osa-handoff] · [Bot 私信][osa-dm] |
| 并行研究与结果 | Heavy 模式在同一个任务 ID 下并行启动多份独立草稿，过滤抛错的分支，交给无工具 Reviewer 合成。即便草稿全失败也会进入合成。Worker 结束工具调用后得到文本，正常返回即可进入 `done`，完成判定到执行返回为止。 [Heavy][osa-heavy] · [Rust 结算][osa-rust-result] |
| 续办与重启 | 对话讨论串由用户针对一条根消息创建。普通新消息进入 Router，活动任务可查询和取消，冲突指令的默认做法是停止后重派。聊天中保存任务派发、状态、结果和步骤，运行对象与审批等待在内存，启动时把旧进行中记录标为中断。 [讨论串][osa-threads] · [路由指引][osa-router-prompt] · [任务 API][osa-task-api] · [Python 重启处理][osa-restart] · [Rust 重启处理][osa-rust-restart] |
| 事件与定时 | 定时、Webhook 和轮询经过规则过滤及可选模型过滤，最后复用后台派工。轮询状态和运行记录入库，同一规则运行期间待处理的事件排在内存，长度有上限。按配置能监测 GitHub 仓库的新发布、新提交、新 Issue 或新 PR。 [事件派工][osa-events] · [规则运行][osa-routine] · [GitHub 轮询][osa-github] |
| 用户看得到的过程 | 任务卡挂在派工当轮的回复下，显示状态、当前动作和接手人。点开详情可看工具步骤，Heavy 可按 Worker 筛选。结果和交接保留在会话时间线。 [任务卡][osa-card] · [回合关联][osa-timeline] · [任务详情][osa-detail] |

## NeuxBot 的具体差距

### 同一位伙伴在三个入口使用不同的协作机制

在集成工作树中，普通私聊与 Superagent 共用聊天发信接口。持续任务接纳器先检查输入，未接管时由 Chat 直接启动 Execution。新候选的准入明确检查 `SuperagentWakerID`。普通伙伴可以受委派执行持久任务，但它在自己私聊里收到新请求时，无法像独立 Super 那样自然取得同一套协调能力。源码入口为 `server/apis/chats/send_message.go` 的 `Output` 和 `server/services/superagent/agent_intake.go` 的 `prepareCandidate`。

团队另外保存 ChannelTask、ChannelTaskAttempt、补充输入和成员报告。负责人的工具包括 `team.tasks.create`，普通成员的团队协作工具只有 `team.reports.submit`。Superagent 则通过 `superagent.tasks.delegate` 创建子 Ticket，以 Assignment 和 `superagent.tasks.report` 记录执行和结果。因此，同一位伙伴在团队里收到任务后，即便团队希望允许它继续委派，现有成员工具也缺少这项操作。两边最终调用同一个 Execution，派工、续办及结算仍由两个领域分别实现。 [团队现有职责](../architecture/teams-cn.md) · [团队工具绑定](../../server/services/channel/agent.go) · [团队任务模型](../../server/repositories/models/channel_task.go)

目标是让普通伙伴、独立 Super 和团队成员共用伙伴接单、委派、续办、报告与原路交付的机制。独立 Super 可以默认承担协调职责。团队负责成员范围、负责人、发言顺序、依赖和验收策略。谁有权继续委派取决于本次授权。以 Lumi 为例，私聊里的委托由结果投回原私聊，团队里的委托按团队策略直接交付或先由负责人汇总，两种入口都能从同一类任务记录查到执行者和交付去向。上游可参照同一个 Bot 的私信唤醒和群内接力。 [私信回传][osa-dm] · [群内接手][osa-handoff]

### 主对话和长任务共享执行位置

当前已把 `ChatID` 作为公开入口与回传目的地，把 Ticket、Assignment、原生会话分别记账。未明确指定目标的本机输入创建候选 Ticket，等待 Super 判断直接处理、接受为任务或挂接旧任务。候选的 `ExecutionChatID` 直接取主聊天 ID。Super 自己接受候选后仍在同一聊天执行。`Ready` 遇到该 Chat 已有活动执行时会推迟领取，后续未引用消息可以先入库，但其候选执行需等这个位置释放。伙伴委派已有独立执行位置。任务级 `NativeHistoryScope` 约束历史恢复范围，执行接纳仍按 Chat 检查占位。 [NeuxBot `intake.go:293`、`candidate.go:25`、`executor.go:54/129`、`service.go:839`][neux-thread]

这是 open-super-agent 值得借鉴的后台边界。主控短轮处理用户意图，耗时工作异步运行。对 NeuxBot，这条路径也适用于 Super 自己承担的长任务，任务责任仍归 Super。这样用户等行程时可以继续问新闻。同一聊天的简短回答、旧任务续办和新派工各自有清晰结果。验证点是长任务明确占位期间发入独立消息，能在它释放前收到普通答复，同时原任务的输入、工作版本和尝试数保持一致。 [上游 Router 互斥][osa-intake] · [后台派工][osa-dispatch]

### 路由和续办以 DSH 的持久归属为参照

DSH 将源事件、话题的责任订阅与背景订阅、接单或挂接决定、任务和执行尝试分别建模。已归责的输入进对应任务，可按原身份归还。没有责任者的输入才进入协调判断。open-super-agent 的主聊天历史、人工讨论串和一次性 Worker 适合并行做新事，Router 的公开任务接口围绕列表、详情和停止。NeuxBot 已有显式引用核验、待答问题、候选挂接、关键词查旧任务，以及按输入和任务记录退回。本轮源码显示上一个任务的存在本身已经不足以自动选中它。 [DSH 收件和订阅][dsh-intake] · [DSH 路由命令][dsh-route] · [NeuxBot 原型对照会话][neux-thread]

“按原任务续办”的验收应读同一条输入的权威决定和实际消费。新闻标题对应直接回应。改成都行程对应挂接原 Ticket，且只把新增要求交给该任务。在主聊天答“上海”对应原待答问题及真正提问的伙伴。多条候选都成立时，先问用户一句。原会话记录了真实 Codex 的两条场景证据，11 月 19 日较晚的一批改动以模型替身验证为主。 [NeuxBot 原型对照会话][neux-thread]

### 执行、业务结论和公开交付各有证据

NeuxBot 已按执行围栏记录报告，等待分支释放和汇合，缺失报告进入待处理，并在业务结算时登记发件箱投递。未知消费留待核验。比较源码时，这比 open-super-agent 的进程内后台任务和 `done` 判定多覆盖了重启、重试身份及子树汇合。当前仍有两条公开结果路径，同会话已有原生答复时沿用该答复，其他情况以持久回执补投。多级协作应逐级核对子结果、父实际消费、最终业务结论、原主聊天的消息 ID 及产物身份。 [NeuxBot `superagent_controller.go:545/622/678`、`superagent_delivery_controller.go:16`、`delivery.go:12`][neux-thread] · [上游结算][osa-rust-result]

11 月 18 日 build09 正式矩阵为 **43/63 通过**，复杂场景 **10/27 通过**，关键 C04 多级委派 **0/3**，三次都报结果路由失败。4 条 `falseCompletion` 是待独立归因的评分信号。11 月 19 日会话记录了后续修订、针对性的真实 Codex 演练，以及最新修订的替身模型和 HTTP 检查。指标归属于原冻结构建，后续记录覆盖的范围以实际场景为准。

## 取舍

NeuxBot 的架构目标是统一伙伴协作。借鉴 open-super-agent 的短主控轮、后台执行与具名交接，用 DSH 的输入归属及持续任务支撑续办。共享执行机制还须覆盖两个具体场景：伙伴自己负责的长任务能释放主聊天执行位置，多级委派能从公开交付逆查到各级实际消费的报告与产物。界面沿用用户指定的会话摘要和右栏。 [NeuxBot 原型对照会话][neux-thread]

[osa-demo]: https://github.com/kadaliao/open-super-agent/tree/c217b8e0dcaaa40bb865febad2deeba483d0e1cd#看看长什么样
[osa-compose]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/compose.yaml#L5-L15
[osa-docker]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/Dockerfile#L45-L55
[osa-desktop]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/desktop/scripts/build-server.sh#L15-L24
[osa-python]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L719-L738
[osa-intake]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L318-L347
[osa-dispatch]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/router.rs#L856-L896
[osa-result]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/router.py#L765-L805
[osa-worker]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/worker.py#L248-L278
[osa-group]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/groups.py#L106-L138
[osa-handoff]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/groups.py#L141-L156
[osa-dm]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/dm.rs#L54-L121
[osa-heavy]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/worker.rs#L412-L440
[osa-rust-result]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/router.rs#L984-L1037
[osa-threads]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/bots.py#L221-L238
[osa-router-prompt]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/prompts/router.txt#L40-L59
[osa-task-api]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/server.rs#L505-L507
[osa-restart]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/tasklog.py#L156-L163
[osa-rust-restart]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/server/crates/osa/src/tasklog.rs#L277-L285
[osa-events]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/events.py#L341-L373
[osa-routine]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/routines.py#L375-L410
[osa-github]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/osa/sources/github_repo.py#L224-L239
[osa-card]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/web/src/components/Chat.tsx#L293-L318
[osa-timeline]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/web/src/components/Chat.tsx#L679-L707
[osa-detail]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/web/src/components/Tasks.tsx#L112-L179
[osa-smoke]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/tests/test_smoke.py#L10-L55
[osa-rust-tests]: https://github.com/kadaliao/open-super-agent/blob/c217b8e0dcaaa40bb865febad2deeba483d0e1cd/contract/test_smoke.py#L39-L76
[dsh-rev]: https://github.com/deepseek-ai/dsh-superagent-prototype/tree/8c32a9dc5d72c033816c6a8110b37f40cf94fba4
[dsh-intake]: https://github.com/deepseek-ai/dsh-superagent-prototype/blob/8c32a9dc5d72c033816c6a8110b37f40cf94fba4/src/core/intake.ts#L19-L60
[dsh-route]: https://github.com/deepseek-ai/dsh-superagent-prototype/blob/8c32a9dc5d72c033816c6a8110b37f40cf94fba4/src/app/tools/main.ts#L88-L128
[neux-thread]: codex://threads/01a1eddb-7986-7132-bd44-7f73edd91cfd
