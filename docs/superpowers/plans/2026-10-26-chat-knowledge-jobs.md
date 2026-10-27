# 聊天知识任务统一入口实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 伙伴通过明确的文档整理与 Wiki 维护入口协调工作，后台复用手动任务、提示词和结果核验。

**Architecture:** 在知识模块增加 `knowledge.wiki.maintain` 能力，与已有 `knowledge.jobs.start` 共用聊天授权、冻结输入和回执恢复。两者继承本轮 Execution 的智能体、账号、模型和推理配置，自动保留用户原文并附加伙伴补充。维护继续由现有独立 ACP 任务执行，聊天执行服务不增加领域分支。

**Tech Stack:** Go、SQLite、现有 allycli 能力契约、内置 Markdown skill。

**Spec:** 本聊天已确认的需求；原件整理、已有 Wiki 维护分别路由，聊天只负责协调，手动入口继续使用弹窗配置。

## Global Constraints

- 保留无关修改；不控制运行中的应用；不修改伙伴聊天实现。
- 复用知识任务队列、授权、幂等回执及统一维护 prompt，不新增任务状态源。
- 用户原始要求不得被伙伴摘要替换，补充发现不能扩大用户范围。
- 本轮模型配置由服务端 Execution 读取，禁止模型指定账号或权限。
- 只运行定向验证，生成资源通过命令更新。
- 跨消息附件和自动任务路径统一不扩展到本次开发。

## Review Focus

- 同键异输入、跨操作回执和重放不得新增任务或改变目标。
- 已接纳任务不依赖聊天存活，后台仍逐次校验库授权。
- 维护不得取得原件导入、原件整理或重试能力。
- 查询报告只返回当前成功维护尝试，不回传失败或旧尝试内容。
- 附件原件真正保存，生成仍走统一文档管线；模型配置与原始要求完整冻结。

### Task 1: 知识任务聊天入口

**Files:** `server/services/knowledge/maintenance.go`、`service.go`、`service_test.go`、`direct_maintenance_test.go`，`server/internal/agentcapability/contract/{knowledge,output,display,catalog_test}.go`。

**Interfaces:** 保留 `knowledge.jobs.start`；新增 accepted 命令 `knowledge.wiki.maintain`，输入 `libraryId/backend/connectionId/message`；两者调用 `Service.Organize`。

- [x] 修改文档整理测试，断言继承本轮全部模型配置并保留原文与补充要求；新增维护接纳、重放、边界和独立 ACP 测试。
- [x] 运行上述测试，确认因缺少行为失败。
- [x] 共用知识域聊天任务接纳逻辑，按明确命令分派，维护不接受原件选择；从 Execution 冻结模型配置。
- [x] 定向测试通过，核对聊天执行服务无修改。

### Task 2: 结果闭环与技能分流

**Files:** `server/internal/knowledge/read_provider.go`、`read_test.go`，`server/internal/agentcapability/contract/output.go`，`skills/ally/{SKILL.md,references/wiki-guide.md,references/wiki-maintenance.md}`，`server/cmd/allycli/README.md`，`docs/architecture/knowledge-cn.md`。

**Interfaces:** `knowledge.jobs.get` 追加 `report`，仅当前成功维护任务返回持久结果；所有入口共用维护指南。

- [x] 写失败测试验证真实维护报告与状态边界。
- [x] 实现报告读取，更新契约、命令目录与 skill：原件整理、Wiki 维护、局部修订、组合任务、结果查询各有明确步骤。
- [x] 运行 `node scripts/builtin-skills.mjs` 更新生成资源。
- [x] 运行受影响 Go 包测试与 vet、技能生成检查、文档检查。

### Task 3: 完整评审与交付

- [x] 独立评审全部差异，修复实质问题并补充定向回归。
- [x] 核对所有需求和证据，交付代码及真实验证边界，不推送或合并。

## 验证记录

- 回归测试先失败：缺少维护入口，原件整理错误继承库默认配置并丢失原文；修复后通过。
- 报告读取测试先失败：任务完成后 CLI 仍无真实报告；复用 HTTP 的当前尝试过滤规则后通过。
- 系统账号边界先失败：Chat 的空账号可能被知识入口重新解析；显式固定为系统账号后，两类任务均通过。
- 定向 race 测试覆盖聊天文档接纳、维护驱动与系统账号继承，无数据竞争。
- 最终知识服务、读取、模型适配、能力契约、CLI 与内置 skill 共七个包测试通过；报告 API 定向测试及受影响包 vet 通过。
- 独立 skill 场景验证：旧文本未把全库维护和组合任务交给维护入口；新文本正确处理新增、维护、组合、单页修订和跨消息附件边界。
- 独立代码评审未发现阻断问题；文档中旧的“不继承聊天推理”描述已同步修正。
- 未运行真实账号或模型任务，未操作运行中的应用；完整前端与全仓库测试不属于本次定向验证。
