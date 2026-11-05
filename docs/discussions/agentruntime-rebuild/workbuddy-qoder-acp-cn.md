# WorkBuddy、Qoder 与 Qoder CN 聊天验收

本轮按[隔离重建方案](../../superpowers/specs/2026-11-04-agentruntime-rebuild-design.md)，将三个智能体的普通聊天接入 `Execution → ThreadLifecycle/Turn → ProviderRuntime → 原生 ACP Client`。NeuxBot 管理聊天、工作范围、账号租约、历史交接和规范终态。acp-go 的共享客户端拥有原生进程、会话、配置、执行、取消和交互。模型发现由同一运行时开设独立探测连接。

## 产品能力

| 能力 | 当前实现与边界 |
| --- | --- |
| 系统账号 | WorkBuddy 使用 CodeBuddy Code 的系统登录，或启动 NeuxBot 时明确提供的 `CODEBUDDY_AUTH_TOKEN`、`CODEBUDDY_API_KEY`，令牌优先。登录主体来自原生本机凭据存储。Qoder 与 Qoder CN 分别使用本地区 CLI、原生登录或对应的 PAT。当前范围限系统账号，应用内受管账号和登录流程仍未接入。 |
| 地区与启动 | WorkBuddy 启动 `codebuddy --acp`，同产品的 `cbc` 可作后备。Qoder 与 Qoder CN 各自选择地区安装。默认配置根为 `~/.codebuddy`、`~/.qoder`、`~/.qoder-cn`，分别对应 `CODEBUDDY_CONFIG_DIR`、`QODER_CONFIG_DIR`、`QODERCN_CONFIG_DIR`。两个 Qoder 地区的配置根和 PAT 独立。设置保存拒绝跨地区配置根别名冲突；已有旧配置在下次保存时检查，不自动迁移。账号服务冻结实际凭据和配置根，拒绝会覆盖该选择或工作范围的启动参数。 |
| 模型、强度与权限 | 刷新目录时按所选账号读取模型和已声明的权限。WorkBuddy 在独立临时配置根内用空会话逐个读取已列出模型的强度，目录只接受选中模型的原生回执。Qoder 读取当前账号实际公布的模型和配置。缺少默认依据时保留未知，选择默认时省略未设定的产品覆盖。标准 ACP 未给出重置语义时，省略选择表示保留原生当前值，不伪造恢复出厂默认。Qoder 将误标为模型类别的 `reasoning_effort` 识别为强度，保留原生配置 ID。Fast 只在该模型明确报告时开放。默认、自动审批与完全访问按原生权限目录映射和验证。应用已经授权的额外目录随 CLI 的 `--add-dir` 启动参数传入。 |
| MCP 与技能 | 执行资源沿同一所有者交付，使用原生支持的 MCP 传输和受管工作区中的技能。MCP 描述随建会话或恢复传递，加载是否成功需以实际工具调用或原生证据核对。WorkBuddy 的原生 `AskUserQuestion` 交给产品表单，并等待原生确认答案。普通工具审批继续沿标准 ACP 处理。 |
| 续聊、取消与恢复 | 热续聊沿用已确认的原生会话，每轮提交新增输入。取消作用于捕获的执行。冷启动仅使用原生声明的恢复方法。WorkBuddy 2.151.0 可能对不存在的会话返回空回执，缺少同会话历史回放或确切 ID 的回执会以 `RuntimeProtocolIncompatible` 失败。用户明确选择新原生会话时，现有交接支持最近完整对话，默认五轮，也可选择自动总结。该错误不会自动开新会话。 |
| 用量与额度 | 每个规范原生终态计入一次调用。Qoder 1.1.53 的 CLI 子进程启用原生 Token 报告。回执相符时统计输出 Token 小计，输入总量和原生总量保留为观测证据。未核实版本及历史零值继续保持未知。WorkBuddy 2.151.0 的上下文用量和单次模型请求样本单独保留，原生积分不计为货币金额。Qoder 的独立账号额度读取已在 acp-go 实现并核对主体。NeuxBot 当前没有账号额度 HTTP 接口或页面，现有用量页展示聊天调用消费。 |

ACP v1 对 `PromptResponse.usage` 的字段与结构体注释分别描述轮次和累计用量，已知实现采用了不同口径。Qoder 1.1.53 默认隐藏 Token 计数，归零回执不作为已知消费。适配器比较同轮标准用量和原生配额回执后确认输出小计。原生输入可能包含缓存 Token，缺少拆分字段时只保留输入总量观测。 [ACP 协议问题 #1860](https://github.com/agentclientprotocol/agent-client-protocol/issues/1860)

WorkBuddy 的原生 Teams、MCP UI、即时插话及未协商的历史操作仍有独立协议边界。标准客户端不会转发可能返回凭据的无类型扩展。Qoder 的模型、权限、附件和工具支持以当前地区版本实际声明为准。

## 本地启动与手工验收

将候选 acp-go 仓库放在 NeuxBot 主目录的相邻位置 `../acp-go`。从 NeuxBot 根目录运行以下命令。已有本地 `go.work` 时只执行 `go work use ./server ../acp-go ../acp-go/sdk`。根目录的 `go.work` 与 `go.work.sum` 为忽略文件。

```sh
go work init ./server ../acp-go ../acp-go/sdk
export ALLY_DATA_DIR="$(mktemp -d)"
pnpm dev:web
```

在浏览器打开终端打印的地址。保持这个终端的 `ALLY_DATA_DIR`，冷恢复验收时使用同一目录重启 Web 和 Core。使用对应 CLI 已登录的测试账号，或从启动终端提供对应凭据。应用、登录和真实模型验证由验收者操作。

| 操作 | 核对 |
| --- | --- |
| 分别选择 WorkBuddy、Qoder 和 Qoder CN 的系统账号并刷新目录 | 每个地区的模型和权限来自对应安装。选择两个具有不同强度能力的模型，确认菜单随模型变化，缺失 Fast 时不提供该设置。 |
| 发送一轮固定短语，再让智能体复述上一轮短语 | 同一聊天完成两轮。确认不同强度选择可应用，聊天记录中每轮用户消息只出现一次。 |
| 在测试目录触发一次可逆工具操作并取消另一轮进行中的回复 | 默认权限按原生规则触发审批，支持的自动审批与完全访问按所选模式执行。取消后再发送一轮，前轮迟到事件不改变新一轮。 |
| WorkBuddy 触发一次内置提问，分别测试回答和取消 | 界面显示可填写的表单。提交后模型使用实际回答继续。取消后，迟到答案不再进入执行。 |
| 在各智能体中使用一个测试 MCP 工具和一个项目技能 | 核对工具响应与技能产生的可识别结果，区分已声明与实际可用。 |
| 同一数据目录重启并继续原聊天 | 原生历史有证据时续聊且不重复消息。WorkBuddy 遇到不明确回执时保留原聊天并报告失败。另行显式选择新原生会话后，核对最近对话或总结交接。 |
| 打开用量统计 | 核对调用次数、已知输出 Token 及覆盖缺口。仅有上下文或范围未确认的原生样本时，用量保持未知。 |

## 实现与测试证据

- acp-go 的 `ba1047b` 为 WorkBuddy 接入，`cc89af2` 与 `137979c` 为地区 Qoder 账号管理和诊断脱敏，`81b06c5` 为有界模型发现，`02c3221` 保留口径未知的原始用量。直接 Go 和公共 ACP 测试共用合成原生子进程，覆盖问答、取消、原生失败、配置回显、地区凭据和 Qoder 额度主体。
- NeuxBot 的 `3cb8dbd2` 与 `2f47831d` 冻结系统账号和启动范围，`5e9e847e` 在设置保存时校验账号与跨地区配置根。[账号测试](../../../server/services/aiaccount/acp_accounts_test.go)覆盖凭据优先级、地区隔离、主体变化与无效选择。[执行测试](../../../server/services/execution/native_acp_test.go)使用 Core、规范存储和合成 ACP 子进程覆盖两轮续聊、取消、已验证冷恢复、权限及用量投影。
- 用户已明确由其进行真实模型、登录、MCP 服务及 UI 验收；本机 CodeBuddy 2.180.0 通过隔离 Go 探针确认 17 个模型的专属强度和完全访问回执，DeepSeek-V4.1-Flash 返回 low、high、max，原设置文件内容一致。Qoder 1.1.53 的同账号 ACP 和 CLI 目录均返回 Qwen3.8-Flash，原生强度为 xhigh、low、medium、none。两项探测都没有发送 Prompt。WorkBuddy 的问答与用量含义另外核对了该版本发布入口 `dist/codebuddy.js` 的 `handleToolApproval`、`handleResolveInterruption`、`loadSession` 和 `AcpUsagePublisher.doPublish`。

固定 OpenBot 参考为 `f44dbe116c89a944f8d7d085e092c967b3fce0df`。通用 ACP 的标准行为与厂商差异见 `src/backend/acp-flavor.ts`，有界目录探测见 `src/backend/acp-configuration.ts`，会话恢复和用量分离见 `src/backend/acp-client.ts`。这份 OpenBot 快照没有 WorkBuddy 或 Qoder 的专属适配。Go 实现依据对应厂商协议和版本证据独立编写。

[CodeBuddy ACP](https://www.codebuddy.cn/docs/cli/acp) · [CodeBuddy 身份与访问管理](https://www.codebuddy.cn/docs/cli/iam) · [Qoder ACP](https://docs.qoder.com/zh/cli/acp) · [Qoder 模型与推理强度](https://docs.qoder.com/cli/model) · [Qoder CLI 认证](https://docs.qoder.com/cli/authentication) · [Qoder CN 认证](https://docs.qoder.cn/cli/authentication)
