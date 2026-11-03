# 全局用量与费用统计 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立覆盖聊天与独立任务、删除聊天仍保留消费、明确未知与部分覆盖的全局 Token 和费用统计。

**Architecture:** 在现有 Session 唯一原生调用收尾冻结不可变 receipt；聊天以 invocation.finished 为权威，独立调用、删除前归档和日志失败恢复归档共用最小 usage journal。SQLite 只做这些记录的单表幂等查询索引；费用独立保存原生来源与保守估算，界面只消费生成的聚合 API 和现有全局事件连接。

**Tech Stack:** Go、acp-go、JSONL、GORM/SQLite、math/big、ginx/toolx、现有 OpenAPI 和全局 WS。

**Spec:** [总体规格](../specs/2026-11-03-provider-runtime-parity-design.md)。前置：[用量基础计划](2026-11-03-provider-usage-foundation.md)、[模型能力计划](2026-11-03-provider-model-capabilities.md)；证据：[用量审计](../../discussions/provider-runtime-parity/usage-audit-cn.md)、[费用来源](../../discussions/provider-runtime-parity/usage-pricing-cn.md)。界面消费见[UI计划](2026-11-03-provider-ui.md)。本文是待实施计划，不代表能力已交付。

## Global Constraints

- 只在独立工作区实施，保留无关修改；实现和审查分开，子代理禁止继续派生。
- 不增加第二个 agent.Manager、执行循环、账号事实或可变用量来源。Chat JSONL 保持权威；execution终态Usage只是消息兼容投影，不进入新统计加法。
- “字段未知与真实零不同，保留缺失状态。”；“推理 Token 作为输出的子集单独记录，绝不再次加到输出上。”；Context不进入消费。
- “成功、失败、取消均可有消耗。”；历史重放、fork继承、终态重提和归档不产生新消费，实际新Invocation才计数。
- “订阅 Token 的 API 等价估算不是实际账单。”；“不同币种不直接相加。”；金额用精确十进制/有理数，不用float64累计。
- 冻结runtime/account/model/归属，不存Prompt、密钥、认证环境、URL、路径或整个InvocationFacts；实际模型/tier不能拿用户选择值补造。
- SQL单表、有界日期和批次、稳定分页；金额不得用SQLite REAL求和，不加载全历史到内存。不启用WAL、不设置额外连接参数。
- 三种耐久保证：未落盘receipt仅进程内可补写，崩溃后unknown；JSONL/journal任一耐久可重建索引；两源同ID/hash幂等，异hash拒绝。没有原生证据不能恢复未耐久Token。
- 原生费用与Token独立scope/coverage/complete；缺baseline、发行方、实际模型、tier、TTL或逐请求阈值时不可估算；历史消费无法证明适用价格时不得回填。未知CLI保留partial，不开展无限来源研究。
- 不复制OpenBot代码/价表；枚举、错误、API只通过对应命令生成。所有新增或修改Go结构体的全部字段有准确中文注释。
- 仅定向验证，不运行全量检查。此计划不授权真实账号/CLI/模型消费或浏览器、桌面操作；真实平台和视觉证据单列。任务完成不等于总体项目完成。

## Review Focus

- 日志失败后仍有真实消费：既有finishExecution可能直接return；never-durable receipt不能靠recovery凭空恢复。Task 2覆盖两源皆失败及进程崩溃。
- 同源重放、删除失败源与归档并存、fork和retry同时出现：按receipt身份计数一次，不按消息副本或execution最后快照计数。Task 2/5覆盖。
- 原生累计金额恢复历史或重置：首样本无baseline不归当前轮，重复通知不相加，Token完整不证明费用完整。Task 3/4覆盖。
- 同轮多模型、长短请求或混合缓存TTL：turn合计不能推断逐请求价项，原生partial与完整API估算不能相加。Task 4覆盖。
- 并发导入、日期/DST、分组跨页：各图表读同一snapshot，准确总量不能变成有限预览，未知不能显示0或免费。Task 5/6覆盖。

---

## 文件与职责

| 范围                                                                                    | 职责                                                                          |
| --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `server/internal/usage/{receipt.go,journal.go,pricing.go}`（新增）                      | 消费值/校验、最小耐久journal、独立精确估算；不导入agent/services/repositories |
| `server/internal/agent/{contract.go,session.go,agent.go}`                               | 同一Session收尾采集与待确认引用，复用原清理所有权                             |
| `server/services/execution/{lifecycle.go,handoff.go}`                                   | 每Invocation调用事实、隐藏交接消费、日志失败归档                              |
| `server/services/usage/recorder.go`（新增）                                             | 生产者/删除/恢复共用的幂等记录和投影，不包装单入口查询                        |
| `server/repositories/{models/usage_receipt.go,controllers/usage_controller.go}`（新增） | 可重建索引、单表有界查询与技术cursor                                          |
| `server/apis/usage/{usage.go,get_usage_summary.go,list_usage_groups.go}`（新增）        | 路由及两个独立Operator；聚合HTTP DTO留在API包                                 |
| acp-go `pkg/acpmeta/cost.go`（新增）及Claude/Pi现有用量文件                             | 来源费用观察，不持有NeuxBot业务或价格目录                                     |

测试同职责文件组织，不因单个helper建立碎片文件。后续任务的接口以本计划为准；foundation/model计划已经实现的字段直接复用。

### Task 1: 不可变receipt合同与最小usage journal

**Dependencies:** 用量基础阶段完成；不依赖统计数据库。

**Files:** Create `server/internal/usage/receipt.go`、`receipt_test.go`、`journal.go`、`journal_test.go`；Create constants下UsageWorkKind、UsageOutcome、UsageUnpricedReason等按类型独立枚举文件；Modify `server/constants/status_error/error.go`及对应生成输出。

**Interfaces:**

- `usage.Attribution{Kind,ChatID,ExecutionID,WakerID,TaskID}`；Kind支持chat、knowledge_generation、knowledge_maintenance、workflow_output、other，没有伙伴证据时WakerID为空。
- `usage.Receipt`：ID/SchemaVersion/Attribution/InvocationID；RuntimeID/BaseRuntimeID/RuntimeVersion/AccountID/BillingProviderID；SelectedModel/ActualModel/ServiceTier；StartedAt/FinishedAt；Outcome/PromptAccepted/OutcomeUnknown；`Tokens *Tokens`、`Costs []Cost`、`Estimate *Estimate`。新ID直接使用32位InvocationID，不再生成第二个消费ID。
- `Tokens`的Input/CacheRead/CacheWrite/Output/Reasoning均为`*int64`，另有Source/Coverage/Model和`Complete *bool`；Context不在此类型。
- `Cost`包含Amount/Currency/Source/Origin/Valuation/Scope/CounterID/ObservationID/Coverage/Model/PriceVersion及`Complete *bool`；`Estimate`含`Costs []Cost`、CatalogVersion、CatalogCheckedAt、PriceSources、UnpricedRequests、Reasons。任务1仅定义合同，不计算金额。
- `ValidateReceipt(Receipt) error`、`CanonicalReceiptHash(Receipt) (string,error)`；`Dispatch`仅含InvocationID、Attribution、冻结runtime/account/model/provider及StartedAt，不保存任务状态机。
- `NewJournal(root string) (*Journal,error)`；`Begin(context.Context,Dispatch) error`、`Finish(context.Context,Receipt) error`、`Archive(context.Context,[]Receipt) error`、`ReadBatch(context.Context,JournalCursor,int) ([]Record,JournalCursor,error)`。

- [ ] **Step 1: 写RED。** `TestReceiptUnknownAndZero`断言nil与显式0往返不同、reasoning不另加输出、负数/非法金额/秘密字段不被接受；`TestJournalReceiptIdentity`断言同ID/hash幂等、异hash返回UsageReceiptConflict；`TestJournalDurability`覆盖append失败、Sync失败、取消、完整行损坏及未提交尾部半行恢复。
- [ ] **Step 2: 运行RED。** 在server运行`go test ./internal/usage -run 'Receipt|Journal' -count=1`，记录真实失败；先建测试和类型骨架，不以编译失败替代业务断言。
- [ ] **Step 3: 实现值与journal。** 应用数据根下独立usage目录按写入月份分段，record有schema/kind/ID/source/payload/hash，仅dispatch与receipt两种。完整行append+Sync后返回；按kind+ID判重，相同hash返回成功且不重复append，异hash拒绝覆盖；dispatch与receipt可共享原InvocationID，不能彼此视为冲突。单条最大64KiB，Archive每批最多256。ID定位技术索引可从journal重建，不能只靠内存去重。完整提交行损坏报告partial，不静默丢行。
- [ ] **Step 4: 定向GREEN。** gofmt修改Go，运行上述包测试及错误/枚举生成检查；证明重开journal后幂等和校验仍成立。
- [ ] **Step 5: 自审并提交任务范围。** 完成条件：未知/零、来源身份和耐久写入合同固定，journal不依赖Chat/Manager或数据库；记录实际测试和commit，不推送。

### Task 2: 每Invocation、独立任务及归档耐久集成

**Dependencies:** Task 1；foundation的最终用量快照合同。

**Files:** Modify `server/internal/agent/{contract.go,session.go,agent.go}`及相关测试；`server/internal/chat/eventlog/types.go`及codec/replay测试；`server/services/execution/{lifecycle.go,handoff.go,runtime_events.go}`及对应测试；`server/services/chat/{service.go,chat.go,journal.go}`及delete/journal测试；`server/internal/knowledge/model/{generator.go,maintenance.go}`、`server/global/{config.go,knowledge_model.go,workflow_output.go}`；Create `server/services/usage/recorder.go`、`recorder_test.go`。

**Interfaces:**

- TurnInput/TextRequest增加`UsageAttribution usage.Attribution`，不进入Session稳定计划/兼容摘要/原生输入；Result增加`UsageReceipt *usage.Receipt`。
- ManagerConfig注入`StandaloneUsage`窄端口（Begin/Finish与Task 1签名一致）。所有实际Prompt仍只有`Session.executeTurn`调用并冻结receipt，不在知识结果再次采集。
- `PreparedTurn.ConfirmUsageRecorded(receiptID string) error`仅确认本轮待耐久引用；重复确认幂等，错误ID为稳定错误。TextResult增加`UsageRecorded bool`及可选持久化诊断，不能把已成功模型调用变成可重试Prompt错误。
- `eventlog.InvocationFinishedData.UsageReceipt *usage.Receipt`是聊天规范来源；ExecutionFinishedData.Usage保持消息兼容投影，不另累计。
- Recorder实现Begin/Finish/Archive，与Journal相同入参；Chat Config注入`ArchiveChats(context.Context,[]string) error`回调。此阶段不依赖索引；Task 5再接提交后投影。

- [ ] **Step 1: 写RED。** `TestInvocationReceiptsAreNotExecutionSnapshot`：同execution两个调用100、150保留两条，兼容Usage不再加；`TestStandaloneUsageFailureDoesNotReplayPrompt`：Begin失败不Prompt、Finish失败Prompt仅一次、先释放账号资源再补写；`TestUsageSurvivesTerminalFailure`：取消/失败/Flush失败已有消费、隐藏handoff也归档且无用户消息。打开失败无消费不造零receipt；未知接纳单列unknown。
- [ ] **Step 2: 写耐久/删除RED。** 调用JSONL写失败→同ID/hash恢复归档；两源都有时幂等、异hash拒绝；两源皆失败保留同Session pending引用，重试只写日志。进程内补写成功与崩溃后只能indeterminate分别测试。删除成功/DB失败/Stage回滚/恢复/伙伴及团队删除均先归档再Stage；Archive失败源文件保持。fork继承不生receipt，retry新Invocation正常生receipt。
- [ ] **Step 3: 运行RED。** `go test ./internal/agent ./services/execution ./services/chat ./services/usage ./internal/chat/eventlog ./internal/knowledge/model -run 'Usage|InvocationReceipt|Handoff|Archive' -count=1`；只扩展实际涉及的具体用例，不运行全量。
- [ ] **Step 4: 实现唯一采集与持久化。** Session在Prompt返回、sink.finish/settle后冻结实际用量/配置，包含失败和取消。聊天由recordInvocationResult一次append规范receipt；该写失败则Recorder.Archive写同receipt恢复归档，任一耐久后Confirm。独立GenerateText/ExecuteTask在Prompt前Begin，结束以WithoutCancel+5秒预算Finish；Begin失败禁止提交输入。归属覆盖知识生成、维护、workflow output与无归属other，不猜伙伴。hidden handoff按独立Invocation归属父execution，不因隐藏而漏计。
- [ ] **Step 5: 实现未耐久所有权。** Session只保留同一frozen receipt的待确认引用；Close先释放真实原生/授权资源，再有界补写，不制造第二执行状态机。不forget尚未完成补写的owner；现Manager清理路径覆盖临时及聊天这两类已无活跃Turn的owner。取消不能阻止收尾。没有任何耐久源时，崩溃后只能从原dispatch/execution起始事实报告unknown；不声称可重建Token。
- [ ] **Step 6: 实现删除前归档并GREEN。** Chat操作锁与既有后台owner等待后、Stage之前分批Archive；文件I/O不进入SQLite事务；删除失败留下归档无害。运行Step 3及具体delete/fork/retry回归；gofmt、自审并提交。完成条件：不重发Prompt、不丢已耐久消费、不把进程内pending误报为可恢复历史。

### Task 3: acp-go独立原生费用观察

**Dependencies:** Task 1的费用值合同；保留既有Token foundation行为。

**Files:** Create acp-go `pkg/acpmeta/cost.go`、`cost_test.go`；Modify `pkg/claude/protocol/protocol.go`、`events.go`、`session.go`、`usage.go`及对应测试；Modify `pkg/pi/{usage.go,events.go,prompt.go}`及对应测试。标准ACP生成结构不手改。

**Interfaces:**

- `acpmeta.CostObservation`包含amount/currency/source/origin/valuation/scope/counterID/observationID/coverage/complete/model/priceVersion；金额规范十进制字符串，缺失与"0"不同。`WithCostObservations(map[string]any,[]CostObservation) map[string]any`、`ReadCostObservations(map[string]any) ([]CostObservation,bool)`；独立key `acp-go/cost-observation`，不藏在TurnUsage。
- Claude解析可空`total_cost_usd`、modelUsage.costUSD/costBasis，使用`*json.Number`等保留十进制；native report的valuation准确区分cli_estimate/provider_reported/unknown。
- Pi解析usage.cost.total和totalFromProvider，按原生消息身份去重；false/缺失是cli_estimate，不强制分项之和等于provider total。
- adapter只交付有来源的观察；native.Connection在Task 4拥有原生累计费用checkpoint，不把同一差分在两层都执行。

- [ ] **Step 1: 写RED。** Claude金额存在但Token缺失、错误result有金额、恢复累计金额原样交付而非当本轮；Pi重复message不重复累计、provider总额与目录分项不同保留total、缺cost与显式0分开。metadata JSON往返保留source/scope/coverage；非法负数/非有限金额拒绝。
- [ ] **Step 2: 运行RED。** acp-go运行`go test ./pkg/acpmeta ./pkg/claude ./pkg/pi -run 'Cost' -count=1`。
- [ ] **Step 3: 实现费用捕获。** 错误/取消判断之前捕获已观察金额，通过现metadata和最后快照交付；不改变原生错误或模型结果。没有原生消息身份的请求金额不得逐通知盲加，保留覆盖限制。来源条件只采用费用来源文档已有证据和fixture；未证实版本字段保持unknown，不要求启动CLI研究。
- [ ] **Step 4: 定向GREEN及提交。** gofmt，运行改动的上述包及费用/终态回归；记录CLI语义版本限制。完成条件：不损坏Token合同、费用独立存在、错误终态能保留观察，尚未证明账单属性的金额不标实扣。

### Task 4: Host费用差分与保守精确估算

**Dependencies:** Tasks 1–3；模型计划提供真实service tier回执，缺失时不可推导。

**Files:** Modify `server/internal/agent/native/{contract.go,connection.go}`及测试、`server/internal/agent/{contract.go,session.go}`及用量映射测试；Create `server/internal/usage/pricing.go`、`pricing_test.go`及版本化小目录数据（同包）；Modify receipt构造与相关测试。价格出处统一引用费用来源文档，不复制OpenBot价表。

**Interfaces:**

- native.Usage增加独立费用分支，读取Task 3 metadata及标准ACP累计cost；Session冻结到Receipt.Costs/Estimate，不能复用Token Complete。
- `usage.PricingCatalog`明确Version、CheckedAt、Sources、ActivatedAt、发行方、精确模型ID、币种、tier、官方已明确的适用有效期与必要阈值/TTL/时段条件；`EstimateReceipt(Receipt,PricingCatalog) (*Estimate,error)`。
- 只有同一价项可证明适用于本轮全部请求时允许turn聚合估算。需要逐请求条件但无请求级证据时，Estimate返回缺失原因，不创建价格所需事实；不要求adapter导入宿主价表或无界保存所有请求。

- [ ] **Step 1: 写RED。** Claude累计0.10→0.25第二轮0.15；resume/fork首样本无baseline未知；累计回退、币种/epoch变更不得夹0当完整；重复通知替换，错误后已知金额仍在。Token main_loop与whole_tree金额覆盖独立；standard ACP cost保留unknown valuation。
- [ ] **Step 2: 写定价RED。** 有来源小目录fixture验证精确公式和12位half-even；相同模型但自定义发行方拒估；缺actual model/tier/TTL/历史有效期拒估；一长一短请求不能按turn总输入跨阈值估价；跨时段无请求时间拒估；不重复加reasoning。原生partial与完整API估算不得叠加；不同币种不合并。
- [ ] **Step 3: 运行RED。** `go test ./internal/agent/native ./internal/usage ./internal/agent -run 'Cost|Pricing|Estimate' -count=1`。
- [ ] **Step 4: 实现checkpoint和估算。** checkpoint仅归同native.Connection，键包含账号、原生session、counter epoch、来源、币种和CLI语义版本；fresh零起点需有证据。math/big.Rat或等价精确十进制计算，原生保留原精度。目录CheckedAt不当官方EffectiveFrom：若有官方有效期则使用；否则本版本只用于ActivatedAt后新调用的API等价估算，并将catalogVersion、来源、核对日期随receipt冻结，明确它是估算。旧消费不自动回填当前价格；没有官方历史有效期不阻塞新调用已知固定费率估算。必要tier/TTL/请求阈值等未证实仍不可估算，不扩大外部调研。API等价只补可证明不重叠消费；否则作为comparisonAmounts，主amounts优先原生。frozen receipt不随目录升级改写旧金额。
- [ ] **Step 5: GREEN及提交。** gofmt并运行Step 3，真实pipe验证metadata→native→Session→receipt独立费用与取消；本地Go workspace联调，不提交绝对路径replace。完成条件：已知小计与不完整原因均可信，不承诺所有CLI都有费用。

### Task 5: 派生索引、历史提取与恢复

**Dependencies:** Tasks 1、2、4。

**Files:** Create `server/repositories/models/usage_receipt.go`、`server/repositories/controllers/usage_controller.go`、`usage_controller_test.go`；Modify `server/services/usage/recorder.go`、`recorder_test.go`、Chat提交后投影/启动恢复和`server/global/config.go`装配；需要SQL规模验证时扩展现`sql_performance_test.go`，不新建迁移框架。

**Interfaces:**

- 注册`usage_receipts`：ID主键、ReceiptHash、RecordKind(dispatch/receipt)、SchemaVersion/SourceKind、全部冻结归属和runtime/account/provider/model/tier、StartedAt/FinishedAt/OccurredAt、Outcome/PromptAccepted/OutcomeUnknown、五项nullable Token、TokenSource/Coverage/Complete、Costs/Estimate(serializer)、CreatedAt/UpdatedAt。OccurredAt取最终消费结束时间；dispatch用开始时间计覆盖缺口。
- 同一派生表投影dispatch的窄行：其ID由requestidempotency按usage-dispatch+InvocationID确定，InvocationID原样保留，Token/金额为空；最终receipt使用原receipt.ID另行插入，不更新dispatch行。消费聚合只读RecordKind=receipt；indeterminate覆盖用同表索引支持的NOT EXISTS判断同InvocationID在同snapshot下是否已有receipt，不JOIN、不将dispatch算零消费。增加(InvocationID,RecordKind)索引保证存在性判断；这只是append-only源证据索引，不复制执行状态机。
- 技术`usage_index_state`只存generation/schema/source cursors/rebuild状态，不保存第二份Token/费用；覆盖缺失查询仍从上面的单表证据得到。legacy已接纳但缺终态的执行也提取同类dispatch证据，不伪造未知接纳为消费。
- `UsageController.InsertReceipt(context.Context,models.UsageReceipt) (bool,error)`；`CaptureSnapshot(context.Context) (UsageSnapshot,error)`；`Summary(context.Context,UsageFilter,UsageSnapshot) (UsageSummary,error)`；`ListGroupKeys(context.Context,UsageFilter,UsageSnapshot,constants.UsageGroupBy,UsageGroupCursor,int) (UsageGroupKeyPage,error)`；`ReadReceiptBatch(context.Context,UsageFilter,UsageSnapshot,UsageReceiptCursor,int) ([]UsageAggregateRow,error)`。Repository结果是聚合值，HTTP DTO仍在API。
- Recorder增加`ProjectChatInvocation(context.Context,usage.Receipt) error`、`Reconcile(context.Context) error`，生产复用为聊天提交、独立收尾、删除归档和启动恢复。

- [ ] **Step 1: 写真实SQLite RED。** 同ID/hash重复不更新，异hash稳定冲突；DB busy整事务重试、取消和插入失败后从耐久源恢复；源+归档重放只一条；清空派生索引后重建结果相同。未durable观察崩溃后只能缺失覆盖，不能恢复Token。
- [ ] **Step 2: 写legacy/fork RED。** 旧execution最多一条确定ID（requestidempotency按chat+execution派生），不猜多Invocation分配；只读取JSON实际存在的数值键，omitempty缺失字段nil、Complete=nil；存在新版receipt时不再叠加旧execution Usage；message.inherited/ForkInvocation不导入。无usage已接纳调用为缺失，历史background无源时次数unknown。删除前复用同一提取器归档legacy。
- [ ] **Step 3: 运行RED。** `go test ./repositories/controllers ./services/usage ./services/chat -run 'Usage|Archive|Legacy' -count=1`。
- [ ] **Step 4: 实现幂等投影和恢复。** 数据只由JSONL或usage journal投影；重复不触发事件。索引不随Chat/Waker/账号级联删除。基础索引`(occurred_at,id)`和`(waker_id,occurred_at,id)`；模型索引仅真实计划使用时保留。分批恢复和cursor可续，不在事务内读文件。重建未完/完整行损坏/持久失败必须partial。
- [ ] **Step 5: 实现稳定snapshot并GREEN。** 同代只插入不可变行，Repository内部以SQLite max(rowid)高水位限定快照，receipt聚合和dispatch的NOT EXISTS两侧均加该上限；generation随重建改变，外部opaque snapshot/cursor绑定generation、过滤和分组。历史导入与新记录不能改同snapshot已有结果；包括dispatch后迟到receipt不能改变旧snapshot的indeterminate数。不得以毫秒created_at代替唯一高水位，不持有长读事务。gofmt、Step 3回归、自审提交；完成条件：可重建、删除保留、缺失覆盖诚实。

### Task 6: 聚合API、全局事件与生成契约

**Dependencies:** Task 5；[UI计划](2026-11-03-provider-ui.md)仅消费此合同。

**Files:** Create `server/apis/usage/{usage.go,get_usage_summary.go,list_usage_groups.go}`及对应测试；Modify `server/apis/root.go`、`server/repositories/controllers/usage_controller.go`及SQL性能测试；`server/apis/events/{protocol.go,sources.go}`和对应测试；`packages/platform-contracts/src/core-event-protocol.ts`及测试；桌面现固定事件通道文件只改usage白名单所需范围；执行API生成命令产生OpenAPI和前端generated文件。

**Interfaces:**

- `GET /api/v1/usage/summary`，Operator `GetUsageSummary`，SDK `getUsageSummary`；`GET /api/v1/usage/groups`，Operator `ListUsageGroups`，SDK `listUsageGroups`。
- 共同过滤from/to为Unix毫秒半开区间，timezone为有效IANA，wakerIds<=100，最大366个当地日；snapshot可选，summary首次返回后groups复用。groups另有groupBy=day/model/waker、limit默认50/max100及cursor；过滤/分组改变丢弃cursor。snapshot绑定同一索引generation+高水位，过期返回稳定错误要求重新读取。
- 两响应固定顶层`range`、`snapshot`、`coverage`、`metrics`；groups另有`groups:[{key,label?,metrics}]`、totalGroups/nextCursor/hasMore。coverage为state(ready/partial/rebuilding)、earliestRetainedAt、legacyBackgroundUnknown、indeterminateInvocations、reasons。
- metrics为invocations/completedInvocations/failedInvocations/cancelledInvocations、missingUsageInvocations/incompleteUsageInvocations/unpricedInvocations；tokens与knownTokens各含input/cacheRead/cacheWrite/output/reasoning；totalWithoutCacheRead/totalWithCacheRead与knownTotalWithoutCacheRead/knownTotalWithCacheRead；amounts及独立comparisonAmounts。
- amounts项为currency/valuation/origin/source/amount(decimal string)/coveredInvocations/unknownInvocations/complete/priceVersions；只有不重叠选择后的金额进入amounts。各Token及完整total为nullable，known小计保留已观测值；reasoning不再加入任何总量。两total分别I+W+O和I+R+W+O。计数用invocations，不冒称消息数。
- 现全局WS新增逻辑topic usage，resource=global、type=usage.changed；payload仅indexGeneration与安全失效时间范围。只在索引提交后发布，重复receipt不发布；首次/重连/缺口查询对账，不新增连接或轮询。

- [ ] **Step 1: 写API/SQL RED。** DST、月底、半开边界与过滤校验；1万/10万条真实SQLite，模型/伙伴多组limit+cursor准确总数；并发插入/历史导入不改已有snapshot；未知与明确0、两种total、partial金额和币种口径。只读当前页伙伴名称，有界批量，不N+1；已删除伙伴显示保留ID与删除状态。
- [ ] **Step 2: 写事件RED。** usage白名单/本机认证/全局资源形状、commit后恰好一次、事务失败和duplicate无事件、重建批次合并失效、断线后读取快照。不得把usage强塞现resources的skill/connector资源形状。
- [ ] **Step 3: 运行RED。** `go test ./apis/usage ./apis/events ./repositories/controllers -run 'Usage' -count=1`；执行platform-contracts对应事件测试。
- [ ] **Step 4: 实现有界单表查询。** SQL先按日期/伙伴过滤。Summary按512行keyset流式累计固定币种/口径桶；day使用IANA真实UTC日边界，最多367桶，不假定一天24h。model/waker先单表GROUP BY准确计数和稳定身份limit+1候选，再对本页候选批量读窄列累计，不建无界模型map。金额big.Rat累计；Token SUM仅作已知小计，同时保留缺失数。EXPLAIN QUERY PLAN证明窄日期/伙伴命中相应索引；SQL次数随批次和候选窗口增长，不逐行补名称。
- [ ] **Step 5: 生成并GREEN。** gofmt；`pnpm api:generate`和对应生成契约检查，核对getUsageSummary/listUsageGroups及生成Options/QueryKey实际名称；执行Step 3和受影响事件测试。UI同步本合同，禁止手写generated或用as补造字段。
- [ ] **Step 6: 自审并提交。** 完成条件：UI可用同snapshot读取metrics和groups，未知/partial明确、金额不双加、准确总数不是预览；提交附真实SQL计划、查询次数和定向测试，不声称真实平台通过。

### Task 7: 边界缺口验证与已实现文档

**Dependencies:** Tasks 1–6；不重跑已通过且未变的全部测试。

**Files:** 仅在缺口需要时扩展`server/internal/agent/native/connection_test.go`、`server/services/{execution,chat,usage}`对应测试、`server/apis/usage`和`server/repositories/controllers/usage_controller_test.go`；Modify `docs/architecture/{runtimes-and-accounts-cn.md,chat-and-execution-cn.md}`、`server/internal/agent/README.md`中真实受影响章节；Create `docs/discussions/provider-runtime-parity/usage-analytics-acceptance-cn.md`。

**Interfaces:** 消费前述最终合同；不新增生产入口、价表来源或平行验收状态源。

- [ ] **Step 1: 只补跨层缺口RED。** fake原生多请求→失败/取消→receipt→JSONL/归档→清空索引恢复→snapshot API；source+archive双写后fork/retry/删除均保持正确小计。特别证明：①两源皆未耐久只能进程内补写，崩溃unknown；②任一耐久可重建；③同ID/hash两源幂等、冲突拒绝。不得把静态fixture写成真实provider验收。
- [ ] **Step 2: 运行缺口RED并最小实现。** 用实际新增测试名在上述受影响包运行`go test ... -run '<具体新增测试名>' -count=1`；只修本计划边界，不扩展业务编排。若已有测试充分覆盖，记录证据而不写镜像测试。
- [ ] **Step 3: 定向GREEN与文档。** 记录两仓库commit、fixture能力矩阵、费用scope/valuation、未知模型和缺价原因、legacy覆盖起点、真实CLI/账号/UI未验证项目。当前架构只写已实现职责，不把待实施方案写成现状。
- [ ] **Step 4: 文档/差异检查。** `pnpm docs:check`、`git diff --check`，确认生成契约无非预期漂移，无个人路径/凭据/临时工件链接；只运行本阶段必要生成校验。
- [ ] **Step 5: 阶段审查与提交。** 完成条件：三种耐久保证、费用保守条件和UI消费合同都有定向证据；明确真实资源验收仍需单独授权和实际运行，不据此宣布总体交付。
