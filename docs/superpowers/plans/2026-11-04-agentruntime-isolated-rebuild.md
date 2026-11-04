# AgentRuntime 隔离重建实施计划

> **For agentic workers:** 使用 subagent-driven-development；主实现拥有共享合同和宿主，Codex、Claude、Cursor 使用独立 worktree、Sol 6.1 high 并行完成整个厂商模块。每个完整模块提交后独立审查，不在旧 Agent 上继续返修。

**Goal:** 在临时 `agent_next` 目录重写三家原生客户端与 NeuxBot 运行底座，通过实际消费者验收后一次替换旧实现。

**Architecture:** 原生 Client 拥有原生会话、输入接纳、控制命令与原生结果；Neux Thread/Turn 拥有公开会话、资源和耐久提交。宿主直接消费 Go 原生结果，公开 ACP CLI 只是同一 Client 的线协议投影。消息和用量只有一份可幂等提交的权威事实。

**Tech Stack:** Go，独立 acp-go 与手写 SDK，NeuxBot server 主模块，JSONL，合成 CLI 与实际 Go/ACP 消费链。

**Spec:** [重建方案](../specs/2026-11-04-agentruntime-rebuild-design.md)、[执行所有权](../../discussions/agentruntime-rebuild/p0-ownership-cn.md)、[五项审查](../../discussions/agentruntime-rebuild/module-review-cn.md)。

## Global Constraints

- 旧执行目录只读；候选不得调用、嵌入、别名包装旧 Manager、Session、PreparedTurn、native.Connection 或厂商 Agent。不能整体复制旧包再改名。
- acp-go 临时根为 `pkg/agent_next`，共享 Go 包名 `agentclient`；厂商子目录为 `codex`、`claude`、`cursor`。最终共享合同迁入 `pkg/agentclient`，三家回到正式厂商目录。
- Neux 临时目录为 `server/internal/agent_next`，最终回到 `server/internal/agent`；沿用 `server/go.mod`。范围外能力目录和 CLI 不因删除旧执行器被误删。
- 独立 worktree 的隔离装配验证实际消费者。不得对用户数据同时运行新旧实现，不增加自动失败回退或永久双轨开关。
- 重写 Go 与测试注释英文，文件 snake_case，中文文档。类型和私有规则跟随真实所有者，平台与生成协议按其真实边界分开。
- OpenBot 固定参考为 `f44dbe116c89a944f8d7d085e092c967b3fce0df`；读取 actual source，按职责和行为独立 Go 实现，保留许可边界。
- SDK、生成协议与经证明无生命周期状态的纯函数可复用。复用清单必须列明来源、职责和依赖；不通过纯函数所在包间接调用旧 Agent。
- 只运行定向包、实际消费者及具名 race；真实 CLI、账号、模型、UI 和平台运行另列证据，不从合成测试推导已验收。
- CLI 切换时的产品历史交接保持现有策略：默认最近五个完整对话轮次，或由切换前的 Agent 总结后交给新 CLI；总结失败回退近期历史。底层原生恢复、重跑与结果归属重建不得改变这项选择及默认计数规则。

## Review Focus

- 准备中的执行不是原生可用控制目标；Prompt、Steering、Compact、Cancel、恢复和关闭按同一所有者规则接纳。
- 取消在读帧时捕获具体执行并启动自身有界控制，不等观察通知队列；旧命令、旧定时器不命中后继。
- 宿主 BeforeSubmit 提交/授权失败时原生输入必须为零；业务接纳、传输写入和原生接纳分别记证据。
- 原生终态、消费、观察故障和传输故障独立保留；没有前台等待者或原生执行消费多个输入也不重复或漏计。
- 会话 ID 尚未返回、配置尚未归一化、响应与 EOF 同到、尾通知交付失败，以及清理失败均有明确未发布/可重试边界。

## 公共合同与文件归属

精确技术合同在编码时由主实现写入 `pkg/agent_next/agent_client.go`，三家不得各自复制。消费者是三家 Client、公开 ACP 线投影和 Neux `agent_client.go`。

- `Client` 提供初始化、认证与退出登录、创建/恢复/查询/关闭会话、配置、`ReservePrompt(ctx, request, options) (func() PromptResult, error)`、同步短路径 `Cancel(notification) error` 和可重试 `Close(ctx)`。可选能力依据实际支持协商，不能虚造成功。
- `HostCallbacks` 只拥有实际使用的观察、审批、表单和表单完成函数；Scope 在原生接收时确定。Cursor 对公开 ACP 客户端的可协商文件/终端/MCP 反向请求单独作为线协议边界，不扩张 Neux 宿主端口。
- `BeforeSubmit` 是锁外的业务提交门禁，接收冻结的 session、宿主 invocation、适配器输入 ID 和协议实际支持的原生输入 ID；`Prepare` 是同一 owner 的可取消交互准备，返回的清理即使同时报错也须在释放 owner 前完成。Codex 使用实际 `clientUserMessageId`，Claude 使用输入 UUID；不向 Cursor 虚构原生输入字段。
- `PromptResult` 的宿主输入回执与原生 `NativeOutcome` 分开。后者以 `acpmeta.NativeTurn` 唯一保存原生 invocation、来源、消费输入、终态身份、状态和 Usage。不存在原生完成证据时不构造成功、取消或失败终态。
- 有 provider 自发或 detached 执行的 Client 提供有界 `ReadNativeOutcomes`，读取不依赖 Observe。同一冻结结果供前台等待与回执读取，宿主以原生 invocation 幂等落盘后 ACK；输入 H 被执行 P 消费不能再计成 H 的第二次原生用量。
- `process.go` 及必要平台文件只拥有进程、stdio、退出和安全诊断，不保存 session/turn。标准 JSON-RPC 继续用 SDK 的单一 pending/读帧/水位；Claude 不同的原生 control 协议归其连接实现。
- 公开 ACP 转换、必要的回执扩展和错误中的已知结果投影归 `acp_agent.go`；无第二份执行表。Neux 不再用 deliveryAgent 和两对 pipe 重服务同一内置 Agent。

### 实施前检查落实

- 原生结果的存放地址由冻结的 provider、账号主体、原生 session 和 invocation 决定，不能由前台返回或回执分页的先后顺序决定。ConversationStore 内部原生流保存唯一正文、终态和 Usage；Chat/Execution 保存关系及引用。先提交并同步结果和精确输入消费证据，再 ACK；多个宿主输入引用同一原生结果只计一次。
- 首次打开成功的原生绑定先留在同一 Thread/connection，不能提前创建公开 Chat。BeforeSubmit 原子提交新 Chat 或预提交 Execution 的 started、绑定和输入关联，成功后启用工具权限。写入前仍复核取消；提交成功不等于原生接纳。
- 保留实际协商的 Load/Fork、Steer、Compact、模型/账号/配额和 Codex 设置/归档能力。分叉携带真实 position、receipt directory、source cwd 与 reconcileOnly，关闭失败仍保留已取得 child ID。迁移历史分叉资格、恢复和用量读取器；旧日志可读，新日志不再内嵌第二份权威 Usage。
- Cursor 使用 SDK 唯一 reader 的接收序和响应水位。技术配置归并先于异步业务观察；打开期按序归并响应和通知，原子发布配置、setter 映射和可接纳 session。未知 ID 暂存有界且只归一个打开代次，不按最后活动会话猜归属。

## Task 1：公共边界与 Codex 完整候选

**Files:** acp-go `pkg/agent_next/agent_client.go`、`process.go`、必要平台文件、`acp_agent.go`；`pkg/agent_next/codex` 下完整协议接入、配置、执行/控制、事件/用量、历史/管理职责文件及对应测试。按职责决定文件，不预建 helper 文件。旧 `pkg/codex` 冻结。

**Interfaces:** 实现上节 Client，消费 HostCallbacks/PromptOptions，返回冻结 PromptResult；公共文件由主实现单一写入，Codex 实现者只写自身候选目录。

- [ ] 写实际子进程测试验证退出后完整 stdout 仍可读取、stderr 完整记录脱敏、子进程和管道释放、启动失败及关闭重试。区分原生退出失败与资源未释放。
- [ ] 写实际 app-server 协议测试：宿主门禁拒绝零提交；准备中/写入前/已写无 ACK 的取消；旧 ACK 和 foreign usage；已知终态与交付失败同时保留；Steering 和 Compact 不越过同一 owner；关闭和恢复不替换活跃代次。
- [ ] 从固定 OpenBot app-server-client、provider-drivers 和已核对原生协议重写整个 Client。接纳在准备之前；写门内核对确切目标；原生解析和最终记账不依赖观察回调；模型/推理/Fast、账号/配额、MCP/技能/指令和已支持管理能力一并接通。
- [ ] 直接 Go 和公开 ACP 投影使用同一协议夹具验证。保留原生结果和操作/观察错误的区别，检验完整日志/重复终态/累计计数归属。
- [ ] `gofmt`、候选包定向测试与具名 race；检查导入无旧执行器。完整模块提交，独立按五项标准审查。SDK 发布 pin 单独记录，不用本地 workspace 冒充发布验收。

## Task 2：Claude 完整候选（与 Task 1、3 按冻结合同并行）

**Files:** acp-go `pkg/agent_next/claude` 的具体 Client/Session/Turn、control 连接、配置与权限、事件/用量、回执/管理与对应测试。纯协议类型的复用或迁移单独列来源。

**Interfaces:** 同一 Client；provider invocation 与宿主输入分别关联；同一 NativeOutcome 供前台和 `ReadNativeOutcomes` 使用。

- [ ] 以真实 ACP 双端及子进程重现准备中 Steering 提前写入、旧 Cancel 命中后继、编号失败输入丢归属、外来送达错误结束当前执行；将其转为候选公开入口的行为测试。
- [ ] 完整实现宿主 FIFO/准备者、已写待消耗输入和当前原生 invocation 三种事实。所有控制共用代次与写门核验，不持入场 mutex 做 stdin I/O。全局 interrupt 的原生原子性限制准确说明。
- [ ] 将 WorkspaceOnly、原生指令/MCP、账号/配额、模型配置、恢复/分叉/压缩和原生自发执行接到同一 Session；保留实际已支持能力，未知仍未知。
- [ ] 验证先冻结终态再观察、结果文本恢复、已知 Usage、bounded receipt 分页/ACK/容量、独立来源计数和关闭后的回执读取；真实协议过大结果不越过 SDK 帧预算。
- [ ] 定向包/race/实际公开入口验证、导入与五项审查、完整提交。通过后才允许进入统一切换。

## Task 3：共用 ACP 客户端与厂商差异（按同一合同并行）

**Files:** acp-go `pkg/agent_next/acpclient` 拥有共同连接、会话、配置、Prompt/Cancel、观察及协议用量；`pkg/agent_next/cursor` 保留启动、账号、配额与私有协议；`pkg/agent_next/qoder` 作为第二个实际协议消费者验证配置差异。禁止嵌入旧 nativeacp.Agent，也不能只把 Cursor 整包换名字。

**Interfaces:** 同一 Client，通过共享进程和 SDK 连接维护真实 ACP 会话；仅配置/历史不是当前执行，不能按最后 active 补配事件。参照 OpenBot AcpAgentClient/AcpFlavor：默认采用标准行为，少量类型化策略表达有实际消费者的模型作用域、配置归一化、计数口径和精确错误差异；厂商私有编解码不拥有第二套会话或回合状态。

- [ ] 将 Cursor 候选的共同协议职责收归唯一 ACP Client；以 Qoder/Qoder CN 的原生配置类别修正证明第二消费者，两家均经真实 SDK 和合成原生子进程验证。Qoder 不触发 Cursor 专属认证、模型探测、回调或进程模型策略。
- [ ] 写首次 new 响应前配置、NormalizeSession 中配置、有效模型目录缺当前值、独立 Fast、相邻 Prompt/Cancel、活通知队列阻塞、准备取消与旧控制后继测试。
- [ ] 为会话打开建立有界、确切的暂存和发布规则，返回原生 ID 后只采纳匹配的证据；不发布无法供 setter 消费的配置，不建跨代未知 ID 大缓存。
- [ ] 完整实现程序/版本、认证账户、模型/推理/Fast、原生设置回执、指令/MCP、恢复、交互、错误/配额；准入与配置冻结属于同一 Client，诊断只经完整记录脱敏出口。
- [ ] 在原生 ACP 响应、水位错误和原生进程退出组合中保留 NativeOutcome。用直接 Go 和公开 ACP 两条实际路径验证已知终态/用量不丢失。
- [ ] 定向验证、导入/职责检查、完整提交、独立五项审查；不以静态 Windows 构造代替平台验证。

后续 WorkBuddy、Kimi、Grok、Gemini 优先接此底座，只增加已证实的厂商差异与对应验收。认证、安装、额度和用量含义不因采用 ACP 自动统一；官方 Gemini 与 OpenBot 的 Antigravity 需分别核对。Pi 使用独立原生 RPC Client，复用公共进程、输入/结果及宿主运行底座。首批三家的产品切换范围不因此被描述成其余厂商也已完成。

## Task 4：Neux 候选运行、存储与实际消费者

**Files:** `server/internal/agent_next/{agent_client,provider_runtime,thread_lifecycle,turn_lifecycle,conversation_store,delta_buffer,usage_collection,developer_instructions,skills,mcp_configuration,model_catalog,session_settings}.go` 按真实职责创建；JSONL 的格式、索引、批次/重放在其存储所有者中完整重建。隔离装配涉及 `global`、`services/execution`、`services/chat`、知识生成和 workflow output 的实际调用点；正式切换前保留旧目录作对照。

**Interfaces:** 依照 P0 的 ThreadLifecycle/Turn、账户/资源租约与 ConversationStore；`agent_client.go` 直接消费候选 Client。应用使用同一 Store 实例，原生事实的唯一幂等键为原生 invocation，宿主输入到它的关系单独存储。

- [ ] 先用当前批次截断复现建立存储验收：半个批次不发布、重复提交不重复、预期版本冲突、索引重建与分页一致。新 codec/读取/索引/恢复同时完成，不在旧 append 上单点加标记。
- [ ] 实现注册先于获取资源、初始化失败保留同项、准确 cwd、直接指令、复制技能、MCP 与设置回执；Thread/Turn 不包装旧 Manager/Session。
- [ ] 实现 Chat 的 BeforeSubmit 提交门禁、GenerateText 同一路径、DeltaBuffer 和最终用量的耐久交付。provider 自发结果通过同一原生事实提交函数归账，不能依赖观察事件成功。
- [ ] 用实际 Chat/知识生成/工作流输出消费者的隔离装配验证连续两轮、取消、重启恢复、观察拒绝仍归账、H→P 不重复计数、配额与消费分离、部分资源失败和可重试关闭。
- [ ] 在候选入口接通探测、安装/版本、账号、模型/强度/Fast、错误与统计的真实产品服务；界面按已有前端/UI 约定迁移，协议生成按原命令更新。
- [ ] 每个完整模块提交并审查，不把接口文件、测试替身或计划完成作为运行模块完成。

## Task 5：一次切换、清理与验收

- [ ] 汇总三家和宿主各自的能力矩阵、OpenBot 偏差、未证明限制与实测记录；阻断发现全部关闭，发布依赖可从固定版本构建。
- [ ] 同一切换更改正式工厂/import/装配，移除旧执行对象、重复消息/用量写入及已废弃路径。公共技术合同回正式目录，临时 agent_next 路径全部退出重写范围的正式装配。
- [ ] 重跑受影响真实消费者、两仓 SDK/生成契约/文档检查；新旧依赖与写入权威检查证明无隐藏转发或第二执行器。
- [ ] 实际平台/CLI/账号/模型/UI 仅以对应实际证据标记；不因合成测试通过自动执行未授权登录或计费调用。不发布、推送或合并未获授权的结果。
