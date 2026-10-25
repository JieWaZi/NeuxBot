# 构建与发布

## 入口

`pnpm dev:web` 启动 Vite 与本机 Core；`pnpm dev:native` 启动原生开发入口。`pnpm build:web` 构建浏览器静态资源，`pnpm start:web` 启动本机 Web 发布入口，`pnpm package:web` 组装当前平台的 Web 发布目录（Core、CLI、前端、插件、Pi Bridge、PTY worker、许可与 Node 启动器）。Web 包要求安装 Node.js 22+，运行包内 `node start-web.mjs`。

`pnpm build` 构建 Core、Renderer 与 Electron 代码。`pnpm build:native`（别名 `build:desktop`）调用 scripts/package-desktop.mjs，在当前 macOS 或 Windows 原生环境生成安装包；desktop 的 electron:package 复用同一流程。

发布版本使用 X.Y.Z，并通过 ALLY_VERSION 写入 Core；版本写入符号随 Go module 保持一致。发布工作流位于 .github/workflows/release.yml，仓库及安装包归属为 JieWaZi/NeuxBot。

## 构建资源

品牌标志的矢量母版为 `packages/frontend/public/ally-logo.svg`。在 macOS 运行 `pnpm icon:generate`，通过系统 `sips` 与 `iconutil` 统一生成浅暗标志、应用 PNG、macOS ICNS、Windows 多分辨率 ICO 和托盘图标；派生资源不手工修改。默认安装图标使用浅色底板，暗色应用图标另存为 `desktop/resources/app-icon-dark.png`。

Core 使用 CGO_ENABLED=0、-trimpath 和 -s -w。Renderer 由 Vite 编译，主进程与 preload 由 esbuild 编译，node-pty 保留原生运行依赖。系统技能、Pi Bridge（源在 `packages/pi-bridge`，生成到 `resources/pi-bridge`）、终端 worker（源在 `packages/terminal-runtime`，构建到 `resources/terminal-runtime`）、工作流 SDK、呈现目录与固定插件源必须和生成源一致。

`pnpm plugins:prepare` 生成自有插件库来源元数据，不生成内置插件安装 ZIP；既有提供者兼容依赖仍按固定来源和摘要准备。自有插件包及其桥接依赖由独立 neux-plugins 仓库构建和发布，用户通过 HTTP 目录安装。`pnpm plugins:check` 定向核对来源声明与本地资产规则，不执行第三方 CLI。来源、更新和缓存用法见 [固定插件源](../../plugin-sources/README.md)。运行期间不提供上传或导入本地插件的入口。

已由 Vite 或 esbuild 打包的 JavaScript 依赖放在开发依赖中，安装包仅额外携带 node-pty 及其必要依赖。node-pty 只保留当前平台与架构的预编译文件；本机编译目录仅保留 Release 的原生模块、动态库、可执行文件及 ConPTY 运行文件，排除调试符号、静态库、对象目录和构建元数据。node-addon-api 只保留包信息与许可，不携带编译期头文件和工具。electron-builder 固定使用 `--publish never`，产物由独立发布任务统一上传，打包任务无需 GitHub 发布令牌。

原生发布前将 Renderer 资源装配到 `desktop/dist-native`，对超过 1 KiB 的 JS、CSS、JSON、SVG 使用 Brotli 无损压缩。`ally` 协议恢复原字节与 MIME，HTML 保留 CSP 校验；开发模式仍使用原 `dist`。资源冒烟逐项比对压缩发布与原始构建字节。启动预算始终统计未压缩产物。

首屏静态 JS 预算为 500,000 bytes，工作台启动 JS 预算为 4,500,000 bytes；统计包含启动前等待或预热的动态依赖。启动错误面板仅在失败时加载，Mermaid 引擎仅在实际绘图时加载。打包检查安装包 170 MiB、安装文件 450 MiB、Resources 215 MiB 的体积上限，输出机器可读报告。Resources 包含 Core、CLI、前端产物及必要原生模块；预算涵盖约 72 MiB 的 Core/CLI、约 40 MiB 的前端及运行资源，以及约 47 MiB 的固定飞书原生程序与完整技能、约 38 MiB 的固定 GitHub CLI。macOS arm64 实测 Resources 约 196.3 MiB（加入 GitHub 隔离构建后比 158.5 MiB 基线增加约 37.8 MiB），资源预算保留约 18 MiB 余量；应用实测约 436.6 MiB，其他平台以各自原生构建报告为准。Windows 即使超限也输出三项实测体积及最大的 30 个文件，失败构建的报告由发布工作流单独保存。实际文件名与默认平台以打包脚本为准。

发布矩阵在原生 runner 上分别构建 macOS arm64、macOS x64（amd64）与 Windows x64，并校验构建机架构。GitHub Release 直接提供用户可安装的 NeuxBot-mac-arm64.dmg、NeuxBot-mac-x64.dmg 和 NeuxBot-windows-x64.exe，附带分架构体积报告及 SHA256SUMS。Actions 中以 NeuxBot 命名的 artifact 仅用于 CI 暂存与跨任务传输，用户下载入口为 Release 中的 DMG 或 EXE。代码签名、公证及签名密钥由发布环境管理；本地签名不表示完成发行签名与公证。

## 发布前

CI 将生成契约与资源、前端类型与 lint、前端测试、Web 与终端、桌面检查和 Go 检查分为独立任务并行运行。前端完整测试按四片分配，单片失败不取消其他片，测试超时与用例集合不变。同组独立检查在前序检查失败后继续执行；依赖安装或必要构建失败时，仅跳过依赖其产物的检查。最终 `verify` 必须取得所有任务的成功结果，失败、取消或跳过均不能通过发布门禁。Release 复用同一 CI，全部检查成功后才开始三平台打包。

版本差异说明维护于 `docs/releases/vX.Y.Z.md`，按前一个发布版本列出新增功能、优化、兼容边界及完整比较链接。发布任务从 tag 对应源码读取说明，并追加安装包签名状态；没有对应文件时才使用 GitHub 自动生成说明。重跑发布会同步更新说明与产物。版本号仍由 tag 和 `stamp-release-version.mjs` 写入发布构建。

运行 pnpm check，确认生成文件、版本、许可和资源链接完整。在对应原生平台验证安装包、终端与 Core 的认证健康检查及退出回收。真实智能体、账号、窗口和视觉验收分别记录；不将本地测试结果等同于全部平台可用。

终端冒烟在独立的 Electron Node 进程中验证真实 PTY 输出、退出码和临时目录清理，完成后刷出结果并明确退出，避免 Windows ConPTY 残留 worker 阻塞测试进程。随后独立验证打包 Core 的认证健康检查与停止回收；该测试不代表完整桌面应用的资源释放验收。

公开发布只包含当前源码和必要构建资源，不包含本机缓存、日志、截图、临时仓库或开发技能安装环境。第三方许可随安装包保留，见根目录 THIRD_PARTY_NOTICES.md。
