# 知识库活动状态与摘要实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 活动记录展示当前整理任务的真实状态与范围，并以页面标题和实际变更数量解释历史写入。

**Architecture:** 当前状态读取已有 Job，与不可变活动历史分别展示；复用库级 Query Cache 和 `knowledge.changed` 事件。Wiki 写入在原事务中保存有界标题摘要，旧记录缺少摘要时保留类型与数量，不猜测对象。

**Tech Stack:** React、TanStack Query、Go、SQLite、现有 neux-knowledge 核心。

**Spec:** 本聊天中已确认的小改动：当前任务状态＋对象名称或范围数量＋历史记录摘要；不实现精确当前文档追踪和任务归并。

## Global Constraints

- 保留两个仓库已有的并行修改，在当前可运行工作区实施，不提交无关修改。
- 不新增轮询、业务连接或生命周期状态源；不读取页面正文补展示字段。
- 普通 Card 无常驻边框和阴影；文案复用 i18n；只运行定向检查。
- 当前任务先支持已有 activeJob 合同的本地库；远端历史继续沿现有接口展示。
- 不打开或截图运行中的页面；真实平台和视觉验收单独说明。

## Review Focus

- 排队、取消请求、宿主暂停不能显示为成功或正常处理中。
- 刷新失败保留已知数据并提示无法确认最新状态，不能伪装为没有任务。
- 任务终态退出当前区；真实活动承接结果，不能由前端伪造完成记录。
- 标题预览有界，数量按真实变更计算；重复请求不能重复记录。
- 批量活动不逐行查正文、任务或页面，旧记录缺字段正常降级。

### Task 1: 任务类型合同与当前活动展示

**Files:** `server/apis/knowledge/job_resource.go`、`get_job_test.go`；前端 `knowledge-queries.ts`、`knowledge-workspace-queries.ts`、新增 `knowledge-current-activity.tsx` 与测试。

**Interfaces:** Job 新增 `maintenance` 布尔字段，由 `core.IsMaintenanceJob` 判定。共享 `jobsOptions(ref)` 继续使用 `knowledgeKeys.jobs(ref)`。

- [x] 先测试维护任务类型、排队／执行／取消／暂停、事件刷新后终态收起及读取失败。
- [x] 运行对应 Go 和 Vitest 用例，确认新行为测试失败。
- [x] 实现字段、共享查询与当前区，更新生成合同。
- [x] 定向测试通过；检查请求共用缓存、不新增轮询。

### Task 2: Wiki 标题摘要与历史呈现

**Files:** 知识核心 `internal/store/audit_log.go`、`wiki_maintenance_test.go`、`activity_lite_test.go`、`lite.go`、`job.go`；前端 `knowledge-library-activity.tsx`、专门活动测试与 i18n。

**Interfaces:** 沿用活动 details 的 `title`、`titles`、`count`、`total`、`changed_pages`。最多保存五个标题，前端最多展示三个。

- [x] 测试真实写入、改名、软删除的标题与数量、重放幂等及多页预览上限。
- [x] 测试有界标题、实际页数、文档范围与缺少摘要的历史记录。
- [x] 先确认新测试失败，再于原事务批量读取有限页面元数据并渲染摘要。
- [x] 定向测试、格式、类型和相关 lint 通过；完成生成合同校验，并记录整体检查的实际结果。

审查修正：真实 `lite.publish` 和延迟取消也必须产生终态活动，不能只验证模拟的历史响应。源失败在已有终态来源读取中冻结最多三条、每条五百字符的任务诊断，不增加列表补查。本次尝试字段实际代表接纳时间，不能标成真实执行开始时间。两项均以真实 SQLite 回归与前端语义测试验证。

### Task 3: 文档与最终核验

**Files:** `docs/architecture/knowledge-cn.md`。

- [x] 更新原活动记录章节，说明实时状态与不可变历史的边界。
- [x] 复核相对本轮基线的修改；运行 `pnpm docs:check` 和本次直接受影响检查。
- [x] 完成代码审查并处理实质问题，报告测试证据与真实视觉验收边界。

## 验证记录

- 前端五个直接受影响测试文件共 78 项通过，包含事件刷新、终态收起、旧尝试结果保护与读取失败保留状态。
- 前端类型检查、修改文件 ESLint 与 Prettier 检查通过。
- 后端知识 API、本机与远端任务映射、任务队列／维护／恢复相关测试及定向 vet 通过。
- 知识核心 Activity／Audit／Lite／Cancel／Job／SourceWork 相关真实 SQLite 测试、核心整理相关用例及定向 vet 通过。
- 全库文档检查被其他任务已有临时记录中的个人路径阻断；本轮两份文档的链接及个人路径定向检查通过。
- `pnpm api:check` 最终通过；本轮任务类型与接纳时间字段经原生成链校验。
- 不进行运行中页面、真实模型或视觉验收。
