# Agent Runtime

独立的嵌入式 Go 执行模块，负责调用、技术会话、交互、输出与资源租约。它不拥有 Chat、伙伴、团队、Workflow、知识内容或产品数据库，不提供 Runtime HTTP 服务。

完整目标和后续切换见[实施计划](../../docs/discussions/agent-runtime-implementation-plan-cn.md)。本模块的存在不表示 NeuxBot 生产入口已经切换。

## 当前边界

- `contract`：请求值、事件、结果、Fault，以及 Driver、资源、凭据和交互端口。
- `runtime`：公开构造、Start、Invoke、Run、调用标识预分配、Cancel、CloseSession、Close。
- `internal/plan`：配置冻结、有效权限与三个摘要。请求输入变化不会改变静态上下文摘要。
- `internal/engine`：Invoke/Run 共用的接纳、会话互斥、取消、事件交付和资源生命周期。
- `internal/output`：按消息身份收集有界最终候选；没有最终证据就报告不可用。
- `driver/acp`：通过公开 ACP SDK、acpserver 和独立 acp-go 的 Codex 适配创建连接，不复制原生协议或进程实现。

依赖方向为 `runtime → engine → contract/plan/output`；`driver/acp → contract + 外部 ACP`。宿主在组合根构造 Driver 和资源端口并注入 Runtime，内核不导入具体 Driver。

## 调用与所有权

先 `New` 冻结配置，再 `Start` 开放接纳。Invoke 使用临时新会话，无产品 Observer；Run 允许 Observer 和独立交互端口，只有 Driver 声明支持时才保留热会话。两者使用同一个执行内核。

输入缺省采用严格输出、显式配置和禁止工具策略。观察交付期限与人工交互等待期限分别配置；单项交互撤销不得自动取消健康的整轮。工具、原生配置、输出和交互分别表达；必须显式请求并实际具备相关能力，不能通过某个“文本模式”开关隐式扩大权限。

调用标识由 Runtime 生成。需要启动前取消时，先 `NewInvocationID` 取得有界预留，再传入请求；未使用的预留可由 `Cancel` 释放，已经消费或取消的标识不能再次提交。取消旧调用不能命中新回合。

宿主资源和凭据 Provider 提供租约，Runtime 是相应租约的唯一持有者。热 Session 仍使用资源时不能在回合返回处释放；临时会话和非热 Driver 在调用结束后释放。Close 先停止接纳、取消并等待活动调用，再关闭 Session/Driver。外部 Driver、Provider、Observer 与交互实现必须遵守传入 Context 的取消和期限；未完成释放要返回准确故障。原生使用者关闭失败时，Runtime 保留失败项及其依赖租约，会话维持 closing 并占容量；后续 CloseSession/Close 可重试，成功释放的项目不重复调用。不能因为出现清理错误就把仍被使用的凭据交还。

执行状态、输出状态、交付故障、清理故障分别记录。原生调用完成但缺少最终消息证据时，执行仍是 completed，输出为 output_unavailable；清理失败不能将已经执行的事实抹掉。宿主据此决定业务终态，不能自动换引擎重跑。

## ACP 首个切片的限制

当前 Codex Driver 只支持新建、单次 Prompt 的独占连接。明确选择 `inherit_native` 与 `allow_all` 且宿主授予相应权限时，才能进入实际 ACP 调用；默认严格 Invoke 的隔离/禁工具能力尚未满足，准确返回 unsupported。

这一策略不证明操作系统目录沙箱。工作目录是原生 cwd，权限和原生工具仍取决于明确继承的环境；禁止把 cwd 或路径前缀称为安全隔离。

尚不支持的贡献、历史 Seed、附件、额外目录、热续聊及冷恢复准确拒绝，模型和推理配置必须经公开配置目录及回执验证。单连接只接受一次 Prompt，避免无回合身份的迟到 ACP 通知串入后续调用。

Driver 保留公开 MessageID 和消息完成边界；只有 `_meta.codex.phaseVerified=true` 的 `final_answer` 才作为原生最终阶段证据。`messageCompleted=true` 只说明消息正文结束，不能单独证明最终答案。明确的 commentary、partial_answer 和 plan 不成为最终候选；缺少证明时报告 output_unavailable，不拼接全部正文猜测。固定 acp-go `2f0ad2051741` 尚不提供该证明标记，因此真实 Codex 的严格输出当前不可用；独立仓库修正必须发布并固定版本后才能解除这一限制。

真实 CLI 的验收方法与当前阻塞见 [Codex 验收计划](../../docs/discussions/agent-runtime-codex-validation-plan-cn.md)。内核 FakeDriver、公开 ACP 本地传输和虚构 Provider 的测试不替代真实账号/模型质量验收，配置隔离与禁工具也必须分别通过。

原生关闭通过公开 `Agent.Close` 执行。Runtime 的重试不代表原生实现必然可恢复：当前固定版本 Codex 的 Close 使用一次性释放并保留首次错误，因此该错误再次返回时仍保留租约并报告清理未完成，不能伪造释放成功。原生关闭语义的后续改进继续归独立 acp-go。

冷恢复、原生分叉、安装探测、空闲会话淘汰，以及产品账号/技能/知识和 Chat 宿主接入按后续批次实现；不预建没有实际行为的公开 API。

## 验证

从仓库根运行：

```sh
node --test scripts/check-agent-runtime.test.mjs
node scripts/check-agent-runtime.mjs
```

第二个命令关闭 Go workspace，检查新模块的真实依赖图，再仅对该模块实际包的显式列表运行 vet/test。定向并发验证：

```sh
cd packages/agent-runtime
GOWORK=off go test -race ./runtime ./internal/engine ./driver/acp
```

工程保护值及性能测量边界见实施计划。测试不使用产品数据库、不操作用户工作区，不代表真实模型或生产功能已验收。

真实 Codex 验收显式启用，连接本地虚构 Provider，不使用用户账号。将变量设为本机已安装 CLI 的绝对路径，再在本模块运行：

```sh
GOWORK=off go test ./driver/acp -run '^TestNativeCodex' -count=1 -timeout=180s -v
```

变量名为 `NEUX_RUNTIME_CODEX_CLI`，未设置时这些测试跳过。验收测试会检查实际工具定义、被污染配置的 MCP 是否启动，以及原生最终阶段；它们是切换门槛，当前固定依赖和 CLI 的已知失败不能当作已通过或静默忽略。
