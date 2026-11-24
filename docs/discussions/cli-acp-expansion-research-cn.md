# DeepSeek Harness、Hermes Agent、OpenClaw 与 Antigravity CLI 的 ACP 接入调研

调研日期为 2026 年 11 月 24 日。本文暂将 `herness` 理解为 Nous Research 的 Hermes Agent。四个候选均有官方 Agent Client Protocol（ACP）stdio 服务端。DeepSeek Harness、Hermes Agent 和 Antigravity CLI 可沿现有本机智能体路径评估，OpenClaw 还需要建模 Gateway 连接与权限。 [DeepSeek ACP](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/acp/acp/README.md) · [Hermes ACP](https://hermes-agent.nousresearch.com/docs/user-guide/features/acp) · [OpenClaw ACP](https://docs.openclaw.ai/cli/acp) · [Antigravity CLI ACP](https://antigravity.google/docs/cli/acp)

## 官方入口与本机证据

| 智能体 | 官方入口 | 本机结果 |
| --- | --- | --- |
| DeepSeek Harness | `dsh --profile acp`，由 `@deepseek-ai/dsh` 发布。 [CLI 命令](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cli-help.md#dsh-acp) | `0.2.2-rc.1` 返回 ACP v1，公布 `list`、`resume`、`close` 与 HTTP MCP。空配置握手的图片能力为 `false`。 |
| Hermes Agent | `hermes acp`，也可用 `hermes-acp`，依赖 ACP extra。 [安装与启动](https://hermes-agent.nousresearch.com/docs/user-guide/features/acp#installation) | `0.17.0` 的 `hermes acp --check` 通过，握手返回 ACP v1，公布 `load`、`list`、`resume`、`fork` 与图片输入，认证方法为 `hermes-setup`。 |
| OpenClaw | `openclaw acp`，本机 stdio 桥通过 WebSocket 连接 Gateway。 [桥接与认证](https://docs.openclaw.ai/cli/acp) | `2026.3.13` 有 ACP 命令。安装包源码的握手公布 `load` 和 `list`，当前上游文档还列出 `resume` 和 `close`。本机未联调 Gateway。 |
| Antigravity CLI | `agy acp`，Google 于 2026 年 11 月 4 日在 CLI `1.5.0` 加入。 [官方发布记录](https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md) | 已装 `1.3.4` 实测拒绝 `acp` 参数。接入验收需要安装支持该命令的版本。 |

本机探测使用临时工作目录、配置目录及不含真实账号凭据的环境。握手仅发送 `initialize`。真实模型请求、工具执行、冷恢复与桌面体验均待验收。

## 各智能体的接入边界

### DeepSeek Harness

官方 ACP profile 面向自动化客户端，提供普通请求、取消、模型与推理强度、权限请求、通用消息与工具事件，以及 stdio 和 Streamable HTTP MCP。`session/resume` 恢复持久会话且省略历史回放，适合 NeuxBot 从自身日志展示历史。官方接口缺少 `session/load`、分叉、额外目录和原生问答表单。消息以已提交事件为准，逐字流式体验待测。ACP 握手无需登录，模型调用使用运行时的服务商凭据，官方 DeepSeek API Key 路由读取 `DEEPSEEK_API_KEY`。首版可复用系统配置并核对 `DSH_HOME`、模型路由和会话身份。 [ACP 协议与事件](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/acp/acp/README.md#protocol-contract) · [官方 ACP profile](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/bundle/acp-app/README.md) · [DeepSeek API Key](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek-api-key/README.md)

### Hermes Agent

官方适配提供流式回复和思考、工具事件、权限请求、模型选择以及客户端传入的会话 MCP。系统账号可使用现有 Hermes profile 和原生 provider 配置，首次设置可以走 `hermes model` 或原生终端设置。当前上游文档还描述由 Hermes profile 的 Gateway daemon 持有会话执行与 MCP。实现时应固定验收版本，核对运行时、进程归属、实际工作目录和已有 profile 工具。带会话 MCP 的冷恢复需验证原声明和凭据能否按该版本契约重新交付，尤其是 NeuxBot Core 重启后端点或临时凭据发生变化的情况。 [Hermes ACP 官方文档](https://hermes-agent.nousresearch.com/docs/user-guide/features/acp) · [Gateway 适配源码](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/gateway_server.py)

### OpenClaw

`openclaw acp` 将 NeuxBot 的 ACP 请求转发给运行中的 Gateway。Gateway 决定 agent、模型、工具及工作区。新会话的 `cwd` 是 Gateway 主机上的路径，通过 session key 或 label 绑定的会话沿用 Gateway 已有目录。可用同机 Gateway 验证普通聊天、取消、两轮以上续聊和冷恢复。首版若允许远端 Gateway，需要先定义远端目录与 NeuxBot 本地授权的对应关系。OpenClaw 的 `/acp spawn` 则由 OpenClaw 启动外部智能体。 [桥接模式与工作目录](https://docs.openclaw.ai/cli/acp) · [OpenClaw 启动外部 ACP 智能体](https://docs.openclaw.ai/tools/acp-agents)

桥模式会拒绝非空的会话 `mcpServers`，也没有标准模型选择选项。OpenClaw 接入应以 Gateway 原生模型与工具为初始能力，在会话接纳时校验连接器和项目授权。NeuxBot 现有用户连接器、插件连接器与内部能力各自有独立配置，不能为让会话启动而丢弃已选连接器。OpenClaw 官方文档也记录了 T3 Code 注入自身 MCP 后无法启动普通对话的实例。 [MCP 限制与公开复现](https://docs.openclaw.ai/cli/acp#t3-code-compatibility)

### Antigravity CLI

Google 官方入口提供会话模型与思考强度、权限、图片与嵌入资源、工作区附加目录、列表、关闭和冷恢复。`agy acp` 可用本机缓存登录，也有 `GEMINI_API_KEY`、企业身份和 ADC 等入口。按 Google 文档，通用客户端可用 Gemini 模型，部分第三方模型只对指定客户端开放。客户端提供 HTTP MCP 需要 `--process-per-session`，正在运行的轮次通过再发 prompt 进行 steering。两项都需实测 NeuxBot 能否保留会话资源和准确关联输入回执后再开放。ACP Registry 的 `antigravity-acp` 是 Google 单独发布的二进制，本调研的 CLI 入口使用 `agy`。 [会话、模型与权限](https://antigravity.google/docs/ide/extensions/acp/) · [ACP Registry 发布清单](https://github.com/agentclientprotocol/registry/blob/main/antigravity-acp/agent.json)

## NeuxBot 接入顺序与验收

1. 先验收 **DeepSeek Harness、Hermes Agent 和 Antigravity CLI** 的官方入口。现有 `ProviderRuntime` 持有进程、账号和资源，`acp-go/pkg/standardacp` 已为四种同类 CLI 复用共享 ACP Client。候选仍需各自的运行时登记、进程参数、账号配置根和能力映射。模型目录、登录和安装诊断沿用独立入口。 [现有运行时架构](../architecture/runtimes-and-accounts-cn.md) · [原生客户端选择](../../server/internal/agent/agent_client.go#L739-L752)
2. **OpenClaw 单独做 Gateway 联调**。先固定支持 `resume` 和 `close` 的发布版本，使用同机 Gateway 与独立配置，核对认证和 session key 隔离、实际 `cwd`、Gateway 断连错误，以及配置非空 MCP 时的明确拒绝。NeuxBot 当前仅在存在工作流能力且 Runtime 支持 stdio MCP 时注入内部 MCP，另有每轮限定的 `botcli` 能力交付路径。OpenClaw 能否使用后者取决于 Gateway 的文件和进程范围。 [现有能力交付](../../server/services/execution/agent.go#L125-L141) · [连接器物化](../../server/services/execution/native_turn.go#L429-L440) · [OpenClaw 协议验证](https://docs.openclaw.ai/cli/acp#protocol-smoke-testing)
3. 四项共用的验收线是普通聊天两轮、进行中取消、关闭并重启后续聊、错误归因，以及一次可逆的工具审批。模型、MCP、图片、分叉、steering、用量分别以原生声明和真实行为核对。工具审批、工作目录和资源可用性也需要单独确认，ACP 握手只能证明传输和能力广告。 [ACP v1 协议](https://agentclientprotocol.com/protocol/v1/overview) · [现有 CLI 验收记录](agentruntime-rebuild/native-cli-completion-cn.md)

## 可复用的开源实例

- DeepSeek 官方 `dsh-subagent-acp` 复用同一 `acp` profile，可参考进程启动、`DSH_HOME`、事件消费及取消收尾。社区 `HarnessDesk/dsh-acp` 提供更丰富的事件投影，可对照判断官方能力缺口。 [官方子进程客户端](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/subagent/subagent-acp) · [HarnessDesk 适配器](https://github.com/HarnessDesk/dsh-acp)
- `acpx` 的启动目录使用 `dsh --profile acp`、`openclaw acp` 和 Antigravity 的独立 ACP 二进制。Hermes 官方记录了 Zed 的 stdio 配置和 Buzz Desktop 的原生模型菜单。上述实例可用于核对启动、认证与模型发现。 [acpx 运行时目录](https://github.com/openclaw/acpx/blob/main/docs/agents.md) · [Hermes 编辑器与 Buzz 集成](https://hermes-agent.nousresearch.com/docs/user-guide/features/acp#host-setup)
