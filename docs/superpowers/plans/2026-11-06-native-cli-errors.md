# CLI 原生错误透传实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 所有 CLI 的已定义原生错误通过既有接口保留类型与可读原因，前端原样展示；未定义错误才使用统一兜底。

**Architecture:** acp-go 的协议边界保留已识别错误的公开原因，不把原始私有 payload 作为文案。Core 沿现有 status_error 和消息 errorMessage 字段传递同一原因，HTTP 与持久消息使用同一错误。前端优先展示接口原因，在输入框上方只呈现一次。

**Tech Stack:** Go、acp-go、ginx status_error、React、Vitest。

**Spec:** 用户本次要求：不只是 OpenCode，所有 CLI 均沿用同一错误处理与展示设计。

## 约束与审查重点

- 保留已有未提交改动，只修改错误原因的传递和本次模型修复。
- 不以配置中的所选模型推断历史用量归属。
- 不传播凭据字段、任意协议数据或无界载荷。
- 同一错误的 HTTP 拒绝、原生终态和消息投影不能丢失原因或重复展示。
- 关闭提示不删除失败事实；较旧提示不能遮住新的失败。
- 只运行对应包和前端文件的定向验证；真实 CLI 与界面验收分别报告。

## 步骤

- [x] 为公共 ACP、Codex、Claude、Pi 和 WorkBuddy 的原生原因保留补失败用例。
- [x] 在拥有原生错误的边界保留可读原因，保持既有分类和不可变回执。
- [x] Core 保留参数化和原生可读错误，HTTP 与消息投影读取同一原因。
- [x] 前端直接展示接口提供的原因，缺失或未知时才兜底；补去重和关闭回归。
- [x] 更新现有架构文档并执行相关包、前端文件、类型与契约验证。

## 验证记录

- 相关 acp-go 包与 Core 的 agent、execution、chat、messageview、status_error 包检查通过。
- 前端六个定向文件共 178 个用例通过；类型、定向 ESLint、生成契约一致性及文档检查通过。
- Cursor 真实短调用完成并返回同轮模型与计数；OpenCode 真实调用保留免费额度耗尽原文。Gemini 0.65.0 模型切换已被真实 CLI 接受，生成在有界验证内未返回后取消，完整回复尚待验收。
- 历史恢复用例“无法证明连续性的终态、缺口与重连仍请求权威历史恢复”仍失败；使用 HEAD 中未修改的 Hook 隔离复跑也失败。未在本次错误契约修复中修改其事件归并行为。
- 本次未重启应用、未进行真实界面视觉验收，改动未提交。
