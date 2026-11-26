# 会话 Git 与工作台重构调研

2026-11-25 · 讨论稿。NeuxBot 基线为 `a15310f4`，T3 Code 采用官方仓库 `20a3bed88020972cafe0b646daaa93d061e44384`。截图用于核对布局，按钮语义与数据口径以源码为准。[T3 Code 仓库及 MIT 许可](https://github.com/pingdotgg/t3code/tree/20a3bed88020972cafe0b646daaa93d061e44384)

## 推荐改成什么样

| 位置 | 布局和操作 |
| --- | --- |
| 会话输入区 | 移除底部常驻分支胶囊。输入区继续服务于发送消息。 |
| 会话详情 | 沿用现有摘要入口。Git 仓库按「分支 ▾」「Commit / Push / PR ▾」「改动 vs 基准 +增 −删」排列。单仓库直接展开，多仓库先选择或展开具体仓库。已存在的 PR 可直接打开。会话产物继续从这里进入。 |
| Diff Tab | 共用工作台 Tab 轨道。次栏左侧为来源、仓库和基准，右侧为总行数、刷新、全部展开／折叠、统一／并排、换行、隐藏空白改动、隐藏测试文件、改动树。主区按文件折叠展示补丁，路径、状态与增删行数放在文件行上。右侧树负责跳转定位，文件标题负责打开普通文件 Tab。切换比较基准只改变审查范围。 |
| 文件 Tab | 从「+ → 文件」先打开浏览器。项目文件从树、聊天引用或 Diff 的文件标题进入同一个文件 Tab，左侧预览或编辑，右侧树跟随当前文件选中。标题下显示相对面包屑，操作从左到右按文件类型与平台能力呈现外部打开、预览／源码、换行、文件树。文件树顶部提供刷新、搜索与全部展开／折叠。 |

采用 NeuxBot 的 Tab、语义表面与紧凑工具栏。T3 Code 顶部三个图标分别打开会话详情、底部终端和右侧面板，双向箭头负责最大化／还原面板。这些职责可对照现有工作台控件，终端抽屉没有必要随本次 Git 重构一起引入。[面板控件](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/chat/PanelLayoutControls.tsx#L61-L175) · [NeuxBot 工作台标准](../development/ui-cn.md#工作台与可访问性)

## T3 Code 源码里值得借鉴的机制

- **按仓库组织工作。** T3 Code 的输入区也有分支选择。会话详情另有分支、Git 动作和改动汇总，NeuxBot 可以把后者作为唯一 Git 入口。T3 Code 的主按钮随未提交改动、上游状态、默认分支和现有 PR 变化，箭头提供其他可执行动作。组合操作按 branch、commit、push、PR 报告进度，失败事件携带失败阶段。分支选择还核验目标 worktree 的真实 HEAD，切换 worktree 在活动回合有单独约束。[输入区](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/ChatView.tsx#L4017-L4048) · [会话详情](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/chat/ThreadDetailsPanel.tsx#L258-L306) · [主动作选择](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/GitActionsControl.logic.ts#L298-L435) · [组合指令](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/server/src/git/GitManager.ts#L4218-L4307) · [分支切换](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/BranchToolbarBranchSelector.tsx#L452-L550)
- **摘要与 Diff 用同一比较口径。** 默认「Changes」以比较分支的 merge-base 对比当前工作树，所以能在一个视图看已提交与未提交的净变更。「Uncommitted」以 HEAD 对比当前工作树。「Commits」可看单次提交相对第一父提交的变化。T3 Code 在支持的情形先回文件状态和统计，展开后再读单文件补丁；未跟踪文件参与比较时使用临时 index。¶ 表示隐藏空白改动，带斜线的烧瓶表示隐藏测试文件。[摘要取数](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/GitActionsControl.tsx#L1085-L1115) · [比较实现](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/server/src/vcs/GitVcsDriverCore.ts#L4909-L5069) · [Diff 工具栏](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/DiffPanel.tsx#L1435-L1572)
- **提交弹窗直接选择工作树文件。** T3 Code 从有变更的文件中确定提交范围，未填写提交说明时自动生成。生成前按所选文件构造暂存补丁，说明与选文件共用这个输入。NeuxBot 的提交弹窗同样支持直接选文件，已有暂存内容时默认保留原来的暂存范围，可切换到选文件模式。[提交弹窗](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/GitActionsControl.tsx#L1389-L1415) · [构造提交补丁](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/server/src/vcs/GitVcsDriverCore.ts#L3403-L3488)
- **文件浏览器负责找文件，文件 Tab 负责内容。** T3 Code 从独立 Files 浏览器打开文件时收起该入口，复用或激活对应路径的 Tab。文件 Tab 复用可调整宽度的右侧浏览器并随当前文件定位。Diff 右侧使用独立的改动树，与完整工作目录树各有用途。[文件 Tab 状态](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/rightPanelStore.ts#L1006-L1038) · [文件工具栏](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/files/FilePreviewPanel.tsx#L1581-L1687) · [文件树](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/apps/web/src/components/files/FileBrowserPanel.tsx#L597-L639)

## NeuxBot 可以收敛和删除的部分

| 当前实现 | 重构处理 |
| --- | --- |
| 输入区按仓库渲染分支，`ChatSummary` 另外取工作树统计，Changes 页再次发现仓库与读取状态。输入区只认选中目录自身的仓库，Changes 页还能发现授权根内的子仓库。 | 移除输入区分支渲染，搬迁分支搜索与切换功能。已接纳聊天统一使用 Chat 冻结范围和规范仓库路径，详情与 Diff 共用仓库选择、状态查询和刷新。迁完调用方可删除仅为摘要服务的 `getWorkspaceGitChanges` 投影，以及旧查询失效前缀。草稿继续使用显式选择的目录。 [输入区](../../packages/frontend/src/modules/chat/components/chat-composer-controls.tsx#L158) · [摘要](../../packages/frontend/src/modules/chat/components/chat-summary.tsx#L45) · [Core 平台适配](../../packages/frontend/src/shared/platform/core-workspace.ts#L699) |
| Changes 页同时承担状态、暂存、历史、单文件 Diff 和底部提交。另有 Git 补丁 Tab，源码搜索只发现声明、渲染和测试，未发现生产分派 `open-git-diff` 的入口。 | 把提交移到会话详情，历史放入 Diff 来源选择，聚合审查集中到一个 Diff Tab。「未提交」显示 HEAD 到工作树的净变化。「暂存与工作区」保留暂存、取消暂存和放弃改动。删除独立 Git 补丁页及其分派前，先在已保存的布局中定向转换或关闭旧项，保住其他 Tab 与分屏。 [Changes 页](../../packages/frontend/src/modules/chat/components/scoped-git-changes.tsx#L58) · `a15310f4:packages/frontend/src/modules/chat/workbench/git-diff-workbench-view.tsx` · [布局恢复](../../packages/frontend/src/modules/chat/workbench/workbench-store.tsx#L98) |
| 切分支的冲突弹层可「提交并切换」，Core 在这个路径执行仓库级 `add -A`。普通 Commit 则只提交已暂存内容。若提交成功而切换失败，调用方收到的是切换错误。 | 保留冲突预览和安全切换，移除弹层内部的隐式全仓暂存及复合提交。冲突时回到同一个提交入口，用户核对提交范围后再切换。提交确认需区分已暂存和未暂存，同一路径同时有两种内容时不能靠路径去重来扩大提交范围。 [Core Commit](../../server/internal/workspacefiles/git.go#L457) · [Core Checkout](../../server/internal/workspacefiles/git.go#L602) |
| 「文件」Tab 在内部保存选中文件、预览模式和编辑缓冲，聊天引用打开的独立文件 Tab 另持一套身份与缓冲。 | 把项目文件浏览和编辑收敛到一条打开路径。文件树只维护导航状态，文件选中和编辑由文件 Tab 及现有缓冲拥有。同一聊天对同一规范路径复用 Tab。浏览器关闭或变形时处理现存未保存内容。会话附件及会话成果继续按原归属授权，支持已有预览、下载和文件操作。 [文件页局部状态](../../packages/frontend/src/modules/chat/workbench/file/workbench-file-workspace.tsx#L134) · [独立文件身份](../../packages/frontend/src/modules/chat/workbench/workbench-model.ts#L460) |
| 文件查看器同时支持来自真实工作树的单文件比较与按消息身份读取的成果快照。当前生产成果入口传入 `executionDiff`，裸工作树比较是查看器的另一条路径。 | 实时 Git 变更走 Git Diff 读路径，历史消息继续读取原成果快照。待确认旧布局和其他调用方后，可以退役裸 `readWorkspaceDiff` 模式；原消息深链与分叉成果应继续按保存版本打开。 [打开消息成果](../../packages/frontend/src/modules/chat/components/use-chat-workspace-bindings.tsx#L198) · [两类读取](../../packages/frontend/src/modules/chat/components/workspace-file-viewer.tsx#L187) · [成果存储](../architecture/workspace-and-storage-cn.md) |

## 需要补齐的能力

1. **比较与状态。** Go Core 统一提供经授权的仓库状态、可选基准、upstream 与 ahead／behind、可读比较和按路径补丁。比较摘要与 Diff 列表使用同样的仓库和基准。无基准时明确落到「未提交」，未跟踪、二进制、重命名和输出超限各自表达；不可统计时显示未知。切分支、修改文件或显式刷新沿用现有事件失效链路。[现有 Git API](../../server/apis/workspace/router.go#L18) · [事件契约](../architecture/notifications-and-work-board-cn.md#实时通道)
2. **分支与远端动作。** 建议支持本地分支创建／切换、仅 Commit、Commit & Push、Push、创建或打开 GitHub PR，以及 Commit → Push → PR 的分阶段操作。含提交的操作先呈现真实提交范围与消息。Push 使用仓库现有 Git 凭据，首版 GitHub PR 可评估复用本机 `gh` 登录，把提供方逻辑留在 Core。Push 或 PR 失败时显示已完成步骤，并从实际 HEAD、远端分支与现有 PR 对账后再继续。远端和 base/head 在动作前可见。同一工作树被多个会话引用时，分支切换影响真实工作树，活动执行、并发写入与旧状态需要在 Core 中核验。
3. **工作树是独立产品决策。** T3 Code 可以给线程绑定 worktree，NeuxBot 现有聊天在接纳时冻结工作范围，真实项目可被多个会话共享。第一轮可操作当前已授权的 checkout，也可使用用户已经选入工作范围的 worktree。若要为每个聊天创建或切换专属 worktree，应另行确定创建时点、续聊绑定、删除清理和多个项目的映射。 [T3 Code 多仓库工作树](https://github.com/pingdotgg/t3code/blob/20a3bed88020972cafe0b646daaa93d061e44384/docs/user/multi-repo-projects.md#worktree-threads) · [NeuxBot 工作范围](../architecture/workspace-and-storage-cn.md)

## 建议落地顺序

1. 先做会话详情统一入口、仓库状态/比较和 Diff/File Tab 的归属重构。把本地 Commit 移到新入口，同时收掉输入区胶囊、重复 Git 统计和隐式全仓暂存；旧会话仍能恢复布局、打开原文件和执行成果。
2. 在统一入口接入新建分支、Push 和 GitHub PR，验证 Commit 成功后 Push 失败、Push 成功后 PR 失败、已有 PR、多仓库、默认分支、没有 upstream、旧状态点击以及切换到其他会话。
3. 在真实桌面核对长分支名、窄工作台、合并与并排 Diff、键盘焦点、文件树定位和未保存编辑；测试分别验证工作树改动、分支比较与消息快照三种来源。

本轮依据已克隆源码与截图做静态分析。上述布局和交互均为讨论建议。
