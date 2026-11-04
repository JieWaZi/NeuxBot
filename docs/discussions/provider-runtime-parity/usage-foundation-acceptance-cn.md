# 用量基础本地验收

本记录只验收原生用量快照、宿主透传和现有日志边界。总体服务商能力对齐仍在实施中；支持矩阵和字段权威说明见[智能体与账号](../../architecture/runtimes-and-accounts-cn.md#用量快照)。

## 基线与实现

| 仓库 | Task 3 基线 | 已实现修复 |
| --- | --- | --- |
| NeuxBot | `c4cd6685d19f0d8d16733186f14bb1c206d27db1` | `87476044` 用量贯通；`bdcc1dbd` 最终复制与预算；Task 3 补现有边界测试及文档 |
| acp-go | `485d6a7f1ad39b294761029febab019e1b38bb94` | `82785d48` 原生快照；`485d6a7f` 回执边界与部分 wire；`af5c952` JSON 精度 |

两个仓库均在 `codex/provider-runtime-parity` 本地联调。NeuxBot 的 `server/go.mod` 仍固定旧 acp-go `a58342da0ddd`，新增 API 依赖由仓库外临时 `go.work` 提供；acp-go 自身测试使用 `GOWORK=off`。没有提交绝对路径 replace，脱离 workspace 的依赖集成、远端版本固定及发布均未完成。

## 已有证据与新增缺口

下列已有用例来自 Task 1/2 及其修复轮，实施报告记录了通过输出，Task 3 不重复运行未变化用例：

- acp-go 各 Provider `TestUsageFoundation*` 与 `TestUsageFix*`：Codex 多请求累计与回退、Claude 消息及 steering 结算、Cursor hook 缺失字段、Kimi 同源替换与截断后失败/取消、Pi 部分请求；相关包和修复轮 race 有定向证据。
- [native connection 测试](../../../server/internal/agent/native/connection_test.go) `TestUsageFoundationTerminals`、`TestUsageFoundationFinalSnapshotReplacesPartial`：真实 ACP SDK 管道验证部分通知、最终替换、无上下文占位以及错误/取消/交付失败后消费保留。
- [Session 集成测试](../../../server/internal/agent/native_integration_test.go) `TestUsageFoundationFinalObserverTerminals`、`TestUsageFinalFixObserverOwnership`、`TestUsageFinalFixMetadataBudget`：最终副本、取消后交付、单事件/累计预算和原停止状态；普通与 race 定向验证已通过。
- [Execution sink 测试](../../../server/services/execution/runtime_events_test.go) `TestUsageFoundationArchiveMergesComponents`：success/failure/canceled 终态保留原始元数据、回放数字与上下文，旧格式不补确定性。

Task 3 新增：

- acp-go `TestTurnUsageJSONIntegerPrecision` 通过真正的 JSON 解码入口重现 `9007199254740993 → 9007199254740992`。修复前默认边界和舍入用例均失败；修复后拒绝 float64 大于 `2^53-1`，`int64` 及 `json.Number` 仍准确接受 `2^53+1` 和 int64 上限，越界、负数和分数拒绝。
- [Execution sink 测试](../../../server/services/execution/runtime_events_test.go) `TestUsageFoundationRepeatedFinalSnapshotArchivesOnce`：同一 invocation 的 `100/10 → 250/30 → 250/30` 只替换快照；中间观察不进入 JSONL，单次合法终态归档和回放仍为 `250/30`，保留来源与完整性。
- [Chat journal 测试](../../../server/services/chat/journal_test.go) `TestUsageFoundationForkPreservesOriginWithoutNewConsumption`：父回合有真实已归档用量，fork 权威继承快照保留直接来源 Chat 和消息 ID，子展示不带 RunMetrics，子日志不产生执行或消费终态。

新增 NeuxBot 用例验证当前已经正确的行为，没有为取得 RED 临时改动生产逻辑。这里的“单次归档”限定正常执行只收尾一次、观察者可重复交付最终快照；没有把第二次任意调用 `CommitTerminal` 当成已实现的幂等接口，也没有新增统计聚合器。

## Task 3 实际检查

```sh
# acp-go：独立依赖，精度 RED 后 GREEN 及同文件回归
GOWORK=off go test ./pkg/acpmeta -run '^TestTurnUsage' -count=1

# NeuxBot/server：外置 Go workspace，仅新增缺口
go test ./services/execution ./services/chat -run '^TestUsageFoundation(RepeatedFinalSnapshotArchivesOnce|ForkPreservesOriginWithoutNewConsumption)$' -count=1

# NeuxBot 根目录
node scripts/check-docs.mjs
git diff --check
```

上述定向 Go 测试与文档检查通过。未重跑未修改的 native 包、已有 failure/cancel 回放用例或全量检查。

## 公开终态交接修复

整体审查发现最终用量事件触及预算或被观察者拒绝时，公开 Run 未把 Session 已保留的终态消费交给 Execution。公开 `Result.Usage` 现承接同一最终事实的独立副本；Execution 收尾以该快照归档，非 nil 的空快照不补旧观察，nil 仅兼容旧 Runner。该修复的实施验证已完成，独立复审尚未完成。

- [Session 集成测试](../../../server/internal/agent/native_integration_test.go) `TestUsageTerminalPublicResult`：真实 Manager → Prepare → Bind → 公开 Run → ACP SDK pipe；覆盖仅最终 `100/10`、部分 `100/10` 后最终 `250/30`、成功/取消/原生故障，以及事件数量、单事件字节、累计字节和观察者拒绝。公开终态仍保存来源、完整性、字段 presence、已知零与未知字段；消费与上下文分别保存，Prompt 仅一次，事件不突破预算。
- 同文件 `TestUsageTerminalResultOwnership`：修改公开结果的数字、字段集合、完整性及上下文不能改写原 Session 终态或下一次读取；公开结果与事件接收副本互不共享。
- [Execution 集成测试](../../../server/services/execution/lifecycle_usage_test.go) `TestUsageTerminalExecutionArchive`：真实 Start、公开 PreparedTurn.Run、Execution sink、Journal.CommitTerminal 和 JSONL 回放，验证最终事件未交付时依旧归档最终消费；原生失败和取消事实保留，正常生命周期只提交一个终态。
- [Execution sink 测试](../../../server/services/execution/runtime_events_test.go) `TestUsageTerminalArchiveContract`：显式空终态覆盖旧消费；只含 turn 或 context 的终态不能从实时观察补另一分量；nil 旧 Runner 保留既有兼容语义。序列化不共享结果指针，也不改写流式观察。

本轮只运行新增用例、既有最终预算/所有权及直接受影响的 Execution 收尾用例；保留 Task 1/2 已有证据，没有重跑原生 Provider suite 或全量测试。普通、race 定向测试与文档检查通过：

```sh
# NeuxBot/server
go test ./internal/agent ./services/execution -run '^Test(UsageTerminal.*|UsageFinalFix.*|ManagerFinalUsageStillOwnsCurrentTurn|ChatFinalUsageDeliveryFailureDoesNotReplayPrompt|ExecutionOutcome.*|FinishExecutionReleasesReservationAndPublishesDurableTerminal)$' -count=1
go test -race ./internal/agent ./services/execution -run '^TestUsageTerminal' -count=1

# NeuxBot 根目录
node scripts/check-docs.mjs
git diff --check
```

## 剩余边界

原生支持由本地 fixture、测试进程替身及 SDK pipe 证明，真实 CLI、账号、模型、操作系统和产品视觉均未验收。Claude 不含 subagent 的完整性、Pi 缺稳定请求身份、Codex 未知 epoch 等已知范围继续保留。无元数据历史仍未知，现有消息指标没有完整性展示。

全局 receipt 身份去重、跨 Chat/团队/工作流聚合、费用、额度、安装与模型能力及界面属于后续阶段。当前 fork 保留直接来源身份且不产生新消费事实，不证明跨分叉链的全局计费去重已经实现。没有推送、合并或发布。
