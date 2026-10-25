# 插件资源与飞书插件实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans，按任务执行并维护独立账本；完成全分支后使用独立子代理审阅。

**Goal:** 实现统一资源库、本地插件安装、共享认证、伙伴版本管理和真实飞书 CLI 能力交付。

**Architecture:** Go Core 拥有资源、版本、安装与共享配置，解析和程序适配位于 `internal/plugins`。执行接纳展开组件，继续使用现有 Skill、MCP、Runtime 和统一资源事件；React 组合专业资源模块，不另建服务器状态源。

**Tech Stack:** Go、GORM/SQLite、ginx/toolx、React、TanStack Query、共享 shadcn/Radix 组件、Vitest、官方飞书 CLI。

**Spec:** [插件资源与飞书插件设计 Spec](../specs/2026-10-16-plugin-resources-design.md)。

## 全局约束

- 沿用独立 worktree 和 `codex/plugin-resources` 分支，保留主工作区。
- 用户已批准进入开发；本计划写好后直接执行，不再插入计划确认。
- 不做历史数据兼容、转换或迁移；以新库目标结构验证。
- 中文文档、注释和产品文案；生成文件只经原生成命令更新。
- Agent Plugins 1.0.0 核心清单和组件位置固定；Claude 仅导入 Skill 与 MCP。
- 首次安装先准备、必要认证，再选择伙伴提交；共享配置和账号无伙伴覆盖。
- 伙伴固定版本；更新明确选择，配置与版本不兼容变更全有或全无。
- 本地内容、官方依赖固定版本与摘要；不在运行时拉 Git 或使用浮动 npx。
- 飞书 CLI v1.0.98，源码 ac81f5056ab88f37d581d25916f3dab85abfca76；官方 Skills 与程序同一发布。
- 按用户要求全部实现、独立 Review 并修复后再提交；任务结束记录账本，不提前提交半成品。
- 最终完整检查使用本机 Node 25 所需 `NODE_OPTIONS=--no-experimental-webstorage pnpm check`。
- Review 干净后提交、push 功能分支，向 main 创建 MR；不直接合并 main。

## Review Focus

1. 导入路径、符号链接及重名内容变化不得越出包根或覆盖被引用版本；由任务 1 的真实目录与 ZIP 用例覆盖。
2. 两个安装面板并发改变共享配置或版本时，失败不能留下半次升级；由任务 2 的真实 SQLite CAS/回滚覆盖。
3. 受限入口显式引用插件不能绕过 MCP 白名单和 CLI 权限；由任务 4 的执行接纳测试覆盖。
4. 授权完成但安装取消、Core 重启或授权过期时，界面不能假报已安装或丢失已有关系；由任务 3、6 覆盖。
5. 固定 CLI 与官方 Skills 在不同平台交付时，离线内容、执行位与来源必须相符；由任务 5 的锁定和资产测试覆盖，真实平台单列证据。

## 文件职责

| 范围     | 文件与职责                                                                                                                         |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 包与存储 | `server/internal/plugins/package.go`、`storage.go` 及测试：格式、组件、边界、摘要、受管暂存；复用 `internal/skills` 的真实归档能力 |
| 资源事实 | `server/repositories/models/plugin.go`、`controllers/plugin_controller.go` 及测试：插件、版本、安装、加密共享配置、条件更新        |
| 生命周期 | `server/apis/plugins/`：每个操作一个 Operator；`server/internal/plugins/feishu/`：官方 CLI 授权进程与共享 profile 生命周期         |
| 执行     | `server/services/execution/`、`internal/workinput/`、`internal/chat/`、`internal/runtimes/`：插件引用、展开、快照与受控环境        |
| 分发     | `scripts/plugin-resources.mjs`、锁定目录及 `server/internal/plugins/catalog/`：固定 Git 快照、生成目录、原生资产与来源             |
| 前端     | `desktop/src/modules/plugin/`、`app/pages/resources-page.tsx`：发现、详情、安装、配置、管理；现有技能和连接器模块提供专业入口      |
| 装配     | `server/global/config.go`、`server/apis/root.go`、统一事件注册和桌面打包脚本：真实入口与生命周期                                   |

### Task 1: 完整插件包解析与安全暂存

**Files:** 新增 `server/internal/plugins/package.go`、`storage.go`、`package_test.go`、`storage_test.go`；必要时在 `internal/skills/importer.go` 暴露真实共用归档能力；新增插件稳定错误与常量。

**Interfaces:** 产出 `Load(root string) (Package, error)`，包内含 Manifest、Skills、Servers、Diagnostics、Format；`Stage(parent, location string) (StagedPackage, error)` 与 `OpenStaged(parent, token string) (StagedPackage, error)` 绑定不可变摘要；暂存内容与包元数据分别管理。

- [x] 写失败用例：通用与 Claude 组件、双格式选择、可选组件损坏边界、未知扩展、相对路径和完整 Skill 引用；不依赖联网 Schema。
- [x] 写真实目录与 ZIP 用例：越界、大小写碰撞、特殊文件、坏链接、摘要变更和有界复制。
- [x] 在 `server` 运行 `go test ./internal/plugins/... ./internal/skills/...`，观察新增行为失败。
- [x] 实现清单校验、组件发现、诊断与安全暂存，保留上游原文件；复用归档、元数据和摘要机制。
- [x] `gofmt` 后运行同一测试集，预期全部通过；在账本记录实际结果。

### Task 2: 资源、版本、共享配置与伙伴安装

**Files:** 新增插件 Model、Controller 和真实 SQLite 契约测试；调整 `skill.go`、`mcp_connector.go` 及其 Controller、归属测试；装配 `global/config.go`。

**Interfaces:** 产出 `PluginController`，提供资源与版本 CRUD、分页查询、安装查询/启停/移除、配置 CAS 与安装版本 CAS；版本绑定资源，安装唯一键为伙伴与插件。组件继续使用任务 1 的包解析，不登记独立资源。

- [x] 写失败契约测试：全局未安装不混入伙伴、自有资源越权、相同内容幂等与不同摘要冲突、重复展示名、分页总数。
- [x] 写失败事务测试：共享配置与所选伙伴版本原子更新，未选旧版或停用版本不兼容时回滚，并发 revision 冲突保留原状态。
- [x] 运行 `go test ./repositories/controllers`，观察新行为失败。
- [x] 实现目标模型、中文字段注释、单表或有界批查及加密配置；直接建立新结构，不实现历史迁移。
- [x] 独立技能与连接器区分全局发现和伙伴安装；卸载只移除关系，自有删除与全局删除分别校验影响。
- [x] 运行 Controller、技能与 MCP API 测试，预期全部通过；账本记录结果。

### Task 3: 插件管理 API、共享认证与事件

**Files:** `server/apis/plugins/` 各 Operator、`internal/plugins/feishu/`、`global/config.go`、`apis/root.go`、统一事件注册；测试随职责放置。

**Interfaces:** API 路径族 `/plugins` 和 `/wakers/:wakerId/plugins`，覆盖目录、预检、资源准备、详情、版本、配置、认证会话和伙伴关系。API 消费任务 1 暂存及任务 2 Controller；认证适配返回真实状态、URL、期限和账号摘要，不返回 token。

- [x] 先写 API 失败测试：无认证不能安装、免认证可安装、归属拒绝、逐伙伴成功与失败、取消后保留资源且无新关系。
- [x] 先写 CLI 进程边界失败测试：重复发起复用、完成后验证身份、过期/取消退出、Core 生命周期退出、外部撤销与共享身份占用互斥。
- [x] 运行新 API 与认证测试，观察失败。
- [x] 实现 Operator 编排；文件与认证等待在事务外，配置和版本切换在同一事务；首次安装复验就绪条件。
- [x] 实现官方 CLI JSON 适配和明确生命周期，只在用户动作后发起认证；状态变更复用统一提交后事件，无页面永久轮询。
- [x] 运行相关 Go 测试并通过原命令生成 OpenAPI 与前端客户端；预期通过。

### Task 4: 执行展开、插件引用与历史快照

**Files:** `services/execution/service.go`、`work.go`、`intent.go`、相关测试；`internal/workinput`、`internal/chat/eventlog`、`internal/chat/messageview`、聊天 API；`internal/runtimes` 结构和环境装配。

**Interfaces:** 输入 `PluginIDs []string` 与既有 Skill/Connector 引用并列；接纳结果固定插件 ID、版本、配置修订、组件身份。接纳时读取任务 2 安装，使用任务 1 内容；向 Runtime 交付现有 Skill/Connector 与受控程序环境。

- [x] 写失败用例：自动按需装配、显式未安装/停用引用拒绝、同名同内容去重与冲突、稳定组件身份。
- [x] 写权限和两轮测试：MCP 白名单约束派生组件，受限入口不注入 CLI Skill/环境；无变更前缀稳定，续聊仅发新增输入。
- [x] 写快照测试：版本更新不改已接纳工作、分叉与重复提交保留完整插件意图、历史显示旧版本。
- [x] 运行执行、聊天与 Runtime 测试，观察新增行为失败。
- [x] 实现接纳展开、真实运行内容占用与释放、固定配置快照；配置变化沿用原生会话重新挂载规则。
- [x] 运行上述测试集，预期全部通过；账本记录结果。

### Task 5: 固定飞书真实插件与构建分发

**Files:** 独立插件源目录和 Git 快照锁定、`scripts/plugin-resources.mjs` 及测试、`internal/plugins/catalog/` 生成内容、桌面构建/打包入口、`THIRD_PARTY_NOTICES.md`。

**Interfaces:** `catalog.json` 收录条目与 `provenance.json`，CLI 平台资产包含固定 URL/SHA-256；Core 只消费准备完成的本地目录；通过 `LARKSUITE_CLI_CONFIG_DIR` 指向应用私有 profile。

- [x] 写构建脚本失败测试：摘要不符拒绝、无网络也能验证准备快照、只选择目标平台资产、固定来源缺项拒绝。
- [x] 运行脚本测试，观察失败。
- [x] 获取固定官方源码与发布资产；完整保留文档、多维表格、日程、任务 Skills 及引用依赖、许可证；不自行改写为演示技能。
- [x] 实现锁定快照准备、目录生成、原生程序交付与可重复检查；不把大型二进制写入 Core JSON。
- [x] 按新生产交付内容更新第三方声明；验证程序帮助、版本及受控配置目录，不读取或覆盖用户外部 CLI profile。
- [x] 运行脚本和 Go 目录测试，验证本机资产；其他平台证据单列。

### Task 6: 统一资源页、安装流程与伙伴管理

**Files:** `desktop/src/modules/plugin/{api,components,pages,model}`；`app/pages/resources-page.tsx`、路由/Scene/导航、现有技能/连接器页面、中文 i18n、统一事件适配、聊天能力选择与引用组件及测试。

**Interfaces:** 消费任务 3 生成 Client/Query；统一资源入口内插件默认、技能和连接器专业入口；聊天提交 `pluginIds` 并呈现任务 4 的消息快照。

- [x] 写用户行为失败测试：三个分类清晰可达、插件库/我的插件、准确空状态与 URL 恢复；全局资源和伙伴安装不混淆。
- [x] 写安装失败测试：准备→认证→伙伴选择，免认证跳过、共享认证复用、取消后继续、已有安装后认证过期、逐伙伴失败重试。
- [x] 写伙伴管理和引用失败测试：真实版本/启停/卸载、更新选择不扩大范围、显式插件引用可提交并在历史呈现。
- [x] 运行目标 Vitest 测试，观察新增行为失败。
- [x] 复用共享资源页、卡片、页签、Dialog、表格与选择组件实现 Spec 第 8 节；不新增永久轮询或第二套服务器 Store。
- [x] 运行目标测试及前端 typecheck、UI 规范检查；预期通过。

### Task 7: 完整验收、独立 Review 与远端 MR

**Files:** 当前架构/指南/领域词汇、验收记录、全部实现文件；生成物仍由对应命令生成。

**Interfaces:** 根据 Spec 第 12 节逐项记录自动化、真实飞书、平台与视觉证据，未运行的真实外部验收保持明确状态；Review 范围包含所有已暂存实现与相对 main 的分支差异。

- [x] 同步当前能力文档和入口；核对来源、共享状态、生命周期、文件职责与包依赖。
- [x] 对修改 Go 文件 `gofmt -w`，运行完整 `NODE_OPTIONS=--no-experimental-webstorage pnpm check`，预期全部通过；新问题才扩大检查。
- [x] 完成可执行的隔离新库/真实 CLI 验收；需要用户扫码或界面授权时，给出已完成的具体可验收结果后请求所缺输入，独立工作继续。
- [x] 汇总 Spec 验收矩阵和实际证据，不用静态或 Fake 测试冒充真实飞书业务调用。
- [x] 派独立新上下文子代理进行全范围代码 Review，提供 Spec、计划、完整 diff、测试证据及账本决定。
- [x] 对确认的重要问题先补复现失败测试，再修复并验证完整套件；处理后按用户要求提交功能分支。
- [x] push 到 origin，创建目标为 main 的 MR，正文包含最终行为、验证与真实验收边界；附加 MR 到当前任务。

## 自检

Spec 的包、安装、配置、执行、引用、UI、来源与分发要求分别由任务 1–6 覆盖；任务 7 负责整体边界和独立审阅。新库初始化由任务 2 的真实 SQLite 测试证明，不包含迁移任务。跨任务沿用 Package、PluginController、PluginIDs 与同一生成协议，不新增平行资源事实。

代码与自动化验收按本计划交付。需要用户明确同意的真实界面和飞书业务验收保持待验，具体边界见 [验收记录](../specs/2026-10-17-plugin-resources-acceptance.md)。

交付分支已推送；[MR #2](https://github.com/JieWaZi/NeuxBot/pull/2) 目标为 main，以草稿保留真实外部验收门槛。独立 worktree 保留用于继续验收和处理反馈。
