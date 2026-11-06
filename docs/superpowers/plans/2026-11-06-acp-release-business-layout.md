# ACP 收尾与业务目录调整计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. 按任务执行并记录验证与独立审查。

**Goal:** 全部既有 CLI 完成审查和定向验证，解决 SDK 与 acp-go 独立构建后合并 main，再落实业务目录所有权调整。

**Architecture:** acp-go 持有原生协议及客户端生命周期；宿主继续以 ThreadLifecycle、Turn、ProviderRuntime 执行。业务工作目录、会话事实、账号状态和成果各自归原所有者，不能只重命名目录。

**Tech Stack:** Go modules、ACP SDK、原生 CLI、JSONL、现有 Go Core 与前端。

**Spec:** [重建方案](../specs/2026-11-04-agentruntime-rebuild-design.md)、[业务清单](../../discussions/agentruntime-rebuild/business-integration-cn.md)、[模块审查](../../discussions/agentruntime-rebuild/module-review-cn.md)。

## Global Constraints

- 用户指定开发子代理 GPT-6.1 Sol xhigh，主代理协调公共合同与集成。
- 全部既有 CLI 均为本次审查目标：Codex、Claude、Cursor、Kimi、Gemini、Grok、OpenCode、Pi、WorkBuddy、Qoder、Qoder CN；自定义 ACP 核对公共合同。
- 保留无关修改；不操作真实账号、模型或运行中 UI；默认定向验证。
- Agent 内核与 acp-go 新代码使用英文注释；宿主其他目录遵守后端约定的中文字段注释。按实际所有者组织，不增加第二生命周期、隐式回退或旧执行器。
- 用户已授权合入 main 并后续使用主分支；不删除其他在用 worktree 或未合入工作。
- 用户已确认目录与提示词一起收尾：普通成果为伙伴 outputs 下的任务范围，临时文件独立；稳定指令与本轮上下文分开，删除重复及过时规则。
- 用户明确不保留历史兼容：不实现旧数据导入、布局转换、双写或旧路径回退。全部重构、复审及定向验证完成后，由协调者核实实际受管根、停止相关写入进程并清空应用自有目录、数据库与状态；保留源码、无关修改及外部真实项目。

## Review Focus

- 脱离 go.work 后 SDK 接纳接口与模块版本仍一致。
- 控制请求、取消、终态与连接故障不混淆所有权或丢失已知消费。
- 各 CLI 的恢复、模型设置、账号和附加目录只采用真实能力。
- 共享伙伴 cwd 不导致技能、账号、Chat 删除与分叉互相覆盖。
- 临时文件、可交付成果与用户项目有不同保留和清理规则。

### Task 1: SDK 与独立构建

- [x] 检查 SDK 新接口所在提交及远端可取得性，明确模块发布顺序。
- [x] 固定可取得版本，更新必要 go.mod/go.sum，禁止本地 replace 掩盖问题。
- [x] GOWORK=off 定向编译测试；独立审查版本与生成边界。

### Task 2: 全 CLI 审查与修复

- [x] 首批三家与公共层沿入口、恢复、取消、终态、用量和退出路径审查。
- [x] Kimi/Gemini/Grok/OpenCode/Pi/WorkBuddy/Qoder/Qoder CN 与自定义 ACP 分别记录证据和能力边界。
- [x] 根据源码因果链修复发现，单一所有者修改共享层，定向测试及必要竞态检查。
- [x] 独立复审修复及最终集成树，不能只以包测试通过作为完成。

### Task 3: 合并与依赖交付

- [x] 精确提交属于 ACP 收尾的改动，核对远端 main，完成最终审查。
- [x] 合入 main，验证合并树；主项目固定可取得的 acp-go/SDK 版本并脱离 workspace 验证消费者。
- [x] acp-go 主工作 checkout 后续使用 main，保留其他在用 worktree。

### Task 4: 业务目录与文件路由

- [x] 对照固定 OpenBot 核对临时文件、成果、下载、共享目录和 prompt，补全业务清单。
- [x] 依确认方案调整伙伴目录、scope、真实项目授权及技能/账号协调。
- [x] 同时调整会话删除/分叉、附件/成果、日志定位与文件 API，不仅修改路径名称。
- [x] 定向验证相关入口，协议和文档按实际修改同步生成。

#### 4.1 原生根与共享技能前置

- Pi 的同一 resources 对象将接纳的规范根及对象身份交付实际 extension，首次加载和 reload 均不重新授信；修复已包含正式 v0.1.0，最后一次窄复审通过，不再重复 Pi 验证；保留同仓库其他修改。
- 共享技能继续归 ProviderRuntime 的 ResourceLease。按规范 cwd 与实际发现目标协调，同内容共享，活动中的异内容拒绝替换；空闲消费者沿现有 ThreadLifecycle 退出，关闭失败不释放租约。
- 修改 `internal/agent/skills.go` 的清单更新，准备一种发现目录不删除其他 Runtime 的受管目录。调用衔接归 `services/execution/native_turn.go`；不新增技能 Service 或执行锁。
- 定向覆盖跨 Chat、跨 Runtime 同目标、版本冲突、空闲退出、关闭失败与并发准备；账号继续复用原有私有 home 和世代隔离。

#### 4.2 目录所有权与完整业务切换

- 在既有 `chatstorage.Layout` 和 workspace owner 中区分 Agents、Conversations、Workspaces；普通 Chat 的 cwd 归 Waker，日志、附件原件及结果记录归 Chat。工作流内部 scripts/state/cache/tmp/runs 保留原领域路径，账号与数据库不因图示名字搬迁。
- 联动 `execution/work.go,chat_group.go`、`workspace/execution_workspace.go,attachments.go,project_files.go`：保留真实项目根，原生支持附加目录时直接交付，明确不支持时创建按 Chat 隔离的 projects 入口并沿现有删除记录清理；本轮精确输入放任务 tmp，prompt 使用 RuntimePath；准入失败只回滚本次独占物。
- 联动 `chatstorage/catalog.go,deletion.go,fork.go` 与 `services/chat`：团队关系来自明确事实，Chat 删除不清理伙伴目录；分叉只复制自身附件/成果引用。团队 workspace 发布保留既有 staging → rename，Chat 日志由既有 CreateDormantChats 流程创建与补偿；仅确有跨目标提交时扩展现有 durable intent，失败可恢复。Waker 删除在原确认及删除入口处理自有目录，不能借 Chat 级联推断目录归属。
- `workspacefiles` 和文件 API 分开 Chat 私有文件根、执行 cwd、任务成果与真实授权项目；句柄及隐藏清单包含 Chat 身份，日志和凭据不得成为浏览或原生执行根。
- 成果采集默认只取本轮 outputs，已有成果沿明确引用取原路径。共享项目差异只表示范围内净变化，不伪称本轮独占生成；不为扫描整个共享 cwd 添加第二套串行执行。
- 全部入口只读写新布局，删除旧布局发现、旧 owner 解析及链接接管等兼容路径，不实现 importer。新数据的崩溃恢复、原生 resume/fork、索引重建仍为正常能力，不得当成历史兼容删除。
- 这些规则必须形成完整生产调用链后才算切换完成；不能以新增路径辅助函数或通过 Layout 单元测试宣告完成。

#### 4.3 提示词与所有入口一致

- `developer_instructions.go` 仅组合必要稳定说明，删除 Chat cwd、attachments/generated、projects 虚拟入口的旧规则；目录归属由业务确定，内核不推导 Chat 附件布局。
- 具体 outputs/tmp/输入路径、团队当前回合模式、工作流运行信息通过现有 TurnInput 装配口交付，不进入稳定资源指纹；稳定身份与角色规则保留。
- 按 CLI 实际项目指令发现能力避免重复注入，不能把 add-dir 当作指令加载证明。原生 system/developer 端口与 ACP 上下文前缀保持区别。
- 收敛 ally 指南、工作流创作提醒与工具说明的重复职责，保留授权、结果提交及角色边界。更新 workflow SDK 知识的生成来源并用对应命令生成。
- 普通聊天、团队、自动任务、IM 与工作流复用同一业务事实；分别验证新建、续聊、恢复、删除、分叉、交付及中断。
- 必须证明两个普通 Chat 同 Waker 固定 cwd、任务输出互不混淆；跨轮动态信息变化不会仅因任务身份改变而重建原生会话。新布局合成数据覆盖历史回放、附件、用量和索引重建。

### Task 5: 宿主流式交付与统一错误契约

- [x] 按用户实际反馈核对宿主 DeltaBuffer、实时观察队列、持久提交与终态交付，区分载荷大小、工具状态和队列积压；不能只增加预算或隐藏错误。
- [x] 统一应用错误身份、公开原生原因和内部诊断的契约；HTTP、实时事件、历史回放和各业务展示使用一致规则，中英文随当前语言展示。
- [x] 不对单个 RuntimeEventQueueOverflow 添加孤立分支或页面正则；在实际生产者与共享消费者修复。
- [x] 定向验证正常长流、慢观察者、工具大输出、取消/终态/用量保留，以及错误身份泄漏和本地化的跨入口一致性。

### Task 6: 全新进入前的数据清理

- [x] 全部实现与独立复审完成，记录仍未进行的真实平台/模型验收。
- [x] 从实际配置与启动参数核实应用 DataDirectory、ManagedFilesDirectory、Electron/Web 状态及相关写入进程，不凭名字猜目录。
- [x] 停止相关应用写入；只清理已核实的应用自有目录、数据库、受管账号与缓存，不遍历项目软链接、不碰外部项目及原生 CLI 的全局账号目录。
- [x] 验证旧应用数据已清空，新布局可从空数据初始化；不以截图或控制运行中 UI 替代源代码与定向验证。
