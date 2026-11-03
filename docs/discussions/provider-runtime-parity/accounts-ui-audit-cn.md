# 服务商、账号与用量界面专项审计

本文是后续规格与实施的审计输入，建议部分尚未实现。研究基线：NeuxBot `e18a0d6e377eda9cdc96954321fb1f0788691a5f`、acp-go `a58342da0ddd1e3ec5d022a0f911b82eba02107d`、OpenBot `21369e1eec420772a7db9f6bbfeae2ef9b3ab83a`。只读分析产品源码，不启动 CLI、模型、浏览器或桌面应用。OpenBot 使用 PolyForm Noncommercial；本文仅提取行为与接口边界，不复制实现代码、样式或素材。

## 1. 结论与边界

NeuxBot 已有可复用的账号与执行安全基础：数据库加密凭据、账号修订冲突检查、共享执行租约、独占登录与配置修改、短期凭据锁、每执行认证投射、令牌刷新合并、旧投射拒绝覆盖，以及 Session 关闭顺序。此次工作应在这些边界上补协议和界面，不能另建一套 provider session/credential manager。

主要缺口分为四组：

1. 现有“AI 账号”和“智能体”两个设置入口需要聚合成服务商总览与详情。程序安装/版本、系统登录、受管账号、默认选择仍是不同事实；不能用“已安装”代替“已连接”。
2. 聊天已有 Runtime→兼容账号，以及模型/effort 两个菜单，需组合为一个 provider/account/model/effort/fast 浮层，并让一组选择原子提交。Fast 需要补真实 capability 与执行参数，不能只增加开关。
3. 已有回合 token 与上下文事件，但没有查到全局费用分析、账号配额读取/reset 的完整产品链路。Token、费用估算、订阅配额和 reset credit 是四种数据，不能共用一组数字。
4. 模型目录必须按实际认证主体区分。OpenBot 的目录仍以 provider 为键，不能借鉴成“所有账号的模型权限已经独立”。NeuxBot 的 Codex 订阅目录接口也存在从 Runtime 默认探测快照回答任意 accountId 的身份边界，实施时需补明确的目录作用域。

本报告覆盖用户指定的行为目标：服务商已连接/其他分组，详情中的账号/API Key/Runtime 管理、启停和默认；聊天模型组合选择；token/费用/完整性页面；账号配额 popup 与 reset banner。没有做截图像素或真实视觉验收。

## 2. OpenBot 多账号的真实行为

### 2.1 账号目录与进程

OpenBot 的首个账号继续使用 CLI 原生默认配置目录；额外账号由 ProviderAccountStore 生成稳定 ID，在专属根下建 `0700` 目录。列表只存 ID/provider/label，认证内容留在原生配置目录，而非列表。创建/写入由 semaphore 串行：`OpenBot:src/backend/agent/provider-account-store.ts:19-24,47-64,97-115,127-145,193-209`。

这里的 home 是 CLI 配置根，不能写成“每个账号有独立操作系统 HOME”。适配声明仅四种内置 provider 提供多账号能力：

| Provider | 专属配置变量 | 行为限制 |
|---|---|---|
| Codex | `CODEX_HOME` | CLI login/logout；API-key login 不读取订阅配额 |
| Claude | `CLAUDE_CONFIG_DIR` | macOS Keychain item 也按目录命名，删除目录前需原生 logout |
| Grok | `GROK_HOME` | 独立 CLI login；账号目录删除时移除认证文件 |
| Antigravity | `GEMINI_HOME` | 需版本确实支持此变量；独立 usage 读取会留 session，因此无运行中进程时保留上次读数 |

证据：`OpenBot:src/backend/provider-drivers.ts:108-127,743-746,776-781,823-830,934-939`。其他 provider descriptor/Client 的存在不构成多账号支持证据。

Agent 指定额外账号时，即便 Full access，也会有自己的一条 provider client/process；不是所有使用同一账号的 Agent 共用一个新进程。复用键包括 executable、version、provider activation、writable roots、excluded folders 和 account ID；键不变才复用。一个账号可被多个 Agent process 使用，同一 Agent 的多 thread 可由 client 管理：`OpenBot:src/backend/agent/provider-runtime.ts:2313-2342,2350-2385,2388-2426`。

### 2.2 登录、凭据并发与活跃切换

新增登录有 connecting/checking/connected/signed-out/error 状态；新账号登录失败或取消会删除新目录，旧账号取消变为 signed-out。检测会拒绝同一 provider 重复 email：`OpenBot:src/backend/agent/provider-accounts.ts:181-298,305-324,562-609`。

重新登录的部分 ACP authenticate 路径先检查账号是否 busy，随后停止该账号的 Agent clients；移除账号前也停止 clients、logout、再删目录。CLI command 路径的保护与 ACP 路径并不完全相同，因此不能概括为“所有登录修改都有一个完整的账号读写锁”。ProviderAccountStore 的 semaphore 保护账号列表文件，ProviderAccounts 中的 sign-in/check maps 管理任务；在所读 provider-account/runtime 文件中未发现类似 NeuxBot CredentialGate 的“刷新轮换结果统一提交”边界。原生 CLI 自身是否另有凭据锁未运行验证。证据：`provider-accounts.ts:217-224,346-365`。

用户修改 provider/account 时，OpenBot 按 provider change 对待：账号 session 存储位于不同目录，不能把旧账号 session ID 直接给新账号。只改 runtime 字段且 busy 时可保存 pendingRuntime，在后续 drain 生效；一些旧 Team API 路径不支持 pending 而要求 idle。见 `OpenBot:src/backend/agent-service.ts:2690-2716,2771-2805,2818-2859`。

这一交互可以借鉴，但 NeuxBot 已有“已接纳工作范围/执行配置不被后续设置改写”的语义。建议 UI 区分当前执行实际值与下一轮草稿，不用服务商设置修改正在生成的工作。

### 2.3 模型目录与配额不是同一作用域

OpenBot ModelCatalog 的输入是 `client(provider)`，保存的选项仅带 provider/model；刷新遍历内置 provider，保留失败时的旧目录。ProviderModelPicker 的账号 rail 也复用同一 provider 模型集合。证据：`OpenBot:src/backend/agent/model-catalog.ts:22-27,33-68,134-148,171-190`；`OpenBot:packages/ui/src/components/ProviderModelPicker.tsx:193-214,235-238`。不能据此保证额外账号拥有相同模型权限。

配额则不同：ProviderAccounts 以 accountId 存 last usage、reset credits、UsageReadGate；读取优先复用该账号某个运行中的 Agent client，否则短开认证 client 后关闭。按 model quota group 选 primary/secondary window；有的 provider 无运行进程时不主动读取。见 `OpenBot:src/backend/agent/provider-accounts.ts:106-109,494-508,615-648`。因此模型目录与 quota 缓存不能互相替代。

reset 是 provider 可声明的一种消耗 credit 的原生操作，并非清空本地统计。执行后重读配额，返回 reset/nothingToReset/noCredit/alreadyRedeemed 等结果，通知所有视图与限额 gate。见 `OpenBot:src/backend/agent/provider-runtime.ts:1120-1158`、`OpenBot:packages/ui/src/features/settings/ProviderSettingsPage.tsx:607-650,772-787`。

## 3. OpenBot UI 可以借鉴的行为

| 用户目标 | 源码行为 | 证据 |
|---|---|---|
| 已连接/其他 | 根据连接状态分组，搜索覆盖 provider 名及账号 label/email，保留 reconnect 时的 Connected 分组 | `OpenBot:packages/ui/src/features/settings/ProviderSettingsPage.tsx:223-237,258-292,478-492` |
| 服务商详情 | 一个详情组合 Accounts/SignIn、API key 和 Runtime sections，能力不支持时不硬展示操作 | 同文件 `:501-522,590-702` |
| 账号动作 | 添加、重连、重命名、移除、必要时 sign-out、带确认的 reset；usedBy 说明实际影响 | 同文件 `:652-702,725-787,807-817,929-932` |
| 模型 picker | provider/account rail + 模型列表 + effort；有 Fast tier 且存在修改回调才显示 Fast | `OpenBot:packages/ui/src/components/ProviderModelPicker.tsx:80-113,193-214,295-307,430-454` |
| Codex Fast | 模型目录读取 serviceTiers 中名为 Fast 的原生 ID，兼容旧 additionalSpeedTiers；关闭用 null 清原 thread tier | `OpenBot:src/backend/agent/provider-models.ts:272-280`；`drain-scheduler.ts:1152-1166` |
| 配额 popup | 显示剩余百分比、窗口重置时间；未报告/不可用/加载区分，不能用 0 假装无额度 | `OpenBot:packages/ui/src/features/account/AccountUsageDetails.tsx:69-106,111-134` |
| 统计页完整性 | 分开 missingUsageTurns、unpricedRecords、incompleteRecords，cost/token 切换；“估算费用/已知 token”语义 | `OpenBot:packages/ui/src/features/usage/AgentUsageReport.tsx:115-154` |

OpenBot 累计计数采用稳定 counter ID/checkpoint 求增量，基线不算新用量，禁止负增量当新消费；重连、SDK session cost 和每 prompt token 的计数范围分别处理。统计不随聊天删除而消失：`OpenBot:src/backend/database/agent-usage.ts:23-47,58-59,88-190`。费用是原生报告或有来源、日期的 API 等价估算；无价格、分量未知、长上下文归属不明时保持 null，而不是猜测：`OpenBot:src/backend/agent/usage-pricing.ts:3-7,152-205`。本文不复用其中价格，也未验证其时效。

## 4. NeuxBot 当前可复用能力

### 4.1 账号数据与认证

`AIAccount` 保存 authMode、唯一协议/endpoint、模型目录、用户选模、显示名、上下文、reasoning menu、默认模型及加密凭据；不直接拥有一个 CLI runtime：`server/repositories/models/ai_account.go:10-50`。协议兼容决定同一个 API 账号可以用于多个 Runtime，例如 OpenCode/Pi/Kimi；不能把账号移动成某个 provider 详情的私有副本。兼容规则：`server/internal/accountconfig/config.go:51-70`；前端对应 `packages/frontend/src/shared/lib/account-runtime-compatibility.ts:3-41`。

目前受管订阅登录只接纳官方 Codex/ChatGPT 与 Kimi 入口，不是所有 CLI 官方登录都已有受管多账号流程：`server/apis/ai_accounts/create.go:115-140`、`server/services/aiaccount/login.go:74-99`。CLI 系统原生认证继续是独立选择，不应强制导入数据库。

凭据与生命周期必须保留：

| 已有能力 | 源码 | 实施要求 |
|---|---|---|
| 数据库加密、列表不带 ciphertext、revision 乐观更新 | `server/repositories/controllers/ai_account_controller.go:47-66,70-105` | 继续使用同一账号 ID/Repository；UI 默认显示脱敏状态 |
| 同账号可并发执行，登录/配置改动独占 | `server/services/aiaccount/service.go:98-173` | 不把 account busy 误改成“同账号禁止两个聊天” |
| 获取独占前回收 idle session，活动 session 不强停 | 同文件 `:91-115`；`server/internal/agent/agent.go:219-239` | 当前生成时就近报 busy，或显式排队；不能偷偷 kill |
| prepare 前短锁，模型期间不持凭据锁；关闭保留可重试 lease | `service.go:239-310` | quota probe 与模型目录读取也复用租约边界 |
| session config key 不含秘密或单纯凭据 revision | `service.go:190-198` | 正常 token 轮换不导致连接重建/丢 cache |
| refresh 合并与旧投射保护 | `server/services/aiaccount/login.go:399-438` | 不用复制原生目录绕过当前权威 |
| 环境清洗与原生配置根投射 | `server/internal/accountconfig/config.go:88-131,230-269` | 保留系统 HOME，清除其他账号 key/home 环境污染 |
| Kimi per-account 持久登录目录 | `server/services/aiaccount/kimi.go:64-95` | 不统一改成每 chat 临时目录而丢原生会话 |

OpenCode/Gemini/Grok API-key 配置按账号 ID+revision 派生隔离目录，OpenCode 分开 XDG data/config/cache/state；见 `server/internal/accountconfig/native.go:71-124`。其余 Codex/Claude/Pi/Kimi 的投射以受管 workspace 下的 accounts/runtime 为根；不能泛化成已经拥有所有 provider 的全局持久账号目录。

### 4.2 当前模型、选择与切换

聊天菜单已有 Runtime→兼容账号子菜单，包含 system；只有 state=ready 的 runtime 可选，disabled 保持不可用。`packages/frontend/src/shared/ui/execution-selection-controls.tsx:43-74,112-174`。Composer 再渲染单独的 ExecutionModelControl，账号切换重置 model/reasoning，模型切换重新选 effort：`packages/frontend/src/modules/chat/components/chat-composer-controls.tsx:34-40,102-139,206-260`。effort 组件的焦点、搜索、模型/强度视图可以复用：`packages/frontend/src/shared/ui/execution-model-control.tsx:21-35,75-131,269-290`。

当前选择字段只有 accountId/model/permissionLevel/reasoning/runtimeId，缺少 fast。RuntimeModelOption 只有 id/name/custom/reasoningOptions/permissionLevels；没有 service tier/fast capability：`server/apis/runtime_options/list_runtimes.go:13-25`。acp-go 已对 Claude 公开 fast 配置和真实 setter：`acp-go:pkg/claude/config.go:130-140,225-238,362-376`；但 NeuxBot 产品合同与 native 映射未消费它。Codex 生成协议有 serviceTier 字段不代表已打通，当前 codex config selection 仍仅 model/effort/mode：`acp-go:pkg/codex/config.go:12-19,37-52`。

受管账号能力探测已有 `DetectModel`，会开临时 workspace、冻结账号 lease、调用无 Prompt detector 并按 native 回执返回真实目录：`server/services/aiaccount/capabilities.go:23-79`。应继续复用，不要从浏览器直接调用厂商 API 或复用另一个账号的目录。

**需补强的静态边界：**`GET /:accountId/models/native` 在 Codex 路径读取 `environment.Detection`，并不核对其探测主体等于请求 accountId；Kimi 则读该账号目录。见 `server/apis/ai_accounts/native_models.go:36-69`。这不等于已复现模型越权，但无法证明账号 A/B 目录隔离。建议模型目录缓存键为 runtimeId+accountId/system identity+launch fingerprint+capability/config revision，credential revision 单独处理；失败保留相同主体的旧目录并标 stale，不跨账号回退。

执行会冻结 account，并在账号变化时不恢复旧 session；Runtime/model/reasoning/permission 改动的冷路径新建会话：`server/services/execution/work.go:1646-1667`。现有 Manager/Session 是唯一 owner，不要为账号增加另一份活跃 Session map。系统默认、聊天草稿和已接纳配置要分开：`RuntimeEnvironment.DefaultAccountID/IsDefault/Enabled` 管新执行；`AIAccount.DefaultModel` 管账号默认；聊天选择最终按本轮接纳结果决定。

### 4.3 当前设置与用量

设置导航分别挂 `ai-accounts` 与 `runtime-environments`：`packages/frontend/src/modules/settings/settings-navigation.ts:3-23`；Scene 分别装配：`pages/settings-scene.tsx:17-24,111-114`。AIAccountsSection 已有账号 CRUD、登录、密钥显示、模型获取/手工设置；相关组件应拆出可复用业务片段，而非另写第二套 API。

RuntimeEnvironment 已持久化 enabled/isDefault/defaultAccountId/binary/prefixArgs/configHome/默认模型/effort/权限/探测：`server/repositories/models/runtime_environment.go:14-50`。现页按 installed 与未发现分组，不按 connected；已实现启停、默认、重新探测、安装路径选择：`runtime-environments-section.tsx:345-355,466-535,764-786`。

当前 `RefreshOne` 是探测，不是更新 CLI 二进制，也不是重启所有活进程。启动页 `cli-installation.ts:20-44,91-107` 只是官方安装指引与命令展示/复制。不能把这两个已有入口改个按钮名就宣称具备 runtime 更新/重启。应由同一 Catalog/原生生命周期 owner 承接真实安装/更新结果；system-managed 与 application-managed 的维护能力需要区别。

用量事实已有 `UsageData.Turn/Context`；token 字段区分非缓存输入、cache read/write、output、reasoning。Context 占用与回合消费是两种数据：`server/internal/agent/contract.go:441-468`，原生最终用量传递在 `server/internal/agent/native/connection.go:476-490`。聊天事件日志有 usage 原始字段：`server/internal/chat/eventlog/types.go:470`，Composer 有上下文 popup。搜索 `server/apis`、settings、app、account APIs 未查到全局 usage/cost 统计页面或账号 quota/reset API。因此不能只换 UI 就完成用量功能。

现有回合字段是非 nullable int64，omitempty 不能单独表达“已知零”和“该分量未知”。全局完整性统计需明确来源可报告哪些分量，不能将省略字段一律计为 0。历史日志回填也必须保留无法恢复的账号/用量信息，不按当前聊天设置倒填历史消费。

## 5. 建议的整合状态模型

下面是规格建议，不是当前已实现结构。

| 对象/作用域 | 应拥有的事实 | 不应拥有 |
|---|---|---|
| Provider 展示入口 | provider 展示 ID、品牌/名称、适用 runtime IDs、连接摘要的派生关系 | 第二套进程、秘密、账号 CRUD 数据 |
| Runtime/安装 | binary/version/source、supported actions、enabled/default、检测状态/时间 | 用户账号额度、全局费用 |
| AIAccount | 稳定 ID、authMode、issuer/endpoint/protocol、credential revision、auth state | 强行绑定单一 harness；凭空的 quota |
| AccountModelCatalog | runtime+account identity、model/effort/fast 能力、fresh/stale/error 与时间 | 另一个账号的上次成功目录 |
| ExecutionSelectionDraft | provider/runtime/account/model/effort/fast 的一组候选 | 覆盖正在运行 execution 的实际配置 |
| Accepted execution | 已冻结选择、实际回执、account identity、native session、usage provenance | 跟随设置默认值实时改变 |
| AccountQuotaSnapshot | provider+account+quota group、windows、reported/resetAt/readAt/stale、reset capability/credits | 本地 token 日志累计值 |
| UsageRecord | execution/turn/counter identity、实际 provider/runtime/account/model、token known mask、reported cost 或估价依据 | 密钥、请求正文、当前可变默认模型 |
| UsageAggregate | 按日期/provider/account/model/伙伴派生 totals 与缺失计数 | 另一套执行状态或 token 累计权威 |

服务商总览可以按 harness/官方服务入口分组；一个可跨 Runtime 的 API 账号通过引用出现在兼容详情中，而非复制。自定义 endpoint 保留独立服务身份，不能仅凭包含 openai/anthropic 的协议名冒用官方“已连接”身份。

连接摘要至少区分：未安装、未探测、检查中、登录中、需登录、已连接、禁用、错误、陈旧。分组使用服务商的真实可用账号/能力；已有账号的临时 refresh 不应让整行跳到“其他”。错误在所属分区保留，不能只发 toast 后显示正常。

账号配额与用量通过 Core 现有全局 WS 资源主题交付，Query Cache 唯一持有服务器事实；不在每个 popup 建连接或永久轮询。第三方 protocol 要求定时读取时，由单一域 owner 合并请求、退避、隐藏时暂停，带 readAt/stale；不把“读取失败”变成“额度为零”。

## 6. 必须改动与可复用触点

| 范围 | 可复用 | 必须补齐/调整 |
|---|---|---|
| Provider 总览/详情 | settings Scene、SettingsContentShell、共享 Card/List/Popover/Button、账号 CRUD 与 Runtime Query | 同一入口聚合 connected/other；详情 accounts/key/runtime；通过 URL 保存选中 provider；窄容器重排 |
| 账号生命周期 | AIAccount controller/service、login job、CredentialGate、LaunchLease | 每新增 provider 官方登录需独立能力声明、原生 logout/clear 范围、版本支持、取消/重连状态；不能先画成功按钮 |
| 模型目录 | Catalog、DetectModel、现有账号 models/thinking API | 账号作用域与 revision、加载/陈旧/失败、同主体旧目录 fallback；前后端能力模型一致 |
| Runtime 维护 | 安装路径探测、默认/启停 API、Session owner | 真正 update/restart API、operation state、managed/system 区分；运行中任务等待策略；探测不能冒充更新 |
| 组合模型选择 | ExecutionRuntimeControl、ExecutionModelControl、兼容过滤、聊天选择 callback | 一个浮层持有组合草稿，确认时原子 patch；Fast 从原生 capability 贯穿契约与执行；provider/account/model切换后重新校验 effort/fast |
| Token/费用 | Usage event、JSONL、实际执行配置、generated client | 有界持久 usage records/幂等 counter、nullable known mask、聚合 API/Query、费用来源日期/覆盖率；历史未知不伪造 |
| Quota/reset | AIAccount lease、Management、统一资源事件 | 原生只读 quota/reset port、per-account/group 缓存、popup/banner、reset confirmed outcome、真实 re-read；不支持则不展示可执行 reset |

主要文件：

- 后端合同与原生映射：`server/internal/agent/contract.go`、`native/contract.go`、`native/connection.go`、`native/registry.go`、Catalog、Session/LaunchLease；厂商 wire/fast/quota 适配在独立 acp-go。
- 账号与目录：`server/services/aiaccount/{service,login,kimi,capabilities}.go`、`server/internal/accountconfig`、`server/apis/ai_accounts`、`server/repositories/models/ai_account.go`。
- Runtime：`server/apis/runtime_options`、`server/services/runtimeenvironment/service.go`、`server/repositories/models/runtime_environment.go`。
- UI：`modules/settings/{settings-navigation,pages/settings-scene,components/ai-accounts-section,components/runtime-environments-section}`；`shared/ui/{execution-selection-controls,execution-model-control}`；`modules/chat/components/chat-composer-controls` 与实际 chat selection/submit hooks；`shared/api/runtime-queries` 与既有资源事件。
- 用量展示新增独立领域模块，但数据源继续消费既有执行/usage 事实；不得让 settings 页扫描所有聊天并计算第二套累计值。OpenAPI/Client 仅经生成命令更新，用户文案进入 i18n。

## 7. 验收矩阵

| 场景 | 必须观察到的结果 | 验证层 |
|---|---|---|
| 空库/CLI 未发现 | Shell 即时挂载，其他列表可见，显示未探测/未安装真实状态 | 前端行为 + 真实平台 |
| 已安装但未登录 | 不进入已连接，不提供可执行模型；连接动作可达 | DTO/前端/真实 CLI |
| 一个已连接账号，多账号 checking | provider 保留已连接；账号状态独立，失败不污染其他账号 | 账号服务 + UI |
| API Key 保存/编辑/取消 | 默认脱敏；错误就近；取消不保存；列表/事件/日志无密钥 | API/脱敏测试 + UI |
| Codex A/B 并发执行 | 使用不同认证主体；不写真实用户配置；允许同账号合理并发 | 服务测试 + 真实 CLI |
| Token 轮换并发/取消 | 一次协调刷新；已收到的新 token 耐久保存；旧投射不覆盖 | CredentialGate/刷新测试 |
| 登录/删除/改配置时忙 | 活跃执行保持 owner；明确 busy 或下一轮生效；不隐式 kill/replay | Session+账号集成 |
| 账号切换后的目录 | 请求/缓存 identity 都是新账号；无跨账号 fallback | API/Query race 测试 |
| 账号 A 有模型、B 无权限 | B 不借用 A 的可用结论；实际 setter 拒绝准确展示 | 目录+真实账号 |
| 修改默认/禁用 provider | 影响后续接纳；已接纳执行事实不变；反向启用/更换默认可恢复 | API/执行回归 |
| 点击重新探测 | 只发生 probe；不声称程序已更新或任务已重启 | API/UI |
| 更新 Runtime | 显示下载/校验/安装/失败/可重试真实状态；失败保留原版本 | 平台安装集成 |
| 重启时存在活跃 turn | 等待或明确说明须停止；无丢失 ownership/重复 Prompt | 生命周期集成 |
| 组合选择 provider/account/model | 一次 patch；不可观察到错配中间态；当前执行与下一轮值区分 | RTL+执行接纳 |
| 模型切换 effort 不支持 | 重选明确默认或要求选择，不发送旧模型档位 | 能力/前端行为 |
| Fast 支持/不支持/关闭 | 仅支持时出现；原生收到真实 setting/tier；关闭显式清除 | acp-go协议+宿主集成+真实模型 |
| Token 累计重复/恢复/重启 | counter 幂等；基线不算消费；不出现负数或重复费用 | Repository/事件回放 |
| 用量缺失/部分分量未知 | 显示完整性与未知值；已知零仍可区分 | 统计 API/UI |
| 无价格/自定义模型/长上下文未知 | 保持未估价；不显示免费；估价依据可查 | 定价规则/统计 UI |
| 配额无读取能力或离线 | 未报告/不可用/陈旧分别显示，不能显示 100% 或 0 的伪读数 | Adapter/API/UI |
| 两账号不同模型 quota group | popup 读取并显示各自窗口/resetAt，账号间不串数据 | 配额服务+真实账号 |
| Reset credit=0/未知/支持 | 只在真实可用时给动作，确认后调用一次；失败/无操作正确说明 | 原生边界/前端 |
| reset 完成 | 依据重读结果更新 quota/banner/hold，不把 toast 当额度事实 | 事件集成 |
| Light/dark/窄容器/长中文/键盘 | 无普通 Card 常驻边框阴影、无全容器焦点框；有可达焦点、返回/恢复、Skeleton | 样式检查+真实视觉 |

专项回归应首先复用 `aiaccount/service_test.go`、`prepare_wait_test.go`、`login_test.go`、账号 controller 测试、`runtimeenvironment/service_test.go`、执行账号/原生账号验证测试，以及已有 settings/composer/model-control 的定向测试。测试不得镜像组件内部 state；应覆盖主体隔离、原生调用参数、用户可见结果和反向恢复。

## 8. 尚未验收

只读审计未运行产品测试、真实认证、模型推理、费用数据采集或视觉验收。上述缓存身份风险与外部 CLI 凭据锁边界属于静态结论，不能写成已复现缺陷。真实账号配额/reset 来源、每 provider 原生支持与平台版本需要在 acp-go 适配实现后逐项验收。没有修改产品代码、依赖或数据库。
