# AgentRuntime 重建：Cursor 原生 ACP 模块

> **For agentic workers:** 使用 subagent-driven-development 执行完整模块，完成后独立审查。

**Goal:** 用唯一原生 ACP 路径重建 Cursor Adapter，移除 PTY、项目 hook 与 SQLite 检查点执行机制，交付准确的能力、配置、交互、故障和配额事实。

**Architecture:** Cursor 只拥有本 CLI 的发现、启动、版本特例、原生配置和独立配额读取；标准连接及会话通用机制由主线统一维护。产品的公开会话、账号调度、文本指令兼容、技能复制、存储和计价归后续运行底座。

**Tech Stack:** Go、acp-go SDK、原生 `cursor-agent acp`、合成 ACP 子进程及本机 HTTP 测试服务。

**Spec:** [云端方案](../specs/2026-11-04-agentruntime-rebuild-design.md)、[P0 矩阵](../../discussions/agentruntime-rebuild/p0-provider-matrix-cn.md)、[五项审查与并行边界](../../discussions/agentruntime-rebuild/module-review-cn.md)。

## Global Constraints

- 用户授权 Codex、Claude、Cursor 在独立 worktree 并行；开发和架构审查只用 GPT-6.1 Sol high。实现者不派生子代理。
- 完整模块重构、验证、提交后审查，不把旧 Agent 包装或仅更名当成果。旧路径只提供行为参考，新入口没有 PTY fallback。
- 实现依据为官方协议、锁定依赖版本和本项目实际调用链，测试使用独立样本。
- 英文代码与测试注释，公开注释以标识符开头；文件 snake_case，Options、状态和专属 helper 高内聚，接口必须有真实消费者。
- 本分支拥有 `pkg/cursor` 及必要直接构造调用适配。`pkg/nativeacp`、SDK、共享 acpmeta 和 NeuxBot 上层由主线统一修改；需要变更提出具体证据和合同，不复制公共执行器或各建同义接口。
- 不使用真实账号、登录、付费模型或 UI；不推送、合并或发布；仅运行相关模块和直接消费者检查。

## 文件与职责

| 文件归属                              | 完整职责                                                                                                                                                    |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pkg/cursor/agent.go`                 | ACP 组合、实际 CLI 身份与启动、该 provider 的生命周期集成；不拥有第二执行进程或业务队列                                                                     |
| `pkg/cursor/session_configuration.go` | 原生模型列表、参数、配置请求与回执、进程级模型状态及并发准入依据；整合旧零散模型启动/选项逻辑                                                               |
| `pkg/cursor/notifications.go`         | 版本有据的完整噪声块、认证失效、原生错误及配置通知；若与主对象状态同生命周期则合回 owner，不强制建文件                                                      |
| `pkg/cursor/auth.go`                  | 原生登录/登出/状态与精确凭据来源；账号数据库和租约留宿主                                                                                                    |
| `pkg/cursor/account_usage.go`         | 独立 HTTP 配额读取、凭据来源约束、原生计数/时间转换和失败；不含周期缓存或跨账号退避                                                                         |
| 真实权限与表单协议边界                | 复用/重组已有可验证的 Cursor 反向请求，保留工具与问答身份，不再依赖 hook/PTY 事件补丁                                                                       |
| 删除的旧执行归属                      | interactive、terminal、hooks、hook lease、store、restore、PTY context、检查点 fork 及只被其消费的类型与测试；逐一核对消费者后删除，不误删其他 provider 依赖 |

表中是职责边界；编码前用真实源码确认最终对象和文件，不能机械按行拆文件。标准 ACP 的连接/等待/状态改动交主线单一所有者。

## Review Focus

- 两个会话选不同模型时，进程级设置不能串用；失败或迟到 ACK 不能恢复错误的 observed 值。
- 原生提交未知、带工具活动的断线和恢复失败，不能转去旧 PTY 或复制数据库重放。
- API Key 与系统原生账号并存时，不能将默认系统 token 的 quota 归给所选 API Key。
- 噪声过滤只能匹配完整已知块，引用同句话的普通正文必须保留；失效登录不能伪装成功回答。
- MCP 静默漏载、没有原生 usage/fork/compact/历史能力时，不能以旧 hook/checkpoint 证据宣称支持。

### Task 1: 完整重构 Cursor 原生适配

**Read first:** 阅读本项目对应 CLI 的原生适配器、运行时调用链和相关测试，并核对官方协议与锁定依赖版本。

**Scope:** `pkg/cursor` 完整 owning 重建、模块 README/来源与必要测试，`cmd/acp-agent` 等实际 Cursor 构造消费者的必要编译更新。不能修改其他 provider、`pkg/nativeacp`、SDK、共享 acpmeta 或 Neux 产品；这些需求交 root 统一裁决和实施。

**Interfaces:** 使用当前 `nativeacp` 的真实标准协议边界以及已存在的 callback/configuration 接入点。共享 account/configuration/error 合同从主线批准版本消费，缺口先提出具体数据来源与消费者。上层不能需要解析 Cursor 私有报文、PTY 文本或 SQLite 状态才能使用新 Adapter。

**Requirements:**

1. 编码前写 owning 表：唯一状态、主调用链、部分资源与失败释放、原生协议依据及差异原因。原生 ACP 创建、两轮运行、交互、完成、取消、退出构成唯一执行路径；无双写和自动旧执行器 fallback。
2. 识别真正的 `cursor-agent`，不误用编辑器 `cursor`。用户显式路径、完整环境、cwd、前置参数与版本读取一致；通用 `agent` 候选须有 Cursor 身份依据。保留未安装、无效路径、启动失败和超时的区别。
3. 模型目录和参数保留原生 ID、分组、默认、未知与空，不能从名字推测 effort/Fast。进程级模型设置由连接作用域串行化：有活动回合时拒绝不兼容修改或使用独立连接，不允许其他会话暗改已接纳配置。设置请求、ACK、实际观察各自有证据，重启清除旧观察。需要通用 owner 支持时交主线，不本地复制连接。
4. new/load/resume、额外根、MCP、fork、compact、原生历史只按真实能力与响应交付。旧 SQLite seed 和数据库复制不代表原生恢复或 fork。项目 hook、PTY `/context`、会话复制不保留为备用路径。默认原生配置仍由 CLI 控制，不能据设置目录推断凭据隔离或授权边界。
5. 复用标准消息/工具/审批/表单，保留精确身份、顺序和原生终态；原生 close/cancel 不支持与已完成分别报告。背压、慢消费者、进程退出、迟到代次通过真实公共连接行为验证，必要修复明确反馈主线。
6. Cursor 已知异常处理必须有版本/形状证据及反例：当前 session ID 的精确缺失、完整噪声块、登录失效与需重启。任何未知接纳、工具后失败不得自动再次提交。错误保留阶段、原生 code/data、安全诊断和已观察事实，清理故障不覆盖原始终态。
7. MCP 禁用或未到达不能当成功，输出实际加载证据或无法观测的限制。不能未经授权修改用户配置的禁用名单。原生 cwd/额外目录不等于工具已被限制；无能力实现要求时准确拒绝。
8. 认证使用真实原生命令/能力。`CURSOR_CONFIG_DIR` 只是配置目录证据；不要虚构额外账号隔离。默认账号、API Key 与明确不可用的操作分别描述，token 驻留进程时的重启需求返回为事实。
9. 原生 ACP 未提供 Turn usage/Context 时保持未知，不用旧 hook 数字或估算补成真实计数。独立 Dashboard quota 属于非标准读取：技术入口显式带已选择的凭据来源/身份绑定，由宿主选择，不零参数暗读默认 token 并归到别的账号。生产读取和测试凭据通过实际窄端口注入；错误/缺失/零、窗口、reset、币种/余额不混淆。只读测试使用虚构 token 与本机 HTTP 服务，不读取用户 Keychain。
10. 模块说明准确描述 Go 模块的职责、原生协议限制和真实未验收边界。清理所有已切路径的废弃状态、转换和分散 helper；不得以接口存在或 README 声明代替可运行功能。

**Steps:**

- [ ] **Step 1:** 检查干净基线并运行 Cursor 定向包；读固定参照与当前消费者，保存职责表、源证据及取舍，向 root 提出必需共享合同需求。
- [ ] **Step 2:** 建立独立 fake ACP 子进程与 `httptest` 样本，覆盖两轮/工具/权限/表单、进程级模型竞争、错误形状反例、无 PTY/hook/store 访问、配额身份与零/缺省。记录实际 RED，使用本项目独立测试样本。
- [ ] **Step 3:** 实现单原生执行并删除被替代路径。涉及共享层的问题先保留可运行复现并交主线，其余 Cursor 工作继续，不另造公共层。
- [ ] **Step 4:** `gofmt -w` 修改文件，`GOWORK=off go test ./pkg/cursor -count=1` 和受影响构造消费者；针对配置/取消/退出改动跑命名 race。共享主线同步后再验证实际集成，不例行全仓测试。
- [ ] **Step 5:** 按五项标准自审、更新真实能力文档，完整模块立即提交。报告 base/head、实际命令结果、公共合同版本、未解阻断及验收层级，交 root 独立 review。
