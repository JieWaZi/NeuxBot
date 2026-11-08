# 标准插件、共享账号与管理扩展设计

> 状态：阶段一已按用户确认范围实现，进入联合验收；阶段二仅为后续方案，另行批准。真实平台与页面验收以实施计划记录为准，不授权清理数据、提交或发布。
> 配套：[实施计划](../plans/2026-11-07-plugin-contracts.md)、[资源库 UI 设计](2026-11-07-plugin-ui-alignment-design.md)。

## 1. 依据与实施范围

本文固定[云端评审稿](https://chatgpt.com/space/page_5e3096e2e94c81919c00e9e6719cb45e)中用户已确认的完整阶段一，以及另行批准的阶段二边界。统一 browse、唯一标准包、独立共享账号、安装更新与前端草稿／管理交互共同构成阶段一；browse 的完成不单独代表整阶段验收通过。

阶段一交付唯一标准包、完整展示资料、提示词草稿交互、新版配置与账号契约，以及安装到固定版本执行的闭环。阶段二增加独立的只读管理 RPC、通用管理分区与 GitHub 可访问仓库样例。其他供应商管理、多账号、GitHub App 授权、执行仓库白名单、自定义页面和业务写操作不在这两个阶段内。

官方包说明支持根 `plugin.json`、`skills/`、`mcp.json` 与 `extensions.com.openai`；官方也允许兼容 overlay。本产品选择唯一根格式并移除 overlay 输入，属于 NeuxBot 的范围选择。每个 MCP 服务的认证声明位于该服务的 `extensions.com.openai.auth`，不能放到插件身份的 OpenAI 扩展中。[OpenAI 包说明](https://developers.openai.com/plugins/build/plugins)

`com.neuxbot`、`.neuxbot` 和 `neuxbot-auth/1` 是本轮实现的 NeuxBot 自有扩展；`neuxbot-management/1` 是阶段二拟定协议名，尚未实现。不能把这些命名或下面的账号模型描述为 OpenAI、Agent Plugins 或 OpenBot 的现有协议。

## 2. 当前实现与职责

| 位置                                                                                                                                                        | 当前职责                                                                                           |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| [package.go](../../../server/internal/plugins/package.go)                                                                                                   | 只接受根标准清单与固定组件路径；记录 neuxbot-plugin/1，拒绝旧输入与不支持的已声明能力              |
| [presentation.go](../../../server/internal/plugins/presentation.go)                                                                                         | 归一根 com.openai.interface 及显式 NeuxBot 本地化引用，校验提示词索引、主题资产和安全链接          |
| [integration.go](../../../server/internal/plugins/integration.go)、[configuration.go](../../../server/internal/plugins/configuration.go)                    | 只解析根 com.neuxbot 的显式配置、MCP／Skill 秘密绑定及授权入口，不推导第二份组件事实或隐式秘密字段 |
| [catalog](../../../server/internal/plugins/catalog/catalog.go)                                                                                              | schemaVersion 1 目录索引与公开资料分别校验来源、摘要及大小；公开读取不取安装归档                   |
| [models/plugin.go](../../../server/repositories/models/plugin.go)、[Plugin Repository](../../../server/repositories/controllers/plugin_account.go)          | 保留资源、版本、伙伴安装与配置四事实，独立 PluginAccount 保存 generation 和加密材料                |
| [authorization](../../../server/internal/plugins/authorization/authorization.go)、[commandbridge](../../../server/internal/plugins/commandbridge/bridge.go) | 同一个资源 owner 管理等待、占用与安全摘要；neuxbot-auth/1 严格初始化和上下文校验，取消等待真实退出 |
| [插件 API](../../../server/apis/plugins/router.go)                                                                                                          | 拆分浏览、静态详情、显式配置预检、cached account、check、会话、安装和原子更新；没有旧入口转发层    |
| [execution/plugins.go](../../../server/services/execution/plugins.go)                                                                                       | 冻结真实安装的版本、配置修订、账号代次与实际组件，取得本地材料后投射至当前目标，不在线授权         |
| neux-plugins 的 Schema、发布与 auth 目录                                                                                                                    | 固定标准／扩展 Schema、来源、工件和供应商程序；包内 launcher 隔离供应商环境，宿主没有供应商分支    |

完整性校验、受管目录、来源键、发布摘要、信任确认、秘密加密、取消等待、最后引用删除和原生消费者租约继续由原模块拥有。阶段二没有独立领域模型、协议入口或 GitHub 仓库查询实现。

## 3. 阶段一：唯一输入包与展示资料

### 3.1 包输入与发布约束

逻辑包根只接受声明 Agent Plugins 1.0.0 Schema 的 `plugin.json`。技能从 `skills/` 发现，MCP 从 `mcp.json` 发现。文件缺失表示该能力未提供；格式错误、越界引用或不支持的已声明执行能力不能被解释为空能力。

OpenAI 信息只来自根 `extensions.com.openai`。不读取根 `interface`、旧客户端展示名、`.codex-plugin/plugin.json` 或 `.claude-plugin/plugin.json` 作为输入；发布包的逻辑根不得夹带冲突的旧入口。根标准字段的缺省展示和隐藏可选区块是正常产品行为，不是旧字段回退。

`extensions.com.neuxbot` 第一阶段固定以下形状，Schema 由包仓库维护并与宿主测试固定在同一版本：

| 字段             | 定义                                                          |
| ---------------- | ------------------------------------------------------------- |
| `schemaVersion`  | 当前为 1；声明但不支持的版本拒绝准备和安装                    |
| `presentation`   | 可选包内相对路径，仅允许引用 `.neuxbot` 内的展示资料          |
| `configuration`  | 可选 `fields` 与 `bindings`；不重复标准身份、技能或服务器组成 |
| `authentication` | 可选独立授权驱动声明；精确协议、入口、参数、权限和组件绑定    |
| `management`     | 当前拒绝；阶段二拟增加独立声明入口，不要求存在 authentication |

本轮只交付阶段一，包不声明 management；遇到该能力必须明确返回本目标未支持，不能静默当作没有声明。没有 `com.neuxbot` 的基础标准包合法。

配置字段保留必要的 key、标签、描述、secret、required、非敏感 default；绑定只允许作用于已发现组件和已声明字段。服务器环境与请求头使用宿主已有的精确注入规则。删除旧 `displayName` 与 `requiredComponents` 重复声明，以及从任意旧模板自动补秘密字段的规则。

发布构建固定标准 Schema 与扩展 Schema 的版本、摘要和校验器；不在用户安装时临时下载可变 Schema。扩展、展示或组件校验失败阻止发布与准备。供应商代码、来源锁及运行依赖继续由包仓库交付。

新的不可变解析快照必须记录宿主目标解析契约版本。资料读取、安装、账号操作、组件诊断和执行接纳只接受该目标快照；缺标记不能猜测为当前格式。仅删除 Load 的旧分支并不足够，因为现有执行会直接使用数据库内的已解析 Package。旧快照保留在存储中，不自动改写或执行；重新准备必须经过可信目录、完整内容及新版解析校验。

### 3.2 标准展示字段与 UI

标准 interface 是名称、简介、长描述、开发者、分类、链接、提示词、能力说明、颜色和图标的唯一作者来源；用户明确重命名仍优先。详情信息继续如实区分业务上游与封装维护者。

`defaultPrompt` 接受标准支持的字符串或字符串数组，内部只归一为一个数组。`capabilities` 只作为作者能力说明；不授予工具、服务器或账号权限。新增 `supportURL`；品牌色使用独立字符串 `brandColor`／`brandColorDark`，图标使用 `logo`／`logoDark` 与 `composerIcon`／`composerIconDark`，不接收对象形状。不把品牌色转换为可执行样式或任意 CSS。[OpenAI 展示字段与提交要求](https://developers.openai.com/plugins/deploy/submission)

Schema 合法性、宿主资源上限与公开商店提交要求分别验证。公开商店要求的所有链接不能机械变成纯技能本地包的安装前提。具体长度及分类集合必须由发布工具锁定的标准资料给出；不能从文档示例拼造一个“官方分类全集”。浏览 API 的 category 筛选只消费已规范化的安全摘要，不负责替代发布校验。

`.neuxbot/presentation.json` 承载已有双语名称／简介映射、`defaultPromptLocalizations` 与 `setup`。`setup.instructions` 使用 en、zh-CN 的安全 Markdown，`setup.helpURL` 为公开 HTTPS 帮助链接。每种提示翻译数组与标准提示数组等长、按固定版本索引对应；缺翻译使用标准原文，错长拒绝发布与准备。不另设 examples 或 prompts 同义来源。

正文顺序为所选语言 README、英文 README、longDescription、短简介；相同内容不重复。主题图标沿用现有大小、静态 SVG 与路径边界校验，声明不同图标时分别固定摘要，不能只保留一张图后猜测另一主题。

提示词点击创建独立真实草稿，只填入输入框。全局入口先选伙伴，伙伴入口保留当前伙伴；无伙伴提供创建入口。只有本伙伴已安装且启用预览固定版本、来源摘要一致时才可继续，并附加真实插件引用；未安装、停用或版本不符时禁用试用并就近说明，安装通过既有入口显式完成。目标选择、安装取消、返回和路由切换不得自动发送或丢弃已有草稿。

### 3.3 目录与公开资料

安装仍核验可信目录的来源提交、完整归档摘要及 packagePath。详情、原文、技能等公开资料继续绑定同一来源与摘要；不得通过公开资料取得可执行入口或信任。

目录索引只增加列表需要的安全摘要；defaultPrompt、连接说明、主题资产及安全认证说明进入内容寻址的 details 资料。认证命令、参数、环境变量、端点秘密绑定、配置值和凭据均不进入公开资料。第二阶段允许进入 details 的 management 信息只有静态标题、字段与查询能力说明，没有驱动入口。

包仓库先生成新的 catalog/details/ZIP 契约夹具，宿主与发布器共同校验。删除仅服务旧 details 或旧目录 Schema 的回退；不能保留“资料失败则安装归档补详情”的隐藏路径。目录索引与公开资料工件均固定为 schemaVersion 1；不可变解析快照标记为 `neuxbot-plugin/1`，`com.neuxbot.schemaVersion` 为 1。两仓库使用同一版本及校验夹具。

## 4. 阶段一：配置、共享账号与固定安装

### 4.1 持久事实及 owner

本节固定阶段一领域边界：

| 事实                 | 权威与约束                                                                                                            |
| -------------------- | --------------------------------------------------------------------------------------------------------------------- |
| 插件资源             | 继续拥有稳定 pluginId、sourceKey、归属和用户资料；名称不作为来源身份                                                  |
| 不可变版本           | 继续拥有完整内容、摘要、来源、解析结果与执行信任；相同版本号不能改指其他内容                                          |
| 伙伴安装             | 继续拥有 versionId 与 enabled；停用仍是安装；准备和认证成功均不自动产生安装                                           |
| 共享配置             | 继续拥有普通值、加密秘密与 revision；不再拥有账号交付材料                                                             |
| 共享账号             | 唯一 `PluginAccount` 记录，以 pluginId 为主键，拥有 `generation` 和加密私有材料；同插件的伙伴共享它，多账号不在范围内 |
| 公开核验摘要         | 仍由当前 Core 的唯一 owner 缓存；包含主体验证信息和宿主 checkedAt；重启后未核验，不恢复动态可用状态                   |
| 进程、授权等待与占用 | 归同一个插件 owner；当前授权、安装预检与执行共用占用边界，阶段二不能另建平行注册表                                    |

账号 generation 是持久身份切换的 CAS 事实，不是第二份配置 revision。登录完成、明确退出和账号材料身份改变时推进；普通描述、配置或页面读取不推进。配置修订与账号代次共同限定后续资源。材料按实际版本摘要、组件和端点固定；对当前安装仍保留的多个版本分别验证，不能把目标新版材料当成旧版本的通用材料。多版本预检在唯一 owner 的同一占用内顺序执行各自固定版本命令，不创建第二个 Authorizer。显式登录已经改变外部身份后，材料解析或本地提交失败仍按原账号代次撤销旧材料；CAS 冲突不得覆盖较新的身份。

账号 owner 的进行中操作还必须有不可复用的 operationId／上下文令牌。开始替换、取消或退出即阻止旧请求发布新摘要；失败操作不能凭相同 generation 恢复迟到身份。数据库提交失败或外部认证失败保留原伙伴安装，公开账号状态准确标为未确认或失败；不能声称外部登录已随数据库回滚。

账号和配置操作可以针对已准备、已信任但尚未安装的明确版本；本轮没有动态管理入口。伙伴静态详情始终显示自己的版本，不为浏览切换驱动。当前 owner 的驱动指纹是实际受管进程事实；只有显式管理操作能请求变更，并受活跃执行和等待会话约束。

执行继续在接纳时冻结安装版本和配置修订，并增加账号代次／材料上下文。热执行持有原租约，退出和更新不能破坏活动工作；无法恢复原事实的重试明确拒绝。聊天候选不读取账号，不因暂未核验隐藏已启用安装。

### 4.2 新授权声明与协议

`authentication` 只描述宿主理解的驱动入口与允许交付边界：协议为 `neuxbot-auth/1`；入口是包内受核验程序，或受信 Node 加包内脚本；argv 独立传递。权限来自明确声明。MCP 绑定固定组件键、HTTPS 地址及允许请求头；Skill 绑定固定组件及允许环境键，私有路径只可指向宿主分配的插件账号目录。浏览器和驱动均不能扩大目标集合。

每个受管进程先完成严格初始化：请求携带 protocol、operationId、pluginId、versionId、digest、configurationRevision、accountGeneration，以及固定 sessionId、dataDirectory 和 scopes；返回准确协议及逐字段回显的 context。后续方法携带同一上下文。旧协议、未初始化、重复初始化、上下文变化与其他 operationId 的通知均拒绝，不提供别名或自动升级：

| 调用                           | 必须冻结的内容                                                                                           |
| ------------------------------ | -------------------------------------------------------------------------------------------------------- |
| `auth/initialize`              | 协议、operationId、pluginId、versionId、完整摘要、配置修订、账号代次；驱动返回准确协议并逐字段回显上下文 |
| `auth/start`                   | 宿主会话 ID、已批准的权限集合；返回或通知该会话的挑战与截止时间                                          |
| `auth/status`                  | 显式核验当前账号与真实权限；不写材料、不隐式登录、不推进账号代次                                         |
| `auth/resolve`                 | 宿主给定组件与端点集合；只返回所需私有材料，精确校验后保存，不公开给前端                                 |
| `auth/cancel`                  | 指向明确会话或请求；宿主等待实际调用及进程终止再释放占用                                                 |
| `auth/logout`                  | 清理受管账号；成功后清除材料并推进代次，失败保留安装供重试                                               |
| `auth/challenge`、`auth/state` | 必须绑定初始化上下文、会话及挑战；迟到或上下文不匹配的通知丢弃                                           |

固定实现与测试向量见 `commandbridge.Config`、`OperationContext`、`Driver.call` 及包仓库协议 Schema；每个受管进程先完成 initialize，后续请求携带同一上下文，通知同时核对 operationId 与 sessionId。复用当前挑战、二维码、验证码、取消等待和进程组清理的安全限制；不通过放宽限制完成新协议。驱动不决定宿主 checkedAt，也不能通过事件直接宣布安装成功。

MCP 标准认证与上述包内授权是不同输入。当前支持边界与目标交付由解析及执行测试固定。阶段一只接纳 none 和 api_key：api_key 固定 HTTP 目标、精确请求头和显式必填秘密配置或包驱动绑定，经实际目标的 MCP 投射交付。oauth、mixed 及当前未支持的执行能力返回 PluginCapabilityUnsupported；格式错误返回 PluginManifestInvalid。Codex 的包驱动头绑定或显式秘密配置走 MCP 投射，不依赖不能覆盖标准 mcp.json 的旧 overlay；没有这些绑定的标准包可直接交付根目录。不能去掉认证后继续，也不能自动借用 AI 账号或独立 MCP 的另一份身份。供应商专用 CLI／扫码只由显式包内驱动实现，不在宿主增加供应商分支。

Skill 的 environmentKeys 精确限定允许环境键，privatePathKeys 是其中必须位于插件私有目录的子集；PATH 也须显式声明。宿主按实际技能组件验证材料并将私有程序目录置于受信继承 PATH 前。飞书和企业微信包只返回固定允许键及私有 launcher 目录；launcher 通过固定 Node 与摘要绑定程序绝对路径启动 CLI，清除继承的供应商覆盖和 Node 预加载变量后设置私有 profile，不能回退用户全局 CLI。完整组件集合的一次 resolve 只准备一次 launcher，不按技能重复改写。

### 4.3 面向前端的当前接口职责

以下固定新版接口职责，字段类型以生成 OpenAPI 为准；不建立旧接口适配或双写：

| 用例                                                                 | 请求和响应重点                                                                                                                   |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `GET /plugins/browse`                                                | 保留已完成的来源合并、category、准确窗口和窄摘要；不扩成配置或账号管理查询                                                       |
| `GET /plugins/catalog/:key`                                          | 只读固定目录目标与公开资料，返回完整标准展示和提示词，不启动程序                                                                 |
| `POST /plugins/catalog/:key/prepare`                                 | 固定预期归档摘要与信任确认；返回明确 pluginId、versionId 和静态组件，不建立伙伴关系                                              |
| `GET /plugins/:pluginId`                                             | 返回当前入口明确的实际版本、组件、安装事实及精确缓存账号；不返回配置字段、值、秘密标记、ready 或 blockers                        |
| `GET /plugins/:pluginId/configuration`                               | 显式读取固定 versionId 的字段、普通值、秘密存在标记、revision、generation 及本地 ready／blockers；不启动供应商                   |
| `PUT /plugins/:pluginId/configuration`                               | 按 expectedRevision 保存配置并验证实际保留版本；不要求账号代次、不切换伙伴版本                                                   |
| `GET /plugins/:pluginId/account`                                     | 安全缓存摘要、明确上下文版本、revision、generation；不在线核验、不创建桥接                                                       |
| `POST /plugins/:pluginId/account/check`                              | 显式核验；请求固定 versionId、配置修订及账号代次，迟到结果不能覆盖新上下文                                                       |
| `POST /plugins/:pluginId/account/authorizations`                     | 固定同一上下文与权限，返回新会话 ID，后续挑战通过既有事件和会话读取对账                                                          |
| `GET`／`DELETE /plugins/:pluginId/account/authorizations/:sessionId` | 通过明确 sessionId 和固定版本读取、取消同一次等待；取消还携带原 revision／generation，关闭弹窗和迟到启动沿原上下文补偿           |
| `POST /plugins/:pluginId/account/logout`                             | 固定 versionId、预期配置修订及账号代次；关闭相关闲置消费者，活动引用返回 busy；退出清理材料，保留安装                            |
| `POST /plugins/:pluginId/installations`                              | 请求 versionId、wakerIds、expectedConfigurationRevision、expectedAccountGeneration；逐伙伴返回真实结果，重复安装保留旧版本和启停 |
| `POST /plugins/:pluginId/updates`                                    | 固定目标版本、预期配置与账号代次、完整旧安装集合及目标配置变更；全体安装与配置同事务提交                                         |
| 伙伴启停／卸载、整体删除                                             | 继续使用真实关系及完整影响确认；最后引用清理失败保留关系供重试                                                                   |

更新的外部准备、真实授权和材料解析在数据库事务外完成并持有内容／身份占用；最终短事务重新核对目标、完整伙伴集合、逐伙伴旧版本、配置修订和账号代次。任一变化整次拒绝。普通配置保存不承担隐式版本切换。

## 5. 阶段二：独立只读管理与 GitHub（后续方案，待另批）

### 5.1 声明、能力与结果

`management` 引用 `.neuxbot/management.json`，声明 `schemaVersion`、独立 driver 及 sections。第一版仅有 information、collection；字段为 text、boolean、integer。认证可以缺省，管理与授权程序也可以是不同入口，但资源和账号 owner 相同。

逐行 JSON-RPC 协议为 `neuxbot-management/1`。`management/initialize` 绑定宿主 sessionId、pluginId、versionId、内容摘要、管理驱动指纹、配置修订、账号代次和 operationId；匿名能力不伪造账号前提。返回的 source、类型、搜索与分页能力必须是静态声明的子集。`management/read` 只接受已允许 source 和受限参数，`management/cancel` 只取消当前 owner 下的明确请求。

结果由宿主按声明投影为字段、列表和游标；checkedAt 来自 Core，不能透传原始 CLI 输出、任意对象、HTML、脚本、凭据或私有路径。首版上限沿云端提案：50 项／页、8 列、4 KiB 文本值、4 KiB 游标、256 KiB 响应、30 秒读取。超过上限返回准确错误，不截断后声称完整。

`search=loaded` 只过滤同一上下文已加载项，不向驱动传 search，也不为了搜索扫描远端所有页；`server` 仅在初始化确认后传递。游标绑定完整上下文与查询参数，不是用户可提供的任意 URL。

### 5.2 HTTP、生命周期与缓存

阶段二拟新增安全描述读取、显式 `POST /plugins/:pluginId/management/reads` 与明确 requestId 的取消用例。描述响应只有分区及能力状态，不启动驱动。POST 返回 requestId；完成、错误或取消经既有资源事件唤醒对应结果查询，无新全局连接或轮询。请求 idempotency、完成结果保留期限及查询路由在阶段二协议任务中同时冻结，不能由前后端各自补参数。

请求由用户点击读取或刷新接纳。Core 核验实际安装、固定内容、执行信任与账号条件，向同一 owner 领取内容和身份租约，再启动管理入口。读取前后核对完整上下文；账号、配置、版本、驱动或 owner 代次变化即丢弃旧结果。取消、超时和退出均等待真实调用终止后释放资源。

管理结果只在当前 Core 内有界保存，键包含完整上下文、source、搜索与游标；上限为 100 个分页结果及 10 MiB，淘汰未使用的最旧结果。重启清空动态结果。账号／配置提交使用现有事件精确失效；管理读取不发布安装变化，不刷新聊天插件候选。

错误必须区分未授权、需信任、忙碌、超时、取消、上游失败、协议不支持、内容无效和上下文过期。管理错误不改写基础能力或伙伴关系，不自动重新登录。

### 5.3 GitHub 验收包

供应商账号登录和仓库查询都在 neux-plugins 的包内实现，宿主不导入 GitHub SDK、不解析 gh 原始输出。样例提供当前账号、实际权限和可访问仓库，采用用户仓库接口的明确关系口径。客户端只搜索当前已加载名称；驱动封装与账号绑定的上游分页，不把上游 URL 作为宿主游标。

没有 GitHub App 的“选择授权仓库”入口；可访问仓库结果也不是执行白名单。成功空集合与查询失败必须分开。至少用两个身份验证迟到结果、退出、游标复用和权限变化，使用虚构测试仓库和独立测试凭据。

## 6. 删除路径与数据边界

阶段一已删除旧输入的解析、生成、正向测试夹具及无消费者调用：旧清单入口、旧命名空间／目录、旧 auth wire 协议、原生源包引用、旧公开资料回退，以及把配置保存当作更新命令的旧 DTO。测试替换为目标格式的正常、拒绝和故障用例，不新增历史兼容矩阵。

Runtime 为其他 CLI 生成适配产物与“接受旧格式作为输入”是两个职责。Codex 验收根标准部分；其他已支持目标只生成其明确支持的派生配置。外部 CLI 原生插件发现、启停及账号仍由外部 CLI 管理，不能随本项目输入格式删除而一并删掉。

旧 `NEUXALLY_*` 等插件专用技术键只在确属本次新协议且所有消费者同步替换时删除；`ALLY_*`、原生账号、知识服务及资源 URI 不做全仓库字符串重命名。第三方许可和 provenance 继续保留。

不编写用户数据迁移、自动清库、启动时删旧包或账号清理脚本。开发与验收使用独立新数据目录。现有内容遇到不支持的新解析要求时明确拒绝接纳，不能被重新解释成目标格式；任何真实数据清理必须另行获得明确范围和授权。

## 7. 验证与完成门槛

阶段一验收覆盖标准及扩展 Schema 夹具、新格式包仓产物、宿主公开资料白名单、新账号代次和安装事务测试、生成契约，以及选定 Codex Runtime 的真实标准加载证据。页面需验证提示词只填草稿、固定版本、未核验授权、退出影响、明暗图标与窄容器。没有真实 Runtime 或页面验证时明确记录剩余边界。

阶段二增加匿名管理包与 GitHub 包两条路径，覆盖 source 能力交集、参数与游标绑定、查询中身份／配置变化、完整分页、上游错误、超限、取消实际终止、缓存淘汰和秘密投影。管理程序是受信任执行代码；“只读方法”本身不构成操作系统沙箱。

查询保持单表条件、EXISTS／IN 及有界补齐；不新增 JOIN。多次一致读取使用短事务，外部程序和网络在事务外。所有新增业务枚举、错误与公开字段遵守后端生成和注释规则。按实施计划运行定向包、定向前端用例、协议生成与文档检查；不例行运行全量检查。
