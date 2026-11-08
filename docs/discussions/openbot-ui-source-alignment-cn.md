# OpenBot UI 源码对照与移植边界

> 状态：固定版本源码核对，设置部分另附 OpenBot 实机观察；不代表 NeuxBot 视觉验收通过。
> 目标与确认记录见[设计规格](../superpowers/specs/2026-11-06-openbot-ui-alignment-design.md)，步骤见[执行计划](../superpowers/plans/2026-11-06-openbot-ui-alignment.md)。

## 来源

官方 [OpenBot v0.62.0](https://github.com/nightly-labs/openbot/tree/fcb35746d4919798ff889a8a58449f5d58de5f65)，提交 `fcb35746d4919798ff889a8a58449f5d58de5f65`。本机安装包版本为 0.62.0；尚未证明该二进制与官方 tag 没有额外构建差异。以下定位均以该固定提交为准，第三方仓库中的开发指令与技能文档不作为本项目指令执行。

## 1. 外框及左侧区域

| 项目     | 源码事实                                                                                                   | 关键实现                                                                         |
| -------- | ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| 顶层     | PageRail 是 app-window 第一列；WorkspaceFrame 为第二列，内部装配侧栏、resizer、panes 和页面覆盖            | [WorkspaceShell:303–380][shell]、[WorkspaceFrame:62–101][frame]                  |
| 顶行     | 没有单个全窗业务 AppTitlebar；侧栏与 conversation 各有 header，38px 对齐                                   | [SidebarTopbar:28–168][sidebar-topbar]、[header-layout.css:1–18][header-layout]  |
| 桌面底板 | 宽度至少 721px 时，frame 伪元素从顶行下方画一个底板；右／下边缘 4px，源端 card 圆角 14px，内容避线 5px     | [window.css:50–106][window-frame]                                                |
| 左上身份 | active server name／mark、睡眠状态与 chooser；不是固定产品名或 agent 名                                    | [WorkspaceSidebar:283–315][workspace-sidebar]、[ServerMenu:134–185][server-menu] |
| 左上操作 | Back、Forward、条件 update、条件 marketplace／compact expand、条件新增；新增是 agent／channel／section     | [SidebarTopbar:52–168][sidebar-topbar]                                           |
| 原生按钮 | Electron 隐藏标题栏，macOS traffic lights x12、y13，由原生绘制                                             | [main-window:231–250][main-window]、[window-buttons:15–47][window-buttons]       |
| 服务器列 | 可选 server rail 是 frame 内独立列；与 52px PageRail 不同。NeuxBot 不据此创建多服务器功能                  | [app-shell.css:24–50][app-shell]、[WorkspaceShell:329–340][shell]                |
| 模块栏   | Agents 恒在；其余有 pin／unpin 与 More，默认 pin Tasks／Workflows／Usage；底部有账号用量、市场、帮助、设置 | [PageRail:183–219,348–430][rail]、[layout:49–68,112–116][layout]                 |

## 2. 宽度与页面覆盖

| 项目           | 源码事实                                                                                                     | 关键实现                                                                                                                                     |
| -------------- | ------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------- |
| PageRail       | 52px；上留白 38+4px；桌面通用规则不大于 720px 时隐藏，Web workspace pane 后置覆盖恢复                        | [page-rail.css:4–14][rail-css]、[window.css:29–37][window-frame]                                                                             |
| 伙伴侧栏       | 默认 280px、正常范围 128–400px、compact 88px；拖动阈值 110／118px                                            | [layout-constants:7–23][constants]、[WorkspaceLeftPanelResizer:25–46][left-resizer]                                                          |
| 自动 compact   | 剩余 conversation 少于 424px 时触发；手动折叠与自动原因分离，自动原因不持久化                                | [layout:98–139,152–180][layout]                                                                                                              |
| 设置／市场导航 | 使用 max(left-panel-width,220px)；不是 compact 88px；打开覆盖页时原 resizer 隐藏                             | [window.css:474–579,615–622][window-page]                                                                                                    |
| 覆盖           | 原 Sidebar 保留，正文隐藏，顶栏身份／历史仍属于原组件；原 panes inert、aria-hidden、visibility hidden        | [WorkspaceFrame:83–99][frame]、[usage.css:7–25][usage-css]                                                                                   |
| 页面组成       | SettingsPageFrame 提供 nav／path／content mount，settings shell portal；Marketplace 有自己 header／nav／body | [SettingsPageFrame:20–59][settings-frame]、[SettingsDialogShell:223–277][settings-shell]、[Marketplace:170–178,254–266,313–316][marketplace] |
| 保活边界       | 常规覆盖页保留聊天；Tasks、Routines 在 middlePane 切换时可卸载 view，server controller 状态另有 scope        | [WorkspaceShell:103–114,350–378][shell]                                                                                                      |

NeuxBot 保留原 Scene、workspace、PTY 和嵌入浏览器所有权，不复制 OpenBot 对象存储或通过页面切换释放现有内容。这是项目边界适配，不声称源端所有页面始终保活。

Web 有独立手机布局：`WebWorkspace` 使用 phone media query 与 workspace／conversation 两种 pane；不大于 720px 时，workspace pane 重新显示 PageRail，底部 `WebMobileNavigation` 切换两个 pane。[WebWorkspace:499,610–618,2289][web-workspace]、[web-client.css:61–185][web-css]。不能仅凭 window.css 的通用隐藏规则推断 Web 没有模块入口。NeuxBot 第一阶段复用既有 Web 窄屏入口，不增加另一套手机导航，差异在验收中单列。

## 3. 路径与历史

| 项目       | 源码事实                                                                                          | 关键实现                                                                              |
| ---------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| 历史对象   | 单 server scope 的 agent／channel／person 身份，最多 50 条内存记录；不记录所有 URL、thread 和文件 | [chat-history:3–58][chat-history]、[navigation:212–247][navigation]                   |
| 返回优先级 | Back 优先 settings detail 的 pageBack，再到聊天实体历史；Forward 仅实体历史                       | [navigation:249–264][navigation]、[window-page-context:110–129][page-context]         |
| 设置路径   | section title 与 detail crumb，由 settings shell 输出；父级按钮调用 crumb.back                    | [SettingsDialogShell:203–214,238–260][settings-shell]                                 |
| 市场路径   | 来自市场自身 stack，少于两层不显示，祖先 backTo(index)                                            | [Marketplace:55–88][marketplace]                                                      |
| 普通聊天   | agent 主聊天身份与 side-thread tabs，不是全局 URL breadcrumb                                      | [ConversationHeader:54–119][chat-header]、[ThreadHeader:64–86,145–177][thread-header] |

## 4. 聊天与右侧面板

| 项目       | 源码事实                                                                                                          | 关键实现                                                                                      |
| ---------- | ----------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| 主区所有权 | 聊天 header 拥有 agent 名、其 threads、model 与 side panel toggle；主聊天不重复为普通 tab                         | [ConversationHeader:54–119][chat-header]、[ConversationHeader 调用:103–165][workspace-header] |
| 右侧顶行   | SidePanelHeader 的顺序是 pane、放右侧的 thread、file tabs；右侧的 + 是 browser／未打开 pane 菜单                  | [SidePanel:420–601][side-panel]                                                               |
| 权限入口   | header 可能显示 access lock；权限选择在 agent settings 的 permissions 页，不是 header 权限下拉                    | [ConversationHeader:75–80][chat-header]、[AgentSettingsPanel:1418–1474][agent-settings]       |
| 文件正文   | file tab 提供标题与关闭；正文工具条隐藏重复 h2 并保留 Back／正文操作                                              | [FilePreviewPanel:220–231,287–295][file-preview]、[side-panel.css:319–330][side-css]          |
| 拖动       | 只 resize；键盘 12px 步进、Home／End，双击 reset；没有 80% 自动最大化                                             | [PanelResizer:34–59,91–159][panel-resizer]                                                    |
| 显式最大化 | header 和 pane 宽度变为当前 conversation 的 100%，隐藏 resizer 与原聊天内容，不覆盖 sidebar／rail                 | [side-panel.css:277–289][side-css]                                                            |
| 页签溢出   | 主 thread row 有 tabs／compact／minimal；右侧按实际容器和数量将未选中项折成图标，不是统一横向滚动                 | [ThreadHeader:9–43][thread-header]、[side-panel.css:187–233][side-css]                        |
| browser    | 右 dock 是 preview sidebar，点 preview 后用 Portal 展示 full view；native surface 的显隐独立于关闭 tab            | [ConversationPanels:558–590][panels]、[BrowserFullViewAdapter:91–167][browser-full]           |
| 关闭／隐藏 | hide 保留 pane／file 元数据；close 按明确顺序选替代，最后一项关闭后保留 launcher                                  | [conversation-scope:503–523,589–624][conversation-scope]                                      |
| agent 切换 | 会 reset settings／files／file-preview 展示并清旧 preview，tab 元数据仍可保留；不能泛化为全部 pane 永久 DOM 保活  | [conversation-scope:1687–1722][conversation-scope]                                            |
| 无直接对应 | scoped pane types 不含 terminal，未发现与 NeuxBot 任意 split／swap、全局 Scene 菜单、浮动 history／摘要相同的入口 | [SidePanel:47–51,268–323][side-panel]、[ConversationPanels:308–314][panels]                   |

这些差异的 NeuxBot 操作位置必须依设计规格的确认表落实，不能仅按相同图标或名称推断业务等价。

## 5. 样式与约束适配

源端依次加载 brand tokens、UI、app-shell、sidebar 扩展、conversation、page-rail、window；必须同时考虑后置选择器、媒体条件和平台主题。[styles.css:4–56][styles]。本文尺寸为静态声明值，不宣称已证明每种状态的最终计算几何。

桌面 Renderer 实际加载 `tokens.css` 与 `tokens-light.css`；`tokens-native.css` 用于 mobile，不是 Electron 的后置主题。浅色 frame／canvas 为 `#e8e8eb`／`#f5f5f6`，深色为 `#0d0d0d`／`#1a1a1a`；frame 的层次首先来自这组表面差。普通／较强边线浅色为 `rgba(20,20,20,.07)`／`.10`，深色为 `rgba(255,255,255,.035)`／`.045`。外框本身仍有低对比 1px 边线，不额外添加 Card 阴影。[tokens.css:10–12,34–38,106–107][brand-dark]、[tokens-light.css:25–28,54–55][brand-light]、[window.css:77–88][window-frame]。

浅色浮层的 ambient／dialog／menu shadow token 为 transparent；分段控件用 inset 1px shadow 表达轮廓，而非改变排版盒的实体 border。需要逐控件核对，不能将所有层次都归结为 drop shadow。[tokens-light.css:146–152][brand-light]、[sliding-tabs.css:11–15][sliding-tabs]。NeuxBot 通过 shell 专属语义 Token 适配，不用旧的较重通用 border-default／border-strong 替代这组细节。

- 普通 rail button CSS 盒 36px、SVG 20px；sidebar icon 盒 28px，部分 panel toggle 24px。NeuxBot 真实命中区至少 36px，需要按控件逐项验证，不把视觉盒等同最终命中区。
- 源端 conversation title 为 13／18px、常规字重；项目排版仍由语义 Token 管理，控件尺寸和字体不得在页面覆盖共享组件。
- 侧栏普通 agent row 高 62px、avatar 46px；源端不同 compact／pinned 形态并非统一尺寸。项目 avatar 和行高调整须由共享变体承载，不影响业务排序和状态。
- 源端 flat tabs、短分隔线与局部 hover／selected 使用语义状态；本项目仍遵循无普通 Card 常驻边框、`focus-visible` 与减少动态规则。
- 复制或改写第三方实现时保留准确来源及许可声明，按仓库规则维护 THIRD_PARTY_NOTICES；不把第三方实现描述为自研。

## 6. 资源界面的实施差异

源端角色和几何以[资源 UI 标准](../development/resource-ui-cn.md)为准。当前代码已在共享资源变体中统一以下差异；这里记录静态实现和自动化边界，不表示完成本轮真实页面视觉验收。

| 核对项          | 当前实现                                                                                                                                                                       |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 按钮            | catalog 为 32px、14px／行高 1、gap 6px、16px glyph；catalog-form 保留 settings 宿主的 14/20。圆角使用已确认的本产品覆盖。Button loading 保留焦点、表达 busy 并阻断重复激活。   |
| 详情身份及 Item | 身份简介使用 12/16；Item 无额外透明边界，40px media 与上下 12px padding 保持 64px；状态 Badge 的透明边界只用于其自身 22px 盒。媒体 SVG 使用独立尺寸及 1.5 描边，不套按钮规则。 |
| 资源表单        | Input／Textarea 使用独立 catalog 外观：32px 输入、14/20；说明文本域最小 76px、允许纵向调整。名称、地址、环境变量等字段由共享控件管理外观。                                     |
| 搜索            | catalog 外框为 32px、6px 圆角、0.5px 边线、9px 横内边距和 16px 图标；键盘环与鼠标焦点分开。                                                                                    |
| 保存条          | ResourcePageFooter 挂到所属 pane 的底部槽，与正文滚动区分开；条体为 surface、顶部普通边线、最小高 60px，内容与表单对齐。                                                       |
| 来源／传输选项  | catalog-form 使用 34px 轨道、28px Trigger、8／6px 圆角和 13/16；shell 仍服务伙伴／团队切换，不与表单合并。                                                                     |
| 数据状态        | 首次内容和技能扫描使用所属区域骨架，Shell 保持挂载；已安装伙伴后台刷新失败保留旧列表，并在附近提供错误重试。                                                                   |

品牌图 75% 留白、连接器 TryCard、技能三个 Tab、Git／ZIP 添加、标题右侧来源切换、连接器秒级超时、Shell 分隔线与真实固定版本草稿引用均为用户确认的产品扩展。它们不计为源端误移植，也不改写成源端原生事实。

技能、MCP、工作流及无品牌图插件的十组分类标识是本产品适配，复用分类名称映射与共享颜色 Token；自设分类稳定选槽，未分类统一使用同一标识。图标选用 FileCodeCorner、SwatchBook、ChartScatter 等 Lucide 图形，与参考图保持区别。固定源端 `MarketplaceParts.SkillMark` 读取技能自身图标并提供中性 fallback，不据用户截图将本产品分类映射描述为该提交的原生机制。

浅暗主题最终计算样式、窄 pane、长中文、实际命中区和平台交互仍需真实页面核对。自动化测试与静态源码审查不代替这些验收。

## 7. 设置界面

固定来源仍为上述提交。`packages/ui/src/features/settings/SettingsDialogShell.tsx:223–277` 将分类、路径和正文挂到已有槽位；详情路径显示可返回的分类和当前名称，Back 优先回到分类列表。`src/renderer/src/features/window/window.css:646–728` 定义侧栏标题、搜索与独立滚动列表；`:818–822` 限制正文外宽 784px 并居中。

`packages/ui/src/features/settings/ProviderSettingsPage.tsx:227–325` 的列表为默认 provider 行、带本地搜索的 Connected 分组和 Other 分组，整行进入详情。`:491–554` 的详情依次为身份头、账号、API key、运行时、可选缓存／模型、使用与删除。账号行的添加、重连、取消与更多菜单位于 `:636–1021`，API key 摘要与更换入口位于 `:1265–1309`，运行时下载、取消、重试和更新位于 `:1315–1472`。`IntegrationLayout.tsx:207–210` 的身份图标使用 48px 大号。

几何来自 `settings-modal.css:233–329,389–428`：正文分组间 24px、桌面水平 32px；填充行最小 64px、内边距 12px／16px；组内分隔左右内缩 16px。最终 page 顶留白来自该文件末尾覆盖规则。NeuxBot 的设置共享变体承载布局，沿用产品圆角和主题色。`primitives.css` 的 `ui-button-size-sm` 实际引用 control-md，即 32px，设置宿主再把按钮字体覆盖为 14/20；不能按 control-sm 名称误取 28px，也不能把通用灰色按钮当作源端描边动作。检测改为无背景图标，图标操作遵守本项目 36px 命中区要求。

NeuxBot 的“智能体”对应源端 Providers；源端另一个 Agents 分类表示数字工作身份，不能按同名误移植。源端 ModelsSection 管理模型可见性，不提供本次要求的详情默认模型选择；NeuxBot 在详情保留默认模型、推理和 Fast 为用户授权的产品适配。源端默认原生账号槽不可任意移除；NeuxBot 保留系统账号与受管订阅／API 账号各自的凭据所有权，并按实际智能体配置隔离受管账号。协议相同不代表账号可跨智能体复用。

已通过 computer use 只读核对 OpenBot 服务商列表、未下载的 Gemini／Grok、已连接的 OpenCode，以及 Claude 版本不满足要求时的详情。观察到未安装版本行右侧为主色实心下载按钮，重新连接、添加密钥、检查更新和重启为浅色描边动作。本机 OpenBot 的 Claude 因版本低于其最低要求而不展示账号添加入口，因此该账号弹层未完成实机核对。没有执行安装、登录或保存设置。

`IntegrationLayout.StatusPill` 的 connected／attention／connecting／idle／unavailable 分别使用成功浅底、警告浅底、中性填充与轻轮廓；状态点固定 6px，只有 unavailable 为空心。`ProviderPage.header` 先判断停用和安装中，再判断错误、不可用及账号登录；标签文案和颜色不按同一个字符串猜测。未安装登录分组保留未安装提示，下载／取消／重试占同一版本行右侧动作位置。NeuxBot 复用这个组件角色，再以本地真实安装、认证与任务事实适配，不把程序可用当作登录成功；伙伴没有固定智能体归属字段，因此不复制源端 usedBy 的假零计数。

这些观察不代表 NeuxBot 改动后的视觉验收，也不代表真实登录、模型或安装验证通过。

### 页面字体与用户定制

页面左栏的 `settings-modal-nav-item` 使用 13/18；只有 `.app-settings-modal-shell`／`.server-settings-modal-shell` 宿主才覆盖为 14px，不能把模态框规则套给页面导航。按用户明确要求，NeuxBot 省去重复“设置”和“集成”分组标题，模型 fieldset 使用白色语义表面及轻边框；后者是本产品定制，不声称 OpenBot 原有相同边框。

添加账号使用纯加号图标与 Tooltip，登录过程和完成结果使用共享图形状态；这两项属于本产品交互适配。模块栏将资源库放在伙伴之后，工作看板沿用通知入口，不重复保留 Rail 或 More 项。字段鼠标焦点不增加深色轮廓，键盘焦点使用主题色语义 Token。

### 安装与登录的源端职责

OpenBot 的 Electron 主进程通过 `provider-runtime-manager.ts` 拥有任务，`provider-runtime-descriptors.ts`、`provider-runtime-releases.ts` 与 `provider-runtime-archive.ts` 分别提供 CLI 描述、官方制品解析和完整包解压；`native-provider-runtime.ts` 协调所用原生程序。本产品由 Go Core 承担这些职责，共用既有 Catalog、ProviderRuntime 与 ThreadLifecycle，不在前端执行安装脚本或复制一套程序状态。

源端 `RuntimeSection` 仅在可更新时显示“更新”，否则显示“检查更新”，并有独立重启行。本产品检查官方元数据与真实本机版本后才提供更新；重启活动会话的排队机制与本地执行所有权不同，当前拒绝活动／准备中的工作，空闲原生进程关闭确认后重新探测，不显示虚假的重启成功。

源端 `codex-login.ts:277–310` 在没有活动客户端时可复用已有账号；显式重连活动客户端会请求 `account/login/start`，由宿主打开返回的 HTTPS 地址。`:165–200` 等待匹配的登录完成事件后读取账号确认身份。当前 acp-go 的 Codex `Authenticate` 则在已有 ChatGPT 账号时直接复用，公开管理端口未提供强制重新授权，空响应不能证明发生了新授权。本产品按后端认证结果区分现有连接确认与新授权完成，不通过删除系统凭据强制打开浏览器；新增独立订阅账号使用原受管账号流程。

### 真实安装验证

2026-11-08 在 macOS 14.5、darwin/amd64 的隔离目录完成下列下载、上游摘要校验（上游提供时）、真实 `--version`、原子发布与 `Installed` 读回。没有向系统 PATH 安装，也没有复用或改写用户账号目录。该结果仅覆盖此平台和下列版本，不等于全部平台、登录或模型调用验收。

| CLI | 实测版本 | 官方来源／校验 | 登录实现依据与未完成项 |
| --- | --- | --- | --- |
| Codex | 0.167.0 | openai/codex GitHub 制品／SHA256 | OpenBot 浏览器 OAuth／设备码；未完成真实账号授权 |
| Claude | 2.1.319 | 官方 SDK 平台 npm 包／SHA512 | OpenBot `auth login --claudeai`；未完成真实账号授权 |
| Cursor | 2026.11.05-86047df | downloads.cursor.com 完整包／HTTPS | OpenBot ACP `cursor_login` 与 CLI 登录；未完成真实账号授权 |
| Kimi Code | 2.5.2 | code.kimi.com manifest 与 binary／SHA256 | CLI `login` 设备授权；仅基础 ACP 与 help 已验证，未完成真实授权 |
| OpenCode | 2.2.6 | 官方平台 npm 包／SHA512 | OpenBot 外部认证／API 配置，CLI 登录需交互终端；未验证真实密钥或终端授权 |
| Pi | 1.5.0 | earendil-works/pi GitHub 制品／SHA256 | OpenBot 终端认证；未验证真实密钥或终端授权 |
| Grok | 1.0.77 | x.ai 官方 binary／HTTPS | OpenBot CLI 登录及设备码，当前原生 agent 广告可发起登录；未完成真实授权 |
| Gemini CLI | 0.67.0 | 官方 npm 包／SHA512、隔离 prefix | OpenBot 的 Gemini 接 Antigravity，并非本 CLI；依本项目原生广告，未完成真实授权 |
| Qoder | 1.1.85 | 官方 manifest 与制品／SHA256 | 固定 OpenBot 无对应 driver；依真实原生广告与本机凭据模式，未完成真实授权 |
| Qoder CN | 1.1.85 | 国内官方 manifest 与制品／SHA256 | 固定 OpenBot 无对应 driver；独立原生账号范围，未完成真实授权 |
| WorkBuddy | 2.181.0 | 官方 COS 制品与 checksums／SHA256 | 固定 OpenBot 无对应 driver；依真实原生广告与本机凭据模式，未完成真实授权 |

真实验证发现 OpenCode 的 `v` 版本前缀被旧比较器误拒，以及 Kimi 旧 Python latest 1.52.0 已成为迁移壳，均已修复。Kimi 改用官方 Kimi Code 制品，现有 acp-go 基础 `Initialize`／`Close` 确认身份与版本，不创建 Session、不认证、不发送 Prompt。账号登录程序定位使用实际智能体配置的受管程序，避免 PATH 中的旧 Kimi 被误用。

用户应用日志中可确认两次上游 HTTP 504；其余通用错误没有足够证据归为相同原因。网络及临时上游错误使用有界重试，安全错误分类和 HTTP 状态供界面说明；未安装或安装失败时保留“官方下载”。校验失败不能换源绕过摘要。复现实测的显式入口是 `server/internal/agent/installation/network_smoke_test.go`，默认测试跳过网络流程，不自动重复安装。

全部真实账号登录仍未完成。源码核对、Fake、握手、help 或出现授权地址不计作登录成功；需要用户在对应官方页面完成授权，或提供其选择的测试 API 凭据。当前两个已打开的 NeuxBot 浏览器页面停在“正在打开工作台”，尚无法开始界面登录验收；没有重启用户 Core 或循环发起授权。

定向前端交互测试、样式检查、相关 Go race／vet 和格式检查通过，公开 API 与客户端由命令生成。认证结果的 `api:generate` 退出码为 0；本轮 `api:check` 期间发生了并行的工作流分类契约修改，生成完成后因该分类差异退出 1，不能计作一致性检查通过。最新客户端已纳入工作流分类，没有改动或回退并行功能，也未重复运行完整生成检查。

## 8. 待验证

固定来源不替代用户按钮映射确认。安装包与 tag 的精确对应、各主题最终计算样式、窄容器命中区、macOS 拖动与原生浏览器／终端身份均须真实核对。尚未做的项目不写作已通过。每阶段对照源端同一状态与逻辑尺寸，截图只承担验证证据。

[shell]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/WorkspaceShell.tsx#L303
[frame]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/WorkspaceFrame.tsx#L62
[sidebar-topbar]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/sidebar/SidebarTopbar.tsx#L28
[header-layout]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/conversation/styles/header-layout.css#L1
[window-frame]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/window/window.css#L50
[workspace-sidebar]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/sidebar/WorkspaceSidebar.tsx#L283
[server-menu]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/servers/ServerMenu.tsx#L134
[main-window]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/main/main-window.ts#L231
[window-buttons]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/main/window-buttons.ts#L15
[app-shell]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/styles/app-shell.css#L24
[rail]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/window/PageRail.tsx#L183
[layout]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/layout.tsx#L98
[rail-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/window/page-rail.css#L4
[constants]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/layout-constants.ts#L7
[left-resizer]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/WorkspaceLeftPanelResizer.tsx#L25
[window-page]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/window/window.css#L474
[usage-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/usage/usage.css#L7
[settings-frame]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/window/SettingsPageFrame.tsx#L20
[settings-shell]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/SettingsDialogShell.tsx#L203
[marketplace]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/Marketplace.tsx#L55
[chat-history]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/chat-history.ts#L3
[navigation]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/navigation.tsx#L212
[page-context]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/window/window-page-context.tsx#L110
[chat-header]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/conversation/ConversationHeader.tsx#L54
[thread-header]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/conversation/threads/ThreadHeader.tsx#L64
[workspace-header]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/conversation/ConversationHeader.tsx#L103
[side-panel]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/conversation/SidePanel.tsx#L420
[agent-settings]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/conversation/AgentSettingsPanel.tsx#L1418
[file-preview]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/conversation/FilePreviewPanel.tsx#L220
[side-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/conversation/styles/side-panel.css#L187
[panel-resizer]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/components/PanelResizer.tsx#L34
[panels]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/conversation/ConversationPanels.tsx#L558
[browser-full]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/conversation/BrowserFullViewAdapter.tsx#L91
[conversation-scope]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/conversation/conversation-scope.ts#L503
[styles]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/styles.css#L4
[web-workspace]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/web-client/WebWorkspace.tsx#L610
[web-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/web-client/web-client.css#L61
[brand-dark]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/brand/src/tokens.css#L10
[brand-light]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/brand/src/tokens-light.css#L25
[sliding-tabs]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/sliding-tabs.css#L11
[resource-button-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L100
[resource-window-icons]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L1371
[resource-item-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L1253
[resource-card-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/settings-modal.css#L261
[resource-header]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/IntegrationLayout.tsx#L206
[resource-savebar]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/settings-modal.css#L649
