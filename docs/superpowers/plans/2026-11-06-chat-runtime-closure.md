# 聊天主链、资源与 API 收口实施清单

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. 逐项实施、定向验证和审查；完成的任务不得重复派发。

**状态：** 主链与 API 清理已实施；复查发现的团队、自动任务和工作流创作默认选择接纳遗漏已补齐，定向自动化与独立审查通过；尚未完成真实整体验收。Task 勾选记录原工作包实施，最新完成边界以验收记录为准。

**Goal:** 删除确认废弃的运行合同、续接索引和 API，精简资源热路径，保留真实接纳、恢复、授权及交付语义。

**Architecture:** Execution 直接使用现有 ThreadRequest、TurnRequest、TurnPorts。ThreadLifecycle 分开判断进程配置和原生历史；资源管理拥有不可变版本，冷获取拥有实际租约，热续聊读取执行声明。公开合同只保留真实业务字段。

**Tech Stack:** Go、acp-go、SQLite/GORM、React/TypeScript、生成 OpenAPI 客户端。

**Spec:** [已确认方案](../specs/2026-11-06-chat-runtime-closure-design.md)

## Global Constraints

- 复用现有 Execution、ThreadLifecycle、Turn、ProviderRuntime、ConversationStore 和资源管理模块；不新增通用 Plan、FrozenContext、ResourceManager、RefreshCoordinator 或兼容适配层。
- 不实现旧应用数据迁移、旧布局认领、新旧执行器双跑、双写或旧接口回退。新数据的重启恢复、原生 resume、fork 和索引重建继续保留。
- 消息、原生绑定、输入接纳和原生结果继续使用同一 ConversationStore；SQLite 保留实际需要的可重建查询投影。
- API 源合同、直接消费者与生成产物一起切换；内部 JSONL、Package、FlowContext 等仍需完整序列化的执行事实不得误删。
- 不重命名保留旧逻辑，不新增长期转发层。不改外部用户目录，不清理用户数据，不启动运行界面；无明确指令不提交、推送或发布。
- 遵守 AGENTS.md 的阅读、注释、单表查询和生成约定；不执行 pnpm check、全量前端、desktop check 或 go test ./...。
- 多人共享当前 feature 分支；每人只改已分配文件。生成文件由协调者统一运行生成命令，不手改。
- 有真实行为取舍时提供证据和影响与用户讨论；机械删除不逐项重新询问。

## Review Focus

- 新输入已原生执行但本地回执丢失：必须沿原输入身份对账，不能换 ID 重跑。
- 资源更新发生在声明读取与冷获取之间：重新核对版本，不启动混合配置或覆盖活动目标。
- 纯默认值、标签或目录展示变化：保存不被活动账号阻止，不替换原生历史。
- 旧提交、fork 与来源删除：继承边界、附件和成果仍来自唯一 Store 与自身所有权。
- 公开字段收窄：内部持久事实完整，真实推理/Fast/权限/人工回答/工作流重试继续工作。

## Task 1: 删除简单无用字段（API-07、API-10）

**Files:** server/apis/plugins/types.go、resources.go；server/repositories/models/workflow_run.go；server/services/workflow/runner.go；server/apis/workflows/answer_human.go；packages/frontend/src/modules/workflow/components/human-request.tsx 及直接测试。

**Interfaces:** 删除 PluginDetailResponse.componentIds；删除 Human questionVersion 请求/响应/模型和恒定校验。保留 plugins.Snapshot.ComponentIDs、HumanID、snapshotDigest、状态、期限、schema、request receipt。

- [x] 搜索真实生产者与消费者；读取方案 API-07/API-10 及对应工程约定。
- [x] 删除字段及专属构造/检查/fixture；不增加替代版本或兼容解析。
- [x] 定向验证人工回答的状态/快照防重和插件详情展示；修改 Go 先 gofmt。机械字段删除使用现有测试，不写仅复述删除的测试。
- [x] 写明改动路径、验证结果和生成需求，交协调者审查；不生成、不提交。

## Task 2: 直接执行合同与普通重试删除（P1）

**Files:** server/services/execution/{intent,work,agent,native_turn,config_validation,service}.go；server/internal/agent/{execution_plan,developer_instructions}.go；server/services/chat/journal.go；server/apis/chats；server/internal/chat 消息投影；普通/团队聊天前端与直接知识入口。

**Interfaces:** 直接构造现有 agent.ThreadRequest、agent.TurnRequest、agent.TurnPorts；DeveloperInstructions 只消费 []Instruction。删除 SessionPlan、SessionTarget、TurnInput 和死字段。复用现有配置输入解析，不新增同义 DTO。

- [x] 先用现有首发/两轮续聊、可靠 pending、审批/问答和 submission receipt 用例建立定向基线。
- [x] 为默认解析有差异的首发/续聊/显式 system/已解析空值添加行为断言并确认当前失败；改用一份有效选择。
- [x] 删除 D01–D03 的旧入口及测试；BeforeSubmit 原子接纳保留。
- [x] API-01 整条删除：retry 路由、RetryExecutionID、retryable 旧事实及回放/按钮；只改 execution/plugins.go 中 retry 专属分支时须先与 Task 3 协调。
- [x] API-05 从全部公开 Execution 输出移出 submissionHash，内部 JSONL 与幂等核对保留。优先已有公开响应边界，不新增 Service 包装层。
- [x] 删除本包 D12 死合同；重新运行直接受影响的 Go 包/前端用例，报告删除证据和待生成产物。

## Task 3: 资源发布、热声明与冷获取（P2）

**Files:** server/internal/skills/builtin/system.go；技能版本仓储；server/repositories/controllers/mcp_connector_controller.go；server/services/execution/{plugins,session_resources}.go；server/internal/plugins 的版本目标解析；资源管理 API 响应。

**Interfaces:** 使用现有 SkillSelection、ResourceSelection、ResourceLease；读取持久版本摘要，冷获取返回实际资源。API-08/09 收窄公开 SkillVersion/Package，完整内部版本保留。

- [x] 增加或调整热续聊计数测试：第二轮无系统技能 staging、插件整包校验与禁用 MCP 解密；先确认失败。
- [x] 系统资产移到现有初始化/资产版本边界；不新建通用缓存服务。
- [x] 插件版本声明保存实际目标 Skill/MCP 元数据；热读取不调用磁盘 SelectTarget/Load，不先运行完整 prepare 再丢弃。
- [x] MCP 执行查询先筛选启用项，冷获取只解密实际交付项；测试禁用秘密损坏不影响发送。
- [x] 删除 D05–D09 不可达 retry/native 成功分支及 D13 同边界重复比较，保留 native-only 组合明确拒绝、共享目标互斥与关闭失败所有者。
- [x] API-08/09 同步全部资源出口与真实前端类型；保留表单、信任、摘要、授权提示和目标组件展示。
- [x] 验证声明/冷获取竞态、版本发布、目标文件保护与第二轮 I/O；报告待生成项。

## Task 4: 分开进程复用与原生历史兼容（P3）

**Files:** server/internal/agent/{thread_lifecycle,provider_runtime,session_settings}.go 及测试；对应现有 acp-go 能力调用。

**Interfaces:** Binding.Configuration 仅保存 session 兼容事实；当前连接持有进程启动事实。保留唯一 Thread 决策入口。

- [x] 核对 ThreadLifecycle 的配置重载／会话规则及锁定 acp-go 实际能力。
- [x] 添加目录/诊断/账号标签变化不替换 session、进程配置改变但历史兼容时 resume 的失败用例。
- [x] 删除整个 DetectionResult/Revision 等宽 hash；显式比较实际启动环境、交付、主体、历史存储及不能重载的原生配置。
- [x] Claude 已证实可重载资源时保留 session；Codex 按原生限制替换；其他 CLI 不虚构支持能力或新增静态策略表。
- [x] 验证未确认输入恢复、关闭失败租约、一次历史接力、模型 setter 和迟到回调隔离。

## Task 5: 账号保存与统一模型合同（P4）

**Files:** server/apis/{runtime_options,ai_accounts}；server/services/aiaccount；server/internal/runtimeenvironment/model_catalog.go 及账号装配；共享执行选择 hook、账号设置与模型查询消费者。

**Interfaces:** 单 endpoint {protocol,baseUrl}；统一 GET /runtimes/{runtimeId}/models?accountId=...；Revision 为 CAS，AuthGeneration 为凭据代次，稳定主体与历史根另有实际事实。

- [x] 增加活动账号改名/默认值可保存且不关进程、目录变化不影响会话的失败用例。
- [x] 移除普通保存 Exclusive；凭据、主体和共享启动材料仍执行真实独占/失效。
- [x] API-02/03 删除旧 native-models/thinking 路由、QueryKey 和 fallback；真实推理目录走统一入口。
- [x] API-04 全链 endpoint 数组改为单值，不增加数组 importer。
- [x] API-06 删除无消费模型透传，保留内部 service tiers 对 Fast 的派生、目录状态、authGeneration；Runtime 列表自身 version/detectedAt 保留。
- [x] 核对系统主体发现与目录缓存同源、同主体重新登录的真实历史能力；无法证明时明确限制，不能删隔离。
- [x] 验证账号创建编辑、两轮选择、默认解析和旧代次回调；更新直接消费者。

## Task 6: 删除 RuntimeSession 旧续接链（P5 核心）

**Files:** server/services/chat/{journal,fork,fork_targets,recovery,live_projection}.go；server/internal/chat 的 Binding/继承事实；RuntimeSession 模型、Controller 与 global 装配。

**Interfaces:** fork 读新 Binding/继承边界 SessionID 和最近执行事实；删除 RuntimeSession 表模型、map、写入、重放、恢复和查询合同。

- [x] 定向运行现有 fork/重启用例；补充来源删除后子 Chat 附件与成果归属，以及再次 fork 的断言。
- [x] fork 改读正确的新会话事实，再整体删除旧索引链；不新增旧数据读取/迁移或另一份持久索引。
- [x] 以真实 Core.Start 生成规范原生输入/结果，贯通现有 Core.ForkChat 路由、原生 fork、子 Chat 首轮恢复与重启；不得用旧 invocation.finished 夹具代替。删除旧调用终态载体，读取同一 Store 的原生结果与最小继承边界，不重建兼容 emitter。
- [x] 保留未确认输入、可靠 pending、查询投影重建和公开 Chat 最近有效选择。
- [x] 用新数据完成 fork、再次 fork、重启、删除的测试，搜索生产残留。

## Task 7: 工作范围与业务入口行为

**Files:** server/services/execution 的工作范围准备；server/services/{channel,workflow,automatic,im} 实际 owning 文件；既有独立任务入口。

**Interfaces:** 规范授权根为上限；本轮有效 roots 子集供原生、项目入口与指令共同使用。所有入口消费同一有效选择和 Execution。

- [x] 添加附加目录离线仍可纯文本续聊、明确离线引用失败、符号链接改指向不扩权的失败用例。
- [x] 按真实可用且身份未变根收窄本轮集合，不改受管 cwd 和原授权事实。
- [x] 团队派发读取当前档案，成员资格/Chat 映射保留；已接纳配置不重新套默认值。
- [x] IM 不再冻结整个 Binding，仅保存目标/范围/有效配置；入口重新核对当前授权，投递未知结果及正文/文件事实保留。
- [x] 定向验证团队、工作流、自动任务、IM 与独立调用共用底座，可靠 pending/撤权保留。

## Task 8: 工作流公开 Run 与 Attempts（API-11/12）

**Files:** server/apis/workflows/{list_runs,get_run,get_instance}.go；server/services/workflow/capabilities.go 及直接工具响应；工作流前端 run-presentation、instance-details 与测试。

**Interfaces:** Run 公开 triggerSource 及确需 workspace；内部 Context/root fiber 保留。Attempts 仅包含真实尝试身份、状态、Chat、失败和必要执行/时间。

- [x] 定向核对 REST、CLI/模型工具、子运行与自动任务出口；用现有真实消费定义公开字段。
- [x] 收窄响应，删除 rootFiberId 与完整 flowContext 透传、attempt fiberId/outputRepairs/finalMessageId 等实现字段。
- [x] 保留内部 DSL flowContext.workspace、恢复状态、当前 effect candidateSubmitted/skipRequested 和 workflow.retry。
- [x] 定向验证实例历史、来源展示、工具与 REST 一致；报告 SDK/Skill 来源变更。

## Task 9: 生成、文档、交叉审查与收口（P6）

**Files:** 原生成契约、直接消费测试；server/internal/agent/README.md；docs/architecture 下 chat、runtimes、plugin、workspace 当前说明。

- [x] 所有源 API 修改完成后由协调者运行 pnpm api:generate，同步修复直接消费者；有相关源码变化才更新其他 SDK/Skill。
- [x] 运行 pnpm api:check、共享前端 typecheck、直接受影响测试和文档检查；不扩展为全仓测试。
- [x] 对 D01–D13、API-01–12、A01–A17 逐项给出删除/保留和验证证据；本次默认选择遗漏已补齐并独立审查直接消费者。未验证真实 CLI 能力明确列出。
- [x] 独立代码审查检查并发、恢复、资源租约和公开序列化；修复实际问题后仅复跑受影响验证。
- [x] 更新原架构与验收记录为实际行为，报告本地修改、提交/发布状态和剩余真实验收边界。

## 实施验收记录

以下为定向自动化与源码审查结果；协议子进程使用测试替身，不代表真实 CLI、模型或 GUI 验收。真实 CLI/模型/GUI 仍待用户验收。真实 Core → 原生 fork → 子聊天续聊与重启已贯通，原生位置与历史身份来自同一 Store 的规范输入、结果和 Binding；旧调用终态、专属枚举与定位列已删除。Kimi 本轮只修成功提交与并发元数据保存；失败或取消维持原处理，原生 home 副作用仍需真实验收。

### 删除与保留

| ID      | 实际结果与证据                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D01–D03 | 删除旧执行合同及 Journal 旧入口；保留 Thread/Turn、BeforeSubmit 和提交身份。[work_test.go](../../../server/services/execution/work_test.go) 的 `TestNativeContinuationKeepsLastEffectiveSelection`；[thread_lifecycle_test.go](../../../server/internal/agent/thread_lifecycle_test.go) 的 `TestThreadTurnResumesAfterUnconfirmedNativeSubmission`。                                                                                                                                                                                                                                            |
| D04     | 删除旧 RuntimeSession 表链、旧调用终态与专属定位列；[native_fork_test.go](../../../server/services/execution/native_fork_test.go) 的 `TestNativeProductCanonicalFork` 贯通首发、旧 Session 精确分叉、子续聊、来源删除和重启再次分叉。                                                                                                                                                                                                                                                                                                                                                           |
| D05–D06 | 删除普通重试及不可达原生插件成功路径；保留实际 Skill/MCP 投射与 native-only 拒绝。[plugins_test.go](../../../server/services/execution/plugins_test.go) 的 `TestPluginRestrictedEntryFiltersMCPAndRejectsProviderCLI`。                                                                                                                                                                                                                                                                                                                                                                         |
| D07–D09 | 系统技能预发布、热声明与冷获取分离；禁用 MCP 不提前解密。[work_test.go](../../../server/services/execution/work_test.go) 的 `TestNativeTwoTurnsUsePublishedSystemSkill`；[mcp_connector_controller_test.go](../../../server/repositories/controllers/mcp_connector_controller_test.go) 的 `TestMCPExecutionDeclarationsExcludeDisabledSecretsAndDeferDecryption`。                                                                                                                                                                                                                              |
| D10–D11 | 分开进程与历史兼容；Runtime 普通配置保存及账号元数据保存不独占，真实认证变更仍受保护。[update_runtime_environment_test.go](../../../server/apis/runtime_options/update_runtime_environment_test.go) 的 `TestUpdateRuntimeEnvironmentKeepsActiveAccountLease` 验证活动租约及闲置消费者不被普通保存回收。[thread_lifecycle_test.go](../../../server/internal/agent/thread_lifecycle_test.go) 的 `TestThreadTurnHotProcessChangeResumesSameHistory`；[update_test.go](../../../server/apis/ai_accounts/update_test.go) 的 `TestUpdateActiveAccountAllowsLabelsAndDefaultsButProtectsCredentials`。 |
| D12–D13 | 删除旧失效合同、Codex-only 失效转发包装及重复比较；登录、更新、删除与退出统一使用现有 `InvalidateAccountAuth`；保留实际租约和冷获取复核。[plugins_test.go](../../../server/services/execution/plugins_test.go) 的 `TestColdPluginResourcesRejectInstallationChangesBeforeAdmission`。                                                                                                                                                                                                                                                                                                           |

### 公开合同

| ID                | 实际结果与证据                                                                                                                                                                                                                                                                                                                                                                                                  |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| API-01、API-05    | 删除普通重试入口及公开 submissionHash；保留内部幂等事实与 workflow.retry。[native_turn_test.go](../../../server/services/execution/native_turn_test.go) 的提交回执用例；[public_runs_test.go](../../../server/apis/workflows/public_runs_test.go) 的 `TestPublicRunAcceptanceAndRetryKeepIndependentRunEvidence`。                                                                                              |
| API-02–04、API-06 | 统一模型目录和单 endpoint；保留推理、Fast 与内部目录事实。[model_catalog_test.go](../../../server/services/runtimeenvironment/model_catalog_test.go) 的 `TestModelCatalogReadVerifiesSystemSubjectBeforePersistedCache`；[update_test.go](../../../server/apis/ai_accounts/update_test.go) 的账号保存用例。                                                                                                     |
| API-07–09         | 移除公开 componentIds、Skill contentPath 和 Package 内部执行字段；保留内部组件、摘要和目标配置。[resources_test.go](../../../server/apis/plugins/resources_test.go) 的 `TestPluginResourceResponsesKeepPublicCapabilitiesWithoutExecutionMetadata`；内部交付由 [plugins_test.go](../../../server/services/execution/plugins_test.go) 的 `TestPluginAdmissionPinsResourcesAndDoesNotSelectEveryComponent` 验证。 |
| API-10            | 删除 questionVersion，保留 Human 身份、快照与状态防重。[human_answer_test.go](../../../server/apis/workflows/human_answer_test.go) 的 `TestHumanAnswerRequiresOriginalSnapshotCAS` 重新独立运行通过。                                                                                                                                                                                                           |
| API-11–12         | Run 暴露 triggerSource，Attempts 收窄；内部 Context、恢复与当前 effect 保留。[public_runs_test.go](../../../server/apis/workflows/public_runs_test.go) 的 `TestPublicRunQueriesKeepBusinessOriginAndInternalRecovery`、`TestPublicAttemptHistoryPreservesFailureAndCurrentControls`。                                                                                                                           |

### 行为验收

| ID      | 自动化证据与边界                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A01–A02 | [work_test.go](../../../server/services/execution/work_test.go) 的 `TestNativeTwoTurnsUsePublishedSystemSkill`；[mcp_connector_controller_test.go](../../../server/repositories/controllers/mcp_connector_controller_test.go) 的 `TestMCPExecutionDeclarationsExcludeDisabledSecretsAndDeferDecryption`。                                                                                                                                         |
| A03–A05 | [native_turn_test.go](../../../server/services/execution/native_turn_test.go) 的 `TestNativeProductMCPDisplayRenameKeepsCodexSession`；[thread_lifecycle_test.go](../../../server/internal/agent/thread_lifecycle_test.go) 的 `TestThreadTurnResourceRefreshUsesNegotiatedReload`。真实 CLI 能力仍待验收。                                                                                                                                        |
| A06–A07 | [plugins_test.go](../../../server/services/execution/plugins_test.go) 的 `TestColdPluginResourcesRejectInstallationChangesBeforeAdmission`、`TestDeduplicatedPluginSkillKeepsLeasedImmutableSource`。                                                                                                                                                                                                                                             |
| A08–A10 | [update_test.go](../../../server/apis/ai_accounts/update_test.go) 的 `TestUpdateActiveAccountAllowsLabelsAndDefaultsButProtectsCredentials`；[kimi_test.go](../../../server/services/aiaccount/kimi_test.go) 的 `TestKimiLoginCommitsNativeIdentityAcrossConcurrentMetadataSave`；[model_catalog_test.go](../../../server/services/runtimeenvironment/model_catalog_test.go) 的 `TestModelCatalogReadVerifiesSystemSubjectBeforePersistedCache`。 |
| A11     | [thread_lifecycle_test.go](../../../server/internal/agent/thread_lifecycle_test.go) 的 `TestThreadTurnResumesAfterUnconfirmedNativeSubmission`、`TestThreadTurnPartialAcquisitionClosesSameOwnerBeforeRetry`。                                                                                                                                                                                                                                    |
| A12     | [native_fork_test.go](../../../server/services/execution/native_fork_test.go) 的 `TestNativeProductCanonicalFork`、`TestNativeProductForkNegotiatesLatestAndUnsupported` 验证旧精确回合、末轮精度、无能力拒绝、子续聊、来源删除、自有附件与成果、重启和再次分叉；同文件覆盖冷失败重试、未知回执与清理恢复。                                                                                                                                       |
| A13     | [managed_workspace_test.go](../../../server/services/execution/managed_workspace_test.go) 的 `TestHostDirectoriesFreezeRealPathsAndPreserveBusinessScope`。                                                                                                                                                                                                                                                                                       |
| A14     | [team_md_test.go](../../../server/services/channel/team_md_test.go) 的 `TestTeamDispatchReadsCurrentProfilesWithoutChangingAdmittedMembership`；[inbox_missing_conversation_test.go](../../../server/services/im/inbox_missing_conversation_test.go) 的 `TestQueuedConversationKeepsAdmittedExecutionSelectionAndChecksCurrentAuthorization`。                                                                                                    |
| A15–A17 | 最终生成、契约检查、公开字段核对及前端类型检查通过；独立审查发现的问题均已修正复核。[public_runs_test.go](../../../server/apis/workflows/public_runs_test.go) 的公开与内部事实测试，以及上述账号、资源和提交回执用例提供定向证据；真实产品界面和模型能力待验收。                                                                                                                                                                                  |

### 最终验证

主重构批次已完成下列定向验证；后续默认选择接纳补齐的独立证据见下一节。两批均不代表真实 CLI、模型或桌面整体验收。提交状态以 Git 历史为准，未推送或发布，未操作运行界面或改动用户数据。真实 CLI、账号、模型和桌面视觉验收不在本轮通过声明内。

- 主重构批次的受影响整包回归：`internal/agent`、`services/chat`、`services/execution`、`internal/chat/conversation`、`internal/chat/messageview`、`internal/chat/eventstream` 全部通过。删除旧调用事件后，投影追平测试改为核对实际权威终态游标，不再写死旧事件条数。
- 新分叉并发路径 `TestThreadFork*` 的竞态检查通过；受影响原生、聊天、执行、仓储 Controller 及聊天 API 的 `go vet` 通过。冷准备失败在真正原生副作用门禁前可重试；登录复核复用原账号租约；历史根比对复用原 Binding 的身份摘要。
- `pnpm api:generate`、`pnpm api:check`、共享前端 `typecheck` 通过；公开路由与字段逐项核对。直接前端回归 12 文件、252 项通过，资源公开合同的 6 文件、67 项在字段收窄后再次通过。
- 账号及目录、资源、团队与 IM、工作流公开响应与真实工作流重试等定向用例通过；Human 快照 CAS 用例独立通过。系统/受管账号验证边界按原 adapter 能力保留。
- 独立审查已关闭账号/进程、资源和分叉发现的问题；分叉补充独立复核了旧 Session 精确分叉、同主体换历史根拒绝及登录门禁。
- `pnpm docs:check`、修改字段注释、Go 格式与 `git diff --check` 通过。未运行全仓前后端套件。

### 默认选择接纳复核

原清单的勾选不能代替入口证据。本次复查补齐 Runtime 设置保存仍取独占、Codex-only 转发包装残留，以及团队配置校验后丢弃解析结果、自动机会与创作输入在派发时再次读取默认的遗漏。

- [x] Runtime 普通配置保存不回收账号消费者；真实凭据修改仍保护活动租约。
- [x] 删除 Codex-only 失效兼容包装，真实调用迁至统一账号失效入口。
- [x] 团队配置保存、首次覆盖、自动团队机会与侧聊保持同一有效选择，合法空值不被展示候选或后改默认覆盖。
- [x] 自动任务机会与工作流创作接纳保存 Fast，重试沿用原值；任务编辑保持显式 false/true。独立自动任务空账号仍沿用原 API 的 system 语义。
- [x] 补齐批次完成独立审查与直接消费者定向验证。
- [x] 最终注释同步后 `pnpm api:generate`、`pnpm api:check` 通过；生成合同与源码一致。
- [ ] 真实 CLI、账号、模型与桌面验收完成；未启动运行界面、未重建或重启用户正在运行的 Core。

补齐批次的定向证据：

- 团队保存与派发由 [channel_api_test.go](../../../server/apis/channels/channel_api_test.go) 的 `TestChannelAPIFreezesResolvedDefaultsBeforeDispatch` 验证；[configuration_admission_test.go](../../../server/services/schedule/configuration_admission_test.go) 的 `TestPendingTeamRunFreezesOverridesAndInheritedNativeDefaults` 验证完整成员选择、未解析覆盖、合法空值及默认变化后的 pending 恢复。
- 同文件 `TestPendingRunKeepsAdmissionConfiguration` 验证独立自动任务 false→true、true→false、nil→true 默认变化后的恢复；[automatic_task_controller_test.go](../../../server/repositories/controllers/automatic_task_controller_test.go) 的 `TestAutomaticTaskConfigurationUpdatesFastTriState` 验证真实数据库更新保留三态。
- [creation_scope_test.go](../../../server/apis/workflows/creation_scope_test.go) 的回执、Fast、普通续聊和显式修改用例通过；[admission_configuration_test.go](../../../server/apis/workflows/admission_configuration_test.go) 验证 consumer 同身份保留原生空值、换身份重新解析、权限不扩大，以及人工等待、重启与重试使用原节点配置。显式不同模型不继承另一模型的默认推理强度。
- Runtime 设置与账号凭据保护、schedule、channels 及自动任务真实 Core 接纳/删除路径通过对应定向检查。共享伙伴 cwd 下仍验证 Chat、Execution、原生会话、日志与私有附件隔离；删除保留其他聊天及伙伴持久成果。
- 共享解析与 Codex/Claude/Cursor 协议子进程回归通过；普通 Fast 续聊、显式反向设置、原生 nil、严格 MCP 白名单拒绝和 host domain scope 分别验证。
- 本批前端团队/聊天 3 文件 73 项、自动任务表单 2 文件 13 项通过；共享前端类型检查、相关 ESLint 与差异检查通过。没有新增 Fast 界面控件，表单仅保留已有配置值。
- `pnpm api:generate`、`pnpm api:check`、共享前端 `typecheck`、`pnpm docs:check` 与 `git diff --check` 通过；生产源码、测试与生成合同的旧合同/兼容包装名称扫描无残留。
- 本批独立审查逐项核对接纳、持久化、恢复、派发及直接消费者。提交前复审补齐团队 Fast 的配置、线程、派发与自动机会三态传递，系统账号只采用适用账号的默认模型，表单改选清除旧 Fast；删除旧错误后保留自动任务原错误码。真实 CLI、账号、模型与桌面验收仍未执行；提交状态以 Git 历史为准，未推送或发布。
