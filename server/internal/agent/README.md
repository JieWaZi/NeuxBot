# Agent 运行时

`ThreadLifecycle` 负责接纳、会话绑定与历史接力；`Turn` 分别交付原生终态、用量与本地错误；`ProviderRuntime` 独占原生进程、账号租约和资源，在确认退出后依次清理。原生协议由 acp-go 客户端适配。Chat 与运行时使用同一个 ConversationStore，SQLite 只保存可重建查询投影。

| 文件或目录 | 职责 |
| --- | --- |
| `thread_lifecycle.go`、`turn_lifecycle.go` | 会话配置兼容、输入接纳、原生执行与关闭 |
| `provider_runtime.go`、`agent_client.go` | 实际连接资源所有者与 acp-go 边界转换 |
| `catalog.go`、`runtime_installation.go` | CLI 注册及 Profile；安装发现、程序定位及探测启动环境 |
| `runtime_options.go`、`model_catalog.go`、`session_settings.go` | 安装探测结果、模型目录与原生有效设置 |
| `client_contracts.go` | 共享原生事件、指令、交互和资源字段 |
| `developer_instructions.go` | 稳定指令组合，交给原生指令入口，不改写工作区指令文件 |
| `skills.go` | 技能复制、摘要清单、暂存发布与仅清理受管安装 |
| `detached_task.go`、`history_handoff.go` | 独立文本及受限任务；摘要和有界历史接力 |
| `conversation_store.go`、`delta_buffer.go`、`native_outcomes.go` | 存储消费合同、实时消息聚合及原生结果持久化 |
| `capability/`、`cli/` | 软件能力目录、精确授权、领域回执与 allycli/MCP 客户端 |
| `agentfixture/`、`testdata/` | 宿主集成测试替身与协议样本，不参与生产装配 |

独立文本调用和受限 Wiki 维护复用同一个 Turn 与终态存储。工具限制以实际原生能力为准。会话替换可交接现有总结或最近五个完整轮次；热续聊只提交新增输入。

Execution 直接准备 ThreadRequest、TurnRequest 和 TurnPorts。连接配置负责进程复用，Binding.Configuration 只表达原生历史兼容；模型目录、展示资料与默认设置不作为会话身份。热续聊读取发布声明，资源内容校验和物化属于冷获取；进程关闭失败继续由原所有者持有租约。
