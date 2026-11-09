# 设置集成 UIUX 实施计划

> **For agentic workers:** Use superpowers:subagent-driven-development task-by-task. 用户已要求按照设计开发，直接实施，不另设审批步骤。

**Goal:** 将已实现的集成导航及三级页，完整落实到获准的 OpenBot 设置 UI/UX 设计。

**Spec:** [设置与集成 UIUX 设计](../specs/2026-11-09-settings-integration-uiux-design.md)。该设计是本轮实施权威，早期迁移计划不覆盖它。

**Architecture:** 复用现有 Query、事件、平台认证与领域表单；设置模块拥有共享表面、行、危险区、footer 和草稿导航保护，app 只负责跨域路由及面包屑装配。只修改集成及直接受影响共享角色，保留其他并行修改。

**Tech Stack:** React、TypeScript、React Router data router、TanStack Query、现有语义 Token 与 UI。

## Global Constraints

- 总览 IM/API/MCP 三块同时显示，无 Tab；IM 三平台→二级平台→三级连接。旧深链与三级 breadcrumb、Scene 归属保留。
- 二级 IM 平台、API/MCP 服务标题恢复共享悬停／键盘返回总览；三级无 PageHeader、品牌身份头、重复标题或悬停返回，状态在连接配置 Card 只出现一次。
- 统一白色语义 Card、64px 起行、内缩16px单分隔；空态放 Card 内，无空表头/零计数；危险区为完整影响说明+右侧危险动作+原依赖确认。
- 编辑在原 Card、当前作用域内；保存/取消留原三级。配置与授权不可伪装跨请求原子提交；后台刷新不丢草稿，凭据与登录生命周期保留。
- 保存条归 Settings Scene footer，与正文滚动区为兄弟；所有普通离开路径统一保护dirty草稿，data router 已由 app/router.tsx createHashRouter 提供。
- API 服务查看/编辑、密钥 Card 行及原服务端10/30/50分页，MCP服务/客户端配置两组、API依赖操作在所属Card。
- 不新增后端协议、不操作运行中UI、不提交或推送；仅定向测试、类型、按文件lint/格式、UI策略与文档检查。

## Review Focus

- 真实删除依赖与revision/digest未弱化。
- 页面离开/取消的未确认登录仅取消所属任务，StrictMode不重复启动。
- dirty导航/浏览器后退/取消继续编辑保留同一草稿。
- 服务失败、未知状态与空数据不混淆；请求成功后读回失败不伪装写失败。
- 共用字段组影响的AI账号表单仍可正常编辑；不得覆盖其他正在进行的智能体修改。

### Task 1: 共享设置宿主与 IM 页完整落实

**Files:** modules/settings 的共享Card/Row/FieldGroup/危险区/footer/编辑保护与SettingsScene；modules/im 页面、只读授权、记录列表、配置/绑定表单；必要app面包屑装配和i18n；对应定向测试。

**Produces:** 在 modules/settings 公开入口导出统一 SettingsCard、SettingsDangerZone，以及 SettingsPageFooter 和同一编辑保护入口。实现者可选具体props，但须写入报告供Task2消费。

- [x] 为三级无身份头/悬停返回、唯一状态、完整危险区、空态Card无表头、原Card内编辑与footer/dirty离开行为写必要失败测试并确认red。
- [x] 实现共享设置表面、行与宿主footer，复用已有资源footer的portal机制；data router草稿保护不改全局路由结构。
- [x] IM三级以配置、授权、记录、危险四分区装配；对应授权项页内配置，保留真实数据、现有客户端10条分页与打开行为。
- [x] 连接普通编辑与绑定编辑的作用域清楚，保留原顺序及部分失败语义、扫码/手工/验证码与准确取消。二级悬停返回总览并清理重复分隔。
- [x] 定向验证IM、SettingsScene/共享角色、相关账号与app导航；按文件lint/格式；写精确修改清单/接口/验证报告，独立审查后继续。

### Task 2: API / MCP 按同一体系落实

**Files:** api-service-settings、api-integration-section、mcp-integration-section、integrations-section及对应测试、i18n。

**Consumes:** Task1共享Card、FieldRow、DangerZone、footer及编辑保护，不另造样式。

- [x] 先写API配置Card查看/原位编辑/footer、密钥Card行与分页、MCP两分区/依赖同Card等失败用例。
- [x] 服务配置草稿整体保存；复制仅用已保存地址。密钥保留CRUD、前缀/日期/撤销状态、原10/30/50服务端分页与一次性秘密流程。
- [x] MCP保留独立开关、真实依赖、错误/重试/复制；客户端配置独立Card，移除嵌套灰组。
- [x] 定向测试/按文件lint和格式，写精确报告，由独立审查核对设计及保留行为。

### Task 3: 集成审查与完成核验

- [x] 同步UI规范到当前真实层级、footer/危险区与设计文档的实施状态，不写未做的视觉验收。
- [x] 运行直接受影响测试集合、前端typecheck、按文件lint/格式、UI策略、docs:check与diff空白检查。
- [x] 独立审查整轮精确差异，修复重要问题并做范围重审；报告本地改动和未做视觉验收，不自动提交/推送。
