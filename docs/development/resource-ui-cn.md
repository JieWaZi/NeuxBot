# 资源目录与详情 UI 标准

本文是插件、技能、连接器目录、详情和添加页的视觉权威；工作流仅在资源目录及导航归属上适用。全局基础、焦点和可访问性要求见 [UI 标准](ui-cn.md)，领域事实见[插件资源](../architecture/plugin-resources-cn.md)。

“源端”均指下节的固定 OpenBot 提交。“本产品扩展”是已确认的 NeuxBot 行为，不表示 OpenBot 原生具备这些能力。组件映射只说明职责归属，不代表现有 React 实现已全部对齐；当前差异单独记录于[源码对照与实施差异](../discussions/openbot-ui-source-alignment-cn.md#6-资源界面的实施差异)。

## 1. 固定来源与判定顺序

固定来源为 [OpenBot v0.62.0](https://github.com/nightly-labs/openbot/tree/fcb35746d4919798ff889a8a58449f5d58de5f65)，提交 `fcb35746d4919798ff889a8a58449f5d58de5f65`。更新版本前不能混用其他提交的组件、Token 或截图；来源与许可见 [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md)。

| 证据                 | 固定源码与职责                                                                                                                                           |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 基础按钮             | [button.tsx][button]、[primitives.css:100][button-css]：变体、尺寸、图标、状态                                                                           |
| 图标按钮             | [button.tsx][button]、[primitives.css:263][icon-css]：IconButton 默认值及独立尺寸                                                                        |
| 文字与详情行         | [primitives.css:1][text-css]、[item.tsx][item]、[primitives.css:1237][item-css]                                                                          |
| 分区                 | [SettingsSection][section]、[primitives.css:1486][section-css]                                                                                           |
| 详情骨架             | [IntegrationLayout.tsx:137][integration]、[integration-layout.css:3][integration-css]                                                                    |
| 详情 Card 与宿主覆盖 | [settings-modal.css:261][settings-card]、[settings-modal.css:320][settings-host]                                                                         |
| 应用与自定义 MCP     | [MarketplaceAppPage.tsx:98][plugin-page]、[AppInformation:493][information]、[CustomServerPage:635][custom-page]                                         |
| 技能列表与详情       | [LibraryRow:198][library-row]、[SkillLibrary:425][skill-library]、[MarketplaceSkillPage:250][skill-page]                                                 |
| 浏览页样式           | [marketplace.css:145][marketplace-shell]、[marketplace.css:289][browse-css]、[marketplace.css:543][browse-row-css]                                       |
| 信息、试用与文档     | [marketplace.css:664][info-css]、[MarketplaceParts.tsx:164][try]、[SkillPreview.tsx:13][skill-preview]、[marketplace.css:697][try-css]                   |
| 添加表单与保存条     | [ServerMcpPanel.tsx][mcp-form]、[ServerSettingsModal.tsx:588][savebar-use]、[SettingsDialogShell.tsx:85][savebar]、[settings-modal.css:649][savebar-css] |
| 数值 Token           | [tokens.css:284][tokens]；主题表面分别核对 [tokens.css][tokens-dark] 与 [tokens-light.css][tokens-light]                                                 |

样式核对顺序是组件调用的 `variant`／`size` → 基础样式 → 实际宿主类与后置规则 → 当前主题 Token → 布局和交互状态。不能只读一个类名或从截图估尺寸。

尤其注意：`.ui-button` 的 14px 字号优先于 `:where(.ui-button-size-sm)` 的字号声明；普通 Marketplace 按钮保持基础 `line-height: 1`。Marketplace 根是 `.skills-marketplace`，没有 `.settings-modal`，因此后者的 14/20 控件和 Item 文字覆盖不能套到 Marketplace 详情。真正位于 ServerSettingsModal 的表单和保存条会命中该覆盖。[Marketplace 根节点][marketplace-root]、[按钮级联][button-css]、[宿主覆盖][settings-host]

本节源端角色优先于全局普通控件的默认尺寸和布局；本产品能力扩展决定真实可用操作。主题映射、键盘焦点、数据与执行所有权仍服从本项目约束。已有 React 类名或静态检查结果不能反向定义源端标准。

## 2. 组件角色与操作矩阵

### 按钮

先确定动作归属，再选共享变体。颜色名称在此表示源端语义，落地时映射为本产品 Token。

| 角色                   | 源端调用与外观                                                                                                | 布置和状态                                                                             |
| ---------------------- | ------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| 详情身份头的下一步     | PluginApp 的 AppAction 是 `default + sm`，复制链接为 `outline + sm`；Skill 的 AddToAgents 也用 `outline + sm` | 按源端实际动作选择，不把所有主 CTA 一律设为 filled；只放真实下一步，文字和尾部换行保留 |
| 分区标题操作           | `outline + sm`                                                                                                | 在标题右侧，与所属分区同一标题行，如添加账号、更新、测试；没有动作不预留按钮           |
| 详情 Item 操作         | `outline + sm`                                                                                                | 在 ItemActions 内，与内容、媒体槽分开；局部操作不触发整行导航                          |
| 行内移除               | `destructive-ghost + sm`                                                                                      | 静止为透明底、次要文字；hover 使用危险语义表面和文字                                   |
| 最后危险区操作         | `destructive-ghost + sm`                                                                                      | 与影响说明同处最后一张 Card；确认后才删除或卸载；不是常驻红色实心主 CTA                |
| 浏览行尾操作           | 按 AppAction 的实际状态使用 `secondary` 等变体                                                                | 与整行详情命中区分离；已安装的安静状态文字不是可点击按钮                               |
| 说明、更多、关闭等图标 | IconButton 的实际尺寸与变体                                                                                   | 必须有访问名称和 Tooltip；不把文字按钮删掉 label 后当作图标规范                        |
| 信息链接               | `link`；信息区另设 13px 字号、自然高度、零内边距                                                              | 文字与 ExternalLink 随所属信息排版；显示具体业务目标或仓库名                           |
| 保存条重置／保存       | ServerSettingsModal 中 `ghost + sm`／`default + sm`                                                           | 归宿主 footer，保存中使用局部 loading 并禁用互斥操作；错误与未保存状态在条内           |

源端 `sm` 文字按钮高 32px、水平内边距 12px、圆角 8px、间隔 6px、字号 14px、字重 500，普通 Marketplace 的行高是 1；基础小按钮 SVG 为 14px；Marketplace 后置窗口规则将按钮直系 SVG 统一为 16px、stroke 2。`default` 高 36px，`lg` 高 40px，`xs` 高 24px；不能从 `sm` 推导整个资源页所有控件都应为 32px。实际宿主可覆盖文字行高，例如 settings 表单中的 14/20。[尺寸与状态][button-css]、[Marketplace 图标后置覆盖][marketplace-icons]、[Token][tokens]

本产品资源按钮的圆角以“新建伙伴”按钮为参照，覆盖上述源端圆角：32px 的 `catalog`、`catalog-form` 和 `catalog-icon` 使用 `radius-control`（12px），40px 的 `catalog-icon-lg` 使用 `radius-xl`（16px）。尺寸差异在共享 Button 中维护；显式圆形、胶囊、拼接按钮及 Try 箭头保留各自形状。此适配不改变输入框、分段控件、导航项和 Card 的圆角。

IconButton 默认是 32×32px 的 `icon-sm`；另有 24、36、40px 档位。Marketplace 内按钮 SVG 最终为 16px、stroke 2；LogoTile 等非按钮媒体 SVG 仍保留自己的尺寸和 1.5 描边，不能互相覆盖。Try 箭头是专用圆形控件，导航项、面包屑和信息链接各有自己的覆盖，均不由上述文字按钮尺寸代替。[图标按钮][icon-css]、[Marketplace 图标后置覆盖][marketplace-icons]、[试用卡][try]

`outline` 静止态有普通语义边线、control 背景和次要文字，hover 转 control-hover 与主要文字；`ghost` 静止无填充；`secondary` 静止为 control-hover，hover 为 control-active；`default` 在普通 Marketplace 使用随主题反转的 surface-light／text-on-light，在真正 settings 宿主内使用 accent 覆盖。危险 ghost 静止不抢主动作，hover 才强调危险。不能把 `surface`、`control`、`control-hover` 合并为一套灰色。[基础变体][button-css]、[settings 覆盖][settings-host]

### 浏览行、详情行与信息字段

三者职责和排版分别维护，不能以一个“资源 row”统一字号、表面和交互。

| 结构                    | 源端规范                                                                                                                                                                                                             |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 浏览 AppRow／LibraryRow | 透明静态表面；8px 内边距、12px gap、12px 圆角、40px 媒体槽；名称 14/21 中等、说明 13/19.5，各一行截断；整行打开详情，尾部动作独立命中；hover 只由行表面绘制                                                          |
| 详情 Item               | 所属填充 Card 内的 `Item.settings-modal-row`；最小高度 64px，12px 16px 内边距、16px gap；Item 自身无边框，40px 媒体加上下内边距得到 64px，不额外加两条透明边界                                                       |
| 详情 Item 内容          | ItemContent gap 2px；普通 Marketplace 内标题 13/18、字重 500，说明 12/16、字重 400、单行截断；ItemActions gap 8px。实际 settings 宿主才覆盖为标题 14/20、说明 13/18 和正常换行                                       |
| 信息 Card               | 一份说明正文与一个 `dl` 置于同一 Card 的内容体中；非行内容使用 12px 16px 内边距及 12px gap，不增加假 Item 分隔线                                                                                                     |
| 信息字段                | `dt`／`dd` 是 `dl` 直接子节点；标签列 `minmax(120px,max-content)`、值列 `minmax(0,1fr)`，列距 24px、行距 8px；caption 标签与 body-sm 值分别定义，不继承按钮字号                                                      |
| 分区                    | 标题、可选说明及右侧局部动作组成标题行，内边距横向 4px；标题 14/20、字重 400、secondary；说明 12/16；标题行底边对齐、内部 gap 12px、文字副本 gap 4px，与单张内容 Card 间隔 8px，动作区间隔 8px；分区整体不重复套外框 |

浏览行和详情 Item 本身没有常驻外框；分组顶线、Card 内缩分隔线、文档阅读边界、按钮轮廓和图标槽边线各自有职责。普通 Card 的“无常驻外框和阴影”不要求删掉这些明确结构。透明 1px 盒边界属于源端 Button，不能复制到 Item 并将多出的行高解释为源端几何。[浏览行][browse-row-css]、[Item][item-css]、[详情 Card][settings-card]、[字段][info-css]

## 3. 页面 Shell 与内容结构

### 目录

资源正文使用一个可缩小的滚动容器，列表与详情共享 880px 最大内容宽度、上 40px、下 32px、横 24px 的框架。左栏与正文分别滚动；导航项保持自身密度，页面不重复写限宽和几何值。窄容器适配沿实际 Scene 宽度重排，本项目可在窄容器使用 16px 横内边距。侧栏与正文边界、顶栏短分隔线遵守本产品 [Shell 布局覆盖](ui-cn.md#工作台与可访问性)，不据源端目录样式隐藏统一边界。[源端正文与框架][marketplace-shell]

左栏 catalog 导航上 16px、横 12px、下 12px，外层 gap 4px；标题下距 12px，分类及快捷项使用独立的导航行角色。仅有真实已安装数据时显示快捷组，不能据搜索结果或首批页推导完整快捷项。当前资源使用 `aria-current=page`，所属分类使用 `aria-current=location`；两者不同时声称 page。[源端导航][nav-css]

列表页标题为 24/24、字重 600，页头说明使用 Text body 的 14/20；这与浏览条目 raw span 的名称 14/21、说明 13/19.5 是不同文字角色。列表页按资源类型展示标题、简短说明与整行搜索；搜索和相邻添加／Info 入口属于同一工具行。分类页保留可回主列表的面包屑与分类内搜索，不增加重复返回箭头、简介或“精选”。源端 browse 页头内部 gap 24px，browse 内容间隔 32px；分组顶线后内缩 16px、组内 gap 12px。[浏览页头调用][browse-head]、[浏览间距][browse-css]、[分组][browse-row-css]

浏览列表按每列至少 320px、列距 24px、行距 4px 排列；880px 内容框形成最多两列，内容不足容纳两列时单列。不是固定两个半屏 Card，也不是不足三张自动拉伸的普通资源网格。

### 详情

源端 IntegrationPage 的固定顺序是身份头 → 读取／操作失败及领域提醒 → intro → 领域内容 → details → danger。各主体分区间隔 24px。读取失败与未连接分别表达；危险区只在真实可移除时存在且始终最后。[详情插槽顺序][integration]

| 源端页面   | 实际内容                                                                                                      |
| ---------- | ------------------------------------------------------------------------------------------------------------- |
| 插件应用   | 身份头；有真实 prompts 才有 TryCard；连接与组件等真实内容；信息 Card；有权限且已连接才显示 DangerZone         |
| 技能       | 身份头；读取详情后的 TryCard；实际安装伙伴行；SKILL.md 阅读表面；信息 Card。Try 的示例来自资源或源端 fallback |
| 自定义 MCP | 身份头；该服务的 serverCard；危险区。源端该页没有原生 TryCard，不能从插件页面推断它已有试用区                 |

详情图标槽源端为 48px，raised 表面、普通边线、12px 圆角；身份头 gap 12px、名称／状态行 gap 8px、名称 16/22、字重 600，简介为 caption 12/16、字重 400、下置。源端标题与简介未统一强制单行；本产品名称自适应、简介单行截断并悬停阅读是明确适配。源端状态 Badge 高 22px（12/16 文字、上下 2px 内边距、透明 1px 边界），字重 500、胶囊圆角、6px 状态点与 4px gap；此透明边界不适用于详情 Item。源端图片填满槽，SVG 占 55%、stroke 1.5；本产品品牌图片按原比例居中占槽的 75% 是用户确认的留白适配，不宣称为源端原值。[身份头与图标][integration-css]

每个普通分区只包含一张 filled Card：源端 `settings-modal-card` 使用 surface、12px 圆角、无外框和阴影；内部相邻行以左右内缩 16px 的细分隔线区分。静态信息 Card 不随鼠标悬停整卡变色；可点击行才承担 hover 和键盘反馈。说明、表单与备注留在所属 Card，不在每张 Card 外再加说明行。[分区约束][integration]、[Card 与内缩分隔][settings-card]

## 4. 滚动、表单与保存条

### 表单控件的角色与宿主密度

源端 MCP 表单实际使用 Input 和 SlidingTabs，宿主为 `SettingsDialogShell` 的 `.settings-modal.settings-modal-page`，再加 `server-settings-modal-shell`；Input 会命中 settings 的 14/20 覆盖。MCP 表单没有 Textarea 或 Select 调用，下面两者是共享控件参照，本产品说明文本域等字段属于能力扩展。[真实表单调用][mcp-form-fields]、[宿主 class][settings-form-host]、[宿主字体覆盖][settings-host]

同名 `sm`／`md` 不保证不同控件同高，不能把项目的 control-sm／md／lg 默认当作源端表单数值。

| 控件角色              | 源端尺寸与排版                                                                                                                                                  | 表面与边界                                                                                                                                                   |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Input／NativeSelect   | sm 高 28px、横内边距 8px；md 高 32px、横 12px；lg 高 36px、横 12px。基础文字 13/18，真实 settings 宿主覆盖为 14/20，MCP 的 Input md 因此是 32px 高、14/20       | 宽 100%、min-width 0、1px strong 边线、8px 圆角、control 背景；placeholder 使用独立 Token                                                                    |
| Textarea              | 最小高 76px、8px 12px 内边距、可竖向 resize；基础 13/18，settings 宿主 14/20。源端没有按 sm／md／lg 分开的高度规则                                              | 与 Input 相同字段边界；不把本产品不同 min-height 档位或说明字段当作 MCP 原生配置                                                                             |
| 自定义 Select Trigger | md 高 36px、13/18；sm 高 32px、12/18；竖内边距 8px、横向起 10px／末 8px、gap 6px                                                                                | 1px strong 边线、8px 圆角、透明背景与 ambient shadow；`.settings-modal` 控件字体覆盖不包含该 Trigger，不能推成 14/20；与 NativeSelect 分开核对               |
| 浏览搜索框            | search-field 外框高 32px、横内边距 9px、gap 8px、6px 圆角、0.5px 普通边线；内部 Input 13/18；前图标最终 16px                                                    | 外框静止为 hover 语义背景，Input 自身透明、无 border／shadow；这是复合搜索控件，外框与内层的几何由共享层一次管理，不能按普通 Input 再包一套业务外框          |
| SlidingTabs 传输选项  | 轨道自然宽、inline-flex、gap 3px、padding 3px、8px 圆角；Trigger 高 28px、4px 12px 内边距、13/16、字重 500、6px 圆角。总轨道高 34px，选中 pill 高 28px、top 3px | 轨道 tabs-bar 背景与 inset 1px 轮廓，Trigger 无 border，pill 为 tabs-pill 表面；Trigger 不是 Button，settings 的 Button 字体规则不适用，不按项目按钮尺寸重写 |
| Field 标签与帮助      | 字段组 grid gap 6px；标签 12/16、字重 500；必要说明及错误 12/16                                                                                                 | 标签 secondary、说明 muted、错误 danger；FieldDescription 可选，不重复解释显而易见的标签，必要格式、权限和信任说明保留                                       |

[Input／Textarea 基础][form-css]、[Select Trigger][select-css]、[Field][field-css]、[搜索框宿主][search-css]、[SlidingTabs][tabs-css]共同决定上述结果。原始搜索源码有一处级联细节：search-field 未重置内层 Input md 的横向 12px padding。这里记录的是源端实际宿主事实，不是要求本产品页面给共享搜索框再叠加两份 padding；最终文字、图标与框边距离应在共享 SearchField 中对照核验。NativeSelect 是原生 select 字段，自定义 Select Trigger 是复合选择器，二者的 md 尺寸和宿主覆盖分别读取。

字段状态也按角色核对：Input／Textarea／NativeSelect 的 `focus-visible` 只把边线换成中性 focus 色，不添加按钮式外 ring；invalid 为 danger 边线，disabled opacity 0.42 且 cursor 为 not-allowed。自定义 Select 的 focus 也是中性边线，展开及 hover 使用 hover 表面，invalid 另有 3px danger-soft ring。搜索由外框 focus-within 换边色和 surface，键盘聚焦时才给外框 1px ring。SlidingTabs 用自身 1px focus-ring、disabled opacity 0.42；这些不能被“所有表单统一加 ring”覆盖。[字段状态][form-state]、[Select 状态][select-css]、[搜索状态][search-css]、[Tabs 状态][tabs-css]

### 布局与 footer

添加页参考源端 ServerMcpPanel 的独立表单视图，不把 source 的对话框宿主误当成必须新开 modal 的业务要求。表单字段填满所属列，传输／来源选项自然宽度排列；字段提供可见标签；需要说明格式、权限、信任或数据关系时再加帮助，不重复解释显而易见的标签。必要帮助与错误各自表达，不能用占位符替代标签。

资源表单的字段“添加”动作使用共享 Button 的 `catalog-outline + catalog-form`：高 32px、14/20、字重 500、gap 6px、水平内边距 12px、glyph 16px，圆角遵循第 2 节的本产品按钮适配。该尺寸对应 settings 宿主的文字密度，不改写 Marketplace 按钮的源端行高。轮廓按钮背景读取独立的 `resource-control` 语义；当前浅／暗值与 surface 相同，仍保留角色区分。

资源表单切换使用 SegmentedControl 的 `catalog-form` 外观，读取既有 tabs-track／tabs-pill 主题 Token，几何遵循上表 SlidingTabs；选中面与裁切均为 6px 圆角，不能只修改 Trigger 而保留胶囊裁切。hover 只提升文字，不增加选项底色；键盘焦点使用 1px 中性环，禁用透明度 0.42。伙伴／团队切换的 shell 外观继续使用自己的命中区规则，不与资源表单合并。

保存条属于页面或 Dialog 的 footer slot，和内容滚动区为兄弟节点。技能资源编辑与连接器表单共用 `ResourcePageFooter`：页面保存区挂到所属 `ResourcePageSurface` 的底部槽，弹窗保留自身 footer；跨出 form 的提交按钮通过原表单标识提交。技能版本号、备注、待保存状态、重置和提交一同固定在底部，页签切换暂时隐藏保存区并保留草稿，重置后收起。源端 SettingsDialogShell 的主区使用 `minmax(0,1fr) auto`，滚动 frame 在第一轨，footer 在第二轨；SaveBarDock 负责有更改时展开、复位后关闭，关闭阶段不可聚焦。ServerSettingsModal 明确把 MCP 保存条放到这个宿主 footer。[宿主布局][settings-main]、[footer 装配][settings-shell]、[保存条调用][savebar-use]

- 短内容时，宿主必须占满可用主区，保存条贴所属 pane 底边；不能只依赖短内容末尾的 `sticky bottom-0`。
- 长内容时，只有正文滚动，保存条保持可操作；正文最后一项可滚到完整可见，不被保存条或浮动错误遮住。
- 保存条覆盖整个所属内容 pane 的宽度；其内边距对齐表单，不因正文限宽形成一块悬浮小条。分屏时以 pane 边界为准。
- 源端条体最小高 60px、12px 24px 内边距、16px gap，顶部普通分隔线、surface 背景；右侧动作 gap 4px。窄容器按实际空间换行，条高随内容增长。[保存条样式][savebar-css]
- 表单未改动时不占用一条永久“确定”操作；有更改后显示未保存状态、重置、提交。失败保留当前输入，提交中字段与互斥动作的禁用保持一致。

## 5. 主题、焦点与状态

复用 Inter、既有语义 Token 与组件变体。源端尺寸经核对后进入共享资源变体，业务页面不通过 `className` 重写 Button／Input／Tab 的高度、字号、圆角、描边或阴影，也不复制色值。布局负责可用空间；基础组件负责外观及状态。

| 源端语义                                  | 本产品映射职责                                            |
| ----------------------------------------- | --------------------------------------------------------- |
| canvas／surface／surface-raised           | 资源画布／详情填充 Card／图标和浮层抬升表面，保留三者差异 |
| bg-control／control-hover／control-active | 轮廓按钮静止／可交互 hover／次级控件 hover 与行尾加强层次 |
| text-primary／secondary／muted            | 内容、次要动作、说明与标签；状态色与文字同时表达          |
| border／border-strong                     | 普通结构分隔／需要较强辨识的边界，不用强边线包住全部 Card |
| danger-soft／danger／danger-focus         | 危险 hover、危险文字、确认／错误焦点；与普通主操作分开    |

源端桌面浅／暗主题的中性色参考如下；实际实现通过语义 Token 读取，不在业务页面复制这些色值。不同角色当前色值相同也保留独立语义，以免宿主或主题覆盖时混淆。[暗色表面][tokens-dark]、[浅色覆盖][tokens-light]

| 源端 Token                     | 浅色                            | 暗色                            |
| ------------------------------ | ------------------------------- | ------------------------------- |
| bg-canvas                      | `#f5f5f6`                       | `#1a1a1a`                       |
| bg-surface／bg-control         | `#ffffff`                       | `#212121`                       |
| bg-surface-raised              | `#fcfcfc`                       | `#242424`                       |
| bg-control-hover／active       | `#f0f0f2`／`#e8e8eb`            | `#242424`／`#2a2a2a`            |
| text-primary／secondary／muted | `#141414`／`#3a3a3c`／`#6b6b70` | `#ffffff`／`#dcdcdc`／`#979797` |
| border                         | `rgba(20,20,20,.07)`            | `rgba(255,255,255,.035)`        |
| surface-light／text-on-light   | `#1c1c1e`／`#ffffff`            | `#f0f0f0`／`#141414`            |

源端普通按钮没有跨变体统一的 active 压下色，不能从单个产品变体推导所有按钮应有同一 active 背景。hover 只在可交互命中区出现；浏览整行与尾部操作不叠加两层相同 hover。disabled 保留真实 disabled 语义、减弱外观并阻断交互，不能仅降低透明度。源端基础按钮 disabled opacity 为 0.42；loading 另保留当前键盘焦点、表达 busy 并阻断重复激活，不将 loading 与失去焦点的普通 disabled 简单等同。实际命中和焦点范围需同时验收，不能仅看 CSS 盒。[按钮状态][button-css]

键盘焦点使用共享 `focus-visible` 与语义 Token，鼠标点击、拖动和程序化导航 focus 不给整个滚动区、画布或页面加轮廓；禁止浏览器默认黄橙外框。返回、弹层关闭与失败重试保留合理焦点位置；图标按钮有 Tooltip 和访问名称。窄容器保证文字可读与控件可操作，不以缩小字体解决溢出。

加载、无数据、无搜索结果、后台刷新、陈旧、失败和禁用分别呈现。NeuxBot 首次内容读取使用匹配结构的 Skeleton，Shell、页头、返回、搜索及 Tab 先挂载，后台刷新保留已知内容；当前操作失败就近展示并可重试。空状态在所属内容区域，不伪造资源、数量、伙伴或授权；这套数据状态契约是本产品约束，不据源端示例图编造事实。

TryCard 保留源端网格渐变与专用 chip／圆箭头结构：Card 最小高 160px、20px 圆角、24px 内边距；Marketplace 请求行最大宽 560px、行高 24px，请求表面内边距 12px；圆箭头为 40px，窗口内 SVG 16px、stroke 2。不能从独立 SkillPreview 的 520px 请求宽度推断 Marketplace 仍是 520px，须应用更具体的窗口选择器。本产品通过 artwork Token 使用低饱和蓝紫、青绿与暖陶色系。CSS 与 Shader 解析同一组 Token；主题变化重建 Shader，离屏、隐藏及 reduced-motion 时释放动画并保留静态渐变。不能替换成普通白色表格 Card。[试用布局][try-css]

## 6. 本产品能力扩展与业务约束

### 目录与导航

- 插件通过标准 category 分组，空组隐藏，未知分类归其他，不按名称推测；公开窄摘要每批至多 100 项，完整读取成功后才能发布完整目录数量，失败或取消不把部分数据当全集。
- 技能库仅含 GitHub 同步、ZIP 上传的独立技能；插件技能从所属插件进入，不重复出现在技能库。用户自设分类使用可输入的多选标签字段，外层统一绘制字段边界，内层输入不重复焦点轮廓或环；输入过滤已有项，回车或点击新建；草稿只在保存所属表单时写入，同类型资源共享选项。多个分类分别参与技能分组，空数组为“未分类”，每组预览六项，分类页每批 24 项；插件与技能滚动接近底部自动追加，不显示“加载更多”。分类与分类内搜索保存于 URL，不伪造 Verified、Public、推荐或安装量。
- 连接器轻量列表仅展示独立资源；插件 MCP 从所属插件进入固定来源详情，不重复展示到独立连接器列表，不保留旧来源分组、来源／启用筛选、批量选择、总数工具条或伙伴数第三行；尾部仅保留真实安装或当前伙伴启停，无真实分类时不编造分组。内置资源遵守实际维护限制。
- 工作流作为全局资源库分类，与插件、技能、连接器共用 catalog 左栏、页头、整行搜索与两列轻量条目，创建入口紧邻搜索；条目只展示名称与说明，不保留总数工具栏、版本底栏或更多菜单，不推导安装状态或分类。列表沿用真实游标每批读取 24 项并滚动追加。进入新建、详情或运行页后隐藏整个资源分类侧栏，画布与创作对话使用完整主容器，模块栏仍选中资源库；版本、运行及管理操作留在画布内，返回资源列表保留原搜索条件。
- 添加技能／连接器进入同一资源 Scene，入口、面包屑和可见页标题使用对应的“添加…”；复用共享身份头的返回行为，不增加第二个返回箭头，返回校验并保留原分类、搜索及伙伴上下文。

### 插件与技能详情

- 插件真实安装伙伴紧随有数据的 TryCard，位于连接、组件、信息之前；伙伴页只显示自身固定版本。共享配置与账号进入已有管理入口，本地化连接说明留在连接 Card。
- 包状态 `unsupported` 保留真实资源、安装、删除和卸载，显示重新安装提示；隐藏依赖旧包的安装、更新、启停、配置、组件和试用，不把详情可读取表述为包可执行。
- 技能详情保留 SKILL.md／资源文件／版本三个可切换入口，使用既有 shell、sm、fill SegmentedControl；数据未就绪仍保持挂载，保留 URL、草稿、真实文件、历史及差异能力。这些 Tab 是本产品扩展，源端技能页直接阅读 SKILL.md。
- 独立技能名称与分类在身份头的同一编辑表单一次保存；分类移除全部标签时清空，不改正文或生成版本，信息区只读。全局删除移除全部版本和实际安装关系；伙伴“移除”仅断开当前关系。详情读取完整实际伙伴，不把最多四头像摘要当完整关系。
- 插件技能及 MCP 从所属入口进入固定来源只读详情，保留版本、目录提交、归档摘要和伙伴上下文；不提供组件独立编辑、删除、启停或更新。信息区展示真实来源链接，不额外在页头加“查看插件”。
- 技能文档沿源端有边界阅读表面与 16px 24px 内边距、12px 正文；本项目 Markdown 标题变体为 h1 20/24、h2 16/22、h3–h6 14/20。“查看完整内容”归所属分区标题右侧。信息只展示真实字段，作者能力声明不作为常驻信息，仓库链接显示具体拥有者／仓库，不以托管域名冒充作者。

### 连接器详情与试用

- 本产品连接器详情顺序为身份头 → 共享 TryCard → 当前伙伴操作 → 工具 → 信息 → 底部危险区。TryCard、工具诊断和信息组合是用户确认的产品扩展；源端自定义 MCP 结构见第 3 节。
- 当前伙伴启停及共享资源移除放在伙伴行；全局删除和伙伴自有资源删除在危险区明确实际影响并确认。全局只展示接口提供的真实计数，不据计数推导伙伴名单。
- 工具名与说明各一行，超出截断并可悬停阅读，不限定说明的固定字符宽度。显式测试与诊断 Info 在工具标题行，状态、时间与耗时在可交互浮层；失败就近保留。页面不展示 CLI 兼容阵列，后台传输兼容校验保留。普通读取不探测、不授权；固定目录预览未匹配真实本地版本时不伪造可测试状态。
- 受支持插件优先使用当前语言的非空作者 defaultPrompt，其次使用默认作者示例，均缺少时使用本地化产品 fallback“帮我使用这个插件。”；技能使用真实示例或 fallback“帮我使用这个技能。”；连接器使用本产品 fallback“帮我使用这个连接器。”。产品 fallback 不冒充作者示例或源端自定义 MCP 原生能力；不受支持的插件保持不可试用。
- 试用只创建独立聊天草稿：独立技能用 skill 引用，独立连接器用 connector 引用，插件技能与 MCP 用所属插件引用；不复制 SKILL.md、工具清单、命令或配置，不自动安装、启用、授权、测试或发送。
- 当前或所选伙伴须有实际已启用关系，技能及插件须与预览固定版本一致；目录预览须先核验本地安装身份与来源摘要。全局入口先选伙伴；未安装、停用、系统场景或身份未核验时保留卡片并禁用箭头，或在选择区就近解释，原因通过可访问描述提供。
- 普通聊天的连接器可选性与 Try 复用同一策略规范化，不以旧连接测试成功／失败快照判断能否建立草稿。输入区等待真实可选资料后一次性写入既有 Mention；后续刷新或用户删除不重复插入。

### 添加能力

- 添加技能的 GitHub／ZIP 来源切换和添加连接器的传输切换，放在共享 PageHeader 的标题右侧 actions 槽；这是本产品页面布局要求。`compact + form` 身份区按标题自然宽度增长，动作保持自然宽，只有实际容纳不足才换行，不用固定 basis 提前拆行。来源切换仍由唯一表单状态持有；扫描、上传、提交及已入库待安装状态保持原有禁用和清理语义。
- 添加技能使用 GitHub／ZIP 两个来源；默认将所选技能加入统一技能库，伙伴入口随后显式安装。未入库且选中至少两个技能时，在主添加动作之前提供次级“添加为插件”；在同页填写插件名称与可选说明，核对所选技能，不新开命名弹窗。取消命名保留扫描批次、勾选及草稿，失败就近重试。创建只使用所选候选生成 skills-only 插件，不重复导入独立技能，也不提供公开／私有或自动发布操作；全局创建后进入真实插件详情并可返回插件库，伙伴入口继续复用已有插件安装确认。安装失败或取消保留已创建插件，重试安装不重复创建。普通独立技能安装失败同样保留已入库资源和安装重试，不重复导入或自动删除。
- ZIP 使用共享 FileDropZone 点击或拖入，业务层校验格式、传输大小和暂存；本地路径扫描保留，浏览器传输与解压批量预算分别表达。“识别到的技能”区始终保留 Card 和标题，未扫描、扫描中、空结果、失败使用对应占位；成功展示真实候选与重复资源信息。
- 连接器添加保留 STDIO、Streamable HTTP、SSE、环境变量、请求头和信任确认；新建不展示超时字段，使用既有默认 10 秒。编辑与详情的启动／测试超时使用秒，范围 0.1–300 秒、保留三位小数精度；表单只持有秒值，在 API 边界与整数毫秒转换，历史毫秒值不因编辑其他字段被取整。环境变量／请求头的“添加”属于字段局部 outline 动作。全局创建仅入库，伙伴入口仍创建自身连接器并安装启用，布局变化不改资源所有权。

## 7. 本项目共享组件映射

以下是实现职责的映射。变体名不构成源端等价证明；若共享组件缺少角色或 Token，先在共享层补齐，再迁移真实调用，不在业务页覆写补洞。

| 源端职责                              | 本项目归属                                                                                                 |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Marketplace frame／nav／browse        | ResourcePageSurface、NavigationPanel／Section／Item、ResourcePageHeader／SearchField、ResourceCatalogGroup |
| AppRow／LibraryRow                    | ResourceListItem 的 row 变体、ResourceCardGrid 的 catalog 变体                                             |
| IntegrationPage／DetailHeader         | PageHeader 的资源身份头、各资源 DetailHeader 与页面装配；共享维护顺序和状态槽                              |
| SettingsSection／filled ItemGroup     | DetailSection 的 catalog 变体及共享 Card                                                                   |
| Item／ItemContent／ItemActions        | ResourceCatalogRow／Rows 与真实局部操作；行高与分隔统一管理                                                |
| AppInformation／integration-card-body | ResourceCatalogFields／Field、同一 Card 中的正文与字段                                                     |
| TryCard                               | ResourceCatalogTry、ResourceGradient 与领域 Presentation                                                   |
| DangerZone                            | 详情末尾的共享分区和局部危险动作，确认弹窗复用既有实现                                                     |
| Button／IconButton                    | shared/ui/Button 的角色变体；PageAction 仅在其语义与角色吻合时装配                                         |
| settings 字段局部操作                 | Button `catalog-outline + catalog-form`，不改变既有 Marketplace catalog 尺寸                               |
| SlidingTabs 表单切换                  | SegmentedControl `catalog-form` 与可继承圆角的 Highlight；表单页头使用 PageHeader `compact + form`         |
| 表单／SaveBarDock／footer             | ResourcePageSurface 的表单布局、字段控件、所属 pane 的保存条 slot；Dialog 使用其 footer                    |

当前实现偏差、尚缺的共享角色和真实验收结果只写入[实施差异](../discussions/openbot-ui-source-alignment-cn.md#6-资源界面的实施差异)，不在本规范把未完成目标写为已实现。

## 8. 验收检查

- 对照同一固定源版本，分别检查基础组件、实际宿主选择器、主题 Token 和最终盒模型；记录 computed 值时同时记录资源类型、上下文和状态。
- 检查身份头、分区、详情行、浏览行、危险区、信息链接和保存条的按钮角色；尺寸相近不能代替 border、文字层级、图标 stroke、hover 和 disabled 一致。
- 浅／暗主题、宽／窄 Scene、长中文、空列表、加载、失败、停用、身份未知均可读；整行导航和尾部动作互不误触，图标按钮可用键盘并有名称。
- 短表单保存条贴 pane 底；长表单仅正文滚动且最后字段不被遮；分屏、窄窗口和错误换行后仍可重置、提交、恢复焦点。
- 添加页标题与自然宽切换在 400px 内容区及更窄容器按实际空间排列；轨道／选项为 34／28px，圆角为 8／6px，选中裁切不回到胶囊。返回按钮在 form 内也只导航；切换与扫描、上传、重试的禁用及清理保持一致。
- 核验真实伙伴、安装、版本、固定摘要、工具及试用引用；无自动安装、授权或发送，无由计数、旧测试或猜测派生的可用状态。
- 定向测试、类型与样式检查按改动执行；实际视觉与原生交互验收需用户授权，自动化通过与真实验收分别报告。未检查的状态明确留作边界。

[button]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/button.tsx#L1
[button-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L100
[icon-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L263
[text-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L1
[item]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/item.tsx#L1
[item-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L1237
[section]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/settings.tsx#L17
[section-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L1486
[integration]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/IntegrationLayout.tsx#L137
[integration-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/integration-layout.css#L3
[settings-card]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/settings-modal.css#L261
[settings-host]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/settings-modal.css#L320
[plugin-page]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceAppPage.tsx#L98
[information]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceAppPage.tsx#L493
[custom-page]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceAppPage.tsx#L635
[library-row]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceSkills.tsx#L198
[skill-library]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceSkills.tsx#L425
[skill-page]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceSkillPage.tsx#L250
[marketplace-root]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/Marketplace.tsx#L313
[marketplace-shell]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L145
[browse-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L289
[browse-row-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L543
[nav-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L42
[info-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L664
[try]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceParts.tsx#L164
[skill-preview]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/components/SkillPreview.tsx#L13
[try-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L697
[mcp-form]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/servers/ServerMcpPanel.tsx#L1
[savebar-use]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/servers/ServerSettingsModal.tsx#L588
[savebar]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/SettingsDialogShell.tsx#L85
[savebar-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/settings-modal.css#L649
[settings-main]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/settings-modal.css#L95
[settings-shell]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/SettingsDialogShell.tsx#L264
[tokens]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/brand/src/tokens.css#L284
[tokens-dark]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/brand/src/tokens.css#L1
[tokens-light]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/brand/src/tokens-light.css#L1
[marketplace-icons]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/marketplace.css#L1371
[browse-head]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/marketplace/MarketplaceBrowse.tsx#L214
[mcp-form-fields]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/features/servers/ServerMcpPanel.tsx#L797
[settings-form-host]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/features/settings/SettingsDialogShell.tsx#L230
[form-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L549
[form-state]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L656
[select-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L679
[field-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/primitives.css#L930
[search-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/src/renderer/src/styles/app-shell.css#L1427
[tabs-css]: https://github.com/nightly-labs/openbot/blob/fcb35746d4919798ff889a8a58449f5d58de5f65/packages/ui/src/styles/sliding-tabs.css#L1
