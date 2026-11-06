# OpenBot 界面分阶段对齐执行计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 直接采用 OpenBot 的界面设计，先迁移完整框架与面包屑，再逐模块对齐页面及交互细节。

**Architecture:** 保留现有 React 路由、AppScene 与 workspace 所有权；在 app 层集中解析模块归属并装配图标栏、侧栏和顶栏。共享组件承载展示和尺寸，领域模块继续持有查询、业务操作和正文；面包屑通过所属 Scene 的展示接口发布，不复制服务器事实。

**Tech Stack:** TypeScript、React、React Router Hash Router、TanStack Query、Tailwind CSS 4、Radix、Vitest、React Testing Library；沿用现有 pnpm 工具链。

**Spec:** [OpenBot 界面分阶段对齐设计](../specs/2026-11-06-openbot-ui-alignment-design.md)。

## Global Constraints

- 依赖方向保持 `app -> modules -> shared`，不增加第二套组件库、状态库或样式体系。
- 服务器事实继续由 TanStack Query 管理，导航和面包屑不得复制领域数据、另开查询通道或建立第二套业务状态。
- 保留现有业务接口、事件连接、执行生命周期、资源授权与文件、终端、原生视图身份。
- 全部颜色、圆角、尺寸与焦点通过语义 Token 和共享组件表达；普通内容分组无常驻边框或阴影。
- 图标操作提供至少 36×36px 命中区及可访问名称；键盘使用语义 `focus-visible`，鼠标点击不得产生布局容器整块外框。
- 默认只运行对应修改点及直接受影响行为的检查；未经明确要求不运行全量验证。
- 本轮文档编写不实现产品代码，不迁移原工作区的未提交修改。

最后一项约束限定本次文档交付；后续获准实施时执行下面的产品任务。本计划不把尚未执行的任务记为已完成。

## Review Focus

1. 直接进入资源、团队或工作流运行深链时，模块选中与面包屑必须一致，不能依赖曾经点击过侧栏；由任务 1、4 验证。
2. 从带筛选列表进入多层详情再返回时，祖先目标和 URL 条件保持正确，不能用历史栈猜父级；由任务 4、7 验证。
3. 隐藏 Scene 的查询迟到、重命名和卸载不能覆盖前台顶栏或触发阅读；由任务 4、5 验证。
4. 重排框架或折叠侧栏时，聊天草稿、未保存缓冲、终端与嵌入视图身份不能重建；由任务 5 验证。
5. 320px 主内容、长中文和不同宿主 inset 下，导航、面包屑与右侧操作不能互相覆盖；由任务 2、3、6 的行为及真实窗口检查验证。

---

## 工作区与执行方式

独立 worktree 名称为 `openbot-ui-alignment`，分支为 `codex/openbot-ui-alignment`，从规格所列提交建立。路径由宿主返回，不把个人绝对路径写入仓库文档。后续执行先核对 `git status --short` 与 `git branch --show-current`，保留他人修改；需要整合原分支后续提交时按明确提交整合，不覆盖复制其工作目录。

当前只交付规格与计划。实施顺序为任务 1–6 完成阶段一，任务 7–8 完成阶段二，任务 9 完成阶段三，任务 10 完成阶段四，任务 11 按模块完成阶段五。各阶段有独立提交与验收记录，不并行改动共享壳层。

实施前使用仓库声明的 pnpm 版本；仅在缺少依赖时运行 `pnpm install --frozen-lockfile`。文档交付无需安装前端依赖或运行产品测试。

## 文件与接口分工

以下新增名称是本计划的实施选择。所有业务路径以当前 [路由装配](../../../packages/frontend/src/app/scenes/app-route-scene.tsx) 为准。

| 文件                                                             | 职责                                                              |
| ---------------------------------------------------------------- | ----------------------------------------------------------------- |
| 新增 `app/layout/app-navigation-model.ts`                        | 纯模块／侧栏归属解析，静态父级与导航目标；无 Query 或独立选中状态 |
| 新增 `app/layout/app-module-rail.tsx`                            | 固定模块图标栏、更多菜单及唯一通知槽位                            |
| 新增 `app/layout/app-titlebar.tsx`                               | 宿主 inset、历史、面包屑、打开内容菜单、场景操作的排列            |
| 新增 `app/layout/resources-context-sidebar.tsx`                  | 迁移现有资源分类，只读 URL，不持有资源副本                        |
| 新增 `shared/ui/breadcrumbs.tsx`                                 | 纯展示的层级导航与溢出、焦点行为                                  |
| 新增 `shared/ui/scene-breadcrumbs.tsx`                           | 按 owner 发布展示项，由 app 选择活跃 owner                        |
| 修改 `app/layout/app-shell.tsx`、`partner-navigation-layout.tsx` | 装配新壳层；保留原 Scene、Query 与内容所有者                      |
| 修改 `app/layout/entity-navigation.tsx`                          | 只保留伙伴／团队上下文，移出全局模块入口                          |
| 修改 `shared/ui/scene-chrome-portal.tsx`、`scene-tab-strip.tsx`  | 顶栏槽位与 pane 页签接入；不建立新打开内容状态                    |
| 修改 `shared/styles/index.css`、`theme.css`                      | 统一尺寸、表面与语义样式，不在页面散写值                          |
| 修改资源、工作流、设置、其他模块                                 | 按后续各任务迁移内部页面，沿用模块公开接口                        |

文档中的文件路径均相对于 `packages/frontend/src/`，除非已写出仓库根目录路径。新增接口如下，实施时使用明确的 readonly 类型，不导入模块内部 DTO 到 shared。

```ts
type AppModuleId =
  | "partners"
  | "workflows"
  | "work-board"
  | "usage"
  | "resources"
  | "knowledge"
  | "automatic-tasks"
  | "settings";
type AppSidebarKind =
  "partners" | "resources" | "knowledge" | "settings" | "none";
type AppNavigationRoute = {
  moduleId: AppModuleId;
  sidebarKind: AppSidebarKind;
  parentHref: string;
};
function resolveAppNavigation(location: {
  pathname: string;
  search: string;
}): AppNavigationRoute;

type BreadcrumbItem = { id: string; label: string; href?: string };
type BreadcrumbsProps = { items: readonly BreadcrumbItem[] };
type SceneBreadcrumbsProps = { items: readonly BreadcrumbItem[] };
type SceneBreadcrumbsProviderProps = {
  ownerId: string;
  publish: (ownerId: string, items: readonly BreadcrumbItem[] | null) => void;
  children: React.ReactNode;
};
```

`Breadcrumbs` 只渲染；`SceneBreadcrumbsProvider` 按 route owner 提供发布范围，`SceneBreadcrumbs` 发布当前页面已读取的名称。app 选择活跃 owner 的展示项，无发布项时使用该路由的稳定类型占位。组件名称与 props 在任务 4 实现。模块匹配的优先级先判伙伴／团队作用域，再判全局资源与工作流分类，避免错误归属。

## 阶段一：整体框架

### 任务 1：集中模块归属与入口目标

**Files:**

- Create: `app/layout/app-navigation-model.ts`、`app-navigation-model.test.ts`。
- Modify: `app/scenes/app-route-scene-model.ts`、`app/layout/app-shell.tsx`。
- Test: `app/scenes/app-scene-model.test.ts`、`app/layout/app-shell.test.tsx`。

**Interfaces:** 消费现有 URL、最近聊天／团队与设置目标；产出上文 `resolveAppNavigation`，既有 `routeSceneForLocation` 复用其模块判断，保留 Scene ID 与挂载身份。

- [ ] **1.1 写行为用例。** 表驱动覆盖 `/wakers/w1/projects/p1` 归伙伴、`/teams/t1/threads/c1` 归伙伴团队模式、`/resources/plugins/p1/skills/s1` 归资源库、`/resources?category=workflows&search=demo` 与 `/workflows/f1/runs/r1` 归工作流、设置旧地址归对应有效分区；未知路径遵守既有路由回退。
- [ ] **1.2 运行新增用例并确认缺少解析函数导致失败。** `pnpm --dir packages/frontend exec vitest run src/app/layout/app-navigation-model.test.ts`。
- [ ] **1.3 实现解析与目标恢复。** 使用纯函数派生模块和侧栏；主入口目标按 spec 4.2，首次／已删除目标回实际列表，旧入口保留参数。不能新增全局 `selectedModule` Store。
- [ ] **1.4 定向验证。** 运行上述用例及 `src/app/scenes/app-scene-model.test.ts`、`src/app/layout/app-shell.test.tsx`；预期全部通过，临时聊天接纳后的 `mountKey` 沿用。
- [ ] **1.5 提交。** 仅暂存本任务文件，提交 `refactor: centralize app navigation ownership`。

### 任务 2：固定模块栏与模块侧栏装配

**Files:**

- Create: `app/layout/app-module-rail.tsx`、`app-module-rail.test.tsx`、`resources-context-sidebar.tsx`、`resources-context-sidebar.test.tsx`。
- Modify: `app/layout/partner-navigation-layout.tsx`、`entity-navigation.tsx`、`partner-navigation-preferences.ts`、`app-shell.tsx`、`app/pages/resources-page.tsx`、`shared/styles/index.css`、`shared/styles/theme.css`、`shared/i18n/index.ts`。
- Test: `app/layout/partner-navigation-layout.test.tsx`、`entity-navigation.test.tsx`、`partner-navigation-preferences.test.ts`、`app/pages/resources-page.test.tsx`。

**Interfaces:** 消费任务 1 的 `AppNavigationRoute`；`AppModuleRail` 接收 `activeModule`、`onNavigate(moduleId)`、`notification` 槽位。`ResourcesContextSidebar` 接收当前 `category` 与 `onCategoryChange`，复用现有分类解析及参数清理规则；产出始终可用的模块栏和按模块装配的侧栏。

- [ ] **2.1 写交互用例。** 模块栏不显示伙伴头像列表；伙伴／设置／资源之间切换只更换侧栏；折叠侧栏仍能访问全部模块；更多菜单有知识库与自动任务；通知仅一个；分类切换 URL 与原逻辑一致且没有重复页面分段导航。补旧偏好缺字段／无效宽度／伙伴顺序保留用例。
- [ ] **2.2 定向运行新增组件用例，确认新框架缺失时失败。** 使用 `pnpm --dir packages/frontend exec vitest run` 后接本任务新增的两个 `.test.tsx` 文件路径。
- [ ] **2.3 迁移容器与入口。** 全局栏宽 52px；侧栏默认 280px、范围 216–360px；全局栏与侧栏折叠解耦。把实体列表和设置导航接入侧栏槽位，资源分类从页面移到侧栏；此步不更改资源条目、设置字段或工作流画布。
- [ ] **2.4 定向验证。** 运行本任务全部列出测试；通过后执行 `pnpm --dir desktop ui:check`。检查长中文、键盘可达性及焦点恢复；真实容器布局留到任务 6 检查。
- [ ] **2.5 提交。** `refactor: adopt OpenBot module rail and context sidebar`。

### 任务 3：统一顶栏与打开内容入口

**Files:**

- Create: `app/layout/app-titlebar.tsx`、`app-titlebar.test.tsx`。
- Modify: `app/layout/app-shell.tsx`、`partner-navigation-layout.tsx`、`shared/ui/scene-chrome-portal.tsx`、`scene-tab-strip.tsx`、`shared/styles/index.css`。
- Test: `shared/ui/scene-tab-strip.test.tsx`、`app/scenes/app-scene-bulk-close.test.tsx`。

**Interfaces:** 消费现有 `AppSceneSnapshot`、`AppSceneCommand`、场景操作及平台 `chrome.navigationInset`；`AppTitlebar` 以 props 接收 `breadcrumbs`、`sceneActions`、`openContents` 展示槽位和历史操作。打开内容菜单直接调用原 Scene 命令，不创建第二份 tabs 状态。

- [ ] **3.1 写用户行为用例。** 顶栏可切换、关闭、重命名及关闭其他已打开内容；选择文件仍选中其所属模块；关闭 owner 沿用已有联动行为；历史按钮在无可用历史时禁用。覆盖 72px Native inset 与 Web 默认 inset 的槽位配置、长标题完整可访问名称。
- [ ] **3.2 运行新增顶栏与原批量关闭测试，确认缺少新入口导致失败。**
- [ ] **3.3 实现顶栏排列。** 顶栏高度 40px；应用身份、历史、面包屑、操作按 spec 4.1／4.3 排列。全应用平铺 tabs 替换为打开内容菜单，pane tabs 继续留在所属 pane；交互区域标记既有 no-drag 约定，不更改平台 Bridge 契约。
- [ ] **3.4 定向验证。** `pnpm --dir packages/frontend exec vitest run src/app/layout/app-titlebar.test.tsx src/shared/ui/scene-tab-strip.test.tsx src/app/scenes/app-scene-bulk-close.test.tsx`；预期原关闭与挂载契约仍通过。原生遮挡和拖动实际结果在任务 6 验收。
- [ ] **3.5 提交。** `refactor: align app titlebar and retained content navigation`。

### 任务 4：完整面包屑与父级返回

**Files:**

- Create: `shared/ui/breadcrumbs.tsx`、`breadcrumbs.test.tsx`、`scene-breadcrumbs.tsx`、`scene-breadcrumbs.test.tsx`、`app/layout/app-breadcrumbs.test.tsx`。
- Modify: `app/layout/app-shell.tsx`、`app-titlebar.tsx`、`app-navigation-model.ts`、`app/scenes/app-route-scene.tsx`、`shared/i18n/index.ts`。
- Modify: 现有 `modules/waker/pages/waker-detail-page.tsx`、`modules/project/pages/project-detail-page.tsx`、`app/pages/team-thread-page.tsx`、`modules/skill/pages/skill-detail-page.tsx`、`modules/connector/pages/connector-detail-page.tsx`、`modules/plugin/pages/plugin-detail-page.tsx`、`plugin-catalog-detail-page.tsx`、`plugin-skill-detail-page.tsx`、`plugin-connector-detail-page.tsx`、`modules/workflow/pages/workflow-detail-page.tsx`、`modules/settings/pages/settings-scene.tsx`、`app/scenes/knowledge-scene.tsx`、`modules/automatic-task/pages/automatic-task-detail-page.tsx`。

**Interfaces:** 实现上文 `BreadcrumbItem`、`Breadcrumbs`、`SceneBreadcrumbsProvider`、`SceneBreadcrumbs`。app 按 owner 保存展示项，稳定静态父级由当前 URL 推导；页面发布从既有 Query 得到的动态名称。列表来源使用已有 `resourceBack` 与 URL，校验仍属于对应父列表后恢复，直达时用规范父路径。

- [ ] **4.1 写层级与返回用例。** 覆盖 spec 4.3 全部路由家族；重点断言插件内技能有四层、伙伴资源不跳全局资源库、工作流运行父级回工作流。当前项不可点击且具有 `aria-current="page"`，祖先用明确 href；父列表 `search/source/view` 及分页恢复，详情直达无需历史栈。
- [ ] **4.2 写异步归属用例。** A 详情请求未完成时切 B，A 后返回不得覆盖 B；后台 Scene 重命名不改前台；卸载一个 owner 不清除另一个；实体删除或名称请求失败时父级仍可用。长中文保留完整访问名称并允许从溢出位置操作祖先。
- [ ] **4.3 运行新增三个测试文件，确认展示接口缺失时失败。**
- [ ] **4.4 实现并接入现有页面。** 每个 route Scene 提供 owner scope；详情只增加展示发布，不改正文。未发布时立即使用新路由的类型占位，已加载时用真实名称。设置及知识库保持自己的既有导航层级；打开文件时主面包屑继续来自 owner route。
- [ ] **4.5 定向验证。** 运行新增三个面包屑测试、`src/app/layout/app-shell.test.tsx`、`src/app/scenes/settings-route.test.ts` 与 `src/app/notifications/notification-navigation.test.ts`；预期所有直达、父级及迟到更新断言通过。
- [ ] **4.6 提交。** `feat: add scene-owned breadcrumbs and parent navigation`。

### 任务 5：保活、历史入口与关闭资源回归

**Files:**

- Modify: `app/layout/partner-navigation-layout.tsx`、`app-shell.tsx`、`shared/workspace/floating-chat-launcher.tsx` 的实际调用点；仅在无消费者后删除旧浮动导航分支。
- Test: `app/layout/app-shell.test.tsx`、`partner-navigation-layout.test.tsx`、`modules/chat/pages/retained-chat-page.test.tsx`、`modules/chat/workbench/workbench-store.lifecycle.test.tsx`、`shared/workspace/workspace-context.test.tsx`、`app/scenes/app-scene-bulk-close.test.tsx`。

**Interfaces:** 复用现有 Scene reducer、`mountKey` 与 workspace 关闭函数；历史控件迁移位置但保留原查询和分组。消费任务 3 的打开内容入口，产出无旧全局悬浮导航且内容身份不变的框架。

- [ ] **5.1 增加真实生命周期断言。** 聊天填写未发送草稿、打开未保存文件和终端后，切设置、资源、工作流再回来，内容 ID 与挂载计数不变；折叠／调整侧栏不触发关闭。真正关闭时只回收对应资源；隐藏页不提交阅读。
- [ ] **5.2 将用例作用于当前迁移结果。** 若发现失败，记录具体被重建／错误关闭的身份；缺少回归保护的行为先写失败用例再修实现，不能用 Snapshot 替代生命周期断言。
- [ ] **5.3 移除已被替代的全局浮动导航。** 历史、分组及新建继续能从对应侧栏／顶栏访问；检查 `floating-chat-launcher` 其他消费者，局部浮动聊天能力不因删除导航而被连带移除。
- [ ] **5.4 定向运行本任务测试。** 预期保活、关闭及后台阅读行为均通过；不扩展到整个聊天或工作流测试套件。
- [ ] **5.5 提交。** `refactor: retain workspace identity across module navigation`。

### 任务 6：阶段一真实界面对照与规范同步

**Files:**

- Create: `docs/discussions/openbot-ui-alignment-acceptance-cn.md`，仅记录已执行结果及未验证边界，不保存私人截图。
- Modify: `docs/development/ui-cn.md`、`docs/development/frontend-cn.md` 的框架与导航章节；不提前改资源列表或设置内页规则。
- Modify: 仅修复前述框架任务中已定位的差异文件。

**Interfaces:** 消费任务 1–5 的完整框架和 spec 6 验收矩阵；产出阶段一可审核的对照记录。

- [ ] **6.1 冻结对照。** 通过已授权的 Computer Use 记录 OpenBot 版本、逻辑窗口／缩放和目标状态；用虚构数据准备 NeuxBot 测试工作区。验收记录按版本、窗口和状态描述，避免写个人账号或路径。
- [ ] **6.2 执行框架检查。** 逐项核对图标栏、侧栏、顶栏、面包屑、历史、内容菜单、拖动区及 pane。按 spec 尺寸、长中文、浅暗主题、键盘和减少动态验收；macOS 和 Web 分别操作。没有 Windows 环境时明确记未验证。
- [ ] **6.3 修复已定位差异并同步权威规范。** 只更新已经实现的框架规则，保留可访问性、焦点、事件和状态边界；不得为通过检查放宽策略。
- [ ] **6.4 执行阶段检查。** `pnpm --dir packages/frontend typecheck`、`pnpm --dir desktop ui:check`、对修改 TS/TSX 文件定向运行 ESLint、对修改文件运行 Prettier 检查、`pnpm docs:check`。类型检查范围是共享前端包，不运行桌面总 `check` 或全量测试。
- [ ] **6.5 提交与阶段交付。** `docs: record OpenBot frame alignment acceptance`；交付中分别说明自动化、Web、macOS、Windows 和视觉结果。框架验收后才进入阶段二。

## 阶段二：资源库

### 任务 7：目录与已安装资源列表

**Files:**

- Modify: `app/pages/resources-page.tsx`、`shared/ui/resource-page.tsx`、`resource-list-item.tsx`、`resource-management-card.tsx`、`resource-loading-state.tsx`、`modules/plugin/pages/plugin-resource-collection.tsx`、`modules/skill/pages/skill-resource-collection.tsx`、`modules/connector/pages/connector-resource-collection.tsx` 及其实际条目组件。
- Test: `app/pages/resources-page.test.tsx`、`resources-page.collections.test.tsx`、`resource-loading.test.tsx`、`shared/ui/resource-list-item.test.tsx`、`resource-management-card.test.tsx`、`modules/plugin/pages/plugin-resource-collection.test.tsx`。

**Interfaces:** 消费阶段一分类侧栏及原集合 props；保留搜索、来源、安装和伙伴选择回调；共享列表添加明确布局变体，只有迁移后的资源库使用两列轻量条目，其他待整改模块不随全局改动提前变化。

- [ ] **7.1 补齐 OpenBot 状态对照。** 实际查看目录、已安装、搜索无结果、添加与管理路径；把观察结果及必要适配写入既有验收文档的资源库段。
- [ ] **7.2 写失败行为用例。** 分类只有侧栏一份；进入带筛选列表、打开详情、点祖先返回后条件保留；安装／更新／卸载仍调用原 mutation，尾部动作不触发详情；局部错误和后台刷新不清空旧列表。
- [ ] **7.3 迁移列表与骨架。** 880px 内容上限，按容器使用两列／单列轻量条目；图标、名称、简介及尾部真实动作按 OpenBot 排列。移除重复管理底栏，保留来源、固定版本、伙伴使用关系和原加载机制；空／错误与骨架跟随同一内容布局。
- [ ] **7.4 定向运行本任务测试及实际修改组件测试。** 先确认新增断言在旧列表失败，再确认迁移后通过；执行 UI 策略、定向格式和类型检查。真实检查 880px、480px、320px 内容，确认两列与单列、搜索和按钮不裁切。
- [ ] **7.5 同步资源列表规范并提交。** 更新前端约定中已迁移资源页的三列规定，保留其他模块适用的原布局；提交 `refactor: adopt OpenBot resource directory layouts`。

### 任务 8：资源详情与管理交互

**Files:**

- Modify: `modules/plugin/pages/plugin-detail-page.tsx`、`plugin-catalog-detail-page.tsx`、`plugin-skill-detail-page.tsx`、`plugin-connector-detail-page.tsx`、`modules/skill/pages/skill-detail-page.tsx`、`modules/connector/pages/connector-detail-page.tsx` 及其现有元数据／编辑弹窗组件。
- Test: 上述六个现有详情文件对应的 `.test.tsx`；另含 `modules/plugin/components/plugin-settings-dialog.test.tsx`。

**Interfaces:** 消费阶段一面包屑与原详情 Query、mutation；正文、元数据和操作分区复用现有模块公开组件，不增加业务端点。

- [ ] **8.1 对照实际详情、管理、安装和关闭返回路径，写入同一验收文档。**
- [ ] **8.2 写失败用例。** 独立资源可管理，插件组件保持只读；原共享配置、版本固定与伙伴启用语义不变；弹窗失败就近呈现，关闭后焦点恢复；迟到详情不能更新其他 Scene 面包屑。
- [ ] **8.3 迁移身份头、正文与信息区。** 按 OpenBot 保留简短简介和正确操作位置；去重复标题与卡片嵌套，复用现有 Markdown。不为试用展示新增产品能力。
- [ ] **8.4 执行六组详情与配置弹窗定向测试，并实际走通列表→详情→原有安装／管理→返回。** 同步对应 UI 规则和验收记录；未观察／未运行状态明确列出。
- [ ] **8.5 提交并交付阶段二。** `refactor: align resource detail and management presentation`；阶段二验收后进入阶段三。

## 阶段三：工作流

### 任务 9：工作流入口、创作、画布与运行布局

**Files:**

- Modify: `modules/workflow/pages/workflow-list-page.tsx`、`workflow-detail-page.tsx`、`workflow-run-page.tsx`、`modules/workflow/components/workflow-canvas.tsx`、`workflow-chat-pane.tsx`、`workflow-detail-dock.tsx`、`node-inspector.tsx`；以及 `app/pages/resources-page.tsx`、`app/layout/app-navigation-model.ts`、`app/scenes/app-route-scene-model.ts`。
- Test: `modules/workflow/pages/workflow-list-page.integration.test.tsx`、`workflow-scene.integration.test.tsx`、`workflow-run-page.integration.test.tsx`、`modules/workflow/components/workflow-canvas-close.test.tsx`、`workflow-chat-pane.integration.test.tsx`、`node-inspector.integration.test.tsx`、`app/layout/app-navigation-model.test.ts`。

**Interfaces:** `/workflows` 成为列表主入口，旧 `/resources?category=workflows` 使用 replace 保留搜索后转入；继续消费同一 `WorkflowResourceCollection`／查询。定义、运行、创作聊天、画布与检查器共用原 owner 身份。

- [ ] **9.1 实机核对 OpenBot 的列表／进入画布、名称与历史、运行、局部工具、聊天和检查器开合路径，记录本项目节点差异。**
- [ ] **9.2 写失败用例。** 新旧列表入口归属一致且参数不丢；从运行返回当前定义；开合聊天／检查器不重建画布或编辑缓冲；关闭画布与关闭整个工作流仍执行原资源释放；人工请求／重试仍可达。
- [ ] **9.3 迁移入口及呈现。** 去掉当前列表重定向，把资源库中的旧入口变为兼容跳转；按 OpenBot 重排顶栏、全宽画布、局部工具、历史和按需面板。仅修改节点外观和操作位置，不修改 DSL、布局／连线算法或执行参数。
- [ ] **9.4 运行本任务定向测试。** 对实际改动的节点控件加对应行为测试；执行类型、UI 策略和格式检查，并实际走通创作、运行、历史、返回和窄窗口。
- [ ] **9.5 同步当前工作流 UI 说明、验收记录并提交阶段三。** `refactor: align workflow navigation and workbench layout`。

## 阶段四：设置

### 任务 10：设置导航、分组与账号／环境展示

**Files:**

- Modify: `modules/settings/components/settings-context-sidebar.tsx`、`settings-section-layout.tsx`、`ai-accounts-section.tsx`、`runtime-environments-section.tsx`、`api-integration-section.tsx`、`mcp-integration-section.tsx`、`modules/settings/pages/settings-scene.tsx`、`modules/settings/settings-navigation.ts`、`app/scenes/im-integration-settings-content.tsx` 与实际 IM 内容组件。
- Test: `modules/settings/settings-navigation.test.ts`、`pages/settings-scene.test.tsx`、`components/ai-accounts-section.test.tsx`、`runtime-environments-section.test.tsx`、`account-login.test.tsx`、`api-integration-section.test.tsx`、`mcp-integration-section.test.tsx`、`app/scenes/im-integration-settings-content.test.tsx`。

**Interfaces:** 分类保持 `general`、`ai-accounts`、`runtime-environments`、`integration-im`、`integration-api`；搜索只过滤分类名称；复用 `SettingsRow`／`SettingsGroup`、原登录与配置 mutation。公共布局兼容现有 props，避免各分区重做字段控件。

- [ ] **10.1 实机核对 OpenBot 的分类搜索、分组行、服务商状态与详情返回；记录哪些对应当前账号与环境，哪些没有本项目业务。**
- [ ] **10.2 写失败用例。** 搜索分类不改变已选分区或提交配置；无结果仍可清除搜索；设置仍唯一 Scene；AI 账号与环境不合并为一份状态；登录／测试／更新错误仍在原区域，API／MCP 的实际开关关系不变。
- [ ] **10.3 迁移导航和设置行。** 内容上限 720px，分组背景及行内控件采用共享语义组件；长路径／多行字段容器不足时换行。保留必要权限说明和字段标签，不增加统一保存按钮。
- [ ] **10.4 运行本任务定向测试，实际核对浅暗主题、分类搜索、原有编辑／测试操作、长内容及窄容器。** 登录等涉及真实外部操作的自动化使用现有 Fake／MSW，不替用户授予新权限。
- [ ] **10.5 同步设置规则、验收记录并提交阶段四。** `refactor: adopt OpenBot settings navigation and rows`。

## 阶段五：其他模块逐个对齐

### 任务 11：按模块完成页面迁移与交付

本任务为七次独立交付，依次为伙伴／团队、聊天、知识库、工作看板、自动任务、用量及全局收尾。每次重复下面步骤，不把多个模块混为一个提交；进入模块前将本节对应清单细化为该模块的文件和交互对照，不提前改无关模块。

| 模块       | 起始文件（相对 frontend/src）                                                                                                                                 | 定向回归入口                                                                                                          |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| 伙伴／团队 | `app/pages/partner-management-page.tsx`、`modules/waker/pages/waker-detail-page.tsx`、`app/pages/team-thread-page.tsx`、`app/layout/team-context-sidebar.tsx` | `app/layout/entity-navigation.test.tsx`、`app/pages/team-thread-page.integration.test.tsx`                            |
| 聊天       | `modules/chat/pages/chat-page.tsx`、`components/chat-thread.tsx`、`components/chat-composer-panel.tsx`                                                        | `modules/chat/pages/chat-page.integration.test.tsx`、`retained-chat-page.test.tsx`，及实际修改的输入／消息组件测试    |
| 知识库     | `app/scenes/knowledge-scene.tsx`、`modules/knowledge/knowledge-page.tsx`、`knowledge-library-navigation.tsx`                                                  | `modules/knowledge/knowledge-page.test.tsx`、`knowledge-loading.test.tsx`、`knowledge-read-lifecycle.test.tsx`        |
| 工作看板   | `modules/work-board/pages/work-board-page.tsx` 及其实际行组件                                                                                                 | `modules/work-board/pages/work-board-page.integration.test.tsx`                                                       |
| 自动任务   | `modules/automatic-task/pages/automatic-task-list-page.tsx`、`automatic-task-detail-page.tsx` 及其实际表单                                                    | `modules/automatic-task/components/task-form-workflow.test.tsx`、`workflow-input-fields.test.tsx`，及对应页面已有测试 |
| 用量       | `modules/usage/pages/usage-page.tsx`、`components/usage-report.tsx`                                                                                           | `modules/usage/pages/usage-page.test.tsx`、`components/usage-report.test.tsx`                                         |

**Interfaces:** 以上模块继续消费原 URL／Query／mutation／事件；仅对齐展示、控件位置和导航。任务 1–10 的 shared 与 app 接口是唯一壳层，不为各模块复制框架。

- [ ] **11.1 为当前模块记录 OpenBot 实际列表、详情、编辑、空／错误、加载、返回及面板状态。** 无对应页面时写清采用哪一已有结构与本项目业务映射；把修改文件、原接口和验收用例补入本计划当前模块段后实施。
- [ ] **11.2 为将变化的导航和行为写失败测试并运行确认。** 纯间距和圆角调整只做样式与视觉验证，不写镜像实现的测试；聊天／知识／通知等已有领域契约只回归直接受影响部分。
- [ ] **11.3 实现当前模块完整页面路径。** 使用已落地的框架、面包屑和共享展示组件；删除重复说明／表面，保留实际字段和操作，不触碰消息接纳、文件成果判定、检索、调度或计量规则。
- [ ] **11.4 定向测试、类型／样式／格式检查并做该模块真实界面对照。** 自动化与平台／视觉结果分别记录，未验证项保持明确；更新已改变的权威页面说明。
- [ ] **11.5 按模块提交并交付。** 使用 `refactor: align <module> pages with OpenBot`；当前模块验收后再进入下一个。
- [ ] **11.6 最后检查全局残留。** 搜索旧导航、重复分类、旧资源卡片分支和失效样式，确认无实际消费者后删除；检查帮助文案、键盘、全模块深链与正文／pane 返回。只回归被清理代码的调用方，运行 `pnpm docs:check` 并更新最终差异记录。

## 本次文档交付验证

- [x] 核对 spec 的框架、面包屑、状态身份及五阶段要求均有任务承接。
- [x] 独立审阅 spec 的重要歧义；计划自审接口名称、文件存在性、测试命令和阶段边界。
- [x] 运行 `pnpm docs:check`，检查新增文档链接与个人路径。
- [x] 对两份新增文档运行 Prettier 检查及 `git diff --check`。
- [x] 仅提交本次规格与计划，报告 worktree、分支、文档位置及验证结果；不把文档完成报告为产品实现完成。
