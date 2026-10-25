# GitHub 插件与授权入口 Spec

状态：按用户已授权的首版插件范围追加开发。本文中的实现选型由开发者依据官方契约决定，不表示用户逐项指定。

## 目标和范围

资源库继续聚合插件、技能与 MCP。增加可安装到伙伴的 GitHub 插件，登录一次后多个伙伴共享同一个 GitHub 身份。首版飞书和 GitHub 均提供“显示二维码”弹窗与“打开授权页面”操作，完成认证后沿用安装、按需 Skill、MCP 投射、配置修订和执行快照。

GitHub 首版支持 github.com 的仓库、Issue 与 Pull Request。使用官方 GitHub CLI 2.102.0 源码的应用隔离构建完成设备授权，使用官方远程 GitHub MCP `https://api.githubcopilot.com/mcp/`，限制工具组为 context、repos、issues、pull_requests、users。GitHub 包直接提供官方 MCP；CLI 仅由 Core 用于共享认证，不向智能体交付登录目录或新增自有 GitHub Skill。

不做历史迁移，不加入企业自托管地址、多账号切换、云端市场、自动升级或后台账号探测。现有飞书能力继续保持。

## 官方依据与选型

- [GitHub MCP 官方仓库](https://github.com/github/github-mcp-server)：MCP 是官方提供的现成能力。
- [官方 MCP OAuth](https://github.com/github/github-mcp-server/blob/main/docs/oauth-login.md)：本地发行版自带 OAuth 应用，但令牌仅保存在单个进程内存，无法直接提供本应用的跨伙伴共享持久登录。
- [远程 MCP 集成](https://github.com/github/github-mcp-server/blob/main/docs/host-integration.md)：远程服务接受 OAuth/PAT Bearer；不支持动态客户端注册。
- [GitHub CLI 登录](https://cli.github.com/manual/gh_auth_login)：官方应用的设备授权不要求用户另建应用。
- [固定版本登录实现](https://github.com/cli/cli/blob/v2.102.0/internal/authflow/flow.go)：非交互模式输出原始 URL 与一次性验证码，不调用浏览器、不写剪贴板。
- [环境约定](https://cli.github.com/manual/gh_help_environment)：`GH_CONFIG_DIR` 选择官方私有配置；环境 token 会覆盖配置，必须清除。

采用官方 CLI 的完整原生流程，不复制 CLI OAuth client ID、不自行交换 token。CLI 固定源码归档由构建阶段下载并核对摘要，使用 Go 1.27.1 和唯一底层钥匙串隔离补丁编译；所有 Set/Get/Delete 的 service 加 neuxally: 前缀，覆盖官方配置与迁移路径。分发名称标记 2.102.0-neuxally.1，Core 在执行认证前验证此构建身份，原版 gh 不得被误用。补丁和真实上游登录隔离回归同时固定摘要，保留原始 MIT 许可，不声称原版发布字节未修改。远程 MCP 由 GitHub 持续部署；仅固定其服务地址和工具组，不声称远端实现版本已固定。

## 共享认证和生命周期

从现有飞书授权组件抽取实际公用的账号摘要、会话状态、工作占用和 Core 生命周期。适配器只负责各自官方进程契约，资源库、安装和执行通过同一授权组件访问提供者，不能把 GitHub 放进飞书领域。

- Core 唯一拥有每个提供者的私有 profile、授权等待和占用记录。
- GitHub 使用应用数据目录下 `plugins/providers/github`，目录 0700。调用 `gh auth login --hostname github.com --web --git-protocol https --skip-ssh-key --clipboard=false --insecure-storage`，输入输出均使用管道，禁用提示与颜色。
- 明确使用官方文件 profile，避免不同配置目录仍命中用户机器已有的同名系统凭据。CLI 自身写入的凭据文件限制为 0600；这些文件属于凭据，不能进入日志、前端、插件清单、数据库或执行快照。
- 授权进程清除继承的全部 `GH_*`、`GITHUB_*`，设置私有 `GH_CONFIG_DIR`、`GH_HOST=github.com`、`GH_PROMPT_DISABLED=1`、`NO_COLOR=1`、禁用更新提示；授权与 MCP 不读取外部 gh 登录。业务指令禁止更改登录或读取其他 profile。
- 使用固定版本输出中的验证码及原始 HTTPS GitHub 设备 URL；不能自造带验证码的 URL。二维码编码原始 URL，用户在手机或网页输入相同验证码。GitHub CLI 不输出官方到期时间，界面显示 Core 的本机等待上限（15 分钟），不称其为服务端精确到期。
- 登录仅由用户点击开始；同提供者并发开始复用同一等待。请求取消、等待超时、Core 退出均终止真实进程；取消先更新界面，但真实进程退出前继续阻止新登录、退出、安装及执行。
- 授权等待只由 Core 根 Context 拥有；HTTP 请求结束不会终止已返回的等待。初始化最多 30 秒，成功/失败通过统一 plugins.changed 事件更新，无新增前端轮询或连接。
- 活跃工作持有提供者占用，结束后释放；占用期间不能变更共享身份。退出只清理本应用 GitHub profile，不操作用户已有 gh profile 或系统钥匙串，不声称已撤销 GitHub 服务器授权。
- `gh auth status --hostname github.com --active --json hosts` 在线验证账号和实际 scopes；权限满足判断复用 GitHub 官方包含关系，如 write:org 满足 read:org，不能要求规范化结果重复返回子权限；未登录可正常返回未就绪摘要，网络错误不能当成已登录。GitHub 必需 scopes 是 repo、read:org、gist；不追加飞书的 offline_access。扩展权限保留明确用户输入。

## MCP 和执行边界

安装与切换版本必须验证固定程序、内容摘要、账号和必需权限，并持有内容及身份占用直到事务结束。同一请求按 provider 复用一次状态查询，不能让两个提供者共享同一缓存条目。

执行冻结 provider 账号标识、版本、配置修订与实际组件标识；重试身份不同必须拒绝。GitHub CLI 仅供 Core 使用私有 profile，智能体只接收官方 MCP 及公开身份指纹。官方 MCP 在准备执行时调用该私有 CLI 的 `auth token`，仅在内存内把 Bearer 加入派生连接器；账号验证和 token 读取期间持有同一占用。绝不把 token 加入 Skill、模型指令或快照。

凭据注入仅允许精确的官方远程地址，要求 streamable HTTP，拒绝其他域、userinfo、query、fragment、重定向地址或本地 stdio。即便本地导入包伪称 github provider，也不能向自定义 URL 或 stdio 环境拿到令牌。远程服务 URL、工具组与认证头不可被普通共享配置覆盖。敏感请求头继续使用现有 Runtime 连接器凭据边界。

## 界面

公共授权面板根据 provider 显示说明：飞书保留应用 ID/密钥配置，GitHub直接显示登录，不展示飞书应用表单。登录等待显示两个并列入口：显示二维码、打开授权页面。二维码在共享 Dialog 中显示，可关闭后重新打开；关闭只关闭弹窗，不取消授权。授权完成或取消后弹窗关闭，短时 URL 与验证码清空。

GitHub 等待区和弹窗均显示一次性验证码及复制按钮，提示在官方网页输入。复制失败和外链失败在当前区域显示。二维码、验证码和网页链接只存在当前授权响应，不进入事件载荷。状态保留等待、已登录、取消、超时、失败和运行占用说明。

复用现有按钮、Dialog、语义样式与 qrcode.react；浅暗主题同结构，弹窗支持键盘、焦点恢复、窄容器和长文本。不自动打开用户浏览器。

## 固定源与分发

保持独立源 Git 仓库与 bundle，新增 github 插件目录，更新固定 source commit/bundle SHA。构建锁从单 provider 改成多 provider 描述；每项包含精确版本、上游 commit、固定输入 URL/SHA、程序名称和许可证位置。飞书提取原版发布资产；GitHub 从固定源码、工具链及隔离补丁构建六种目标。只把对应平台的两个提供者程序放入安装包，保留准确许可证和来源声明。构建检查执行真实包解析，不扫描文本假装功能测试。

## 验收标准

1. 固定 catalog 同时包含飞书与 GitHub，两者通过标准插件解析；离线启动无需访问源 Git 仓库。
2. GitHub 可从未登录开始，无需填写应用 ID/secret；官方 CLI 原始 URL/验证码能够驱动公共等待界面。
3. 飞书与 GitHub 的二维码均通过弹窗打开；网页入口使用原始 URL；GitHub 显示、复制验证码。关闭弹窗不取消等待。
4. 未登录、权限缺失、网络失败、外部账号覆盖、设备拒绝/超时、取消与延迟进程退出均正确反映，不能错误显示可安装。
5. 两个伙伴复用同一个应用私有身份；外部 gh 配置和 token 不影响授权/执行；活跃工作锁阻止身份改变。
6. 安装、更新、自动投射、显式选择、重试复用现有准入；官方 MCP 在受支持 Runtime 得到真实凭据，伪装 provider 的其他地址被拒绝。
7. token、完整 CLI 输出和设备内部凭据不进入 API、事件、数据库、快照与日志；代码测试覆盖可观察边界。
8. 完整前后端检查与 Go race 通过；测量当前 macOS 包体，不无依据增加预算。用户未授权真实页面操作时不打开 GUI；真实账号授权、GitHub 业务调用和视觉验收独立标记尚未运行。
9. 完成后由新的独立子代理审查最终差异，修复确认问题，提交并推送当前独立分支，更新现有面向 main 的 PR #2，不合并。
