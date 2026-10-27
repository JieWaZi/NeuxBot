# Codex 严格纯生成验收与最终性修正计划

> 执行方式：subagent-driven-development；复杂实现与评审使用 Sol 6.1 xhigh，独立目录任务可并行，协调者串行提交。

**Goal:** 用真实 CLI 验证阶段 B 门槛，修正最终输出证据，满足门槛后才替换知识纯生成。

**Architecture:** 原生协议和消息最终性归独立 acp-go；Agent Runtime 只消费公开 ACP 元数据。知识接入和旧代码删除依赖完整验收，不以工具发生后的取消冒充禁工具。

**Tech Stack:** Go 1.25.8、公开 ACP SDK、Codex app-server、回环假 Provider。

**Spec:** [Runtime Spec](agent-runtime-refactor-spec-cn.md)、[分阶段计划](agent-runtime-implementation-plan-cn.md)。

## 当前决策与门槛

用户已明确选择“保持 Codex 优先，暂不切换”。本轮保留知识、Chat 和其他 CLI 的生产入口，不删除仍有调用者的旧实现；不扩展 Claude 迁移。

Codex 0.163.0 实测中，旧纯生成配置仍向模型提供提问及目标管理工具；关闭 goals 后仍提供 request_user_input，关闭 default_mode_request_user_input 和 collaboration_modes 也未消除。读取或接受配置不等于模型工具列表为空。真实 CLI 连接本地虚构 Provider 的验证不使用真实账号，也不能替代 OAuth/平台/真实模型验收。

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
- 真实 CLI 的工具定义、污染标记和输出阶段分别验收，不能用一项成功覆盖另一项失败。

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

- [x] 实际记录 CLI 版本；使用污染指令/工具配置、虚构认证、临时工作区和受限 Provider 检查实际请求。
- [x] 独立断言配置污染未进入请求、tools 为空、明确 final 可用、unknown/partial/plan 不冒充最终答案；原生进程有界关闭。
- [x] Codex 禁工具不通过时保留准确失败，知识不切换、旧实现不删除；报告仍需上游能力的阻塞点。
- [x] 更新实际能力文档、执行定向检查和最终复核，保留可重复证据及独立 acp-go 提交引用。

## 后续 B 迁移约束

只有真实 Codex 禁工具门槛通过且最终性修正有可获取的固定 acp-go 版本后，才接入 runtimehost。迁移必须覆盖实际模型/推理回执、缓存用量及未知用量、无活动超时、凭据共享执行租约、刷新协调和清理失败不触发知识模型重放。每个已迁入口的旧职责同时删除；维护 Runner、普通 Chat 和未迁 CLI 的共享职责继续由原实现拥有。


## 已取得的真实证据

测试平台为 macOS arm64，Codex CLI 0.163.0。链路为独立 Runtime → 公开 ACP → 真实 Codex → 本地虚构 Responses Provider；没有使用真实模型账号。环境、工作区及 CLI 进程启动目录均为临时目录。版本探测和长期进程使用同一测试启动器，不继承测试源码目录。

| 门槛 | 当前观察 | 结论 |
| --- | --- | --- |
| 指令正文隔离 | GLOBAL/PROJECT/SKILL/CATALOG 污染标记未进入模型请求，显式 system/user 原文到达真实消息通道 | 该子项通过，不代表完整配置隔离 |
| 配置副作用隔离 | 工作目录 `.codex/config.toml` 中的测试 MCP 命令实际写出标记 | 不通过；换 HOME 和关闭指令发现不足以阻止项目配置 |
| 禁工具 | 仍声明 `list_mcp_resources`、`list_mcp_resource_templates`、`read_mcp_resource`、`request_user_input` | 不通过；无污染 MCP 的探针仍有提问工具 |
| 最终输出证据 | 独立 acp-go 修正组合下，明确 final 及完成时重分类的 final 可用，commentary/partial/缺 phase 不被采纳 | 对应原生子项通过，依赖修正尚未发布 |
| 当前固定依赖 | `2f0ad2051741` 不提供阶段来源证明 | 新内核报告 output_unavailable，不宣称当前组合支持严格生成 |

独立 acp-go 本地修复为 `3a7c512`，分支 `codex/runtime-finality`，未发布。组合验证仅通过临时 modfile 引用它；项目 go.mod/go.sum 未改变。

复现入口是 `packages/agent-runtime/driver/acp/codex_native_test.go`。设置 `NEUX_RUNTIME_CODEX_CLI` 为 CLI 绝对路径后，在独立模块运行：

```sh
GOWORK=off go test ./driver/acp -run '^TestNativeCodex' -count=1 -timeout=180s -v
```

当前整组原生验收会准确报告门槛失败；未设置变量时跳过原生用例，普通模块测试不覆盖这些真实能力。未验证真实账号刷新、模型质量、其他系统平台或生产功能切换。

外部 acp-go 的事件回归、vet 和事件 race 通过；完整 pkg/codex 测试被既有 WorkBuddy 字段中文注释审计失败阻断，该失败已在修复前基线独立复现，本轮未修改无关文件。

本轮三项开发/验证任务已完成并通过独立复核；这不表示阶段 B 的生产切换门槛已通过。知识纯生成尚未迁移，旧实现未删除，等待 Codex 配置副作用隔离与完全禁工具能力得到可重复证明。
