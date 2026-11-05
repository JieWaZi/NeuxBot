# 既有 CLI 迁移与 Token 用量核对

## 范围与源码依据

本次将既有 Kimi、Gemini CLI、Grok Build、OpenCode 和 Pi 接入 `Execution → ThreadLifecycle/Turn → ProviderRuntime`。Codex、Claude、Cursor、WorkBuddy、Qoder、Qoder CN 继续使用同一运行架构。本次新增的是既有 CLI 的原生接线。独立知识生成、Wiki 维护和工作流输出修复在正式切换时接入同一生命周期，当前通过 Claude 执行。

主要参考固定 OpenBot [`f44dbe1`](https://github.com/nightly-labs/openbot/tree/f44dbe116c89a944f8d7d085e092c967b3fce0df) 中的 `src/backend/acp-client.ts`、`acp-flavor.ts`、`acp-flavor-opencode.ts`、`pi-client.ts`、`pi-rpc.ts` 和账号、用量实现；再与原生 CLI 的版本化协议核对。Go 代码复用项目的 Process、原生 Client、输入接纳与终态合同，不移植另一个状态所有者。

## 所有权与配置

标准 ACP 的进程、通知屏障、会话目录、设置、取消、终态交付在 `acp-go/pkg/acpclient` 统一管理。`standardacp` 只持有四种 CLI 的启动及有证据的协议差异；Pi 的 RPC 与标准 ACP 不同，在 `acp-go/pkg/pi` 独立适配后提供同一 Client 合同。

宿主账号服务冻结原生根目录、凭据身份和配置指纹；ProviderRuntime 持有租约直至原生进程退出。受管 API 账号按认证世代和配置修订隔离。模型原始 ID 与 OpenCode、Pi 的 `ally/` 原生命名空间只在边界转换；原生目录中其他服务商的同名模型不能进入受管账号菜单。模型切换使用原生 setter；进程级权限变更重建连接。

Kimi 同时保留 Python `KIMI_SHARE_DIR` 与 TypeScript `KIMI_CODE_HOME` 的原生语义；Gemini 的 `GEMINI_CLI_HOME` 是包含 `.gemini` 的目录；Pi 使用 `PI_CODING_AGENT_DIR`；OpenCode 同时固定 XDG 配置、数据、缓存和状态目录，单设 `OPENCODE_CONFIG_DIR` 不视为隔离凭据。系统账号的本地身份用于发现切换，不冒充服务器验证的登录状态。OAuth 正常更新 access/expiry 不改变已有账号主体；有可信账号标识时使用它，否则保守绑定保存的授权凭据。

## 提前处理的边界

| OpenBot 或原生实现中的问题 | 当前处理 |
| --- | --- |
| 成功模型探测不等于已经登录 | 标准 ACP/Pi 目录成功不制造登录回执；Qoder 可使用账号租约独立核实的原生登录证据 |
| 不存在会话也返回空的 load 成功 | WorkBuddy 要求真实恢复证据；不足时报错，不能用新会话冒充恢复 |
| OpenCode 启动尚未就绪或只有思考没有回答 | 仅对精确错误执行有界恢复；空回答不冒充正常完成，也不猜测上游原因 |
| 通用资源缺失被当作会话丢失 | 标准 ACP 只接受对应原生会话的精确缺失证据 |
| 原生 setter 会改写默认模型 | WorkBuddy 逐项探测只发生在一次性配置根内。Qoder 目录读取保留已公布的原生配置。显式切换仍用原生 setter，凭据身份不包含可变默认模型 |
| 模型可选但可选配置尚未确认 | 模型、强度／Fast 和权限分别校验；已列出模型可使用原生默认配置，未知可选设置不制造菜单 |
| Pi 先结束执行、后返回 ACK 或丢失 ACK | 原生终态与请求确认独立处理，先到的事件和 Token 保留 |
| Pi 取消后仍有排队输入 | 先清队列，再 abort；旧版不能确认清空则退出进程，保留不确定终态 |
| 相同计数被多个结束事件重复携带 | Pi 按本轮 assistant message 消费记账，不再累加 agent_end、turn_end 或累计上下文 |
| OpenCode 同目录会话覆盖 MCP 凭据 | Neux 每个会话连接独立原生进程；不采用 OpenBot 多会话同进程的共享凭据方案 |
| Gemini 同一分钟冷恢复可能覆盖已有历史 | 恢复前验证 UUID、项目和原生目录，只为可能碰撞的文件保留独立保护副本；后续原生追加沿其实际选中路径进行 |
| 原生历史路径指向会话根之外 | Pi 验证目录包含关系和真实头部，再建立会话绑定或分叉 |
| 未确认的本地日志结算 | Kimi 区分原生失败和日志等待失败，不把缺少终态记录制造为原生失败 |

会话替换继续支持最近五个完整轮次或现有总结接力。热续聊、冷恢复只发当前输入。WorkBuddy 恢复证据不足不是“历史接力被删除”：它是拒绝不确定的原生恢复，用户明确新建时仍走现有接力。

## Token 产品合同

删除价格表、Token 计价、费用投影、费用接口字段与前端费用展示。用量页只显示 Token、调用数和覆盖缺口。原生结果中的金额可作为原始协议证据保留，但不进入产品统计。旧的可重建查询投影升级后重新读取规范终态，清除曾经只有费用的模型明细。

规范原生终态 JSONL 是消费权威，SQLite 是可重建查询投影；查询和分页共享快照。模型明细与调用总计分开，不重复累加；缓存输入和普通输入互斥，reasoning 是 output 子集。未知不按零处理，部分消费不伪装整轮完整。

| 新迁移 CLI | 消费证据与边界 |
| --- | --- |
| Pi | 当前 invocation 的 assistant message；重试请求消费保留，重复终态不累加；上下文统计单独处理 |
| Kimi | 已核实 TypeScript 2.0.2 和 Python 1.52.0 的本轮 Wire 追加区间；Python 按原生工作目录 MD5 定位，逐个请求的 StatusUpdate 消费累计；其他版本不猜测本地文件协议 |
| Grok | 原生响应中的结构化消费，按原生定义处理缓存和推理；缺字段保留未知 |
| Gemini | quota 计数中已知的本轮观测；不能从总输入推出未缓存输入，也不能把未上报用量的全零占位当作真实零 |
| OpenCode | 已核实 1.18.34 最后一个 assistant 请求为部分消费；其他版本或范围不明的数据保留观察证据，不冒充整轮 |

## 验证与用户验收

自动化覆盖账号租约及 OAuth 刷新、模型命名空间及强度目录、进程权限兼容、标准 ACP 生命周期、Pi RPC 终态、规范日志回放和 Token 查询。产品集成测试使用合成子进程，经真实 Execution、ConversationStore 和 SQLite，不调用真实模型。

用户启动测试时逐个 CLI 验证：选择账号和模型 → 第一轮 → 续聊 → 改强度／权限 → 取消 → 重启应用后续聊 → 查看 Token 及缺失提示。切换账号必须换凭据范围；模型查询不产生聊天。真实账号、安装版本、平台进程树、界面和模型效果由用户验收，不因自动化通过而标记通过。

原生能力边界：Gemini/OpenCode 没有本架构可验证的自动风险审批模式；Kimi 的 auto/yolo 以原生菜单为准；Grok 精确目标 steering 与原生仅接收 sessionId 的队列语义不兼容，因此不宣称支持；Pi 的 Fast 和账号额度没有原生合同。Pi 的 MCP 依赖随应用交付的桥接模块，注册就绪不等于每个 MCP 服务已连接，严格验证要求仍需真实原生证据。独立生成要求原生客户端能够证明工具范围。

### Gemini 0.62 强度限制

固定官方源码的 [settings.ts](https://github.com/google-gemini/gemini-cli/blob/b460678f3db508407554afd604cc9d6635becb2a/packages/cli/src/config/settings.ts#L854-L875) 和 [security.ts](https://github.com/google-gemini/gemini-cli/blob/b460678f3db508407554afd604cc9d6635becb2a/packages/core/src/utils/security.ts#L680-L726) 要求 system settings/defaults 文件及其祖先目录满足 root／管理员所有权检查。普通用户临时文件即使由环境变量指定，也不会生效；该版本 ACP 本身没有 Thinking setter。因此当前候选实现明确不提供此项强度调整，不复用旧私有 HOME、符号链接和进程重建组合来伪装统一 ACP 设置。恢复保护和正常模型对话单独验证，不把它们当作强度已验收。

## 模块交付与证据

- Token-only：`591baa18`；规范终态保留，费用产品链路删除，生成契约校验通过。
- 账号：`8b96a02b`、`3617be32`；五类账号租约与 OAuth 刷新身份；`4480accc` 删除旧模型探测与文件目录旁路。
- 宿主：`09d3839a`；11 类 CLI 接线、账号模型目录、三级权限边界、热设置失败的进程回收和持久会话恢复。
- acp-go：标准 ACP 基线 `508081b`，OpenCode 失败恢复 `1e4dc34`，原生 legacy 模型回执 `38e6945`，Gemini 历史保护 `bdcf1d5`，Python Kimi Token `b97fc08`；Pi 基线 `c4d580b`，终态读取修正 `99bff36`，已知 Token 小计 `074a41c`。

定向验证涵盖 `agent_next`、Execution、runtimeenvironment、账号与用量服务及相关 API；Pi、标准 ACP、共享 ACP 有竞态检查。前端目录／用量 17 个测试、类型检查和对应格式检查通过；Core 主包及装配包编译通过。此前执行测试的公开默认模型与其合成原生模型目录不一致，修正夹具后该包通过，不为测试放宽真实模型校验。

上述提交与测试是开发证据，不是已安装 CLI 或界面的真实验收。Gemini 0.62 Thinking 与其他未由原生声明的能力仍由各 CLI 的能力菜单决定。

## WorkBuddy／Qoder 模型与登录回归

用户界面暴露了此前合成测试未覆盖的真实形状：WorkBuddy 的 DeepSeek 模型已在目录中，但非当前模型的可选设置尚未确认。迁移后的前端把 `configurationKnown=false` 作为整项禁用条件，破坏了旧实现对模型、设置和权限分别校验的行为。当前恢复已列出模型的默认配置使用路径；未知强度和 Fast 不提供选项，显式不支持的设置仍拒绝。该行为直接对应 OpenBot `acp-configuration.ts` 对可选探测失败不移除模型的处理，不借用其他模型的强度列表。

Qoder 原生设置命令会持久化默认模型，旧适配器关闭逐模型探测。共享 ACP 的 Qoder 模型发现保持只读。WorkBuddy 使用同一探测逻辑，在临时配置根内核实逐模型强度。没有 legacy modes 时，只从同一原生回执明确声明的 mode 配置读取权限；显式切换仍需原生确认。

前端与 API 分别提交 `92654cc4`、`934ba0ec`，ACP 提交 `3f18dea`。回归覆盖截图同类模型、未知可选设置、权限独立校验及目录读取零 setter；定向测试和交叉审查通过，不代表用户真实登录或发送验收已经通过。

Qoder 宿主账号修复提交 `98ec152f`：原生确认的身份仅在同一地区、程序、工作目录、配置根及加密凭据文件快照下短期复用，缓存不负责创建身份。凭据变化、过期或明确退出登录触发重新核验；查询期间已知快照变化时最多重新读取一次，仍变化则拒绝旧回执。服务账号直接使用原生主体 ID 和可选组织 ID，不强加原生没有要求的组织字段。目录复用租约的已验证登录证据，不重复启动账号控制进程；原生 `session/new` 返回未登录时优先采信失败。

账号、运行时及目录三个受影响 Go 包的定向竞态检查通过，包括探测期间换号、退出后恢复旧凭据、双地区隔离和查询期间关闭。主目录通过本地 Go workspace 使用 acp-go `3f18dea`；尚未发布该依赖版本，不能把本地验证等同于独立远程检出的构建验证。
