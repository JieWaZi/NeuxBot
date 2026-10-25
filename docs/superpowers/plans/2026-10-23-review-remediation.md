# 审查问题修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. 用户已明确要求多子代理实现、回归测试、复审及完整检查，不提交。

**Goal:** 修复审查中除并发重放以外的七项问题，保留工作区其他修改。

**Architecture:** 沿用既有 Query、反馈组件、集成授权协调器与知识事件。API 只增加目录搜索参数；恢复候选不再展开全部历史任务。独立职责分工，生成文件统一串行更新。

**Tech Stack:** React、TanStack Query、Go、GORM、SQLite、知识核心 SDK。

**Spec:** 本轮用户确认的七项审查问题及现有 development 约束。

## 全局约束

- 原审查第 3 项并发重放不处理；不提交、不推送、不控制运行中的应用。
- 使用当前工作区中未提交的实现，先记录状态，只修改负责文件中的必要逻辑。
- 遵守前后端、UI、通知与文档约定；复用共享组件及事件，生成文件只通过命令更新。
- Go 结构体全部字段使用准确中文注释；不增加 JOIN、第二套领域状态或页面轮询。

## Review Focus

- 首次与后台读取失败均可感知和重试，已有内容保留，取消不被呈现为业务错误。
- 搜索跨分页、匹配中文与描述，筛选与 Query Key、URL、总量一致。
- 并发删除与撤权采用一致锁序；回滚后不发成功事件。
- 自动工作内部链路保留来源，普通用户续聊及其新成员任务不继承旧身份。
- 完成历史增加不增加恢复任务的逐条查询；队列、取消、删除与重启仍可恢复。

## Task 1: 前端知识读取反馈与目录搜索

**Files:** 修改 knowledge-read-feedback、knowledge-library、knowledge-page、knowledge-queries、knowledge-workspace-queries 及对应测试，必要时调整共享知识 Adapter 类型。

**Interfaces:** 目录接口接收可选 `search` 字符串；搜索进入 URL 和 Query Key，切换搜索归零分页。根代理实现服务器搜索并统一生成 SDK。

- [ ] 先补错误恢复、后台保留内容、搜索请求与 URL 的失败测试并验证失败。
- [ ] 复用 QueryState 等既有反馈组件，移除仅当前页过滤，保持稳定 Shell。
- [ ] 运行定向回归和类型检查；报告文件清单、失败到通过证据。

## Task 2: 删除锁序与团队工作来源

**Files:** 修改 automatic_tasks/management、channel 的派发与输入规则、对应测试及 automation-and-im 架构文档。

**Interfaces:** 最终删除事务在 WithAuthorizationWrite 内执行并传递重入 Context；自动调用身份由当前工作来源决定。

- [ ] 先补并发删除/撤权与自动运行结束后用户续聊的失败回归。
- [ ] 复用授权协调器及原自动运行封闭规则，不改知识服务和生成文件。
- [ ] 运行定向 Go 回归并 gofmt，检查实际自动成员链路未被清除。

## Task 3: 知识维护事件与有界恢复

**Files:** 修改 services/knowledge 的提交与恢复、repositories/controllers 的必要查询、相关测试及 knowledge 架构文档。

**Interfaces:** 维护提交后复用 knowledge.changed；使用核心已有活动任务能力与有界候选查询，不访问核心私有数据库，不修改外部核心仓库。

- [ ] 先补成功事件、失败不发事件、历史增长与队列恢复的失败测试。
- [ ] 移除启动全历史 GetJob，保留原核心任务权威、冻结配置及取消语义。
- [ ] gofmt 并运行相关恢复、队列和事件回归，报告规模增长的查询证据。

## Task 4: 服务端目录搜索与集成验证

**Files:** 修改 knowledge_connections/list_available_libraries 及测试；生成文件通过 api:generate 更新。

- [ ] 补跨 50 条、描述/中文匹配、偏移和总量及远端来源搜索回归，先验证失败。
- [ ] 在服务器分页前匹配名称和描述，按有界批次遍历不支持搜索的来源，仅保留返回窗口。
- [ ] 统一生成 SDK；独立代理复审所有任务，处理有效反馈。
- [ ] 串行运行完整 pnpm check，必要时补跑被提前阻断的分项；明确既有问题和真实平台验收边界，不提交。
