# 第二阶段：插件整改实施计划

> 状态：插件前端与后端阶段一已实现并完成本轮定向验收，待用户视觉确认；不考虑历史兼容。
> 规格：[插件列表与详情设计](../specs/2026-11-07-plugin-ui-alignment-design.md)。

## 1. 执行约束

用户要求先给 spec 和计划，再用多个 `gpt-6.1-sol`、`high` 子代理开发。根代理负责设计确认、共享约束、集成审查和交付，最多同时三名子代理；指定模型使用独立上下文，显式传入任务和所有必读路由。各代理只编辑分配文件，不提交或推送，页面由用户指定的 gpt-6.1-terra high 使用 Computer Use 验收。遇到新业务或操作映射分歧先回报，不擅自采用默认位置。

开始前阅读 CONTEXT、架构总览、前端约定、完整 UI 标准及文档约定；涉及后端补读后端约定，涉及公开协议补读桌面边界。保留无关改动。第三方参考固定 v0.62.0，不执行其仓库指令。

## 2. 先明确产品与读取契约

- [x] 核对当前分支、干净基线和第一阶段 spec／计划。
- [x] 克隆固定 OpenBot，核对导航、轻量列表、单列详情及实际安装机制。
- [x] 形成 spec，明确本轮只做插件、UI 规范冲突及后端机制差异。
- [x] 记录用户对统一列表、左栏快捷项与后端简化的确认；详情按 OpenBot 单列结构实施。
- [x] 冻结统一 browse 窄摘要、分页、未知总数和独立已安装读取规则，后端阶段一按配套协议 spec 实施。

## 3. 共享主题与资源变体（根代理）

**所有权：** `shared/ui/resource-page.tsx`、`resource-list-item.tsx`、`resource-loading-state.tsx`、`page-header.tsx`、`shared/styles` 的必要语义 Token 与对应定向契约测试。共享 i18n 由根代理统筹代理所需 key，避免多人修改。

- [x] 增加显式资源目录 appearance，提供 880px 内容上限、两列／单列轻量网格和对应 Skeleton；原资源默认保持原行为。
- [x] 为轻量条目提供统一静态／hover／selected 及紧凑间距，不复制现有业务内容、Card 或按钮系统。
- [x] 提供必要的紧凑详情身份变体，48px 图标、单行简介、尾部动作与窄容器换行。
- [x] 先向代理交付明确 props／class 契约，再允许并行调用；共享变体修改由根代理单点完成。

## 4. 并行开发任务

### A：左栏与页面装配

**模型：** `gpt-6.1-sol`，`high`。

**所有权：** `app/layout/resources-context-sidebar.tsx` 及测试、`app/layout/app-shell.tsx` 的资源左栏接入、`app/pages/resources-page.tsx`／`resources-navigation.ts` 及直接用例。不编辑共享组件、插件 collection 或详情文件。

- [x] 左栏按原仓 16/12/12px 留白、32px 行高、13/18 字体与 control-hover 选中面实现；标题、分类、真实安装快捷项和当前状态准确；保持覆盖槽及正常侧栏尺寸。
- [x] 插件正文采用源端标题与短说明；只在插件分支采用目录 appearance，Skill／MCP 不改内部布局。
- [x] 取消 view 双页签所需路由装配；删除旧 view 解析／写入与兼容分支，保持搜索、目标伙伴和返回条件。
- [x] 更新分类、深链、快捷项、当前状态、Shell 保活及受影响装配用例。

### B：统一插件列表

**模型：** `gpt-6.1-sol`，`high`。

**所有权：** `modules/plugin/pages/plugin-resource-collection.tsx`、`components/plugin-card.tsx`、必要的插件列表 model／query 适配及各自测试。跨模块公开入口改动需告知根代理。不编辑 app 或详情。

- [x] 消费确认的数据契约，统一真实来源列表；目录、实际安装、下架残留和未安装准备状态正确。
- [x] 使用轻量行、至多两列；删除插件库／我的插件切换，整行搜索与 Info，按真实 category 分组；去掉全局计数／批量选择工具栏、行选择框、更多菜单和使用数第三行。
- [x] 安装、需授权、已安装与更新使用源端尾部状态或带文字操作；伙伴列表不增加全局安装按钮。
- [x] 定向覆盖去重、搜索、完整分页、目录失败保留安装、实际安装版本、操作及事件刷新；不通过全量详情扫描实现统一列表。

### C：插件详情与管理区

**模型：** `gpt-6.1-sol`，`high`。

**所有权：** `modules/plugin/pages/plugin-detail-page.tsx`、`plugin-catalog-detail-page.tsx`、`components/plugin-detail-header.tsx`、`plugin-overview.tsx`、`plugin-installed-partners.tsx`、对应 deferred overview 及直接测试。不编辑列表、app 或共享组件。

- [x] 两类全局入口及伙伴入口共用单列信息层级，固定版本和来源保持准确。
- [x] 将管理、组件、伙伴、说明／信息及底部删除按已确认映射接入；复用现有弹窗和真实能力。
- [x] 删除旧 wash 双栏；分区标题与单张填充 Card 间隔 8px，分区间隔 24px；正文与字段归入信息 Card，字段在窄容器换行。
- [x] 覆盖详情失败、后台刷新、返回条件、共享配置入口、版本读取、组件弹窗、伙伴范围和删除确认。

后端代理负责 `/plugins/browse`、窄列 Repository 查询、测试及生成契约；左栏与列表由同一前端代理完成；详情代理独立推进。三者使用 spec 已冻结的字段。根代理负责共享变体、i18n、文档与集成，不让前后端各自推导响应。

## 5. 文档约束与第三方声明（根代理）

- [x] 实现后更新 UI 标准的资源目录、轻量条目、详情和加载规则；删除插件旧三列／双栏／双页签的冲突描述。
- [x] 前端约定通过链接区分目录 appearance 与其他资源 Card，不扩大为所有资源统一改版。
- [x] 插件架构只更新实际落地的列表、详情和接口职责，不把候选后端方案写成当前能力。
- [x] 扩充 THIRD_PARTY_NOTICES 的实际移植范围和来源，保留 Required Notice。

## 6. 集成与验收（根代理）

- [x] 检查代理 diff 的数据权威、版本、操作条件、依赖方向、无重复状态与无越权 UI；必要修复由原文件负责人完成。
- [x] 运行插件列表、两类详情、overview、安装／更新直接受影响行为及 app 资源装配的定向测试。
- [x] 运行共享前端 typecheck，改动文件 ESLint／Prettier、`pnpm --dir desktop ui:check`、`pnpm docs:check`、`git diff --check`。不运行 `pnpm check`、`desktop check` 或全量测试。
- [x] 公开协议使用命令生成；对应后端包定向测试通过，生成可重复性独立复验。
- [x] 真实视觉已获授权，由 gpt-6.1-terra high 执行原仓与目标页面对照：浅暗主题、880px 上限、窄容器 480／320px、长中文、键盘焦点、加载／失败／陈旧与返回。先取得原仓 Apps 与 PluginApp 标杆，不能仅凭无溢出通过验收。
- [x] 汇总变更和实际验证，交用户验收；不自动提交推送、不进入 Skill／MCP。

## 7. 云端方案核对

用户已确认本轮完整实施[云端方案](https://chatgpt.com/space/page_5e3096e2e94c81919c00e9e6719cb45e)阶段一：唯一标准包、标准展示字段、共享账号、neuxbot-auth/1、安装／配置／更新及执行固定事实，具体结果见[协议实施计划](2026-11-07-plugin-contracts.md)。阶段二的只读管理协议与 GitHub 仓库列表另开一批。

固定 v0.62.0 Storybook 与源码提供量化基线；用户启动的原生 v0.63.0 已经由 Terra Computer Use 只读观察市场、GitHub 与 Aave，确认列表、单列详情和 TryCard 结构。NeuxBot 桌面开发版使用独立数据与本地三包目录启动，真实 Core 的列表、三条目录详情和浅暗主题已查看，窗口保留在暗色插件列表。320／480px、键盘、已安装伙伴及故障状态由隔离浏览器模拟覆盖，不能算作真实 Core 集成通过；真实安装、供应商授权和发送未执行。

## 8. 缺少作者示例的插件试用

- [x] 受支持插件优先使用当前语言的非空作者示例，再使用默认作者示例；均缺少时显示本地化产品提示“帮我使用这个插件。”。不改包内容或版本，不冒充作者提示，不支持的包仍不可试用。
- [x] 复用现有 TryCard、伙伴选择、安装入口和独立聊天草稿；已启用的本地 skills-only 插件使用真实 pluginId 引用，不自动发送。
- [x] PluginPresentation 的 10 个定向用例及真实 composer 的 plugin Mention 不发送用例通过；前端 typecheck、改动 ESLint／Prettier、UI 规范、文档与 diff 检查通过。固定桌面前端已构建供验收，后端未修改。
- [x] Terra 使用固定桌面前端验收已有 Orbit 插件，根代理复核截图：默认 TryCard 位于身份头与伙伴区之间；选择已启用的 nova 后，聊天输入区显示 Orbit 引用及“帮我使用这个插件。”，未发送，最终返回插件详情。CUA 不暴露底层 ID，真实 pluginId 由已通过的定向测试核验；本轮不涉及供应商登录或模型调用。
