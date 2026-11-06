# OpenBot UI 源码对照与移植边界

> 状态：固定版本的静态源码核对；不代表 NeuxBot 实现或视觉验收通过。
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

## 6. 待验证

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
