# Agent 会话与原生连接重构 Spec

## 范围与目标

将外部 Agent CLI 接入收敛为 `agent.Manager → agent.Session → native.Connection → acp-go`。根 Agent 包承载真实产品执行能力，以 Session 为唯一执行和资源所有者；本机接入包隔离协议、CLI 工厂和启动认证资源。命令能力桥与客户端一并归入 Agent。

本轮保留现有产品行为、HTTP/OpenAPI、JSONL、事件、CLI 命令、环境变量、账号及插件事实、原生恢复和分叉语义。独立 acp-go 依赖版本保持不变。不修改前端界面或业务逻辑；Go 类型归属变化导致的 OpenAPI 元数据及客户端类型只通过既有命令生成。不操作用户运行中的页面、桌面或真实模型 CLI。文件数量和行数不是功能验收指标。

用户已经确认本设计方向并授权安装 Superpowers、完成书面 Spec/Plan 后实施；不重复请求相同实施授权。工作在现有 `codex/agent-runtime-refactor` 分支进行，保留无关的 `desktop/resources/plugins/` 文件。

## 为什么调整当前结构

当前代码管理外部 CLI 的会话，不负责模型推理与工具循环。Host 同时维护聊天 Runtime 关联和独立任务清理集合；每个工作区 Runtime 又维护 sessions/active，实际只允许一个受管会话执行。Host/Core 的模型转换和准备句柄包装进一步放大了对象层数。

本次必须解决这些结构问题，而不是只移动路径：

1. 删除 Host/Core 两份相同生命周期模型及 preparedChat 的技术转发链。
2. 一个 Manager 登记消费者，一个 Session 持有自己的当前 Turn；每工作区不再嵌套通用 Runtime 管理器。
3. 分开安装/Profile 事实与活的启动认证租约，取消 Environment 泛化层。
4. 原生连接只面对一次会话，不维护未被实际复用的通用多连接驱动框架。
5. 同一对象的方法和测试集中维护，删除第二层 internal、无消费者辅助函数和重复拷贝规则。

## 对象与所有权

| 对象 | 唯一拥有的事实 |
| --- | --- |
| Manager | 工作区索引、临时 Session 登记、根生命周期、尚未清理完成的 Session 集合 |
| Session | 稳定计划、兼容依据、当前 Turn、真实原生句柄、连接、实际资源和可重试释放进度 |
| PreparedTurn / Turn | 同一回合的准备占位、准备 Context、执行 Context、事件、交互和有界最终候选 |
| Catalog | 本地 Profile、安装与程序定位、探测解释，以及独立管理连接的调用编排 |
| native.Connection | 原生 Agent、ACP 传输和会话、协议通知与取消；关闭失败时保留本连接 |
| native.LaunchLease | 本次认证刷新入口、认证文件对账和冻结启动环境 |
| capability.Registry | 本轮精确授权、撤销、在途能力调用和有界观察 |

Manager 可以维护索引与所有者集合，但不得复制 Session 的 busy、配置摘要、原生句柄、资源及关闭步骤。Session 不含用于管理其他 Session 的 sessions/active map。旧消费者未关闭成功时不能提前释放账号、认证或工作区资源，也不能打开替代消费者绕过失败。

账号、插件、技能、连接器、业务接纳、日志和终态仍归原领域。根 Agent 和 native 不导入 `apis`、`services`、`repositories`。本机包不反向导入根 Agent，也不导入 capability/cli。

## 模型与协议边界

根 Agent 保留一套 SessionPlan、TurnInput、SessionResources 和产品 Result/Event。稳定目标与连接器声明采用窄类型，不包含秘密环境、请求头、刷新或释放回调。本轮输入不进入兼容摘要。

SessionResources 保存实际目标、账号和插件租约、技能及连接器秘密、业务快照和释放能力。资源取得失败时也能交接部分结果。回读资源用于原有接纳、插件使用和范围提交，不新增第二份热资源表。

native 只定义连接、Prompt、协议事件、原生完成事实和故障所需的类型。不得把产品 SessionPlan、PreparedTurn、业务资源租约或 Manager 移入 native。原来的技术合同中，真正的原生 I/O 类型归 native；纯会话准备、计划兼容和资源所有权归根 Agent。Agent 在连接边界一次转换计划和输入；不保留 Host → Core → Native 的重复完整请求映射。

原生适配器报告消息身份、阶段和完成证明；Turn 的有界收集器据此判断最终答案。执行状态、答案可用性、事件交付失败和清理失败继续分别表达。产品错误在根 Agent 映射一次；原生技术故障可以保留结构化原因，不抛出新增产品错误字面量。

## 唯一执行流程

```text
Manager.PrepareSession(plan, directive)
  → Session 占用本轮并判断热复用
  → 冷路径由调用方使用 PreparedTurn.Context 取得资源
  → PreparedTurn.Bind 接管实际或部分资源，准备启动租约
  → PreparedTurn.Run 打开必要连接并发送本轮输入
  → 撤销当轮授权，按留存策略保留或关闭 Session
```

Prepare 不启动进程、不反调业务 Acquire。Bind 不提交 Prompt。首次有效冷 Bind 立即接管部分资源，后续校验失败也由同一 Session 清理。重复 Bind 和消费句柄后的 Bind 不接管新资源。

取得句柄后所有提前返回路径都登记 Close。准备取消能够取消 Acquire，但不能遗弃迟到资源。成功冷 Bind 解除短接纳 Context 的取消转发；热准备返回前完成同样交接。Run 使用执行 Context，原接纳请求结束不能取消已交接后台的执行。

新会话只播种一次有界完整历史。热续聊和冷恢复仅发送新增消息，真实 NativeHandle 直通。无配置变化时静态模型前缀、技能/MCP 名称与顺序稳定；临时端口、凭据和本轮授权不进入摘要或模型可见身份。

聊天、GenerateText 和 ExecuteTask 共用这套 Session 机制。独立文本/工具任务创建临时 Session，结束关闭；普通聊天允许保留。只读交接保留当前能力和强制关闭规则。任务授权在 Prompt 返回时撤销，文件等会话资源仍保留至原生消费者退出。

## 关闭、并发与管理操作

- 每个 Session 同时最多一个 Turn；不同工作区和独立任务可并发。Manager 的集合锁不覆盖原生 I/O、账号等待、事件回调或资源清理。
- 会话关闭按原生连接、启动认证资源、领域资源、临时工作区的依赖顺序执行。成功步骤不重复执行；失败保留同一所有者和剩余步骤。
- 取消、关闭、Prompt 完成和交互返回的竞争继续有唯一线性化点。清理故障不覆盖已经完成的答案，不自动重新提交 Prompt。
- Manager.Start 显式启动原有临时任务清理重试：每 10 秒只重试执行已结束且待清理的临时 Session，不关闭活动或正常留存的聊天。独立任务接纳保留原来同时执行与待清理所有者合计 64 的容量上限，聊天不计入此上限。应用退出停止新接纳，取消并等待准备及执行，再关闭闲置和清理未完成的所有者。超时返回准确错误，可再次 Close。
- Probe/Fork 使用无 Prompt 的独立管理连接；共用唯一 CLI Descriptor、工厂和 LaunchLease。管理连接退出失败同样不能提前归还账号与目录。
- 安装/Profile 配置不在每轮聊天进行远端健康或授权预检。Fork 耐久回执、ReconcileOnly 和原生定位语义保持不变。

## 能力调用桥

`agentcapability`、其 contract 和 `agentcommand` 合并为 `agent/capability`；`agentcli` 合并为 `agent/cli/cli.go`。命令 Schema、精确授权、有界观察、未知结果查询和领域事务内 Commit 保留原行为。

数据库表、命令身份摘要、协议路径、allycli 名称、环境变量及知识 HTTP MCP 契约不变。Unix/Windows 私有连接文件权限实现继续分别编译。

## 目标文件职责

```text
server/internal/agent/
  agent.go             Manager 与产品入口
  session.go           Session、PreparedTurn、Turn 及关闭
  contract.go          产品计划、输入、结果和交互
  catalog.go           Profile、安装、探测、分叉编排
  config.go            目录与产品配置值、模型选项规则
  resources.go         启动及领域资源装配、工作区文件
  diagnostic.go        产品故障解释
  logging.go           原生日志脱敏和投射
  native/
    connection.go      一次原生会话与协议
    contract.go        原生连接、事件和完成事实
    registry.go        CLI 声明、工厂和厂商输入差异
    launch.go          启动认证租约
    management.go      无 Prompt 管理连接
  capability/          软件能力目录、授权、回执与平台连接文件
  cli/                 allycli 解析和客户端
```

同一对象的方法按完整职责集中，允许 2000–3000 行文件。测试随对象合并，保留真实 CLI 环境开关及平台边界，不减少既有行为断言，不新增仅供测试的生产包装层。实施中如名字冲突，按语义区分 TextMessage/聊天输入、CatalogConfig/启动配置；不把不同模型机械合成宽类型。

## 验证与完成标准

先保存原 Agent/能力/CLI 子系统定向测试基线，再迁移断言。生命周期变化增加针对 Manager/Session 的真实行为回归，验证并发、热资源复用、迟到交接和清理重试。原生连接继续覆盖受控进程、消息阶段、原生配置与实际发送内容。能力桥覆盖精确授权、事务回执及知识 MCP。

完成时必须满足：

1. 旧 core/host/environment 和三个顶层外围目录删除，没有兼容转发壳。
2. 没有第二层 internal；同一对象和测试完成实质内聚。
3. Session 状态与资源只保存在一个所有者中；删除每工作区 Runtime 嵌套管理集合。
4. 原生包不反导产品 Agent，不包含产品计划或领域资源获取机制。
5. 原有关键功能测试通过，相关包 vet、生命周期 race、生产构建和文档链接检查通过。
6. 只运行修改范围及直接受影响包的检查；不运行 go test ./... 或前端全量检查。
7. 真实账号、模型和平台验收如未执行，明确记录边界，不声称自动化证明所有平台和模型功能完全无回归。
