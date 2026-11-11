# Execution 的 Codex 产品调用链

以 NeuxBot 已确认的 Agent Runtime 设计、当前工作树 为依据。验收入口是 `execution.Service.Start`，测试用临时 SQLite、共享 Conversation Store 和通过管道通信的 Codex app-server 夹具。

## 职责对应

| Execution 的接法                                                                                                                                                                                                                      |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agent_next.ProviderRuntime` 独占原生连接和账号租约。Execution 解析已冻结账号，冷路径交付技能、MCP 和插件资源，由同一连接关闭释放。                                                                                                   |
| `ThreadLifecycle.Prepare` 处理兼容热复用、重启续接和显式替换。已有原生会话只收本轮输入。可用旧会话生成私有交接，无法生成时从共享日志选择最近五个完整轮次。                                                                            |
| Execution 保留业务单 Chat 占用、首发幂等、续发回执、附件回滚和编排入口的 pending 接纳。NeuxBot 的 BeforeSubmit 批次按确认设计先写业务启动、绑定与实际输入身份，再投影接纳并激活本轮短期授权。接纳后即使取消也可按已提交回执完成投影。 |
| `Turn.Run` 收敛原生输入、正文、原生结果和用量。Chat 从已提交检查点投影订阅和历史。Execution 在此之后提交唯一业务终态，释放占用后发布，文件采集仍独立收尾。                                                                            |

NeuxBot 保留既有的落盘、投影、回传顺序。取消与投影重叠时，原生回调从同一等待者接收已提交决定，避免再次作废或误报超时。

生产的 Start 路径移除了旧 `PrepareSession/Bind/PreparedChatRun`、旧 Runtime 消息 Sink、重复的 Session 句柄记录与旧终态用量归档。旧包的纯配置 DTO、静态探测判断及不启动进程的工作区文件准备仍供宿主复用。

## 验证

- `GOWORK=/private/tmp/neux-thread-turn.work go test ./server/services/execution -run '^Test(Native|Plugin|CLIQuestions|Approval|Elicitation|FinishExecution|ExecutionRegistry|CancelExecutionAndWait|Work|PrepareChatGroup|CreateChatReplays|Submission|ContinuationSubmission|SteerRelation|FirstSendWithout|HostDirectoriesFreeze|HiddenEntry|IntegrationAutomatic|SendMessage)' -count=1`。覆盖真实 Start → Codex 帧 → 共享日志 → Chat 订阅与历史、成功和失败终态、运行中取消、文件实际写入与快照、公开终态和用量、单账号租约热复用、重启后原生续接，也覆盖业务幂等、插件快照、工作区与交互校验。
- `GOWORK=/private/tmp/neux-thread-turn.work go test -race ./server/services/execution -run '^TestNative' -count=1`。通过正常包入口验证同一链路，以及显式替换使用旧会话私有交接且不公开交接正文。
- 旧 Runtime sink、调用替身和原生手工资源释放测试随对应接口退役。共享测试准备改为 Codex app-server 子进程，保留工作区、团队接纳、插件准备、首发与续发幂等、权限和审批等业务测试。原生增量、近期完整历史、私有交接、资源失败与用量的底层覆盖位于 `server/internal/agent_next`。
- 夹具只使用本地临时数据。真实登录、真实模型和页面验收分别由产品联调验证。

## 保留的边界

- 此入口先开放 Codex 普通会话。其他 CLI、旧原生分支接口和公开执行重试保留明确拒绝。原生重试需要可验证地约束在原绑定，避免用替换会话自动重放旧输入。
- 受限 MCP 或内部能力名单目前显式拒绝。Codex 适配器还没有证明可以压制账号配置中继承的原生 MCP 与外部插件。受限工作流执行和知识自动入口因此暂缓。
- 保留可投射技能和 MCP 的插件路径，必须原生装载的选中插件明确拒绝。后续须在不替换账号配置根的前提下装载。
- 活跃旧连接支持私有交接。重启后若无法按完整旧配置验证先前资源，由 Thread 选择最近完整轮次。手工指定摘要模型、跳过团队内已携带的历史项、URL elicitation、Codex 普通模式主动提问的默认启用策略和工作流内部 MCP 启动时序保留为后续核对项。
