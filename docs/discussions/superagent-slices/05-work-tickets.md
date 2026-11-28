# 05 一个长期交办可以按原工单续办并回报

## 用户问题

同一项目中的两个长期交办可能复用一个工作 Chat。仅凭会话和 CLI 回合，无法区分各自的责任、补充输入与结论。

## 解决方案

明确交办的一件事由一张 Ticket 持续跟踪；用户在原会话询问、补充或停止该项工作，得到相应结果。简单问答直接回复。

## 用户故事

1. 作为用户，我希望在相同目录交办两件事后能分别跟进。
2. 作为交办人，我希望一个 CLI 回合退出后仍可继续同一件事。
3. 作为从普通会话交办的用户，我希望结果仍交还给当时交办的会话。
4. 作为负责人，我希望区分等待输入、受阻、已交付结论和人为停止，以便只续办允许继续的工作。

## 实现决定

- Ticket 保存来源、授权、责任主体、输入和结论。Assignment 保存某次执行尝试，允许跨回合。执行的成功终态由既有宿主管理，业务结案由工单管理。
- 同一接纳保存授权与首代尝试，当前尝试才能结算。显式引用追加到原单，重新接纳使用现有收据。可重开的工单重验原授权，人为停止撤销授权。
- 普通会话和主会话按同一主体模型处理。复用消息身份、执行和工作记录，按责任边界创建工单，直接回复可以零工单。

## 测试与人工验收

1. 从同一普通会话给同一项目交办 A 与 B。分别补充、结束 A 后查看原交办，断言 B 仍然开放且不含 A 的输入。重复提交 A 的同一请求只得到原收据。
2. 覆盖简单问答、一次任务跨两轮、迟到的旧报告、已停止后收到补充、原授权生效时允许重开。用既有执行测试宿主观察实际历史与报告。
3. 桌面开两件可区分的工作，逐条引用跟进，再停止其中一件并查看原会话与工作记录。

## 不在本项范围

跨主体委派、外部事件订阅、独立工单管理页面及故障后的异步补件。

## 接手说明

依托现有主会话、普通聊天与执行宿主。本项状态见[开发与验收索引](README.md)。

## 实现对照

| OpenSuperAgent 固定版本行为 | NeuxBot 对应行为 |
| --- | --- |
| 派工消息 ID 直接用作 Task ID。Worker 沿用该 ID，并保存接单 Bot 和发起聊天。[派工](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/server/crates/osa/src/router.rs#L860-L899) · [过程记录](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/server/crates/osa/src/tasklog.rs#L68-L94) | Go 用原交办消息作为工单 ID，接纳时保存原会话与冻结授权。尝试按工单和整数代次定位。原有 Chat 和执行继续保存过程。 |
| Worker 结果带原 Task ID 写到发起聊天。停止按同一个 ID 定位任务。[报告](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/server/crates/osa/src/router.rs#L1018-L1049) · [停止](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/server/crates/osa/src/router.rs#L1052-L1073) | 由 NeuxBot 的 `work.tickets.report` 提交本轮报告。Go 核对当前代次与执行，沿原会话回报。引用通过已接纳输入和已交付报告找到原单。 |
| 聊天中按任务展示状态、结果、过程和停止操作。[任务卡](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/web/src/components/Chat.tsx#L293-L320) · [结果消息](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/web/src/components/Chat.tsx#L340-L363) | 操作跟随原消息，沿用引用、聊天过程与来源工作记录。 |

工单跨回合与重开参考 [DeepSeek 工单模型](https://github.com/deepseek-ai/dsh-superagent-prototype/blob/8c32a9dc5d72c033816c6a8110b37f40cf94fba4/src/core/model.ts#L277-L307)。回报以原请求为回址，按原调用查收据，结算核对当前尝试。借鉴 [OpenBot 回件](https://github.com/nightly-labs/openbot/blob/100c60e893eda3ce4761b4d10001a8e490c43536/src/backend/channel-request-returns.ts#L103-L139)和[当前代次判断](https://github.com/nightly-labs/openbot/blob/100c60e893eda3ce4761b4d10001a8e490c43536/src/backend/channel-service.ts#L2818-L2850)。工单通过结构化 CLI 命令回报。主会话可以补交经核验的原生最终正文，规则见[聊天与执行](../../architecture/chat-and-execution-cn.md#长期交办)。参考 [Nuphos 显式发布](https://github.com/nuphos/nuphos/blob/2c2f2e27fab0a9098de430734f05079680ff71c8/apps/backend/src/lib/agent/channel-chat.ts#L123-L169)及[任务报告](https://github.com/nuphos/nuphos/blob/2c2f2e27fab0a9098de430734f05079680ff71c8/apps/backend/src/lib/agent/task-reports.ts#L44-L139)。

## 验证记录

- 现有执行宿主配合测试 CLI 验证同一 Chat 和项目内的 A/B 隔离、主会话原址回报、已结案工单重开、原授权变化、停止后拒绝续办，以及旧回合凭据在下一轮被拒绝。覆盖普通回答、同一提交键重试、已发布报告的回执对账和原执行终态恢复。
- Node 22 下运行 11 个直接相关的前端测试文件，344 个用例通过。覆盖普通和主会话交办、来源工作记录、报告引用、停止确认，以及工单读取失败时就地重试。
- 桌面验收待用户执行。通过 [06 主会话派工](06-router-project.md) 在同一项目先后交办 A 和 B，分别从原消息菜单打开工作记录。引用 A 的回报补充一项要求，核对工作记录与公开回报的对应关系。工单终止和授权撤销通过定向 Go 测试验证。
