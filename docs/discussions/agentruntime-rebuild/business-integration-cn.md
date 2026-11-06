# Agent 重构后的业务接入梳理

本文记录既有[重建方案](../../superpowers/specs/2026-11-04-agentruntime-rebuild-design.md)的业务接入、来源证据与验收边界。代码、独立复审、定向检查和全新数据重置已完成；真实模型及平台验收仍单独记录。当前布局见[工作区与存储](../../architecture/workspace-and-storage-cn.md)。

## 目录与原生授权

固定 OpenBot `f44dbe116c89a944f8d7d085e092c967b3fce0df` 的 `src/backend/agent-store.ts` 在构造器中建立 `Agents` 根，并在创建记录时用 `join(agentsRoot, id)` 保存伙伴的 `workspacePath`。同一伙伴的会话使用其固定工作目录，外部项目通过原生目录能力提供。

这不是所有后端都追加一个同名 CLI 参数：

| 后端 | OpenBot 固定源码 | 本项目实际原生落点 |
| --- | --- | --- |
| Claude | `claude-client.ts` 将 cwd 和 runtimeWorkspaceRoots 转为 SDK `additionalDirectories` | acp-go `pkg/claude/launch.go` 转成 `--add-dir` |
| Codex | `agent/workspace-sandbox.ts` 生成 `writableRoots` 与 `sandbox_workspace_write.writable_roots` | acp-go `pkg/codex/session.go` 生成原生 sandbox policy |
| 标准 ACP | `acp-client.ts` 仅向声明能力的后端传 `additionalDirectories` | acp-go `pkg/acpclient/session.go` 校验能力与目录，再创建或恢复会话 |

本项目方案要求：无法满足所请求的授权时明确拒绝，不静默丢弃额外目录，也不用提示词冒充沙箱。cwd、允许写入的根、产品文件预览权限各有职责，不能相互替代。外部项目保留真实位置。用户确认：未选目录时使用伙伴 cwd；选目录时原生支持的走原生参数，明确不支持的使用按 Chat 隔离的 projects 链接入口，不复制整个项目。入口不构成额外原生沙箱保证。

目标布局沿用重建方案第 4.1 节：`Agents/<agent-id>` 是长期伙伴默认 cwd；`Workspaces/<scope-id>` 是显式隔离的团队或任务 cwd；`Conversations/<id>` 拥有会话日志；`State` 拥有受管运行状态；`Shared` 仅用于显式共享。这里的长期 agent 身份对应业务 Waker，不是 Runtime ID，也不是 Chat ID。

目标树不意味着自动迁移所有产品数据。Electron 数据、业务数据库、知识库、工作流源码与版本等仍有原领域所有者；其路径是否调整须逐项确定，不能从 Agent 的默认目录图推导整体搬迁。本次按用户要求从全新数据进入，不实现历史兼容、导入或旧路径回退。完成代码与验证后统一清空已核实的应用自有数据，保留外部真实项目与源码。新数据的正常恢复、回放和分叉仍须完整。

## 临时文件与持久成果

用户已选择：普通聊天的持久成果统一放在伙伴目录的 `outputs/` 下，按任务区分；项目文件留在真实项目中，临时文件单独管理。目录中的任务身份复用现有执行或任务标识，不新建另一套任务事实。最终具体路径由宿主提供，不让模型猜测。

`internal/agent/developer_instructions.go` 只组合稳定指令；文件布局由业务层提供。旧 attachments/generated 路由已删除，普通新成果和本轮临时文件分别使用 outputs 与 tmp。

固定 OpenBot 的 `agent/developer-instructions.ts` 将伙伴工作目录描述为自身持久文件的位置，将 `Shared` 用于其他伙伴需要的文件，并要求自己的文件不要污染用户附加目录。其 `workspace-sandbox.ts` 允许系统临时目录供编译器、包管理器和测试使用；没有把所有生成文件统一塞入 `generated/`。`agent-store.ts` 单列 `Downloads`，`browser-host.ts` 将浏览器下载分配到其中的唯一文件名。`agent/attachment-gateway.ts` 则对成果真实路径和授权根校验后交付附件；生成位置与附件交付是不同职责。

本项目将按以下语义落实，具体目录属于业务实现而非 acp-go 协议：

| 文件类型 | 目标规则 | 生命周期 |
| --- | --- | --- |
| 用户项目文件 | 已授权真实项目位置；原文件原位修改 | 用户拥有，Chat 删除不清理 |
| 普通聊天持久成果 | `Agents/<id>/outputs/<task-id>/` | 伙伴拥有，保留来源 Chat/Execution；Chat 删除不顺带删除成果 |
| 模型主动生成的临时脚本和中间文件 | 在当前真实 cwd 内单独的任务临时范围，与 outputs 分开 | 只有原生使用已结束且无需恢复或引用时才能清理；不能按一次 Prompt 返回就删除 |
| 上传原件与交付附件 | 保持明确 Chat/Run 归属，不与临时文件共用目录 | 按原领域保留与删除规则处理；交付引用不等于复制整个伙伴目录 |
| 工作流文件 | 保留 `scripts/state/cache/tmp/runs` 的既有职责；最终业务文件默认所属 runDirectory | 工作流定义和 Run 分别拥有，不能机械改到普通聊天 outputs |
| CLI 自身缓存与宿主私有状态 | 由原生客户端/账号租约与 State 范围管理 | 不作为可交付成果或普通文件授权根 |

稳定 prompt 说明文件类别与伙伴目录；本轮任务路径通过本轮上下文给出，避免每次任务 ID 改变都重写静态前缀。文件已经明确属于某个成果时，后续修订沿用其真实路径，不因新一轮 Execution ID 自动搬迁。只把确认的最终成果交付给用户，不将暂存文件自动标记为成果。

`internal/workflow/dsl/sdk_knowledge.go` 已清理旧的通用项目链接与原生指令文件加载描述，指南通过生成命令同步。`services/workflow/worker.go` 继续明确工作流自己的 scripts/state/cache/tmp/runDirectory 合同，未将业务成果机械改到普通聊天目录。系统工具使用的 OS 临时目录与模型主动创建的业务中间文件也需分别表达，不能声称 prompt 能约束所有原生 CLI 缓存行为。

## 指令组成与收敛范围

当前普通聊天的静态指令由 `services/execution/work.go` 收集，`services/execution/agent.go` 补充能力说明，最终由 `internal/agent/developer_instructions.go` 组合。下表区分实际交付位置；并非每个场景都包含全部内容。

| 内容 | 当前交付位置 | 收敛要求 |
| --- | --- | --- |
| 目录职责与本轮路径 | 稳定说明保留用途，本轮路径进入 ContextInstructions | cwd/outputs/tmp 与项目入口均来自宿主实际准备，不用文本模拟授权 |
| 伙伴身份、风格、工作方式 | 静态 IDENTITY.md、PERSONA.md、PLAYBOOK.md 段 | 保持用户档案事实；当前只遍历三种 EditableKinds，没有自动附加 MEMORY.md |
| 项目根指令文件 | 开启 LoadInstructions 的项目，其对应 CLI 文件内容进入静态指令 | 按原生发现行为核对重复读取，不假定附加目录参数等于加载项目指令，也不一律再注入全文 |
| 团队、工作流业务说明 | TEAM.md 稳定角色保留，回合模式及工作流本轮目录进入 ContextInstructions | 不让任务数据改变稳定配置 |
| 软件能力与创作指南 | ally 能力发现说明进入静态指令；工作流创作入口另加完整创作指南 | 保留必要入口与业务约束，核对指南、每轮提醒和工具说明的重复职责 |
| 当前输入与授权上下文 | 用户输入中追加附件、代码引用、选中技能/连接器/插件及本轮 ally 上下文 | 保留精确输入和本轮授权引用，不把变化的执行路径移回静态前缀 |
| 工作流引用、结果协议、历史接力 | 对应场景的本轮输入 | 与用户正文的持久记录区分；按场景交付，不作为所有聊天的常驻提示 |

原生 CLI 自身的系统指令、自动发现的项目文件、技能和工具 Schema 另由 CLI 管理。Codex 的 developer 指令、Claude/Pi 的原生 system append 与通用 ACP 的 prompt prefix 不是相同的消息角色。通用 ACP 的兼容前缀也不应描述为每轮重复发送整套系统指令。

固定 OpenBot 的 `agent/developer-instructions.ts` 同样包含工作目录说明：伙伴 cwd 存放自己的持久文件，Shared 用于明确共享，用户附加目录保留用户归属。它还根据实际 CLI 读取能力决定是否补入工作区指令文件。`agent/thread-lifecycle.ts` 针对 Claude 将变化的记忆与同组伙伴名单单独交给回合上下文，避免修改系统前缀导致整段会话缓存重写；不能把这一行为泛化成所有后端均如此。

因此收敛目标是减少重复规则、明确静态与动态边界，而非清空目录说明或新建一套 Prompt Service。目录、授权、成果归属仍由既有业务及原生资源所有者实现，指令仅说明已经成立的事实。提示词修改须与目录实现同步，不先向模型宣称尚不存在的 outputs/tmp 布局。

## 已完成与仍有偏差的边界

生产执行已接到 `Execution → ThreadLifecycle/Turn → ProviderRuntime → 原生 Client`。`nativeThreadRequest` 已传递真实附加目录与直接指令，技能已改为复制与受管清单。Catalog 的安装发现、Profile 和程序定位仍有生产消费者，需要保留。

已完成普通 Agents cwd、独立 Conversations 日志/附件/成果、团队 Workspaces 及工作流领域 cwd 的生产切换。Chat 删除保留伙伴文件，分叉独立复制必要附件与不可变成果；Waker 删除复用现有暂存与补偿，并在原操作锁中区分共享接纳和独占删除。文件 API 对共享 cwd 的不同 Chat 保留独立私有权限。

共享技能由现有 ProviderRuntime 按物理目标与冻结指纹预约，内容相同共享，冲突的活动消费者拒绝，闲置消费者确认关闭后替换；关闭失败保留 owner。不存在第二套资源或执行生命周期。

稳定角色、指南及目录用途进入 SessionPlan，本轮引用、结果、临时路径和动态授权路径进入 TurnInput.ContextInstructions。工作流引用清空会显式发送空列表，避免沿用上一轮选择；统一原生输入只渲染一次。配套目录、删除、分叉、文件范围、提示词和共享资源定向检查已有通过证据。

显式引用旧成果默认保留原文件路径，仅要求另存时才复制。接纳前核验的真实文件集合进入现有 Capture 和同一能力 Grant；精确文件根不遍历父目录，IM 及工作流输入冻结消费相同授权。普通编辑、原子替换和删除可比较，链接或目录重绑不会扩大范围。此次修复的生产链、成果卡点击定向检查及最终窄复审通过。

## acp-go 交付与后续边界

acp-go 已完成全部既有 CLI 的源码审查、已确认问题修复和定向验证，合入并推送 `main`，当前主分支提交为 `beaa58f4e1cc1bde1a65f8152f6a151980597c4a`，宿主固定可取得的版本 `v0.1.1-0.20261106060638-beaa58f4e1cc`。主工作 checkout 使用 `main`，其他在用 worktree 与未合入工作保留。没有将真实账号、模型、平台或 UI 验收写成已完成。

SDK 已独立为官方 fork `github.com/JieWaZi/acp-go-sdk`，固定 `v0.13.6-0.20261106052225-bbb4e22da7a0`。主项目固定上述 acp-go 版本与这一 SDK 版本；关闭 `go.work` 后，两者均从模块缓存解析，不使用本地 `replace`，不再导入旧 `acp-go/sdk` 子模块。

| 验证范围 | 结果与边界 |
| --- | --- |
| ACP 公共协议与各既有 CLI 包 | 定向检查通过；已确认的 Codex pre-ACK 终态保留、Pi ACK 与事件消费、SDK 连接清理问题均经独立复审 |
| 最终 Codex/Pi 流式路径 | `GOWORK=off` 包级 race 通过；Pi 独立复审实际覆盖 20 秒 ACK 超时后的活连接终态 |
| ACP 命令构建 | 当前 macOS、Windows amd64、Linux amd64 交叉构建通过；不替代对应平台真实运行 |
| main 合并树 | 共享协议、身份关联、服务退出及长流定向测试通过，远端 main 提交已核对 |
| 主项目直接消费者 | `GOWORK=off` Agent 与 Execution 定向测试通过，覆盖各既有 CLI 的生产装配、续聊、取消和恢复合成链路；不依赖本地 acp-go 源码替换 |

仍需保留三项明确边界：Pi 已发布的扩展消费接纳时冻结的目录及对象身份，默认权限只对符合条件的已存在读取目标免询问；首次加载换根修正已完成最后一次窄复审，不能据此宣称具备操作系统沙箱。OpenCode 自主后台回合补齐尚未完成与固定 OpenBot 的等价验证。WorkBuddy 原生历史在 result 后晚写入的真实时序尚未验证，缺失数据继续保留未知或已知小计。

依赖交付与 ACP 合并已闭环。宿主流式批量持久化与集合三态、统一错误展示已分别通过独立复审；精确成果范围修复、API 契约生成检查、宿主集成构建及实际应用数据清理已完成。空数据初始化与关键业务入口经过定向测试；真实模型、外部平台发送和视觉验收未运行，不能用这些自动化结果代替。
