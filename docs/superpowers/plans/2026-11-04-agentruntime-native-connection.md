# AgentRuntime 重建：公共原生连接

> **For agentic workers:** 主实现使用 executing-plans 与 test-driven-development，完整模块提交后按五项标准独立审查。厂商实现者不得另建公共传输。

**Goal:** 将原生 ACP 的准备、实际发送、取消、终态等待和进程诊断放回同一所有者，修复公开 Cursor 调用链已经复现的竞态与泄漏。

**Architecture:** SDK 拥有单连接写入顺序与实际字节事实；nativeacp 拥有一次 Prompt 的准入、准备取消、写入状态、交互撤销及响应等待；厂商只提供准备函数和原生差异。CLI 版本、认证、运行通过同一平台命令构造入口。

**Tech Stack:** Go、手写 ACP SDK、nativeacp、合成子进程及 pipe。

**Spec:** [重建方案](../specs/2026-11-04-agentruntime-rebuild-design.md)、[五项审查](../../discussions/agentruntime-rebuild/module-review-cn.md)。

## Global Constraints

- 主实现负责共享合同，开发及审查使用 Sol 6.1 high。三家厂商 worktree 不同时写共享文件。
- 英文注释，snake_case；同一所有者的状态和辅助函数就近组织，不新增另一套 Prompt 队列或任意重放。
- 只修改手写 SDK；生成文件不手改。保留第三方声明，更新补丁说明。
- 读取固定 OpenBot 的 acp-connection、acp-client 取消与终态、cli 平台启动和 stderr-diagnostics，独立 Go 实现。
- 仅定向包、命名竞态、实际包装链合成测试；不调用真实账号、模型或 UI，不发布。

## Review Focus

- 编码尚未完成时的取消不得先向原生发送无效 cancel，随后再发 Prompt。
- 已完整写出的 Prompt 明确取消后继续等待真实终态；请求 context 取消和断线不能伪造原生完成，也不能放行旧取消会影响的下一轮。
- 部分写入不能当作未发送；必须中止损坏的连接。SDK 原生响应错误与传输错误分别保留。
- stderr 分块与多行结构不能绕过脱敏，超长行不截出秘密后半段；日志和退出错误只能引用安全诊断。
- Windows batch launcher 的空格、引号和命令解释器字符不能改变参数边界；静态构造与交叉编译不冒充 Windows 运行验收。

## 唯一所有者与接口

| 位置 | 职责与实际消费者 |
| --- | --- |
| `sdk/connection.go` | 在已有连接上提供请求写入钩子：编码在写锁外；取得写入权后检查 context，调用一次准入 commit；写入结果报告真实字节数和完整帧长度；同连接通知按同一写入顺序。nativeacp 的同一 Prompt owner 消费钩子，不把它当异步观察事件。 |
| `pkg/nativeacp/prompt_queue.go` | 收敛 FIFO 与拒绝并发两种准入策略下的同一执行算法。当前操作拥有准备 context、交互 context、取消意图、送达事实和响应等待，独立于排队请求。 |
| `pkg/nativeacp/agent.go` | 进程、连接与诊断生命周期；保留退出失败的可重试所有者。准备、运行、关闭通过同一原生句柄。 |
| `pkg/nativeacp/environment.go` 与平台文件 | 真实命令身份、环境和平台启动目标；版本、认证、ACP 共用，不通过泛型字符串 shell 拼接执行任意命令。 |

厂商直接消费以下接口：

- `Config.PreparePrompt func(context.Context, acp.PromptRequest) error`：同一原生操作登记后，在编码和提交之前运行，context 响应准备阶段取消。
- `Agent.PromptWithReceipt(context.Context, acp.PromptRequest) (acp.PromptResponse, PromptDelivery, error)`：返回同一操作不可变的实际送达事实。标准 `Prompt` 调用同一算法。
- `PromptDelivery` 的 `DeliveryNotSent`、`DeliverySent`、`DeliveryUncertain` 表达帧未写、完整写出、可能部分写出，不能将完整写出当作服务端接纳。
- `Agent.PromptCommitted(acp.SessionId) bool`：读当前 owner 是否已经经过写入准入，先于实际 Write 返回；用于锁外 pre-delivery filter，不发 I/O。
- `Config.RedactDiagnostic func(string) string`：纯脱敏函数，完整结构化记录处理后才进入诊断日志和尾部；没有脱敏器时不交付原文。
- `CommandContext(context.Context, string, []string, []string) (*exec.Cmd, error)`：版本、认证、ACP 共用的完整环境与平台命令构造。

取消在写入准入前只撤销准备和待发送请求，不发送原生 cancel。准入之后取消与 Prompt 使用同一 wire 写入顺序，撤销交互但保留原生响应等待。若原生不返回终态，有限宽限期后使同一连接失效，后续排队不得在未知原生执行上继续提交。Close 解除准备、排队、在途写和响应等待，旧代次不得取消新 owner。

## Task 1: 完整公共连接模块

**Files:** 手写 `sdk/connection.go`、相应测试与 `sdk/PATCHES.md`；`pkg/nativeacp/{agent,prompt_queue,session,environment,permission,callbacks}.go` 和必要平台文件、对应行为测试。只为实际依赖的生命周期收敛调整其他 nativeacp 文件。Cursor 配置与调用迁移由其 owning worker 完成。

- [ ] 建立 RED：确定性阻塞编码时取消、写入 gate 前取消、部分写、完整写后原生取消终态、关闭旧代次与新操作、stderr 分块秘密与超长行、平台命令参数构造。
- [ ] 实现 SDK 写入准入与结果事实；保留实际 pending owner 与通知水位，不创建第二份请求登记。调用者取消前后语义有准确测试。
- [ ] 收敛 nativeacp 的两套 Prompt 执行为一个操作所有者，将准备与实际提交放入同一流程；迁移交互/权限读取，去掉被替换的重复状态。
- [ ] 实现整记录安全诊断和统一平台目标；诊断失败不丢原生 code/phase。更新真实直接消费者。
- [ ] 验证 SDK/nativeacp 定向用例和实际 Cursor 包装链，具名 race；Windows 定向交叉编译并注明未做原生运行。与厂商 worker 在安全 checkpoint 同步。
- [ ] `gofmt`、补丁说明、模块报告，完整提交并独立五项审查。旧 SDK pin 的独立发布仍是另一道集成门槛。

## 当前实施证据

共享模块已提交 `a89003d5850c1e388d961fefcb8a5e78e28b88cd`，位于独立 `provider-native-rebuild/acp-go` worktree，待独立五项审查。SDK 保留原 pending owner 与通知水位；nativeacp 使用同一 Prompt owner，取消不再提前释放原生执行。结构化 stderr 在整记录脱敏后进入日志/尾部，64 KiB 原始预算、4 KiB 安全文本上限。

SDK 定向 race、nativeacp 直接修改包测试与定向 race 已通过；Windows 仅交叉编译通过。Cursor 已安全同步并正在完成真实包装链合成测试。远端 SDK 固定版本、真实 CLI、账号、模型及 UI 尚未验收。这个模块不代表 P2–P6 上层运行底座和产品闭环完成。
