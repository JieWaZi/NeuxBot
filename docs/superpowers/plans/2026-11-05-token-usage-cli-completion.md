# Token 用量与既有 CLI 迁移实施计划

> **For agentic workers:** 使用 subagent-driven-development，Sol 6.1 high 并行实施；每个模块独立提交和 review。

**Goal:** 删除产品费用功能，仅展示 Token，并将既有 Kimi、Gemini、Grok、OpenCode、Pi 迁入已确认的运行架构。

**Architecture:** 标准 ACP 复用唯一原生连接与会话所有者；Pi 使用其原生 RPC 并复用进程、输入和结果合同。NeuxBot 继续通过 Execution、ThreadLifecycle/Turn、ProviderRuntime 执行，不回退旧 Manager。原生事实进入规范日志，查询投影只统计 Token。

**Tech Stack:** Go、ACP SDK、Pi RPC、JSONL、SQLite 查询投影、React。

**Spec:** [重建方案](../specs/2026-11-04-agentruntime-rebuild-design.md)，本计划按用户当前要求替代其中的产品计价、费用统计与费用展示范围。

## Global Constraints

- 直接在两个仓库主检出的已有开发分支工作，不创建 worktree；保留此前具名 stash。
- 用户负责真实 CLI、账号、模型与界面验收；本次只编写、定向自动化检查和 review，不启动用户应用或执行真实模型。
- 读取固定 OpenBot `f44dbe116c89a944f8d7d085e092c967b3fce0df`、对应官方文档和旧实现；不把 Gemini 当作 Antigravity，不把 Pi 当作标准 ACP。
- 英文代码注释、snake_case Go 文件、按所有权保持高内聚；原生差异集中于 acp-go，宿主拥有工作范围、账号租约和历史交接。
- 不新增未注册的 CLI。本次补齐 Kimi、Gemini、Grok、OpenCode、Pi；保留已经迁移的六种运行时。
- 删除应用计价、费用投影、费用接口和费用界面；原生报告中的技术证据可在原生日志中保留，不参与产品统计。
- 缺失、不完整或范围未知的 Token 不当作零，不按上下文计数推算消费。
- CLI 替换继续使用最近五个完整轮次或既有总结交接；原生恢复证据不足不冒充恢复成功。

## Review Focus

1. 正常续聊、取消、关闭、冷恢复、模型与权限从实际产品入口贯通。
2. 与 OpenBot 的差异有厂商协议或当前产品范围依据。
3. 不增加重复状态所有者、旧执行回退、无职责转换层或碎片文件。
4. 使用真实协议形状的合成子进程复现失败；不将合成检查称为真实验收。
5. 能直接移植的标准行为在共享 ACP 中只实现一次；厂商差异不能污染其他 CLI。

## Tasks

- [x] 用量模块：删除计价和费用投影及前端展示，保留 Token 覆盖、维度和分页；重新生成公开契约，验证旧规范日志可重建 Token 投影。
- [x] 标准 ACP 家族：为 Kimi、Gemini、Grok、OpenCode 实现内聚的启动与协议配置，验证目录、有效设置、权限、恢复、错误和取消。
- [x] Pi 原生 RPC：实现独立原生客户端，区分 prompt ACK 与执行完成，接入模型、thinking、工具桥、取消、会话和 Token。
- [x] 宿主接入：统一账号选择、配置根、原生能力与设置入口，保持已有受管 API 凭据能力；核对 WorkBuddy 恢复保护与历史交接的关系。
- [x] 按模块 review、定向验证、提交；更新当前架构和用户验收矩阵，注明原生不支持和未验收部分。

## 并行边界

| 工作包 | 独占写入 | 共享边界与处理 |
| --- | --- | --- |
| Token 用量 | 用量领域、仓储投影、用量前端、生成契约 | 执行测试中的费用断言由主实现协调迁移；生成输出只有本工作包写入 |
| 标准 ACP | standardacp、经确认的 acpclient 策略钩子 | 原生 Client 合同不另建执行所有者；宿主由主实现接线 |
| Pi | agent_next/pi | 共用现有 Process、Client、NativeOutcome，不修改其他厂商 |
| 主实现 | 账号、运行时、Execution、历史交接 | 与以上具体合同对接；不并发修改其独占文件 |

上述工作包均按既有重建方案实现，不把用户“删除费用”改成隐藏费用开关，也不以启动握手代替实际产品能力。

## 用户验收回归

- [x] WorkBuddy：恢复已列出模型的默认配置使用路径，未知可选设置不阻断模型；目录查询不切换并保存 CLI 默认模型。
- [x] Qoder：处理原生用户／服务账号身份差异，减少同一凭据快照的重复账号进程启动；模型目录不改变原生默认模型。
- [x] 对上述失败形状增加回归测试及独立 review，仍由用户完成真实 CLI／登录和界面验收。

开发和自动化验证已完成；真实 CLI、登录和界面验收仍由用户执行，原生能力限制见 [CLI 迁移核对](../../discussions/agentruntime-rebuild/native-cli-completion-cn.md)。
