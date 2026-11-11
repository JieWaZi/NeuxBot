# 空间目录与工作目录实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development。用户指定两个开发子代理，主代理负责全局边界与审查。Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 伙伴固定空间自由组织文件，取消每轮成果与临时目录，文件界面区分空间目录与用户工作目录。

**Architecture:** 保留 Chat.WorkspaceDirectory 作为伙伴受管 cwd，真实项目继续使用已有授权目录。上传原件仍归 Chat，文件成果使用现有采集与不可变记录；不新增数据库、接口或第二套状态源。

**Tech Stack:** Go Core、acp-go、React、TanStack Query、现有工作台与文件授权服务。

**Spec:** 用户在本会话确认的方案：固定空间不强制 outputs/tmp；允许系统临时目录；界面名称为空间目录／工作目录；没有用户工作目录时不显示 Tab。

## 全局约束

- 当前 codex/openbot-ui-alignment 分支包含已有界面改动；原地实现以保留当前可运行基线，不切换、暂存、提交或覆盖用户已有修改。改动前补丁保存在系统临时目录，两个开发代理按前后端分工避免写入竞争。
- 不迁移、清理或删除用户历史目录与文件，不添加历史路径回退。
- 不改变工作流专用业务目录，不改变真实项目位置、已接纳授权或只读权限。
- 不增加账号、额度、模型探测；不放宽文件服务的预览、编辑或跨 Chat 附件授权。
- 无用户授权不操作运行中的应用，不启动真实模型任务；只做定向自动化验证。
- 生成文件通过生成命令更新；后端修改结构体时检查全部字段注释。

## Review Focus

- 同伙伴跨会话、同会话多轮：cwd 稳定且不生成每轮目录；无文件修改不产生虚假成果。
- cwd 中内部配置和技能不作为业务成果；根目录与自建子目录里的真实文件都可以采集。
- 附件原件仍归当前 Chat，CLI 能读取明确引用的附件，不能获准访问其他 Chat 的私有内容。
- 未选择工作目录、自定义工作目录、多项目目录、目录离线：Tab 与实际文件根一致；默认 cwd 不误认为工作目录。
- 切换目录后文件预览、编辑、拖放、重命名、删除仍使用真实根，未保存编辑和缓存身份不串根。

### Task 1：后端固定空间与成果采集

**Owner:** backend 子代理，仅修改 server/。

**Files:** server/services/execution/{work,agent}.go、server/internal/workspace/{execution_workspace,attachments}.go 及直接关联的权限、采集测试与调用方。

**Interfaces:** 保留公开 API 与 WorkspaceDirectory/additionalWorkingDirectories；空间 cwd 固定，附加目录仍是批准的真实目录。系统临时目录不成为成果范围或前端文件根。

- [x] 先补充或调整定向行为测试，验证原实现不能满足无每轮目录、自由路径成果和精确附件读取。
- [x] 去除宿主自动创建与指示 outputs/tmp 的普通执行路径，复用现有附件与文件授权，不新建临时持久副本层。
- [x] 现有净变更采集覆盖空间业务文件与用户工作目录，并排除内部文件；保持旧成果原地修订和工作流边界。
- [x] 核对原生客户端系统临时目录权限，保留只读执行约束；如依赖存在限制，及时向主代理报告，不修改外部仓库。
- [x] gofmt 修改文件，定向 go test 相关包和用例，报告准确命令、结果及未覆盖边界。

### Task 2：前端空间目录／工作目录

**Owner:** frontend 子代理，仅修改 packages/frontend/。

**Files:** chat/workbench/file/workbench-file-workspace.tsx、chat/components/use-chat-workspace-bindings.tsx、相关模型和工作台入口、i18n 与定向测试。

**Interfaces:** 空间根来自 chat.workspaceDirectory；用户工作目录来自 additionalWorkingDirectories，排除与空间根重复项。保留现有会话成果列表入口与文件引用能力。

- [x] 先补充定向 UI 测试：无工作目录无 Tab，有工作目录出现两个指定名称的 Tab，实际读取根随 Tab 改变。
- [x] 复用共享 Tab 与现有状态，不创建存储事实或新 API；多个用户目录在工作目录下使用现有树组织。
- [x] 空间树不展示内部 CLI 配置；过滤不得影响真实用户工作目录中的正常文件。
- [x] 验证目录切换、预览编辑、文件操作和原有会话文件入口，保留此前未提交 UI 改动。
- [x] 运行受影响测试、定向格式／lint 及类型检查，不运行全量 check 或全量测试。

### Task 3：主代理整合审查

- [x] 检查两端差异，核对空间根、附加根、附件授权、内部排除和采集一致性。
- [x] 更新 docs/architecture/workspace-and-storage-cn.md 和必要当前架构词汇，移除已失效的目录说明。
- [x] 独立复跑关键跨边界测试与 docs:check，修复本次引入的问题。
- [x] 汇总准确交付状态；未进行真实 CLI／视觉验收不得声称已经验收，不自动提交、推送或重启 Core。

## 验证记录

- 后端两个开发边界复用原契约；相关 `workspace`、`workspacefiles`、`chat`、`execution` 包及自动任务定向集成测试通过，未调用真实模型。
- 主代理独立复跑四个相关包中的九项关键行为回归通过。
- 前端五个定向测试文件共 57 项用例、两项页面集成用例及类型检查通过；主代理另复跑三个关键文件共 42 项用例通过。
- 审查发现并修复内部目录遗漏、同名附件删除身份和嵌套工作根操作三项问题；定点复审及独立回归通过。
- 修改文件格式与 lint、文档检查、差异空白检查通过。未提交、推送、重启 Core 或进行真实 CLI 与视觉验收。
- 草稿尚无真实空间根时仅浏览已有工作目录，首发后沿用后端空间根；不推导伙伴路径或增加草稿接口。
