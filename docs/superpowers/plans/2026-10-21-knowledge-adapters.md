# 知识库统一 Adapter 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: 使用 executing-plans 顺序集成；独立后端合同按 dispatching-parallel-agents 分离处理。步骤采用测试先行。

**Goal:** 所有知识库操作统一经本地与远端 Adapter，补齐目录菜单、远端层级及能力权限反馈。

**Architecture:** 前端知识库 Adapter 是生成 SDK 的唯一领域调用入口，query 和交互只依赖统一方法。远端所有请求携带完整连接和库身份；服务端保留真实能力、授权和版本约束。

**Tech Stack:** React、TypeScript、TanStack Query、Vitest/MSW、Go、ginx、neux-knowledge。

**Spec:** [统一操作设计](../specs/2026-10-21-knowledge-adapters-design.md)

## Global Constraints

- 保留当前两层目录及空目录删除规则；不增加级联删除。
- 不手改生成文件；不回退本地资源；不提交或推送。
- 未知写入沿用 requestId；远端整理只使用服务器 profile。

## Review Focus

- 同 ID 的本地与远端库：远端失败不得触碰本地请求。
- 权限撤销与能力查询失败：不能据缓存或连接成功放行写入。
- 非空目录、系统目录、同级重名、第三层目录：稳定拒绝且保留草稿。
- 多层目录的错误、循环与取消：不得返回不完整的成功树。
- 写入响应丢失：恢复原身份，不重复上传或删除。

### Task 1: 后端远端合同

**Files:** server/apis/knowledge_connections/ 下能力、目录、重建相关接口与测试；必要时独立核心通过同步生成器交付。

**Interfaces:** 保留现有 API 名称；能力响应增加 supported/permission/condition 明细，operations 保留兼容；远端重建明确 rebuild 参数及服务端 profile。

- [x] 增加能力明细、逐层目录、目录写入和重建合同失败测试。
- [x] 实现自有远端真实转发，官方缺少合同稳定拒绝。
- [x] gofmt，运行知识 API 测试、go vet ./...、go test ./...；生成 API。

### Task 2: 统一 Adapter 与全部调用迁移

**Files:** desktop/src/shared/api/knowledge/ 下统一边界；知识库与聊天中既有 query、编辑器、文件、设置与图谱调用文件。

**Interfaces:** knowledgeAdapter(ref) 返回本地／远端统一领域操作；能力检查产生可本地化的 unsupported/forbidden 错误；目录列表对调用方返回完整树。

- [x] 增加本地与 custom 路由、官方不支持、权限拒绝和远端子目录失败测试。
- [x] 实现 Adapter，迁移全部生成 SDK 调用与后端分支；目录递归完整读取。
- [x] 对全部知识操作逐项运行合同测试和现有回归。

### Task 3: 目录菜单与远端交互

**Files:** knowledge-wiki-tree/view/editor、目录操作 Dialog、文档操作、整理 Dialog、知识库操作及共享文案。

**Interfaces:** 复用 Task 2 的统一能力检查与领域方法；目录菜单带真实对象与打开时的结构版本。

- [x] 增加 hover/focus 菜单、创建、重命名、删除、失败草稿及远端拒绝测试。
- [x] 接入统一 Adapter，远端整理使用服务器 profile，入口不再按 backend 隐藏。
- [x] 运行全部知识库前端测试及 typecheck。

### Task 4: 复核与交付

**Files:** docs/architecture/knowledge-cn.md。

- [x] 审计全部操作无直接生成 SDK 调用、无远端本地回退。
- [x] 更新现有架构与验收边界。
- [x] 运行 NODE_OPTIONS=--no-experimental-webstorage pnpm check 与 api:check；独立代码审查并修复问题。接口与边界见[知识库架构](../../architecture/knowledge-cn.md)。
