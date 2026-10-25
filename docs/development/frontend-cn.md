# NeuxBot 前端工程开发约定

> 文档状态：开发基线
> 适用范围：React、TypeScript、Web、Electron Renderer、前端状态和测试
> 领域词汇：[NeuxBot 领域词汇](../../CONTEXT.md)
> UI 标准：[NeuxBot UI 设计标准](./ui-cn.md)

本文记录前端实现规则。功能范围以用户当前确认的需求为准；页面、组件、样式和交互改动还必须完整阅读并遵守 UI 设计标准，UI 视觉规则以 UI 设计标准为准。

## 0. 范围与契约

- 按用户确认的当前范围开发，不从设计设想推导额外功能；
- 数据、协议和界面行为以当前实现及对应架构文档为准；
- 变更公开契约时明确影响、支持范围和验证方式；不为未承诺的使用场景预建兼容层；
- 对外部输入、Runtime 故障和未知扩展值保留必要校验与安全降级。

## 1. 工具链和 TypeScript

- 包管理器统一使用 pnpm；
- 仓库只保留根目录 `pnpm-lock.yaml`；
- 根 `package.json` 使用精确 `packageManager` 字段；
- CI 使用 frozen lockfile；
- 禁止提交 `package-lock.json`、`yarn.lock` 或模块内第二份锁文件；
- OpenAPI 等生成器锁定精确版本；
- TypeScript 启用 `strict` 和 `noUncheckedIndexedAccess`；
- 业务代码禁止 `any`；
- 外部数据先视为 `unknown`，经生成类型、Schema 或类型守卫收窄；
- 禁止用 `as` 掩盖无法证明的协议不一致；
- 使用 ESLint、Prettier 和 Tailwind class 排序；
- 生产业务代码禁止直接 `console.*`。

## 2. 目录和模块边界

```text
packages/frontend/src/
  app/
  modules/
    waker/
    chat/
    team/
    workflow/
    project/
  shared/
    api/
    platform/
    diagnostics/
    hooks/
    i18n/
    lib/
    styles/
    ui/
```

`web/src` 和 `desktop/src` 只负责宿主装配，业务组件放在上述共享包。公共包不得导入 Electron、Node.js 或任一端的实现；通过 `configurePlatform` 注入宿主能力。文件、Git、终端使用共享 Core 适配器，原生窗口、选择器、系统打开、通知与嵌入网页由 Desktop 实现。能力不可用时按能力关闭入口，不能返回空成功数据。

依赖只允许：

```text
app -> modules -> shared
```

- `shared` 不导入 `modules` 或 `app`；
- 模块不得导入其他模块内部文件；
- 跨模块流程放在 `app`，或通过模块公开入口组合；
- 禁止建立无边界的 `utils`、`types`、`common` 大杂烩；
- `shared` 只保存稳定边界或至少两个真实调用方的实现；
- 路由页面负责装配，不承担全部领域逻辑；
- 组件按职责拆分，不以行数作为唯一拆分规则；
- 一次性 JSX 不为形式上的“纯展示组件”规则强制碎片化。
- 一组明显服务于同一交互目的且会共同变化的逻辑应放入名称具体的专门文件，例如输入法提交策略、消息 Markdown、附件展示或工作过程；不得长期堆积在页面或宽泛组件文件中；
- 文件拆分以高内聚、低耦合为准，不按单个 Hook 或辅助函数机械拆分。

## 3. UI、样式和可访问性

- Tailwind CSS 4 是唯一页面样式体系；
- shadcn/ui 源码组件位于 `shared/ui`，Radix 用于复杂交互原语；
- Lucide 是默认图标库；
- 变体使用 `cva`，class 合并使用统一 `cn`；
- 禁止引入第二套组件或样式体系；
- 只有运行时计算的尺寸、坐标和颜色允许 inline style；
- 颜色、间距、圆角、阴影、排版和状态必须使用语义 Token；
- 基础组件来自 `shared/ui`，业务页面不得覆盖其高度、字号、圆角、图标尺寸或阴影；
- 基础控件负责高度、内边距、圆角和状态，页面布局负责可用空间；业务页面不得根据占位文案随手指定输入框宽度，也不得通过 `containerClassName` 重复资源页搜索框宽度；
- 选择控件按职责复用 `Select`、`ResourceFilterSelect` 和 `SearchableMultiSelect`；字段附着的展开层由共享组件匹配触发框宽度并限制在视口内。业务层使用 `PopoverContent` 的 `width` 变体，不用局部 `className` 重新定义浮层宽度；专用异步选择流程仅组合共享浮层与控件，不复制通用选择器；
- Popover 型执行者选择与日期范围触发框复用 `FieldControlTrigger`，与单选及多选字段共享背景、边框和圆角；不得使用操作 Button 的灰底变体充当列表筛选字段；
- 文字 Button 统一通过 `size="sm | md | lg"` 使用 28 / 32 / 36px 按钮阶梯；页面操作和 Dialog 默认使用 `md`，业务页面不得用 `h-*`、`text-*`、`px-*` 或 `rounded-*` 复刻按钮尺寸；
- 普通文字 Button、Scene Tab 和分段控件外轨统一使用 `radius-control`，分段选项使用 `radius-control-inner`；只有圆形图标操作、头像、开关、状态点或显式 `shape="capsule"` 才使用全圆角；
- 项目、记忆、技能、连接器、伙伴和团队等列表页必须复用 `ResourcePageSurface`、`ResourcePageHeader`、`ResourcePageToolbar`、`ResourcePageSearchField` 和 `ResourceCardGrid` 中适用的部分；日期范围与枚举筛选分别复用 `DateRangePicker` 和 `ResourceFilterSelect`；
- 资源卡片网格必须按所属 Scene 内容容器使用三列、两列、单列的等分轨道响应，卡片本身不设置固定宽度；不得使用会让不足三张的卡片自动拉伸为半屏的 `auto-fit`；
- Scene 内页面的响应式判断以主内容容器的实际宽度为准，不能只使用 `sm:`、`md:`、`lg:`、`xl:` 等视口断点决定卡片列数或工具栏布局；侧栏、分屏和检查器都可能让 Scene 内容显著窄于窗口；
- `html`、`body`、`#root` 必须填满并服从实际窗口尺寸，保持 `min-width: 0` 与 `min-height: 0`；桌面窗口最小尺寸属于 Electron 窗口配置，禁止通过 DOM 根节点固定最小宽高，否则会让全部容器查询在窄窗口中按错误尺寸计算；
- 表单字段默认填满所属表单列，由表单容器决定列宽；列表工具栏搜索、枚举筛选、日期范围等高频结构使用 UI 标准规定的语义宽度，不在调用页面重复写 `w-*`；
- 工具栏字段的语义最大宽度必须是唯一生效的 `max-width`；共享宽度变体不得同时输出 `max-w-full` 和 `max-w-field-*`，避免 CSS 顺序把语义上限覆盖为 100%；
- 资源页 Toolbar 必须优先把计数、选择入口、筛选、日期范围和搜索放在同一行；右侧控件组占用剩余空间，筛选、日期范围和搜索以设计系统规定的可读下限参与流式排版，再按字段角色增长到语义基准宽度；只有下限宽度仍无法容纳时才换行，禁止使用 `flex-none`、页面级固定宽度或固定容器断点提前拆行；
- 页面标题区的新建、编辑、删除、发起对话等页面级操作统一使用 `PageAction`；工具栏与 Card 内次级操作使用紧凑按钮，重复局部操作才使用带可访问名称的图标按钮；
- 设置项、详情元数据和运行时操作行不得用固定多列假设可用宽度；应按所属内容容器使用换行、`auto-fit + minmax` 或容器查询降列；
- 浅色和暗色主题只改变语义颜色，不改变尺寸、结构或信息层级；
- 所有操作可通过键盘完成，图标按钮具有可访问名称；
- 动画遵循 `prefers-reduced-motion`，颜色不得成为唯一状态表达；
- Shell 主面板、分组 Card 和 Card 内模块的层级遵守 `ui-cn.md`，内层静态 Card 默认无阴影；资源与详情分组使用共享 `Card` 的语义变体，底栏复用 `CardFooter`，普通 Card 通过背景与留白分层，不在业务组件重复定义表面、圆角或常驻外框。

## 4. 路由和状态

- 使用 React Router Hash Router，由 `app/router` 统一装配；
- 稳定、可分享的筛选、排序、分页和 Tab 状态进入 URL；
- 临时弹窗、Hover 和未提交表单状态保留在本地；
- 服务器事实只由 TanStack Query 管理；
- 实时事件通过 `shared/api` 的统一适配入口更新 Query Cache，WS 与本地文件/Git 观察按通知契约管理；
- 局部交互使用 `useState` 或 `useReducer`；
- 页面范围共享使用状态提升或 Context；
- 当前不安装 Zustand；
- 禁止把 Query 数据复制进第二套全局 Store。

## 5. Effect 和性能

- 业务请求不写在 `useEffect` 中；
- 可由 props 或 state 计算的值不保存为派生 state；
- Effect 只同步外部系统，并返回完整清理函数；
- 不随意禁用 hooks dependency 规则；
- 不默认使用 `useMemo` 和 `useCallback`；
- 开发环境保持 React StrictMode；
- 动态列表使用稳定业务 ID，不使用数组下标；
- 不为假设的数据量全面增加 Memo、缓存或虚拟化。

## 6. OpenAPI、Query 和表单

- 生成目录为 `packages/frontend/src/shared/api/generated`；
- 业务代码禁止修改生成目录；
- 自定义认证、错误和请求配置放在生成目录外的 Client Adapter；
- `pnpm api:generate` 负责生成，`pnpm api:check` 负责检查差异；
- Query Key 使用生成结果或统一工厂，不在组件随手拼数组；
- 列表分页、筛选和排序参数必须进入 Query Key；
- Mutation 成功后精确更新或失效相关 Query；
- 后台刷新保留旧数据，避免页面闪回全屏加载；
- 取消请求不显示为业务错误；
- Core 离线、陈旧数据和普通请求失败使用不同状态表达；
- 复杂业务表单使用 React Hook Form 和 Zod；
- 简单搜索、筛选和少量本地字段可以使用受控状态；
- API DTO 与表单 Model 不一致时显式转换。
- 文本输入框和文本域必须提供表达预期内容或示例的 `placeholder`，但 `placeholder` 不得替代可见标签；
- 每个表单字段或语义完整的字段组必须在邻近位置提供简短说明，解释用途、格式或数据关系；校验错误与说明分别表达，不互相替代；

## 7. 统一通知与流式界面

详细契约见 [通知与工作看板](../architecture/notifications-and-work-board-cn.md)。一条 Core WS 承载服务器更新，页面使用资源子订阅，本地文件与 Git 使用共享原生观察。

- 新增类似待办、阅读提醒、实时状态或进度能力优先注册统一主题/资源订阅，不新增永久 `refetchInterval`、逐实体扫描或页面自建连接；确由外部协议/真实时间驱动的例外需在通知契约记录原因与退出条件；
- 页面、组件和领域 Hook 不直接创建 `WebSocket` 或 `EventSource`；通过 `shared/api` 的窄事件接口订阅，同资源多个消费者共享订阅并引用计数释放；
- 用户通知与仅用于状态更新的事件分开：前者统一供铃铛、看板和唯一提醒组件使用，后者不能自动增加待办；本地操作错误保留原就近反馈；
- 首次加载与正常推送使用带 epoch/revision 的同一摘要；陈旧 REST 响应不能覆盖较新 WS 数据，断线保留陈旧/未知语义而非显示正常零值；
- 提示收起不确认阅读或解决请求；只有前台实际呈现的准确版本可以自动提交阅读。用户主动清空待查看使用独立确认用例，不能伪装真实阅读或处理待办；审批、回答、验收继续调用原接口；
- 原领域持久游标与连接交付序号分开；Chat transient 不推进 JSONL sequence。缺口、重连与缓冲超限通过原权威快照/历史对账；
- 重连采用有上限退避，事件幂等并按稳定 ID/原版本去重；首次/重连快照不逐条重播历史 Toast；
- Token、日志等高频内容只更新对应资源并批量交付，不能逐 Token 重算通知或刷新全局看板；
- 小型摘要与精确结果回执可直接更新缓存；复杂分页仅失效受影响的 Query，保持筛选与旧数据；不得因任意 changed 事件 invalidate 全部缓存；
- 隐藏 Scene 只标记相关数据陈旧，恢复可见时一次对账；业务订阅的释放不取消后端执行；
- 同一来源通过统一订阅与提示入口交付，避免重复连接与重复提醒；所有服务器事实仍只由 Query Cache 管理，不复制进 Context/第二套 Store；
- 生命周期切换、窗口隐藏/重载、Core 重启与应用退出分别处理，订阅和定时器必须可释放，桌面与浏览器适配遵守同一协议。

## 8. 桌面边界

- 页面、组件和领域 Hook 禁止导入 `electron` 或 Node 内置模块；
- 业务 HTTP 与实时订阅统一通过 `shared/api`；全局 WS 的桌面连接由主进程拥有，Renderer 仅通过受限 `shared/platform` 事件适配接收，不把普通 CRUD 改为 IPC；
- 目录选择、外链、日志目录和窗口能力通过 `shared/platform`；
- Bridge 暴露窄且有类型的业务无关接口；
- Bridge 订阅方法必须返回清理函数；
- 浏览器开发环境提供行为可控的 Fake；
- 普通业务 CRUD 不通过 桌面 IPC；
- 新增桌面权限时同步更新 IPC 白名单、来源校验、威胁分析和测试。

## 9. Markdown 和 i18n

- 聊天流式消息使用 assistant-ui 的 `@assistant-ui/react-streamdown`；
- 其他富文本使用 `shared/ui` 的静态 Streamdown 包装；
- 普通说明、标签和短文本使用原生语义元素；
- 禁止原始 HTML，远程图片默认不加载；
- 外部链接经 `shared/platform` 打开；
- Markdown 中的代码只展示和复制，不自动执行；
- 使用 `i18next + react-i18next`；
- 初始只交付 `zh-CN` 并作为 fallback；
- namespace 按领域组织，由 `app` 统一注册；
- Key 使用语义命名并启用 TypeScript 类型；
- 日期、数字和相对时间使用 `Intl`；
- 用户可见文案不得散落为组件内硬编码字符串。

## 10. 反馈、数据状态和诊断

- API 适配层统一转换为 `ApiError`；
- Query 错误在数据区域内展示并提供重试；
- Mutation 失败使用就近错误或 Toast；
- 成功结果已经明显可见时，不重复弹成功 Toast；
- 删除、卸载、取消执行等破坏性操作需要确认；
- 路由级和根级 Error Boundary 必须存在；
- 数据组件处理首次加载、空状态、正常数据、后台刷新、陈旧数据、Core 离线和请求失败；
- 后台刷新不清空已有数据；
- 使用 `shared/diagnostics` Logger；
- 捕获 Error Boundary、全局事件连接、桌面 Bridge 和未处理错误；
- 日志写入失败不得阻塞用户操作。

## 11. 列表和大数据量

- 列表界面默认服务端分页，每页 10 条；使用共享分页组件展示总条数与每页条数选择，查询参数与界面选择保持一致；
- 筛选、排序和分页写入 URL；
- 当前不预装虚拟列表库；
- 实际性能数据证明需要时再为单个长列表引入虚拟化；
- 实时日志窗口有数量上限，较早日志通过 REST 分页加载；
- 聊天历史分页加载。

## 12. 异步页面渲染

- 页面 Shell、页签、输入区和可使用默认值初始化的操作控件必须在路由命中后立即挂载，不得以远端数据是否返回作为渲染前置条件；
- 数据请求只控制其所属内容区。内容未就绪时保留稳定尺寸的容器，并用 `role="status"` 与 `aria-busy="true"` 表达加载状态；错误和空状态也在同一容器内替换，不得卸载页面 Shell；
- 路由参数变化时先提交新路由与加载状态，再请求并填充新数据。不得继续展示旧路由的数据等待请求完成，也不得让请求、编辑器或原生视图初始化阻塞页面切换；
- 组件初始化依赖 DOM 尺寸时，零尺寸表示尚未完成布局，不是失败。组件应先挂载并等待尺寸可用，再创建外部资源；卸载时必须取消监听并释放已经创建的资源。

## 13. 测试

- Vitest 测试纯函数、事件归并、错误转换和 Query Key；
- React Testing Library 按用户行为测试组件；
- MSW 拦截生成 Client 发出的真实请求；
- `shared/platform` 使用 Fake 做契约测试；
- 当前不把 Playwright 和 Storybook 加入项目依赖；
- 不设置全局覆盖率百分比门槛；
- 不使用大面积 Snapshot 替代行为断言。

## 14. 前端完成清单

- [ ] 术语和用户文案符合领域词汇；
- [ ] 没有新增用户确认范围之外的功能或依赖；
- [ ] 依赖方向符合 `app -> modules -> shared`；
- [ ] 页面和组件没有直接调用 Electron、`fetch` 或 `EventSource`；
- [ ] 服务器事实没有复制到第二套 Store；
- [ ] UI 改动完整遵守 UI 设计标准和容器层级；
- [ ] 列表页搜索、筛选、日期范围和卡片网格使用共享资源页组件，没有页面级重复宽度；
- [ ] 单选、短枚举和搜索多选复用共享选择组件，字段展开宽度与触发框一致且受视口约束；
- [ ] Scene 变窄时按内容容器重排，标题、计数和操作不逐字换行，控件和卡片不被裁切；
- [ ] DOM 根节点没有固定最小宽高，窄窗口不会出现整页横向裁切；
- [ ] 加载、空、错误、离线和取消状态已处理；
- [ ] 生成客户端可重复生成且无差异；
- [ ] `pnpm --dir desktop check` 和 `pnpm api:check` 通过。
