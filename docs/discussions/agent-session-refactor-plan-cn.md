# Agent 会话与原生连接重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 保持现有功能，将 Agent 重构为一个产品会话子系统和一个原生连接边界，解决嵌套生命周期、重复模型和文件碎片。

**Architecture:** 根 agent 包实现 Manager、Session、Catalog 及实际产品合同。native 只持有原生连接、协议类型、CLI 工厂和启动认证租约。能力目录、授权和事务回执合入 capability，allycli 合入 cli。

**Tech Stack:** Go 1.25、现有固定 acp-go、现有测试及受控进程；不新增运行依赖。

**Spec:** [Agent 会话重构 Spec](agent-session-refactor-spec-cn.md)

## Global Constraints

- 保留现有产品行为、HTTP/OpenAPI、JSONL、事件、CLI 命令、环境变量、账号及插件事实、原生恢复和分叉语义。
- 独立 acp-go 依赖版本保持不变。
- 不修改前端界面或业务逻辑；Go 类型归属变化导致的 OpenAPI 元数据及客户端类型只通过既有命令生成。不操作用户运行中的页面、桌面或真实模型 CLI。
- 根 Agent 和 native 不导入 apis、services、repositories；native 不反导根 Agent、capability、cli。
- SessionPlan、TurnInput、SessionResources 严格分型；静态声明无秘密，本轮输入不进入兼容摘要。
- Session 唯一持有活动调用、兼容事实和资源释放进度；Manager 只登记和索引所有者。
- Prepare 不启动进程，Bind 接管部分资源，Run 才打开连接并提交输入。
- 失败清理保留同一所有者，成功步骤不重复；不自动重放 Prompt。
- 保留无关 desktop/resources/plugins/；所有修改的 Go 结构体字段有准确中文注释。
- 不运行 go test ./... 或前端全量检查；验证仅覆盖修改及直接受影响包。

## 执行组织

使用现有 codex/agent-runtime-refactor 工作分支，不另建工作区，不改变用户的分支归属。先完成 Spec/Plan 及自审，再进入代码阶段。已获实施和多代理授权，不重复询问是否继续。

并行文件所有权：Native 实现者只修改新 native 包；Session 实现者修改根 agent.go/session.go/contract.go/resources.go 及相应测试；能力桥实现者只修改 capability/cli 新包。协调者负责 Catalog/config/diagnostic/logging、外围 Agent 导入、装配、架构测试和文档。不同任务不得同时编辑同一文件；外围导入更新由协调者串行集成。

本计划的短期报告、diff 和进度放入 `.superpowers/sdd/agent-session-refactor-plan-cn/`，不加入产品代码。不自动推送、合并或创建 PR。每个任务报告实际测试命令和输出；独立审查规格符合性与代码质量。

## Task 1: 行为基线与回归清单

**Files:** 读取现有 Agent、能力桥、CLI 和直接受影响业务测试；记录在本计划进度目录。

**Interfaces:** 保留 `PrepareSession/NeedsResources/Bind/Run/Close` 的现有调用顺序，以及原始原生请求与事件断言。

- [x] 记录 git HEAD、工作区差异、生产文件数量和测试名称。确认真实 CLI 环境开关未启用。
- [x] 在 server 运行 `GOWORK=off go test ./internal/agent/... ./internal/agentcapability/... ./internal/agentcli/... ./internal/agentcommand/... -count=1`，保存完整输出。
- [x] 将 staged/runtime 的冷热恢复、配置变化、迟到 Bind、短 Context 交接、临时关闭和清理失败用例列为不可删除断言；建立全部原测试名到新测试名/文件的映射，包含 plan 的校验/权限/预算、output 的最终证据、stream 的交互/交付、原生配置、启动和管理；合并测试函数时逐项记录原断言的归属。
- [x] 对原有失败区分环境限制和代码失败，不通过删除断言或扩大重试隐藏失败。

## Task 2: 原生连接边界

**Files:** 创建 `server/internal/agent/native/{contract,connection,registry,launch,management}.go` 及对应合并测试；读取原 core/contract、driver/acp、environment/launch。

**Interfaces:** native 提供原生连接、Prompt、事件、故障、Descriptor、Agent 工厂、LaunchLease 和独立 Management。输入输出不得依赖根 Agent 类型。Native 实现者先发布最终接口清单，Session 与 Catalog 任务按此消费。

- [x] 先迁移受控协议断言到新包并运行，确认新边界尚不存在时失败；保留全部有效消息、配置、恢复、清理断言。
- [x] 原生类型只保留 OpenRequest、TurnRequest、原生事件、完成事实、Limits/Fault 等连接所需值；PreparedRun、SessionPlan、资源获取和管理集合不进入 native。
- [x] 将执行 Driver 与 Session 收拢成一次连接所有者。构造不启动进程；Open 失败时返回已取得的可关闭部分连接；Close 支持并发、有界等待和失败重试。
- [x] 合并唯一 Descriptor/工厂与厂商输入规则；完成策略和模式候选仍从唯一注册表读取。
- [x] 将启动认证整理为 LaunchLease；域账号权威只经 Refresh 回调消费。原生消费者退出后才允许租约对账与关闭。
- [x] 保留 `OpenManagement`、Probe、Fork、ReconcileOnly 与耐久原生回执语义，更新其原生类型引用。
- [x] 运行 `gofmt -w` 和 `GOWORK=off go test ./internal/agent/native -count=1`，报告接口与测试结果。

## Task 3: 单一 Manager/Session 和产品模型

**Files:** 创建根 `agent.go/session.go/contract.go/resources.go` 及对应合并测试；替换原 host、core/runtime、core/internal/plan、core/internal/output。

**Interfaces:** 根包保留 `PrepareSession(context.Context, SessionPlan, SessionDirective)`、PreparedTurn 的 Context/NeedsResources/Bind/Resources/Session/Run/Close，以及 GenerateText/ExecuteTask、CloseWorkspace、CloseIdleAccount/Plugin 和 Close。消费 Task 2 的原生连接接口，不保留旧 Core 适配链。

- [x] 新回归先覆盖：同一 Session Busy、不同 Session 并行、聊天与任务复用同一机制、关闭失败后不释放账号；在新 Manager 尚未实现时确认失败。
- [x] 将产品合同统一到根包；正文角色消息与带附件聊天输入保留独立类型。纯原生 I/O 类型使用 native 合同，不重复建立技术 SessionPlan。
- [x] Manager 登记聊天及临时 Session；Session 保存当前 Turn 和释放进度，去除每工作区 Runtime 以及内部 sessions/active map。Manager.Start 保留每 10 秒只重试已结束临时 Session 清理的行为；临时执行和待清理所有者合计容量仍为 64，聊天不计入。先写清理故障恢复后后台回收、不重放Prompt、失败所有者占容量的回归。
- [x] PreparedTurn 直接持有同一 Session/Turn，删除 preparedChat 对 Core PreparedRun 的包装。Session 在 Prepare/Bind/Run 之间保持唯一占位和 Context 交接。
- [x] 将 plan 的校验、冻结、权限上界和兼容依据内收到 Session；最终输出 Collector 保持有界和独立状态，随 Turn 存活。
- [x] 将 nativeResources、工作区物化和任务环境装配合入 resources.go；部分取得结果首先登记到 Session，再返回错误。
- [x] 聊天与独立任务共用 Session；保留独立任务授权在 Prompt 返回时撤销，文件在原生退出后释放。
- [x] 将原生命周期测试迁入根包，保留 Fake 原生边界及有效行为断言；测试不增加生产 facade。
- [x] 运行根 Agent 定向测试和生命周期 race，报告所有权字段与已删除模型清单。

## Task 4: Catalog、配置与装配

**Files:** 创建根 `catalog.go/config.go/diagnostic.go/logging.go` 及合并测试；修改 global、services、apis、repositories 中 Agent 导入与构造。

**Interfaces:** 提供 NewCatalog、Catalog.Runtimes/Detect/PrepareLaunch/ForkSession/Close；配置、RuntimeTarget、检测结果和模型选项规则归根 Agent。Manager 消费 Catalog 已冻结的事实和原生工厂。

- [x] 合并 catalog/config/diagnostic 的值类型和方法，按语义重命名 CatalogConfig；去重 clone 方法并保留 nil/深拷贝语义。
- [x] 删除无引用 cloneStrings/cloneStringMap、RuntimeContextStatus、无引用重导出别名及 ValidateDirectory 未使用的 name 参数。
- [x] Catalog 的管理操作使用 Task 2 的唯一工厂和 LaunchLease；失败关闭保留独立管理连接所有者。
- [x] 将全仓旧 host/contract、environment/config/diagnostic、workspace 消费更新到根 Agent；正式装配只构造一个 Manager；global 的聊天与知识调用共享该 Manager，Resolve/Prepare 文本资源端口只装配一次，应用退出只关闭一次。
- [x] 删除原 core/host/environment；生成文件不手改，协议值不变。
- [x] 定向验证 Agent、global、Execution、Knowledge 及相关 API/Repository 消费包的构建和测试。

## Task 5: 能力桥与 allycli

**Files:** 创建 `agent/capability/{capability,catalog,connection_unix,connection_windows}.go`、`agent/cli/cli.go` 及合并测试；替换原三个顶层外围包。

**Interfaces:** Registry/Grant/Binding、命令目录和 Schema、Request/Result、Receipt/Store/Execute/Lookup/Commit 原语义保留；cli.Execute 与 IO 保留。

- [x] 先运行现有契约和 CLI 测试，保存结果；新增目录测试在目标包未创建时确认失败。
- [x] 合并授权、观察、连接文件与回执到 capability；合并 contract 的命令目录和 Schema 到 catalog.go。
- [x] 合并 client.go 与 command.go 到 cli.go；修正技能、README 和样例相对路径。
- [x] 保留平台文件构建变体；不改变 Token/Grant、命令身份算法、HTTP 路径和环境变量。
- [x] 外围导入替换交由协调者串行执行，避免与 Task 4 编辑同一文件。
- [x] 定向验证 capability/cli、Channel/Workflow 回执、Execution 授权和知识 HTTP MCP。

## Task 6: 集成审查、文档与最终验证

**Files:** 根 Agent dependencies_test.go、Agent README、架构总览、智能体与账号、聊天执行、知识能力说明、后端约定及本计划。

**Interfaces:** 架构测试保护实际边界，不以旧路径为豁免；不保留纯转发或别名兼容包。

- [x] 独立审查完整 diff：对照 Spec 核对状态、类型、取消、秘密、事件、授权与事务回执；发现项先写回归再修复。
- [x] AST 架构测试拒绝 native→根 Agent/业务，拒绝根 Agent→业务层；检查无旧目录导入、重复 Session 管理表及孤立辅助函数。
- [x] 对所有修改的 Go 文件 gofmt；运行 `GOWORK=off go test ./internal/agent/... -count=1` 与对应包 vet。
- [x] 对根 Agent 和 Native 生命周期执行 `GOWORK=off go test -race ./internal/agent ./internal/agent/native -count=1`。
- [x] 对 Execution、Knowledge、Channel、Workflow、global、集成 MCP 及相关 Runtime API/Repository 运行实际受影响测试；Core 与 allycli 正式构建。
- [x] 同步当前文档并运行 `pnpm docs:check`、`git diff --check`。历史讨论保留历史性质，不将旧路径当成当前实现。
- [x] 记录文件/包变化、真实执行的测试和剩余平台验收边界；最终结论不得声称未经执行的真实模型验收已通过。

## 实施与验证记录

实现已按 Manager / Session / NativeConnection 的所有权落地。旧 core/host/environment 及三个顶层外围包已删除，生产 Go 文件 64 → 18，测试文件 65 → 16；没有保留旧导入或兼容转发包。原321个顶层Test函数均有保留或替代说明，已删除通用/Fake专有接口的测试有逐项映射。新树有328个顶层Test函数，真实CLI开关继续保留。

| 验证 | 结果 |
| --- | --- |
| 新 Agent 四包完整定向测试 | 通过；根包32.219s、capability 0.573s、cli 0.965s、native 8.355s |
| 根 Session 与 Native 完整 race | 通过；根包47.804s、Native 25.403s |
| Execution、知识模型、Channel、Workflow、RuntimeEnvironment、global、集成MCP、Agent API八包 | 全部通过；包括接纳、恢复、授权、回执及退出依赖 |
| 自动任务、工作流编排、插件连接器精确集成入口 | 通过，使用真实Manager→Session与受控原生协议 |
| Agent及直接调用方vet，正式Core/allycli构建 | 通过；二进制只输出到临时目录 |
| 项目生成契约一致性检查 | 通过；重复生成与当前OpenAPI/客户端无差异 |
| 生成器测试及生成客户端定向TypeScript检查 | 通过；没有执行前端全量检查 |
| 文档链接/图片/个人路径检查与文档测试 | 通过 |
| 34份新Agent Go文件结构体中文字段注释 | 无缺漏 |

独立审查分别覆盖Native、能力桥、Manager/Session及跨模块整合。审查发现并修复：装配尚未完成时过早停止准备取消、最终Usage交付前释放回合占位、替换阶段没有保护新账号/插件身份、预执行失败清理未完成后误判热复用、闲置清理覆盖永久关闭意图。最后两项的六个相关用例各重复十轮race通过；最终整组race也通过。

补充三路独立审查后修复两处边界回归：Execution 的资源回执只投影会话 Mode，冷恢复的原生 ID 和配置变化后的 New 动作继续由 Prepare 唯一确定；本机凭据配置故障使用独立技术分类，并在 Manager、Catalog 分叉/对账及关闭入口恢复原有 RuntimeConfigurationInvalid。新增回归均先复现失败再修复，覆盖真实 buildSessionPlan → acquireChatSessionResources → Bind、异常认证文件、真实 Catalog 分叉准备及失败关闭重试。相关 Agent、Native、Execution 定向 race 与分叉专项 race 均通过，补修经原审查代理复核。

OpenAPI仅有CapabilitySet的Go归属/生成类型名，以及既有错误响应的说明和x-status-errors元数据更新。规范化这些元数据后，请求/响应字段、HTTP状态、路径和Schema约束相同；integration-openapi与基线逐字节一致。前端仅生成客户端类型和导出更新，无界面或业务逻辑改动。

本机全局pnpm入口持续无响应；文档检查运行其原始Node脚本，生成管线使用同一已安装openapi-ts入口替代pnpm脚本别名。未改变项目配置或包管理器版本。

取舍：沿用已有功能分支与工作区，保留无关插件资源；使用现有行为断言作为重构基线，新增生命周期回归而不制造产品行为倒退；按既有生成器同步Go类型归属引起的契约元数据。这些取舍分别通过限定diff、原测试映射与Schema/生成客户端检查核对。

真实账号、真实模型和不同平台CLI未执行实际验收；Windows能力桥/CLI仅做交叉编译，不把它写成真实Windows DACL验收。受控测试、race和正式构建没有替代这些实际平台边界。无推送或合并操作；实现与书面设计保留在现有功能分支供审阅。
