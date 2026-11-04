# P0：Codex、Claude Code、Cursor 原生适配差距矩阵

范围：重建方案第 8.2 节和 P0/P1。这里只读核对已有审计、固定源码与测试入口，不实现产品、不调用真实 CLI/账号/模型、不访问凭据或界面。OpenBot 只用于行为研究，后续 Go 实现与 synthetic fixture 独立编写，不复制其 PolyForm Noncommercial 代码或测试。

## 1. 基线与判定方法

本次实际核对：NeuxBot `da99a0923c13788f35d64fbf9fd3006eb9a341e3`，acp-go `af5c952858e8cd5f3a396a93220e3ed59d3d9bc9`，OpenBot `f44dbe116c89a944f8d7d085e092c967b3fce0df`。以下 `N:`、`A:`、`O:` 分别表示三个仓库根目录；OpenBot 取任务固定参考目录，没有重新 clone 或追踪 main。

复用资料均在 `docs/discussions/provider-runtime-parity/`：

| 资料 | 本次复用内容 | 适用边界 |
| --- | --- | --- |
| `runtime-models-errors-audit-cn.md` | CLI 发现、模型投影、Fast、结构错误的旧代码事实 | 原审计 OpenBot 是 `21369e1`；关键三家结论重新核对 `f44dbe1` |
| `accounts-ui-audit-cn.md` | 账号权威、租约/刷新、认证根、产品入口缺口 | “保留 Manager/Session/Catalog”的实施建议已被重建方案取代 |
| `installer-sources-cn.md` | 2026-11-03 官方源、平台资产、摘要、完整布局 | 历史来源核查，未重下载、未验收安装；不是当前 latest 承诺 |
| `usage-audit-cn.md`、`usage-foundation-acceptance-cn.md`、`implementation-status-cn.md` | 缺陷复现、已实施修复、定向普通/race/SDK pipe/终态 JSONL 证据 | 历史 FAIL 不算修复通过；旧生命周期测试是行为样本，不是新底座通过 |
| `quota-protocol-cn.md`、`usage-pricing-cn.md` | 官方 schema/SDK 固定版本和费用/额度 scope | 协议研究和建议，尚未交付产品；不复制价表，不读取真实凭据 |

状态组合使用：**已有**＝当前源码/测试存在；**已验证记录**＝上述验收文档记载命令通过；**需重写**＝旧路径不满足新职责/机制；**需补齐**＝合同或链路缺失；**原生不支持/未知**＝有明确限制或没有足够证据；**未验收**＝真实 CLI、账号、模型、平台或产品链路未证实。测试仅存在时不写“已通过”。本次不重复运行已通过 suite。

共同结论：三家均未完成第 8.2 节闭环。旧 `Manager → Session → Catalog` 不是新实现的架构约束；可保留业务权威、认证安全不变量、协议事实和独立回归场景，不能用包装旧对象交付新 runtime。

## 2. Codex

| 维度 | 固定 OpenBot 参照与 acp-go 当前证据 | 处理状态与归属 |
| --- | --- | --- |
| CLI、版本、安装来源 | O:`cli.ts` 的 `resolveCodexCli` 区分显式路径、来源、登录 shell PATH；N 旧发现能枚举路径/版本。既有官方源审计确认 `rust-v0.166.0` 规范完整包及 release SHA-256，不能只拷主 binary。 | **已有**发现行为；**需重写**到 runtime 的程序/安装所有者。受管一键安装**可实施候选、未实现**：版本目录、摘要、完整资源、无 Prompt 探测后激活；官方外部入口保留。刷新查询、取消、旧安装保留及平台启动均**未验收**。acp-go 只返回真实程序/版本/启动事实，不维护宿主安装记录。 |
| 模型、设置、Fast | O:`agent/model-catalog.ts:79-105` 分页，`provider-models.ts:273-280` 从真实 service tier 找 Fast，`drain-scheduler.ts:1161-1166` 区分继承/关闭/null/开启。A:`codex/appserver_client.go:433` 已分页并拒绝重复 cursor；`config.go` 只有 mode/model/reasoning_effort。`agent.go:SetSessionConfigOption` 仅改本地选择，下一轮才发送。 | 分页/逐模型 effort **已有**，`TestAppServerClientRejectsRepeatedModelCursor` 等存在；Fast **需补齐**目录与 start/resume/turn 参数。生成 DTO 的 `serviceTier *string,omitempty` 丢 absent/null 区别，须修生成链再贯通。setter 本地 ACK 不能宣称原生 effective。runtime 保存账号作用域、新鲜度、known/empty/unknown、requested/effective；宿主当前投影丢额外参数，产品链**未验收**。 |
| 账号 | O:`provider-drivers.ts` 的 `CODEX_HOME` 账号能力及 CLI 登录/登出；A:`codex/auth.go` 已有 account read、API key/ChatGPT 登录、取消与脱敏测试。N 已有加密账号、租约和刷新协调样本。 | 原生认证**已有**；runtime **需重写**账号/进程绑定及管理操作，不复制 AIAccount 权威。系统原生与受管账号、配置根和模型缓存主体必须分开；API key 不冒充订阅额度。切换不能给新账号旧 session ID。真实登录/登出、轮换、撤销、隔离根和产品入口**未验收**。 |
| 指令、Skills、MCP、cwd | O:`agent/thread-lifecycle.ts` 原生 developerInstructions、工具指纹和 MCP resume 边界；A:`codex/agent.go:413,548` start/resume 只传 cwd/config，未交付 developerInstructions。`workspace.go`、`RefreshSkills` 已有真实 cwd/附加根及扫描；`mcp_config.go`、`mcp_status.go` 已处理 transport、原生同名配置和启动状态。 | bot 指令交付**需补齐**到原生入口；受管技能复制/哈希、真实工作区、稳定前缀和更新策略归 runtime，**需重写**旧生成文件/软链接机制。MCP 转换/状态**已有**，仍须验同名 stdio/HTTP 冲突、resume 更新与加载失败；OAuth/桥接 token 归 runtime。不因原生读取项目 AGENTS.md 就宣称应用仍应生成它。 |
| 消息身份、生命周期、取消、恢复 | A:`codex/event_router.go` 有 thread/turn/generation fence，`appserver_client.go` 处理终态先于 start 回执；`agent_runtime_test.go` 覆盖 late start、close、cancel/completed 竞争。`history.go`/`fork.go` 已有历史和冷分叉；O:`app-server-client.ts` 有原生分页历史、归档恢复与挂起诊断。 | 原生顺序、身份、cancel/close **已有**；新公共合同仍须锁定 accepted/unknown、消息覆盖/完成及断线事实。公开聊天绑定、排队、JSONL 分页、替换/交接归 runtime **需重写**。原生历史不能成为每次打开聊天的唯一数据源；fork 不是新消费。原生 compact 的调用/通知需单独补齐，不能用生成字段冒充已接通。 |
| Usage、Context、quota/费用 | A:`usage_foundation_test.go` 已修多请求累计、cache 互斥、回退和 hot checkpoint；既有验收记录包含相关包/race。未知 epoch 保持 partial。O:`agent/usage-collection.ts`/`database/agent-usage.ts` 对照累计基线和去重；schema 有 rate-limit 响应，A roots/envelope 尚未接通。 | Token/Context **已有且有已验证记录**；新 runtime 须复用语义，重新验证不可变终态到 JSONL。账号 quota **需补齐** typed `account/rateLimits/read`，不需 Prompt/thread；按账号/桶/重置窗保存。费用/统计、跨聊天 receipt 去重 **需补齐**，不得累计 Context 或把旧 fork 视为新消耗。真实服务返回**未验收**。 |
| 错误 | A:`codex/request_error.go` 已按 enum/status 分类 auth、billing、quota、rate、context 等，并保留原生数据；O provider diagnostics 和 thread lifecycle 参考精确会话缺失与阶段。N 旧映射曾合并 billing/quota、泛化 -32002、过早发 PromptAccepted。 | 原生分类**已有**；新的原生故障合同**需补齐** phase、submission、精确缺失资源/ID、retryAfter 和清理失败。runtime 一次转换稳定失败及恢复动作；未知接纳/工具后失败不自动重发。旧产品映射不作为新实现默认。 |

## 3. Claude Code

| 维度 | 固定 OpenBot 参照与 acp-go 当前证据 | 处理状态与归属 |
| --- | --- | --- |
| CLI、版本、安装来源 | O:`cli.ts:resolveClaudeCli` 与独立 SDK/CLI 版本；A:`claude/executable.go`/`process.go` 现有显式路径、版本与有界 stderr。官方源审计确认 native manifest SHA-256，npm platform SHA-512 为备用；审计观察 latest 2.1.317、stable 2.1.311。 | 发现**已有**；受管安装**可实施候选、未实现**，runtime 管 channel/固定版本/来源/激活与官方外部入口。只下载 raw binary 不证明省略官方 install 后全部功能可用；不得运行 npm postinstall 兜底。平台、完整许可、签名声明和产品刷新/配置**未验收**。 |
| 模型、设置、Fast | A:`claude/config.go` 已按 value/resolvedModel 精确匹配，模型级 effort/Fast，`applyFlagSettings` 后更新；`protocol.go` 有 fast 状态/禁用原因。O:`claude-client.ts:625-658,1022-1033` 有 resolved ID 和 live model/effort，但本固定版未投影/调用 Fast；未知 effort 还存在兼容默认。 | 模型/effort/Fast 原生技术路径**已有**，不是待重造能力；新 runtime **需补齐**目录、可空选择、ACK/独立观察强度、禁用原因和产品全链。不能把 OpenBot 本固定版说成已有 Claude Fast 闭环，也不继承未经验证的 medium 默认。实际 setter/恢复后生效**未验收**。 |
| 账号 | O:`provider-drivers.ts` 有 `CLAUDE_CONFIG_DIR`、CLI auth login/logout，目录与 macOS Keychain 身份需一致；N 现有受管官方订阅登录只含 Codex/Kimi，不能据通用 API key 投射认定 Claude OAuth 多账号已交付。 | 原生机制有参照；Claude 受管登录/登出与隔离账号**需补齐**。acp-go 声明真实接口/版本/认证根，runtime 管任务状态、秘密端口、租约与刷新；复用宿主账号权威。真实 keychain、取消、撤销及跨账号隔离**未验收**，不生成成功按钮掩盖不支持。 |
| 指令、Skills、MCP、cwd | O:`claude-client.ts:909-955` 原生 system append、版本门控 snapshot off、strict MCP、受管技能 plugin、cwd/额外根。A:`claude/launch.go` 仅 stream-json、setting sources、可选 mcp-config/add-dir；无 system append、snapshot off、strict-mcp-config、plugin-dir。`system/init` 可读 MCP 状态。 | **需补齐**原生 launch/control 技术字段和加载报告；不能声称 Go 使用 TS SDK。runtime **需重写**稳定指令组合、复制安装和版本、settings/资源指纹；普通 resume 只发新输入，改指令不被旧 prompt snapshot 固化。MCP 空目录也应严格约束来源，避免继承未授权 servers。OpenBot home-skill 链接不是本方案复制技能的实现依据。 |
| 消息身份、生命周期、取消、恢复 | A:`claude/session.go`/`events.go` 已有输入 echo/UUID、turn 队列、tool 身份、interrupt/无响应杀进程、恢复和 fork；存在 `TestClaudeAgentSessionPromptPermissionConfigAndCancel`、`TestClaudeAgentCancelUnresponsiveInterrupt` 等。O 接收 `compact_boundary`/压缩状态，主循环 result 与后台/中断结果区分。 | 原生会话/交互/取消**已有**；**需补齐**压缩事实、请求接纳强度、恢复配置和终态 scope。runtime 拥有公开会话/消息/恢复/交接和 GenerateText 同路径，**需重写**旧生命周期。自动压缩为 CLI 原生能力，不承诺显式 compact RPC；进程停止不等于回合原生成功。 |
| Usage、Context、quota/费用 | A 已修缺失字段、主循环部分用量、steering 结算、失败/取消不抹消费，相关用例有历史通过记录。O:`claude-client.ts:1438-1458` 用模型累计、恢复首轮请求、counter reset；A `ResultMessage` 仍不含 total_cost_usd/costUSD/costBasis。既有 quota 审计固定 SDK 0.3.309/CLI 2.1.309 的 get_usage。 | 主循环 Token/Context **已有且有已验证记录**，不能当含子智能体全树。模型累计/恢复基线和费用观察**需补齐**。quota 用独立临时 session/control `get_usage + skip_behaviors`，无空 Prompt/无 MCP；最低支持版本**未知**，method error 保守报告。runtime 管 checkpoint、费用来源、节流/退避与查询。真实模型/额度**未验收**。 |
| 错误 | A:`claude/events.go` 已保留 assistant error、result is_error；transport control error 仍有字符串化边界。O 对 API error、interrupt、result subtype 以及失败仍有用量分别处理。 | 分类基础**已有**；**需补齐** typed control error、phase/submission、恢复缺失会话与 auth/billing/quota 区别。不能用任意错误文本认定旧 CLI 不支持，也不能失败时丢费用/已知消费。runtime 决定可执行恢复入口，不在 adapter 实现业务重试。 |

## 4. Cursor

| 维度 | 固定 OpenBot 参照与 acp-go 当前证据 | 处理状态与归属 |
| --- | --- | --- |
| CLI、版本、安装来源 | O:`cli.ts:403` 明确 `cursor-agent` 是 CLI、`cursor` 是编辑器；`provider-drivers.ts:1013-1053` 使用 `acp`。A:`cursor/agent.go` 默认 cursor-agent，缺失时回退 agent，显式路径解析后启动 ACP control。官方源审计观察 2026.10.22-27dcf5f，多文件包，未找到可信公开强摘要，六资产 HEAD 403。 | 发现**已有**；须验证 `agent` 候选真实身份，不能把任意同名程序认作 Cursor。当前安装明确 **external**：提供官方安装页/入口和安装后手动刷新，不虚构受管一键能力。403 不等于平台不支持。版本、安装来源、平台运行和配置体验**未验收**。 |
| 模型、设置、Fast | O:`acp-flavor.ts:165` 指出此版本模型是进程级、每次 set 会请求目录，不能每轮重设。A:`session_options.go` 读 cursor/list_available_models 并保留参数；`interactive.go:start` 另开 PTY 用 --model，`environment.go` 校验 selectedModel。 | 模型参数目录**已有**；执行路径/作用域**需重写**：先独立 fake 证明原生 ACP 同进程跨 session 模型行为，再确定串行/隔离进程边界。保留厂商参数 ID，不能统一猜 reasoning/Fast；Fast 只在真实目录支持时显示，否则**未知/不支持**。runtime 保存 requested/effective、配置冻结。 |
| 账号 | O:`provider-drivers.ts` 有 cursor_login、无浏览器 login 链接、logout 和 CURSOR_API_KEY；没有 Cursor 额外账号目录支持声明。`acp-flavor.ts` 记录此版本 token 在进程内缓存，失效后须结束进程再读认证。 | 原生登录机制有参照；受管多账号根**未知/未支持**，不因 Codex/Claude 支持专属 home 就推导 Cursor 也支持。acp-go 补 auth-lost/restart-required 事实；runtime 协调登录任务、撤销/重启和账号身份，不能未经明确操作更换运行中账号。真实认证源**未验收**。 |
| 指令、Skills、MCP、cwd | O:`acp-client.ts:2016-2026` 对 ACP 用明确文本兼容注入；`cursor-mcp-disabled.ts`/flavor 处理该版本项目禁用名单导致静默漏载，并验证 bridge 到达。A 当前通过 `interactive.go:start` plugin-dir/approve-mcps、add-dir、项目 hook lease 交付，另有 ACP control。 | **需重写**当前双路径，不默认保留 PTY/项目 hook 修改。原生 ACP 无原生 system append 的证据，文本兼容必须明确并由 runtime 管稳定前缀/刷新。受管技能复制和真实工作根归 runtime；MCP 名称/transport/禁用和漏载诊断归 adapter。不能照抄 OpenBot 直接修改用户 disabled 文件策略；只能处理有权受管配置或明确报冲突。 |
| 消息身份、生命周期、取消、恢复 | O:`acp-flavor.ts:150-171` 精确匹配 -32602 + data.message 中当前 session ID；仅整块过滤 WritableIterable closed；失效认证块不能当成功答复。A `interactive.go`、`store.go`、`restore.go`、`events.go` 组合 PTY/hook/SQLite checkpoint，已有 cancel/fork/store fake。 | **需重写**为经版本验证的原生 ACP 单路径；已有测试只提供行为回归素材，不证明原生 ACP 等价。原生缺失、噪声、auth-lost 必须版本/形状/ID 精确限定。runtime 负责公开消息、JSONL、替代会话/交接；无可靠恢复不自动重放，未证实 native fork/compact/history 保持**未知**。 |
| Usage、Context、quota/费用 | A `hooks.go` 按 generation 去重，`usage_foundation_test.go` 已修 cache presence、缺失与零；`context_usage.go` 从 PTY /context 解析。O `cursor-usage.ts` 是独立 DashboardService 两个只读 POST，非 ACP 标准；现有审计未找到官方稳定 schema。 | 旧 hook Token 有**已验证记录**，迁移 ACP 后不可自动继承“已支持”；ACP 原生 Usage/Context **未验收**，缺失保持未知。quota **条件支持候选、未实现**：先证明所选账号 token 来源，再做受控 reader；系统 Keychain fallback 不能替代选择账号。runtime 管缓存/节流/窗口；费用未知不估成实扣。 |
| 错误 | A `outcome.go` 只读当前进程新增日志归一 upgrade/rate/auth/server/timeout；O 直接处理 ACP error/噪声/失效登录块。 | **需重写**生产错误来源到原生 ACP 事实，精确版本补充文本；PTY 日志规则不能移植成未知实现的全局补丁。保留 phase、native code/data、session ID、submission 和输出/工具副作用；未知不能默认成功。真实套餐、失效登录、限流和恢复均**未验收**。 |

## 5. 首先可实施的 P1 工作包

以下路径是当前实际 owning 文件；“新增入口”是实施建议而非已存在能力。代码与测试的新写/重写注释使用英文，方案文档中文。

| 顺序 | 产物与 owning 文件 | 独立 fake 入口与通过口径 | runtime 边界 |
| --- | --- | --- | --- |
| 1 | 统一技术声明、事件身份、配置有效性/提交/故障合同：A:`pkg/acpmeta/runtime.go`、`message.go`、`model.go`；原生标准连接重审 `pkg/nativeacp/agent.go`、`session.go`、`forward.go`、`message.go` 与 `sdk/connection.go`。版本差异只放对应厂商。 | 从现有 `nativeacp/agent_test.go:TestACPProcess` 测试进程、`message_test.go`、`lifecycle_test.go` 和 SDK notification barrier/backpressure/cancel 用例增加三家共享合同场景：握手能力、同消息分片、终态前排空、断线、cancel、close 唤醒、迟到代次隔离。只跑新增/直接影响用例。 | 只产技术事实；不搬入公开聊天绑定、产品队列、JSONL 或业务接纳。旧 PromptAccepted 不能成为新合同依据。 |
| 2 | Codex settings/instructions：A:`pkg/codex/config.go`、`agent.go`、`appserver_client.go`；wire 三态由 `tools/protocolgen/main.go`、`pkg/codex/protocol/schema/protocol.root.json`/生成入口及 `optional_nullable.go` 维护，运行既有生成命令，不手改 generated。 | 复用 `appserver_client_test.go` fake RPC、`agent_wiring_test.go:TestAgentSessionConfigurationFlowsIntoTurnStart`；新增分页 tier、inherit/off-null/on、模型切换失效覆盖、start/resume 指令、ACK 与回执差异；`protocol/protocol_test.go` actual DTO round-trip 加 serviceTier 三态。 | 目录缓存/账号主体、选择保存、兼容校验、稳定指令指纹归 runtime。 |
| 3 | Claude 原生准备/回执：A:`pkg/claude/launch.go`、`session.go`、`config.go`、`events.go`、`protocol/protocol.go`。增加 system append、版本门控 snapshot、严格 MCP、受管 plugin 目录与观察到的设置事实。 | `launch_test.go` 检查新建/resume 参数和空 MCP；`transport_test.go` 控制请求/迟到响应；`agent_runtime_test.go` fake CLI 两轮只提交新输入、改指令恢复、fast 禁用/原生拒绝、MCP 缺失、cancel/close。不引入 TS SDK 或桥接进程。 | 指令内容、技能复制、settings scope、资源版本和恢复是否替换由 runtime 决策。 |
| 4 | Cursor 单原生路径：A:`pkg/cursor/agent.go`、`session_options.go`、`callbacks.go`/`events.go` 按真实 ACP 所有者重组；替换 `interactive.go` 的 PTY 执行及 checkpoint 依赖。仅对 cursor-agent 与声明版本启用差异。 | 新增 `pkg/cursor` 原生 ACP fake 进程，复用 `nativeacp/agent_test.go` 协议驱动方式：进程级模型、两 session、精确 missing ID、近似错误不触发替换、整块噪声、auth-lost、MCP 漏载、取消/退出、native usage presence。既有 `terminal_test.go` 不能充当新路径验收。 | 连接隔离、auth-lost 后安全重启、指令文本兼容和公开会话交接由 runtime；不在 adapter 创建第二套业务队列/消息库。 |
| 5 | 三家管理事实与读数：Codex typed rate-limit roots/envelope/client；Claude protocol get_usage + typed control error；Cursor credential reader 的窄端口/unsupported 声明。费用另立 observation scope，不塞入 Token。 | Codex `protocol_test.go`/`appserver_client_test.go`；Claude `transport_test.go`/`protocol_test.go`；Cursor 新 synthetic decoder + httptest。验证无 Prompt、空/未知/已知零、多窗、账号 A/B、取消/关闭、method-not-found、恢复首样本不重记。Cursor 身份不能证明时必须 unsupported。 | P1 给读取技术合同；账号持久化/登录任务、探测 freshness、安装器、统计投影、quota gate 与界面在 P2/P4/P5，由新 runtime/产品消费者实现。 |

P1 最早可先落合同与 Codex/Claude 原生准备；Cursor 同时完成单路径的独立 fake 证据后再切消费者。不能以先交付空的统一 interface、保留旧执行器透明 fallback 的方式结束 P1。

Codex 首包可直接消费的最小入口是：`acpmeta` 中具名的 session instructions/options 元数据（New/Resume/Load 请求 `_meta`，不改 SDK 生成类型）、模型级 tier 目录/配置选项、设置回执中的 requested 与 observed/ACK 证据强度。当前固定 schema 已含 start/resume 的 developerInstructions、start/resume/turn 的 nullable serviceTier 和 start/resume 回执的 serviceTier，**无需升级上游 schema**；须修 `tools/protocolgen/main.go` 对相应请求 DTO 的三态保真后执行 `go generate ./pkg/codex/protocol`。不能对所有同名字段盲目全局替换：响应的 nullable 语义与请求 absent/null/value 意图不同。`TurnStartResponse` 只有 turn，不能从该 ACK 虚构 effective tier；start/resume 原生回执可作为 observed，未独立报告的 turn 设置保留 ACK-only/unknown。

## 6. 复用验证与首批交付门槛

可直接保留的语义证据：三家 `TestUsageFoundation*`/`TestUsageFix*`；Neux 旧 `TestUsageTerminalPublicResult`、`TestUsageTerminalExecutionArchive`、`TestUsageTerminalArchiveContract` 的最终快照/观察者失败/取消/JSONL 场景。它们已有普通与 race 定向通过记录；本次没有重跑。迁入新消费者后必须重新证明同样的不变量，不保留旧对象链作为测试前提。

尚缺的跨层验收是：每家 CLI/版本/模型/账号的可查询探测；安装来源与操作结果；设置保存到原生 effective；两轮稳定注入；技能复制；真实 cwd/额外根；MCP 加载/认证；正常/失败/取消的消息与最终消费归档；JSONL 分页/尾部恢复/幂等；新进程恢复/缺失交接；费用/配额新鲜度和产品实际显示。GenerateText、团队、工作流、IM 与后台任务全部走同一底座事实。

本次结果属于源码矩阵与 P1 输入，**不构成真实 CLI、账号、模型、macOS/Linux/Windows、UI 或新 runtime 的通过声明**。真实模型调用和登录不因进入计划而自动执行。
