# Codex 最终结果验收与最终性修正计划

> 历史阶段记录：本文的目录、独立 Go module 方案、迁移顺序和验收结果均以当时为准，不作为当前实现或重新验收的依据。当前执行能力已归入 `server/internal/agent/{core,host,environment}` 并属于 `server` 主模块；会话、启动与认证所有权以[职责收拢 Spec](agent-runtime-boundaries-spec-cn.md)及[当前架构说明](../architecture/runtimes-and-accounts-cn.md)为准。

> 执行方式：subagent-driven-development；复杂实现与评审使用 Sol 6.1 xhigh，独立目录任务可并行，协调者串行提交。

**Goal:** 用真实 CLI 验证阶段 B 门槛，修正最终输出证据，满足实际迁移门槛后替换知识生成入口。

**Architecture:** 原生协议和消息最终性归独立 acp-go；Agent Runtime 只消费公开 ACP 元数据。知识生成只消费最终结果，中间允许正常工具调用；知识入口独立启动一个 CLI 会话，传入任务输入并收集最终回答；不主动注入 AGENTS.md、Skill 或 MCP，不接管 CLI 自身的发现、加载和工具执行。知识接入和旧代码删除依赖实际迁移验收。

**Tech Stack:** Go 1.25.8、公开 ACP SDK、Codex app-server、回环假 Provider。

**Spec:** [Runtime Spec](agent-runtime-refactor-spec-cn.md)、[分阶段计划](agent-runtime-implementation-plan-cn.md)。

## 当前决策与门槛

保持 Codex 优先；知识宿主接入已实现，两个独立修复已推送并固定真实版本，已完成公开依赖组合验证。当时 Chat 和其他未迁入口保留旧实现；后续迁移状态见上述当前计划。

此前按“全部禁工具”设计的探针已删除；正常工具循环属于 CLI 自身行为。真实 CLI 连接本地虚构 Provider 的验证不使用真实账号，也不能替代 OAuth/平台/真实模型验收。

## 全局约束

- 不创建 Runtime HTTP，不复制厂商协议或进程代码，不增加永久兼容层。
- 不修改用户凭据、登录状态、运行中的应用或其他 dirty 文件。
- acp-go 保持独立仓库和独立提交；未发布版本不写入可提交依赖，不提交绝对路径 replace。
- 新增和修改 Go 结构体的全部字段有中文注释，生成文件只用对应生成命令修改。
- 只运行修改包与直接影响的测试、vet/race；原生测试显式选择 CLI 路径且有超时。
- 本地 Provider 只接受虚构输入，配置、HOME 和工作目录使用临时目录；日志不包含真实认证信息。

## 评审重点

- 缺 phase 的完整文本不能被包装为 final；完成事件到达后才出现的阶段不能丢失。
- 非流式、流式、多消息和空最终消息均保留同一原生 messageId；不重复输出正文。
- 历史 plan 和 partial_answer 不能成为严格或 best_effort 最终候选。
- 旧版本适配器没有证据标记时保守处理，不能从成功 end_turn 推断正文最终性。
- 不探测或限制原生工具清单和 MCP 加载；真实 CLI 验收聚焦独立调用、取消退出、工具循环后的最终结果。

### Task 1: 独立 acp-go 的 Codex 最终性证据

**Files:** 独立 acp-go 的 `pkg/codex/event_handler.go`、对应行为测试及该模块现有说明。不得修改 SDK 生成协议或其他 Adapter。

**Interfaces:** `_meta.codex.phase` 保留原生阶段；新增 `_meta.codex.phaseVerified`（bool，只有原生 started/completed 中明确提供阶段才为 true）和 `_meta.codex.messageCompleted`（bool，仅原生 item/completed）。这些字段是公开 ACP 扩展，不包含业务状态。历史 plan 使用 commentary 且 phaseVerified=false。

- [x] 新增失败回归：缺阶段非流式完成、delta 后完成项首次提供 final、started 阶段继承、partial 透传、plan 非最终、空最终消息、无重复正文。
- [x] 改为缺阶段保留未知；流式完成发同 messageId 的空文本边界和已知阶段；非流式完成一次发送正文与边界。
- [x] 运行 `go test ./pkg/codex`、`go vet ./pkg/codex` 及事件相关定向 race；报告 RED/GREEN 与外部提交边界。
- [x] 独立复核并本地提交；不推送、不合并、不修改 NeuxBot 固定依赖。

### Task 2: Runtime 消费可信阶段和完成边界

**Files:** `packages/agent-runtime/driver/acp/session.go` 及 ACP 测试；必要时修改 output 的实际候选撤销逻辑及测试。不得改宿主入口或依赖版本。

**Interfaces:** 消费 Task 1 的三个元数据字段；只有 phaseVerified=true 的 final_answer 映射为 PhaseFinal。明确 commentary、partial_answer 均是非最终；messageCompleted 映射为 Complete，不单独构成 CompletionVerified。

- [x] 先复现旧适配器伪 final 被采纳、late final 被忽略和 partial 被 best_effort 采用的问题。
- [x] 实现公开元数据转换；缺失/错误类型证明标记不升级最终性，不因 turn end 猜测最终答案。
- [x] 调整测试 Fixture，使其显式表达来自原生的阶段证据；保留原生缺证据、错误证据、非最终与正常最终的行为回归。
- [x] 执行 ACP/output 定向 test/vet/race，独立复核后由协调者提交。

### Task 3: 可重复的真实 CLI 验收与迁移决策

**Files:** Runtime 的显式启用原生验收测试/测试夹具、模块说明、本计划；旧文本测试作为读取参考，不搬生产实现。协调者负责。

**Interfaces:** CLI → 本地 Provider 的真实请求和公开 ACP 输出。固定版本模块用于证明当前结果；Task 1 工作树仅通过临时 modfile 做组合验证，不进入提交依赖。

- [x] 实际记录 CLI 版本；使用虚构认证、临时工作区和本机 Provider 验证输入与最终输出。
- [x] 验证真实工具执行后 CLI 继续模型回合；只返回 final，unknown/partial/plan 不冒充最终答案，原生进程有界关闭。
- [x] 删除配置污染、MCP 加载与工具清单探针，不为知识调用新增原生配置治理。
- [x] 更新实际能力文档、执行定向检查和最终复核，保留可重复证据及独立 acp-go 提交引用。

## 后续 B 迁移约束

最终性修正需有可获取的固定 acp-go 版本并通过对应集成验证，才能完成生产切换。配置隔离和禁用全部工具不是知识迁移门槛。迁移必须覆盖实际模型/推理回执、缓存用量及未知用量、无活动超时、凭据共享执行租约、刷新协调和清理失败不触发知识模型重放。每个已迁入口的旧职责同时删除；当时维护 Runner、普通 Chat 和未迁 CLI 的共享职责仍由原实现拥有；后续已迁移，见上述当前计划。


## 已取得的真实证据

测试平台为 macOS arm64，Codex CLI 0.163.0。链路为独立 Runtime → 公开 ACP → 真实 Codex → 本地虚构 Responses Provider；没有使用真实模型账号。环境、工作区及 CLI 进程启动目录均为临时目录。版本探测和长期进程使用同一测试启动器，不继承测试源码目录。

| 验证项 | 当前观察 | 结论 |
| --- | --- | --- |
| 输入 | 任务系统输入和用户原文到达真实 CLI 模型请求 | 不注入产品 AGENTS.md、Skill 或 MCP |
| 工具循环 | 真实 CLI 执行测试 shell，工具结果进入第二次模型请求，最终只返回指定回答 | 同一 CLI 回合内正常执行，宿主没有重放 |
| 最终输出证据 | 明确 final 及完成时重分类 final 可用，commentary/partial/缺 phase 不被采纳 | 原生最终性 5 个场景通过，已使用公开固定依赖复验 |
| 当前固定依赖 | acp-go `3a7c512df7a1`、neux-knowledge `1fc1fd660983` | 原生最终性与结构化禁止重试均已接入 |

独立 acp-go 修复 `3a7c512` 已推送到 `codex/runtime-finality`，知识核心修复 `1fc1fd6` 已推送到 `codex/runtime-result-retry`。实际固定版本分别为 `v0.0.0-20261027080232-3a7c512df7a1` 与 `v0.1.2-0.20261027090513-1fc1fd660983`；同仓 Runtime 使用相对模块引用，无临时绝对替换。

复现入口是 `packages/agent-runtime/driver/acp/codex_native_test.go`。设置 `NEUX_RUNTIME_CODEX_CLI` 为 CLI 绝对路径后，在独立模块运行：

```sh
GOWORK=off go test ./driver/acp -run '^TestNativeCodex' -count=1 -timeout=180s -v
```

未设置变量时跳过原生用例，普通模块测试不覆盖这些真实能力。未验证真实账号刷新、模型质量、其他系统平台或生产功能切换。

外部 acp-go 的事件回归、vet 和事件 race 通过；完整 pkg/codex 测试被既有 WorkBuddy 字段中文注释审计失败阻断，该失败已在修复前基线独立复现，本轮未修改无关文件。

阶段 B 的实现已删除旧 Codex 文本配置净化路径、TextOnly/禁工具耦合及 isolated_text_generation 前后端筛选。Codex 知识入口改为独立 Runtime；当时其他 CLI 通过既有执行实现返回最后结果，Chat 和 Wiki 维护保留原执行路径；后续这些入口已迁移。宿主负责所选账号、原生进程退出与租约回收，不管理 CLI 自身工具或 MCP 加载。

验证覆盖真实 CLI 工具循环及五种输出阶段、宿主路由/取消/清理不重放、账号共享租约与回收、未迁 CLI 及维护回归；七项前端选择测试、定向格式、文档与生成契约检查通过。服务模块经过正常 go.mod 的 readonly 编译检查；真实 Codex 工具循环及五种输出阶段、宿主和结果适配测试已使用公开固定依赖复验通过。

阶段 B 的 Codex 知识结果调用接入已完成。后续先执行[全 CLI 接入计划](agent-runtime-all-cli-plan-cn.md)，再推进其他业务入口。真实账号刷新、真实模型质量及其他平台尚未验收。
