# 15 会话交办的工作流交还整图结果

## 用户问题

用户在普通会话里交办一张含 Agent 和人工确认步骤的工作流。Agent 回合结束后流程可能仍在等批准，用户此时需要知道待处理的步骤。用户从画布直接运行同一张图时，希望在运行详情查看结果。

## 解决方案

运行接纳时保存实际发起入口及可选的原会话和原消息。每个需要派工的步骤等自己的结果，人工决定只处理对应步骤。整图成功、失败或停止后，有会话交办的运行向原处交一份结论，画布手动运行在自己的详情中收尾。

## 用户故事

1. **15.1** 我在主会话或普通会话交办流程后，可以去做别的事。流程终态回到最初那条交办，带结论及可打开的运行。
2. **15.2** 我能处理流程的原审批。Agent 步骤结束或一个步骤发送产物后，仍能看见整张图有哪些工作未完成。
3. **15.3** 我从画布启动纯脚本或含 Agent 步骤的流程，能在画布查看进度、产物和停止原因，无需选择收件人。

## 实现决定

复用 Workflow 的不可变 Revision、Run、步骤实例、Effect 与已冻结的 worker 配置。现有普通 Chat 已保存来源消息及执行，有终态成果补交与手动工作台隔离。接入第 07 项的会话投递并关联已有的运行详情时，为来自主会话的交办保留其公开地址和原消息，隐藏工作 Chat 继续保存执行证据。[运行记录](../../../server/repositories/models/workflow_run.go) · [现有聊天交付](../../../server/services/workflow/chat_delivery.go) · [聊天与工作台隔离](../../../server/services/workflow/chat_delivery_isolation_test.go)

Agent 步骤按稳定的步骤实例登记一次投递，保存 delivery 与实际执行的绑定。通知只唤醒对账，按绑定读取真实终态，校验当次产物及 Schema 后再推进依赖。已有 Effect 足以标识一次投递时复用它。人工决定核验原 Run、Step、待决状态和回答资格。需要委托伙伴或团队的步骤沿用第 09 项受托协议，由步骤接回结果。工作流持有运行结论及步骤推进权。[现有 Worker](../../../server/services/workflow/worker.go) · [人工决定](../../../server/services/workflow/human.go) · [OpenBot 步骤派工](https://github.com/nightly-labs/openbot/blob/100c60e893eda3ce4761b4d10001a8e490c43536/src/backend/workflows/workflows.ts#L508-L547) · [重启对账](https://github.com/nightly-labs/openbot/blob/100c60e893eda3ce4761b4d10001a8e490c43536/src/backend/workflows/workflows.ts#L1485-L1534)

步骤产物按“步骤实例＋目的地”发送。整图终态在同一事务登记“运行＋结论用途＋收件人”的结果意图。如果一个输出节点本来就在为同一收件人准备整图结论，保留其内容，到终态统一发送。有权发出的其他步骤产物照常投递。拒绝审批或失败先于输出节点发生时，原交办仍能收到停止或失败原因。画布手动运行没有自动会话回址。[目标交付地址](../superagent-conversation-architecture-cn.md#各入口的交付地址) · [OpenBot 运行终态](https://github.com/nightly-labs/openbot/blob/100c60e893eda3ce4761b4d10001a8e490c43536/src/backend/workflows/workflows.ts#L1453-L1482)

## 测试与人工验收

- **先自动化。** 比较主会话交办、普通会话交办及画布手动三种入口。对纯脚本与含 Agent 图覆盖成功、失败和停止。逐项断言最终状态、产物、实际收件位置和终态消息数，画布均为零，有交办的均为一。
- **在故障点验证。** Agent 步骤已产出、消费通知前重启，期望原步骤从同一投递对账一次。格式不合法的结果须经过现有校验或修正规则，仍不合格时不得推进下游。对错误步骤或已经处理的审批重放决定，原决定保持有效。拒绝批准时输出节点可未执行，原会话仍收到一份说明。测试同收件人整图内容和不同目的地步骤产物不会重复送达。
- **再由用户桌面验收。** 从普通会话交办含人工确认的测试流程，分别批准、拒绝两次独立运行，从回件进入对应运行。再从画布启动同一流程，核对产物与会话消息数。记录运行、步骤、原交办和回件。

## 不在本项范围

独立工作流定时回报的收件人选择见第 16 项。事件等待某个外部 CI 变化见第 14 项。此处沿用现有工作流语言和画布编辑能力。

## 实施与验证记录

- 接纳保存主会话公开地址及原交办。终态运行行与未结算的回件条件一起提交。后台扫描按运行 ID 恢复；普通来源复用原回合投递，主会话回件与 SQLite 送达收据一起提交。手动画布运行保留详情。主会话原交办与回件可打开确切运行。
- `TestWorkflowConclusionReturnsOnceToOrdinaryChatAndNeverToCanvas` 对脚本成功、失败、停止分别验证定向投递及重复扫描。定向验证同时覆盖原回合补写、worker 最终消息对账、无权威输出时的拒绝及格式修正预算，与原人工回答及操作许可用例。
- `TestWorkflowAgentResultStillWaitsForOriginalApprovalBeforeChatConclusion` 通过 Core 和真实聊天日志验证 Agent 节点结束后仍保留原审批。批准和拒绝均在正确阶段结算，错误步骤不能作答，重复补交不会产生第二份结论。 `TestWorkflowRestartReconcilesTheFrozenWorkerBeforeOpeningApproval` 重开 SQLite 后按原 Effect、Chat 与 Execution 消费一次已有执行结果，随后进入原审批。
- `TestWorkflowOriginAndConclusionStayWithTheOriginalMainInput` 与 `TestWorkflowApprovalReturnsToTheOriginalPublicInput` 验证公开交办及同键重投使用原主会话地址和实际运行标识，隐藏来源没有第二份结论。
- 定向前端测试 `main-conversation-message.test.tsx` 与 `chat-workflow-timeline.test.tsx` 共 20 项通过。Node 25 以 `NODE_OPTIONS=--no-experimental-webstorage` 使用测试环境提供的存储实现。
- 现有 `worker.resolve` 接受 `{ kind: "waker", wakerId }`，运行接纳从伙伴仓储核验该 ID，节点编辑器也使用伙伴目录。编译器对 `kind: "team"` 返回 `RESOLVE_STATIC_REQUIRED`。团队和第 09 项受托工单作为步骤目标需要扩展工作流语言。已有伙伴步骤通过 Effect 和冻结配置接纳与对账。步骤投递可使用独立的 IM 目的地，步骤级主会话回报与整图合并也需要新增步骤目标。桌面验收待执行。[编译器](../../../server/internal/workflow/dsl/compiler.go#L952) · [画布契约](../../../server/internal/workflow/dsl/canvas_schema.go#L94) · [执行接纳](../../../server/services/workflow/runs.go#L561)

## 接手说明

以现有聊天成果补交和工作台隔离测试为起点，先验证整图与步骤的两个交付时机，再接入主会话和跨主体步骤。已有同等行为要保留并复用。交付时区分自动化与桌面验收结果。完成本项后停止，后续规格另行启动。
