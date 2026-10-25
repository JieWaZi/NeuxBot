# Desktop / Web 共享应用规格

> 状态：重构已实现并完成独立代码 review；实际平台与人工业务验收边界见实施计划。
> 范围：浏览器访问本机 Core，个人本地使用。本文替换同路径草案，作为此次拆分的规格来源。
> 实施任务与验证见[实施计划](../plans/2026-10-23-desktop-web-split.md)。

## 1. 目标与边界

保留顶层 desktop 和 web 两个独立入口，将共同变化的业务界面、API、查询、事件恢复和设计系统抽到共享包。Desktop 保持原生体验，Web 无需安装、启动或连接 Electron；两端使用同一套 Go 领域实现。

已确认需求：

- Web 首期只访问同机 Core；项目目录、Git、终端、CLI 和数据均属于这台机器。
- 聊天、伙伴、团队、工作流、知识库、资源管理、设置等业务页面共享。
- 标题栏、系统窗口按钮、拖动区域、原生文件对话框、系统打开文件和原生浏览器视图按端维护。
- 文件、上传下载、Git、终端在后续里程碑逐项接入 Web；受限的核心业务里程碑不能宣称全功能等价。
- 每项能力迁移完成后，两端使用一个服务实现；不长期维护两套文件或 Git 规则。

本次不包括：远程自托管、局域网或公网开放、多用户与租户、远端 Core 切换、移动端重设计、任意网页的等价内嵌、跨机器数据迁移。不得把这些场景的账号体系、代理或同步机制作为本机 Web 的前置条件。

## 2. 当前依据

基线提交为 c49be4406a6b8cdb5451377d522b1f6f4d89793c。主工作区当时存在未解决的合并冲突，独立 worktree 从该已提交版本建立，不包含未提交修改。实施开始前重新核对已合并的新基线。

| 当前位置                                                               | 现有职责                          | 拆分影响                                   |
| ---------------------------------------------------------------------- | --------------------------------- | ------------------------------------------ |
| [Renderer 入口](../../../desktop/src/main.tsx)                         | 启动 React 应用                   | 变为按端装配的薄入口                       |
| [AppShell](../../../packages/frontend/src/app/layout/app-shell.tsx)    | 共享布局混有宿主判断              | 抽标题栏插槽，业务导航继续共享             |
| [桌面适配](../../../packages/frontend/src/shared/platform/index.ts)    | 连接、文件、Git、终端与浏览器调用 | 拆成接口和两端实现，移除共享页面的宿主判断 |
| [API 初始化](../../../packages/frontend/src/shared/api/runtime.ts)     | 本地 Bearer、回环重定向           | 引入受信连接和认证策略，不能只改 baseUrl   |
| [全局事件](../../../packages/frontend/src/shared/api/global-events.ts) | Main 转发与浏览器直连             | 共用协议和恢复逻辑，保留两种连接所有权     |
| [文件范围](../../../server/internal/workspacefiles/scope.go)           | Main 校验范围、版本、观察         | 将执行与校验迁入 Core                      |
| [Git](../../../server/internal/workspacefiles/git.go)                  | Main 绑定 Chat 范围和仓库         | 将实际执行迁入 Core                        |
| [终端](../../../packages/terminal-runtime/src/manager.ts)              | node-pty、恢复屏幕、附着与背压    | 单独迁移生命周期与流协议                   |
| [路由](../../../server/apis/root.go)                                   | 管理 API 本地令牌认证             | 增加明确的本机 Web 会话认证分支            |
| [Core 启动](../../../server/cmd/ally-core/main.go)                     | 独立进程入口，可选桌面父进程监控  | 增加可选静态站点与本机配对入口             |

既有架构约束见[架构总览](../../architecture/overview-cn.md)、[桌面边界](../../architecture/desktop-lifecycle-cn.md)、[存储边界](../../architecture/workspace-and-storage-cn.md)和[通知契约](../../architecture/notifications-and-work-board-cn.md)。

## 3. 目录、包与依赖

实施采用以下目录结构：

```text
desktop/
  src/main.tsx
  src/shell/                       # 原生窗口装配
  src/platform/                    # Desktop 实现
  electron/                        # Main、preload、IPC、原生视图
  resources/                       # 桌面安装包输出资源
web/
  src/main.tsx
  src/shell/                       # Web 页头与配对入口
  src/platform/                    # 浏览器宿主实现
packages/
  frontend/
    src/app/                       # 路由、Scene、跨模块装配
    src/modules/                   # 共用业务界面
    src/shared/                    # UI、API、Query、主题、事件、i18n
    public/                        # 共用源素材
    vendor/                        # 共用渲染资源及许可
    openapi-ts.config.ts            # 唯一客户端生成配置
  platform-contracts/
    src/                           # 中立能力类型、事件协议与中立事件客户端
  pi-bridge/                       # 服务执行资源构建，不依赖 Desktop
  terminal-runtime/                # Core拥有的Node PTY / xterm屏幕运行时
server/
  apis/                            # 原业务入口及工作区、会话、终端入口
  internal/                        # 文件范围、监听、Git、PTY 等深模块
  services/                        # 仅存在多个真实入口复用的业务编排
scripts/                           # 两端共用生成、构建与检查
```

包名为 @neuxbot/frontend、@neuxbot/platform-contracts，均为仓库内部包。pi-bridge 在独立 Web 交付前迁出 desktop，使用原生成源和许可。

依赖方向：

```text
desktop/src ─┬─> packages/frontend ─> packages/platform-contracts
             └─> Desktop adapter ──> packages/platform-contracts
web/src ─────┬─> packages/frontend
             └─> Web adapter ──────> packages/platform-contracts

packages/frontend 内部仍为 app -> modules -> shared
公共生成 Client / 资源客户端 -> Core -> 文件、Git、PTY、Runtime
Desktop adapter -> 白名单 preload -> Electron Main
```

硬边界：

- Desktop 与 Web 不互相导入；公共应用不引用两端目录、Electron、Node 或 window.allyDesktop。
- platform-contracts 不依赖 React、应用模块、Electron 或 Node；只容纳实际跨端需要的接口与中立协议，不成为工具集合。
- API SDK 只生成一份，最终位于 packages/frontend/src/shared/api/generated；迁移配置和命令后通过生成器更新。
- IPC 专用 Schema、来源校验和命令白名单留在 desktop/electron；不把整份 DesktopBridge 变成公共接口。
- React、Query Client、主题和业务事件管理在每个应用实例中各装配一次，服务器事实继续只由 Query Cache 管理。

## 4. 共享与差异矩阵

| 能力            | 公共部分                         | Desktop                          | Web                                  |
| --------------- | -------------------------------- | -------------------------------- | ------------------------------------ |
| 页头            | 返回、导航、Scene 标签           | 系统窗口按钮、拖动区域、系统间距 | 普通页头，无红黄绿按钮或对应占位     |
| 业务页面        | 页面、查询、表单、审批与阅读语义 | 注入桌面宿主                     | 注入浏览器宿主                       |
| 文件工作台      | 文件树、搜索、编辑与预览         | 系统打开、目录选择               | 已授权目录选择、文件上传下载         |
| 文件与 Git 执行 | Core 统一校验与执行              | 最终调用 Core                    | 调用同一 Core                        |
| 上传下载        | 附件身份、进度、限制、失败恢复   | 原生文件选择、另存为             | File / Blob 与浏览器下载             |
| 终端            | xterm 展示、输入与会话交互       | 最终消费 Core PTY                | 消费 Core PTY                        |
| 任意网站        | 可复用的地址与导航交互           | 原生内嵌视图                     | 首期打开外部标签页                   |
| HTML 产物       | 授权读取、产物身份               | 原受限预览                       | 隔离预览或下载，不能继承管理应用权限 |
| 通知            | 应用内通知、看板、阅读状态       | 原生通知和点击回到应用           | 应用内通知；系统通知不是首版前置能力 |
| 执行与存储      | 原 Core、SQLite、JSONL、资源     | 本机 Core                        | 同机 Core，不新增执行引擎或数据副本  |

截图中红黄绿按钮归 Desktop Shell；返回按钮归共享导航。公共布局通过标题栏和网页内容插槽组合，不复制整个 AppShell。

## 5. 平台接口与能力声明

宿主行为和服务器执行能力分开组织：

- Connection：受信 Core 地址、认证策略、连接恢复、API 版本；桌面是本地 Bearer，Web 是同源会话。
- HostActions：选择上传文件、保存下载、打开外链；原生目录对话框和系统打开文件是明确可选能力。
- WorkspaceResources / Git / Terminal：共同界面依赖的窄接口。过渡期 Desktop 注入旧实现，能力迁入 Core 后两端注入同一客户端。
- BrowserSurface：区分 native、external 和受管产物预览，不用空方法伪装原生内嵌。
- ShellSlots：窗口装饰等组件由入口提供；无须把 React 组件放进中立契约包。

宿主能力由装配声明；服务能力来自认证后的 Core 能力响应，不能在 Web 再维护一份业务开关。服务能力包含必要的协议版本与支持状态。

不支持、未授权、离线、认证失效和暂时加载中分别表达。尚未迁移的入口隐藏；打开旧标签或深链接时在内容区说明限制并提供可用操作。禁止用空文件列表、空 Git 状态或假成功掩盖未实现能力。能力声明不能替代服务端授权。

## 6. 本机启动、配对与认证

### 6.1 进程和交付

正式使用时，Core 在明确启用 Web 后，从安装资源目录提供 web/dist 和同源 API / WS；只绑定 127.0.0.1，使用实际绑定端口。不依赖源码 checkout 或 desktop/node_modules。沿用 Hash Router 和根路径部署。

本机 Web 模式使用独立启动的 Core，不设置桌面父进程环境变量。关闭网页不结束 Core。Desktop 继续保留原有 Core 进程所有权和退出规则；Web 模式不是同一数据目录上的第二个进程。

同一数据根只能由一个 Core 管理。启动发现已被占用时明确报错，不以随机端口绕过数据锁。首期不承诺 Electron 与独立 Core 自动接管；使用独立 Core 时，可沿用现有显式外部 Core 配置，桌面退出不得停止外部 Core。

开发模式由 Web Vite 代理静态同源路径至回环 Core；代理固定目标、精确 Origin 和开发配置，不向网页注入本地令牌。生产能力以构建后的 Web 和 Core 验收，不能以 Vite 页面能打开代替。

### 6.2 最小本机会话

以下为本机 Web 的拟定默认行为，不引入管理员密码、注册或用户数据库：

1. 本机启动命令向操作者输出网页地址和一次性配对码；地址中不携带凭据。
2. 用户在网页输入配对码，以 POST 请求正文换取会话。配对码使用至少 256 bit 随机量、5 分钟有效期，成功后立即失效。
3. Core 仅在内存保存码的摘要、会话和撤销状态；配对端点限速。提供仅本机启动工具可调用的重新签发方式，不提供未认证网页获取配对码的接口。
4. 会话 Cookie 使用 HttpOnly、SameSite=Strict、host-only 和明确 Path；本机 HTTP 不依赖 Secure Cookie。会话最长 12 小时，Core 重启、退出配对或用户主动退出后失效。
5. 页面刷新复用有效会话；配对码过期时提示通过本机工具重新签发。退出清除当前会话及其订阅，其他浏览器会话独立处理。

现有 /api/v1 管理入口保持单份路由和处理器，认证边界明确识别两类请求：

- Desktop 或既有受信客户端：继续验证本地 Bearer；提供无效 Authorization 时不能悄悄降级到 Cookie。
- Web：仅在启用 Web 且 Host 匹配绑定地址和端口时接受有效会话；写请求校验精确 Origin 和会话关联的 CSRF 证明。
- WS：在同一认证策略下识别桌面子协议令牌或 Web 会话，Web 握手必须校验精确 Origin；退出和过期关闭相关事件与终端附件。
- 启用 Cookie 不放宽原 Bearer 的来源与令牌要求，不影响 agent 执行授权及对外集成 Key 的命名空间。
- Web 静态首页和配对入口不暴露业务数据；Cookie 请求不启用通配跨域凭据访问。生产只允许一个明确的本机 Web Origin。
- 凭据不进入 URL、localStorage、Vite 变量、静态产物或常规诊断日志；一次性码只在签发时交付操作者。
- 静态 HTML 不长期缓存，哈希资产可缓存；静态文件和 Core 按同版本发布，协议不匹配时明确要求更新。

精确配置名、配对工具命令及最小会话 DTO 在认证任务中结合现有 Cobra/ginx 装配确定，通过生成契约冻结；上述授权语义不得在实现时降级。

## 7. 文件、项目和附件

Core 是授权和文件执行的唯一所有者；界面不能凭绝对路径扩大范围。

- 已有 Chat 的文件操作绑定 Chat 冻结工作范围；草稿与项目选择使用独立的、用户明确授予的根，不冒用已有 Chat 授权。
- Web 项目配置允许用户明确输入本机目录，由 Core 校验并确认授权；已有项目可直接选择。文件选择器上传目录不能自动变成 Core 项目根，不新增默认全盘浏览器。
- 文件范围句柄由 Core 签发并绑定主体、业务身份及当前 Core epoch。每次访问复核归属、真实路径、软链接和读写限制；重启后重新获取。
- 列表、搜索、读取、写入、重命名、删除和观察保留原语义；枚举有界且区分截断与完整结果。写入携带 expectedVersion，冲突时保留用户编辑并显示对账入口。
- 文件读取可取消且有大小上限；二进制通过字节流传输，不以数字数组 JSON 复制现有 IPC。保留原字节下载和准确文件名。
- 浏览器上传接收文件名和字节，由 Core 分配暂存身份与目的位置。Chat 创建前使用草稿暂存，发送接纳时原子绑定归属；取消、过期和失败上传清理。
- Desktop 原生路径选择可在宿主完成，但交付给业务时转换为同一资源语义；不得继续靠路径字符串绕过暂存与授权。
- 插件、技能导入、知识原件、角色模板、工作流产物及聊天附件逐个接入或明确标注限制；已有适用领域接口复用，不新建重复数据源。
- 文件与 Git 观察按范围共享、引用计数释放；变化注册统一事件并精确更新或失效 Query，不新增页面轮询。

Shell 和原生对话框仍按端维护，实际文件执行迁入 server/internal 的深模块。每批迁移以通过旧约束回归并切换 Desktop 后，删除该批 Main 服务和 IPC 为完成条件。

## 8. Git

保留共享列表、分支选择、历史、Diff 和操作确认。Core 从获准根发现仓库，校验所选仓库属于该范围；相对文件路径、revision 和参数逐项校验，不把客户端输入拼接成 shell 命令。

迁移先读后写：状态、历史和差异先接入；再迁分支预检与切换、暂存、取消暂存、提交和丢弃。保留冲突预检和危险操作确认；Git 返回错误不能显示为干净仓库。写操作与文件观察关联失效，重复请求不能导致重复提交或静默覆盖未知结果。

两端切换后删除旧 scoped-git 相关 IPC，Git 实际执行不再属于 Electron。

## 9. 终端

公共 xterm 展示继续复用。Core 负责会话授权与生命周期，通过 stdio JSONL 管理 Node worker；worker 复用迁出的 node-pty 与 xterm 屏幕实现。Native 使用 Electron 的 Node 模式，Web 发布包要求 Node.js 22+。macOS 已验证真实创建、输入、输出和进程回收；Windows 保留原 node-pty 实现和目标二进制装配，但必须经过真实平台验收后才宣称已验证。Linux 不作为当前新增支持承诺。

会话与 UI 附件分开：

| 事件               | 要求                                                           |
| ------------------ | -------------------------------------------------------------- |
| 创建终端           | 绑定授权根、内容 owner、操作者与稳定 sessionId；幂等创建       |
| 附着或页面刷新     | 新建 attachment，恢复有界屏幕和 sequence，不重复启动 shell     |
| 网络断开           | 分离附件，保留 60 秒重连窗口；到期回收无人附着的交互 PTY       |
| 关闭终端内容       | 明确关闭所属会话并释放子进程                                   |
| 主动退出或会话过期 | 立即撤销附件访问，终止该 Web 会话独占的交互 PTY                |
| Core 退出或重启    | 有界结束 PTY；旧 sessionId 不复活；业务 Execution 按原机制处理 |

终端不是业务 Execution，不因为网页断线取消聊天或工作流。UI 显示断线、恢复、已退出状态，不自动创建替代会话。

PTY 字节采用独立受认证双向 WS，带序号、背压、有界缓冲、屏幕快照恢复与 resize 校验；统一业务 WS 只承载终端状态及资源失效。创建、关闭等资源操作进入 OpenAPI，字节协议以唯一协议定义和契约测试维护。

终端执行实现只有 packages/terminal-runtime 中一份；worker 是 Core 的受管子进程，不提供额外监听。当前支持证据以实际运行平台为准，交叉编译不能替代真实 Windows PTY 验收。

## 10. 网页视图、启动与实时状态

Desktop 的原生网页视图与浮层 presence、生命周期保持现状，相关代码归 Desktop adapter。Web 任意 URL 打开外部标签页；已有原生视图标签进入 Web 时在内容区提供外部打开操作，不反复尝试原生初始化。

受管 HTML 默认下载或在隔离 sandbox 中预览，不能同时获得管理源身份和脚本权限；本机预览地址不等于允许加载任意文件。公共文件预览仍按原类型处理 PDF、Office、Markdown、图片和代码，资源/worker 路径由两端构建保证。

公共启动协调管理主题、品牌与工作区挂载。Web 在完成配对后读取本机 Runtime 状态，桌面继续注入原连接；未配置 Runtime 时使用现有设置流程，不阻塞整个 Shell。

Desktop 主进程继续拥有一条业务 WS；Web 每个页面实例拥有一条。共享主题订阅、去重、epoch/revision、快照恢复、通知阅读与精确 Query 失效；不新增第二套服务器状态。隐藏页面减少无效刷新，重新可见时对账。认证失效停止重连，清理 Query 和句柄；旧响应不能写回新会话。

## 11. 工具链、交付与迁移规则

- 单一 pnpm workspace 和锁文件；共用运行依赖迁到正确包，不能依赖 desktop/node_modules 的偶然可见性。
- Web 的过滤安装与构建不拉起 Electron 安装脚本；Desktop 原生依赖只在桌面目标安装。
- 共用 Tailwind、语义 Token、字体、i18n、Vite 公共配置和实际共用静态源；不新建第二套 UI。
- 同步 PDF.js CMaps / fonts / WASM / worker、Office vendor、图谱、伙伴素材、图标、工作流 presentation、插件、技能和许可路径。
- API 生成、检查、UI 策略、边界检查、格式检查和启动预算覆盖新目录；不得因迁目录而跳过生产约束。保留现有桌面预算阈值，Web 使用同口径测量。
- 运行资源迁出 Desktop 归属，独立 Web 的发布目录包含 Core、Web、Pi bridge、系统技能、插件及实际必需资源；不能仅交付静态首页。
- 源码迁移不改 SQLite 表、Chat/Run/项目 ID、JSONL 或用户数据布局；不复制数据库。
- 结构移动与行为迁移分开提交。过渡代理在公共应用移动完成时删除；每种运行能力切换时只有一个执行实现，不自动失败回退到另一个实现。
- 活跃 PTY 不跨进程热迁移；安排关闭或维护窗口后切换。回退配套的 Core 与前端版本，不能恢复已关闭的交互进程。

## 12. 验收

| 编号 | 可观察的通过条件                                                                |
| ---- | ------------------------------------------------------------------------------- |
| A1   | 共享包没有两端目录、Electron、Node 或全局 Bridge 依赖；两端不复制业务页面       |
| A2   | Desktop 页头和原生交互保持基线；Web 无窗口按钮占位，保留共享导航                |
| A3   | 无 Electron 的独立交付可启动本机 Web，配对后完成真实聊天执行、审批与成果读取    |
| A4   | 未配对、过期、错误 Host/Origin、伪造 CSRF、旧配对码及越界文件请求均拒绝         |
| A5   | 上传草稿到发送的归属正确；下载原字节；编辑版本冲突不丢本地内容                  |
| A6   | 两端共用 Core 文件与 Git；软链接越界、错误仓库、失败 Git 查询不冒充正常         |
| A7   | 终端刷新恢复同一会话，断线有界保留，关闭和退出正确回收；目标平台分别验证        |
| A8   | 多页面及 Desktop 对同一 Core 的修改可对账，无第二套领域事实或无界监听           |
| A9   | API 只生成一份；两端 PDF/Office/主题/静态资源完整；Web 不执行 Electron 安装脚本 |
| A10  | 网页关闭不取消已接纳执行；进程与数据根所有权明确，不能启动重复调度器            |

实施过程默认只跑变更点及直接受影响的定向测试、类型、样式、格式和生成契约检查；全量检查只在用户明确要求时执行。真实页面、浏览器和桌面验收需要用户明确授权，不能把静态检查记作视觉或真实模型通过。

核心业务里程碑满足 A1–A4 中对应范围；文件、Git、终端按计划分别关闭剩余验收项。未实现项必须列为限制，不能提前在当前架构和使用指南宣称支持。

## 实施决定（2026-10-23）

- PTY 复用已验证的 node-pty 与 xterm 屏幕恢复，由 Core 通过 JSONL stdio 管理独立 worker，不新增对外监听或独立守护进程。Web 包使用 Node.js 22+；Native 使用随 Electron 交付的 Node 模式。平台安装包分别携带对应 PTY 二进制。
- Chat 资源范围由已接纳 Chat 的冻结目录签发。创建 Chat 前的文件搜索与分支选择通过显式选择目录的 draft scope 实现，同样绑定配对主体、真实根目录与期限；draft scope 不授予终端创建权限。
- 文件观察复用全局事件通道的 workspace 主题；每个观察先登记不可猜测标识，再按主体订阅。断线重新获取权威快照，注销释放观察。
- 文件/Git/终端的公共适配器按首次使用加载，宿主装配本身不启动这些服务；首屏预算继续包含实际等待的宿主模块。
