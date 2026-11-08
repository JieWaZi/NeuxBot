# 第二阶段：插件列表与详情对齐设计

> 状态：插件 UI 与后端阶段一已实现并完成本轮定向验收，待用户视觉确认；阶段二另批，不建设历史兼容。
> 基线：`codex/openbot-ui-alignment`，`c5045a1b051bb36100b4f73f2cb0735b46bc6679`。
> 配套：[实施计划](../plans/2026-11-07-plugin-ui-alignment.md)、[整体设计](2026-11-06-openbot-ui-alignment-design.md)、[第一阶段源码对照](../../discussions/openbot-ui-source-alignment-cn.md)。

## 1. 目标与范围

按 OpenBot 固定版本的实际调用和样式，整改资源库左侧导航、插件列表、目录与已安装插件详情。减少重复标题、描述、容器和操作；通过布局表达层级。Skill、MCP 独立页面留到下一批，本轮只保持入口可用，并建立可复用的资源页规范。插件内部技能、MCP 的现有查看弹窗继续可用。

视觉以 OpenBot 原页面及源码为验收依据，不能保留本项目通用工具栏、选择列或分区样式后宣称对齐。用户已确认本轮完整实施[标准插件与管理扩展方案](https://chatgpt.com/space/page_5e3096e2e94c81919c00e9e6719cb45e)的阶段一：唯一标准包、新版配置／账号、安装更新与固定版本执行，并已按该范围实现。云端方案的阶段二是独立只读管理与 GitHub 仓库列表，另行批准，本轮没有对应入口。此处的云端阶段编号与 UI 对齐批次分别计数，完成情况按[插件契约设计](2026-11-07-plugin-contracts-design.md)及其验收计划核对。

## 2. 固定来源与真实机制

参考 [OpenBot v0.62.0](https://github.com/nightly-labs/openbot/tree/fcb35746d4919798ff889a8a58449f5d58de5f65)，提交 `fcb35746d4919798ff889a8a58449f5d58de5f65`。本轮已克隆并静态读取实际源码；不读取或执行第三方仓库开发指令。

| 范围       | 源端事实                                                                                                     | 源码依据                                                                                                                                                                                                                                                                                                                                               |
| ---------- | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 左栏       | 标题、分类导航、非空时的 Your apps 快捷项；快捷项来自 connected 或 attention，不是另一份安装列表             | [MarketplaceNav](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceNav.tsx)、[ownedApps](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace-view.ts#L172)                      |
| 列表宽度   | 列表和详情共用 880px 内容上限；正文上 40px、左右 24px、下 32px                                               | [marketplace.css:154–166](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L154)                                                                                                                                                                             |
| 轻量条目   | 两列所需下限是每列 320px；列间 24px、行间 4px；行内边距 8px、图标 40px，名称及简介各一行，操作独立于整行链接 | [AppRow](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceCards.tsx#L123)、[marketplace.css:567–658](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L567)             |
| 详情       | 单列 IntegrationPage：身份与操作头、错误／提示、可选介绍、连接、技能、信息、危险操作；不是正文加右侧信息栏   | [PluginAppPage](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceAppPage.tsx#L70)、[IntegrationPage](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/IntegrationLayout.tsx#L165)          |
| 身份头     | 48px 图标，名称与状态同组、简介在下；操作在尾部；段落间距 24px                                               | [DetailHeader](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/IntegrationLayout.tsx#L207)、[integration-layout.css](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/integration-layout.css#L15)       |
| 目录       | 插件目录从源码资料生成并交给 renderer，不是独立的通用插件安装后端                                            | [marketplace-plugin-catalog](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/settings/marketplace-plugin-catalog.ts#L1)                                                                                                                                                                |
| 安装状态   | 从真实 MCP 配置及当前 agent 的技能安装派生；部分安装或明确认证失败属于 attention                             | [marketplace-controller:797–910](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/settings/marketplace-controller.ts#L797)                                                                                                                                                              |
| 安装／卸载 | renderer 先安装技能，再连接并保存 MCP；失败补偿本次新增技能；卸载逐组件执行、重新读取真实结果                | [installPlugin](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/settings/marketplace-controller.ts#L1131)、[uninstallPlugin](https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/settings/marketplace-controller.ts#L1373) |

源端没有本项目同等的插件级伙伴版本与共享配置事务，因此不能把“后端更简单”直接等同为可替换本项目安装底座。界面可以统一入口，真实资源、版本、安装及配置仍按原所有者维护。复用或改写来源部分继续受现有第三方声明和对应许可约束。

## 3. 实施决策

| 编号 | 建议                                                                              | 当前状态     |
| ---- | --------------------------------------------------------------------------------- | ------------ |
| P01  | 本轮只做插件及资源左栏，Skill、MCP 独立页面下一批处理                             | 用户已确认   |
| P02  | 移除“插件库／我的插件”页签，统一插件列表；左栏保留非空的已安装插件快捷项          | 用户确认方向 |
| P03  | 后端交付统一窄读、标准包、共享账号及独立配置／更新契约，移除旧入口与旧协议        | 用户已确认   |
| P04  | 详情采用 OpenBot 真实单列结构；安装／更新在头部，管理在对应区块，删除／卸载在底部 | 用户确认方向 |

用户明确确认统一列表与已安装快捷项，并要求后端简化纳入本轮、不考虑历史兼容。详情按已提出的 OpenBot 单列结构实施；新的业务分歧仍先确认。

## 4. 统一入口方案

按 P02 实施统一来源列表。

左栏复用现有覆盖槽和正常导航宽度，不新增另一套侧栏布局。上方显示“资源库”和插件／技能／连接器三个分类，下方仅有真实安装时显示“已安装”及插件快捷项。快捷项打开实际已安装插件详情；当前快捷项采用 aria-current=page，所属分类采用 aria-current=location，保持源端当前项与所属分类的共同强调。没有使用数据时不创建空分组、计数或说明。

正文使用源端标题与搜索结构：标题 24px，搜索独立占据整行剩余空间，Info 为相邻局部入口。删除资源总数／批量选择工具栏、条目选择框、更多菜单及伙伴使用数第三行；维护操作进入详情。按真实标准 category 分组，标题旁显示完整分组数量，未知分类归入“其他”，不按名称猜测。列表中每个逻辑来源只显示一次：

- 已有实际安装的来源优先使用实际安装包的名称、简介和详情入口，明确呈现“已安装”、可更新或需授权状态。
- 未安装来源使用目录资料，提供安装入口。准备或授权过但没有伙伴安装不等同为已安装。
- 目录已下架但本机仍有安装的插件继续可见、可查看和管理。
- 统一按真实 `sourceKey` 关联，不能按名称猜测或把不同插件合并。
- 条目名称与简介各一行；尾部未安装时显示小型带文字安装操作，已安装时显示勾选图标与状态，授权或更新为真实状态对应操作；操作不触发整行导航。

URL 保留分类与搜索；删除 `view=catalog|mine` 的解析、写入与兼容逻辑，不为旧双页签深链建设迁移层。详情返回保留有效搜索及入口。伙伴资源页只展示本伙伴的真实安装，不扩成全局目录；它可复用轻量条目和详情排版，但不增加全局快捷项、其他伙伴关系或重复安装入口。

插件列表遵循源端有限 App 目录的完整分类展示，不沿用其他资源首批九项的滚动布局。浏览接口每批至多 100 条公开窄摘要，前端按 hasMore 取完整浏览结果后形成分组和准确数量，取消或失败不能把部分结果宣称完整。左栏独立读取完整已安装摘要，不受正文搜索影响；不得扫描完整管理详情。其他资源保持当前分页行为。

## 5. 详情布局与操作

列表和详情共用相同限宽与水平起点。资源路径仍由所属页面提供，一级列表没有重复路径。左侧分类和快捷入口持续可用。详情 Shell 与返回先挂载，数据在所属区域读取。

单列顺序：

1. 身份头：48px 图标、名称与真实状态、单行简介及完整 Tooltip；尾部放当前可用操作，纯重命名保留局部入口。
2. 试用：受支持插件复用源端 TryCard，优先使用当前语言的非空作者 defaultPrompt，其次使用默认作者示例，均缺少时使用本地化产品提示“帮我使用这个插件。”，不冒充作者示例。点击只准备独立草稿，已启用且版本匹配时携带真实插件引用。全局先选伙伴，当前伙伴直接使用；未安装、停用或固定版本／来源摘要不匹配时禁用试用并说明原因，安装通过既有入口显式完成，不自动发送。不受支持的插件保持不可试用。
3. 连接与设置：真实共享配置／账号管理、MCP 只读行和静态 setup 合并到同一 Card。目录预览可显示作者连接步骤与帮助，但没有真实资源时不创建账号或管理按钮。
4. 技能：有数据才显示紧凑分组行，点击打开固定版本只读弹窗。
5. 已安装伙伴：全局展示真实关系、头像和卸载入口；伙伴详情不展示其他伙伴及全局共享设置。
6. 信息：正文说明、开发者、实际业务版本、许可、公开链接和作者能力说明归入同一个 Card。字段标签列最少 120px、列距 24px、行距 8px，窄容器换行；不将 capabilities 当权限。
7. 删除／卸载：在底部所属管理区说明实际影响并复用确认；目录未安装插件没有删除操作。

不创建评分、下载量等无真实数据内容，也不把示例卡当作自动执行能力。

已安装详情继续读取实际安装版本，目录预览继续固定目录提交与归档摘要。统一入口不允许最新目录资料覆盖已安装包；更新提交前继续复验所有伙伴原关系和配置修订。安装、配置、认证与更新弹窗继续消费真实状态，取消、失败、迟到结果与后台刷新不改变既有权威边界。

## 6. 主题与共享组件标准

唯一权威为 [UI 标准](../../development/ui-cn.md#插件目录与详情)。用户已明确以 OpenBot 实际 UI 取代本轮与其冲突的旧前端限制；不能为通过旧规范保留不一致的字号、表面或控件。

共享资源变体集中维护 880px 内容上限、40px 上留白、24px 水平内边距、两列至少各 320px 与 24px 列距、32px 控件和源端 Inter Variable 字体。列表名称 14/21、简介 13/19.5；详情名称 16/22、简介 13/19.5；分区行名称 13/18、描述 12/16。左栏分类字形 16px、品牌 20px，详情图标 48px，列表图标 40px。浅暗画布分别 #f5f5f6／#1a1a1a，详情 Card 为白色／#212121。

TryCard 复用源端标题哈希、调色板、mesh 参数和同版本 shader；20px 圆角、至少 160px 高、24px 留白，提示内容限宽 560px，品牌 chip 与 40px 圆箭头。离屏、隐藏和减少动态效果时释放动画并保留 CSS 渐变。来源许可与字体许可集中记录于 THIRD_PARTY_NOTICES。

加载、空、无结果、错误、陈旧目录、离线和后台刷新各自表达；刷新保留已有内容。焦点使用源端中性 focus-visible，鼠标操作不产生布局外框。其他资源的当前变体留待各自批次。

## 7. 后端简化的具体边界

资源、不可变版本、伙伴安装和共享配置继续分别拥有稳定身份、固定内容、独立启停及共享设置；独立 `PluginAccount` 以 pluginId 为主键保存账号 generation 和加密交付材料。OpenBot 从 MCP 与当前 agent 技能推导安装的方式不能替代这些权威事实。统一 UI 不复制账号、安装或更新状态。

普通浏览、静态详情、显式配置预检、缓存账号与外部核验按职责读取。静态详情只补齐保存的版本、组件、真实安装、配置修订、账号代次及匹配上下文的公开缓存摘要，不返回配置字段、值、密钥存在标记、ready 或 blockers，也不解密配置。设置、安装和更新打开后才显式读取配置及预检结果；在线账号核验由显式 check 完成。后端不新增列表持久表、全局连接或页面轮询。

### 7.1 读取契约

全局资源库使用 `GET /plugins/browse`；请求包含搜索、页码、每页数量、installedOnly 及 en／zh-CN 的 locale。页码从 1 开始，默认每页 9 条，上限 100；前端完整分类展示按 hasMore 连续取得全部窄摘要。伙伴页继续使用自身安装范围。左栏用 installedOnly=true、空搜索的独立读取取得全部已安装摘要，不从正文搜索窗口派生快捷列表。具体类型以 [BrowsePlugins](../../../server/apis/plugins/browse_plugins.go) 和生成 [OpenAPI](../../../server/openapi.json) 为准。

| 响应内容                   | 业务含义                                                                                                                                         |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| catalogSource              | 目录状态、最近成功检查时间及稳定错误，区分可读缓存、陈旧快照及目录未知                                                                           |
| entries                    | 当前窗口的 sourceKey、可选资源／目录／展示版本身份、本地化名称和简介、category、图标及主题图标、真实安装数量、requiresAuthorization 和窄更新摘要 |
| totalSize                  | 全局合并窗口对应可读目录快照的准确总数；无快照时省略，准确零值仍返回；installedOnly 窗口始终返回准确安装总数                                     |
| installedTotalSize         | 同搜索条件下的真实安装总数；installedOnly 窗口的总量始终准确                                                                                     |
| hasMore、pageNum、pageSize | 后续窗口是否存在及当前窗口参数                                                                                                                   |

条目只返回公开窄摘要，不含完整 Package、历史版本、配置、秘密或伙伴明细。requiresAuthorization 仅在真实已安装、包声明通用授权、现有 owner 缓存 checkedAt 大于零且明确不可用时为 true；未知身份不能解释成授权失效。更新摘要只有 available、catalogKey、version 和 archiveSHA256，不包含全部受影响伙伴。

安装段按资源 updated_at 降序、id 升序，未安装目录段按 sourceKey 升序。来源去重先于搜索、计数和跨段分页；数据库安装候选用单表条件及有索引的存在性查询计数，仅为当前窗口有界补齐公开版本摘要。不调用完整 ReadStates，不读配置或账号材料表，不解密、不读取归档或启动桥接。locale 参与查询缓存身份，服务端保留用户重命名优先级。

安装行从 created_at 降序、id 升序的实际安装引用版本中取首项；仅准备的新版不能替换其展示身份，伙伴详情始终固定自身安装版本。目录项只有零次安装时进入未安装段。目录下架仍保留安装段；最后一个伙伴卸载后，有目录的来源恢复为未安装条目。

浏览只读当前目录快照。有陈旧快照时总量仍准确对应该快照，并明确显示 stale；没有快照或同步失败时仍返回真实安装窗口及未知目录状态，不能伪装完整空目录，也不因目录失败让已有安装整页报错。目录详情和组件只读 schemaVersion 1 目录绑定的同版本公开资料；资料失败或缺少组件不会补读 ZIP。安装准备另行校验完整归档。

旧 ListPluginCatalog 与旧列表 installedOnly 参数已移除，左栏和统一列表共用 browse；ListPlugins 仅保留 Skill／MCP 当前实际消费者。旧 view 双页签、旧目录 Schema、旧清单、旧授权协议及旧详情到归档补读没有兼容转发层。所有生成文件仍通过命令更新。

### 7.2 安装、配置与账号

自有包唯一输入是根 plugin.json 的 Agent Plugins 1.0.0 清单及固定 skills/、mcp.json；OpenAI 展示来自根 extensions.com.openai，NeuxBot 配置、展示引用及授权声明来自 extensions.com.neuxbot。不可变解析快照标记为 neuxbot-plugin/1。缺标记旧快照不被重新解释为当前执行包，也不自动迁移或清库。

GET configuration 返回明确版本的配置字段、普通值、秘密存在标记、revision、generation 及本地 ready／blockers；它只用本地配置和精确缓存账号预检，不启动供应商。PUT configuration 按 expectedRevision 保存配置并验证实际保留版本，不切换伙伴版本。POST updates 在外部预检后，以短事务再次核对完整伙伴集合、各自旧版本、配置修订和账号代次，全部切到同一目标并保留启停。

普通 account GET 及会话 GET 只读当前 owner 缓存；check、授权开始及退出固定明确 versionId 和请求确认的配置修订／账号代次。关闭或晚到启动结果的补偿取消仍使用原 sessionId 与原上下文，不取消后来会话。新 neuxbot-auth/1 每个进程先完成严格 initialize，通知同时匹配 operationId 与 sessionId；未核验摘要、配置变化或账号变化不能借用旧核验结果。

登录完成后分别核验所选版本与全部仍安装版本，包含停用关系；多版本在唯一 owner 占用内顺序执行。外部登录已改变身份后，解析或提交失败撤销原代次旧材料，但保留真实安装，不声称供应商状态回滚。安装继续逐伙伴返回结果，准备与授权成功均不创建占位安装；失败的最后引用清理保留关系供重试。完整行为与验证范围见[插件契约设计](2026-11-07-plugin-contracts-design.md)。

## 8. 验收

- 列表无“插件库／我的插件”页签；真实安装、未安装、准备未安装、下架仍安装、多个伙伴及版本差异不混淆。
- 左栏分类与当前插件状态正确，深链、搜索和返回不丢目标伙伴；不保留旧 view 语义。
- 安装、共享配置、授权、更新、启停、逐伙伴卸载与整体删除原行为通过直接受影响用例。
- 880px 内容上限及双列／单列切换由共享变体实现，Skeleton 与真实布局一致，长中文和控件不裁切。
- 定向测试、共享前端 typecheck、改动文件 ESLint／Prettier、UI 策略及 docs:check；后端变化追加 gofmt、对应包定向测试和生成协议检查。不开全量验证。
- 用户已授权使用独立数据进行页面验证，指定 gpt-6.1-terra high。先运行原仓 Settings/Marketplace 的 Apps／PluginApp，再与本项目同尺寸截图及计算样式对照；仅无溢出不能通过源码对齐验收。
- 缺少作者示例的产品提示已通过固定桌面验收：已有 Orbit 插件在身份头与伙伴区之间显示 TryCard；选择已启用的 nova 后，输入区保留 Orbit 引用与“帮我使用这个插件。”，未发送，随后返回插件详情。根代理已复核截图；底层 pluginId 由定向测试验证，CUA 不暴露该 ID。本轮不涉及供应商登录或模型调用。
- 用户验收前不自动提交、推送或进入 Skill／MCP 批次。

## 9. 云端阶段一交付与后续边界

用户已确认本轮完整阶段一，包仓库、Core 与前端按[插件契约设计](2026-11-07-plugin-contracts-design.md)实现；不能再将范围写成仅 browse，也不能将尚未批准的阶段二列成本轮缺口。实现、自动化证据与真实平台／页面验收分别记录于[契约实施计划](../plans/2026-11-07-plugin-contracts.md)。

| 能力                       | 本轮实现                                                                                                               | 范围             |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ---------------- |
| 插件浏览                   | 合并目录与真实安装、按来源去重、准确窗口、本地化、category 与安全图标                                                  | 阶段一           |
| 唯一标准包                 | 根 plugin.json、skills/、mcp.json；com.openai 展示、com.neuxbot schemaVersion 1；neuxbot-plugin/1 解析快照             | 阶段一           |
| 标准展示与草稿             | defaultPrompt 唯一数组、本地化、capabilities、支持链接、明暗图标与品牌色；示例只填草稿                                 | 阶段一           |
| 公开资料                   | 目录／公开资料 schemaVersion 1 绑定完整来源摘要；公开查看不下载 ZIP，不取得执行能力                                    | 阶段一           |
| 账号与安装                 | 独立 PluginAccount generation／加密材料、严格 neuxbot-auth/1、显式配置预检与账号 check、独立原子 updates、执行代次固定 | 阶段一           |
| 标准 MCP 认证              | none 及具备显式秘密配置或精确驱动绑定的 api_key；oauth／mixed 稳定拒绝，不降级匿名                                     | 阶段一支持边界   |
| 只读管理与 GitHub 仓库列表 | 当前无 management RPC、管理 collection 或仓库读取入口                                                                  | 阶段二，另行批准 |

官方包规范支持根标准包与 extensions.com.openai.interface；本产品选择唯一根输入，Runtime 输出适配不构成旧输入兼容。NeuxBot 的授权与管理协议属于自有契约，不描述为 OpenAI 或 OpenBot 的既有标准。未运行的真实账号、Runtime 或平台验收不能由 Schema、UI 外观或模拟边界测试代替；不通过清空真实数据完成切换。
