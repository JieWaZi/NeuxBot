# Wiki 轻量目录实施计划

> **For agentic workers:** 使用 executing-plans 在当前会话实现，按 test-driven-development 验证；不提交、不推送，保留两仓库已有修改。

**Goal:** 本地与自有远端目录只查询、传输导航和索引展示字段，正文在选中页面后独立读取。

**Architecture:** 核心提供独立 ListPageDirectory 与窄结果类型，宿主本地、远端代理和公开集成 API 复用同一能力。每批上限 1000，自有文件夹目录一次读取整表并明确返回 complete；保留旧完整列表供其他读取用途。WeKnora 继续官方分页协议，代理裁剪字段，不声称改变官方数据库查询。

**Tech Stack:** Go、SQLite/GORM、生成 OpenAPI、React/TanStack Query、Vitest/MSW。

**Spec:** 本会话已确认的轻量目录、数据库字段投影、正文按需读取及本地远端统一处理要求。

## 约束与审查重点

- 目录只含 ID、slug、标题、类型、简介、目录归属、路径、排序与版本；核心另含所属库用于远端范围验证。
- SQL 必须显式 Select；默认排除归档和软删除，按 updated_at DESC、id ASC 稳定分页，保留真实总数。
- 正文、证据、引用和链关系不得出现在目录响应；不把轻量记录放进正文缓存。
- 分页空页、重复 ID、取消和读取失败不能变成完整目录。
- 远端只读授权及库身份不变；WeKnora 缺少轻量协议时明确保留上游限制。

## Task 1: 核心查询与远端协议

- [x] 增加真实 SQLite 回归：SQL 投影、500 条单批、分页总量、归档/软删除/跨库过滤、正文单独读取。
- [x] 观察新能力缺失导致失败，实现窄类型、核心查询及独立 HTTP/远端 SDK。
- [x] 增加自有 HTTP/集成端到端读取和身份校验测试。

## Task 2: 宿主与前端

- [x] 添加本地、远端代理、公开集成的 page-directory Operator，沿原权限注册并生成契约。
- [x] 添加前端回归：本地与远端均只读目录，点击后取正文，保留超过一批与异常分页反馈。
- [x] 切换 adapter 与目录 Query，保留正文 Query 和现有界面能力。

- [x] 复现并修复自有文件夹目录瀑布请求；通过 all 查询和 complete 响应声明整表，官方协议保留分层枚举。

## Task 3: 文档与验证

- [x] 更新知识架构及核心 SDK/HTTP 文档。
- [ ] gofmt；核心 go vet/test；宿主 pnpm check，涵盖 API、前端和 Go 全量检查。
- [ ] 审查本次增量并记录未发布核心依赖、WeKnora 与真实远端性能验收边界。
