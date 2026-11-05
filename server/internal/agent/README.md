# Agent 运行时

`ThreadLifecycle` 负责接纳、会话绑定与历史接力。`Turn` 在原生提交之前写入输入接纳，分别交付原生终态、用量与本地错误。`ProviderRuntime` 持有原生进程、账号租约和资源，确认退出后按依赖顺序清理。原生协议由 acp-go 客户端适配。

`Catalog` 维护 CLI 注册、安装发现和 Profile 解析。模型目录由 `ModelCatalog` 经 ProviderRuntime 探测。Chat 持有消息，SQLite 查询投影可从 ConversationStore 重建。软件能力和 allycli 分别在 `capability` 与 `cli`。

独立文本调用和受限 Wiki 维护复用相同回合与终态存储。当前只有 Claude 提供所需工具限制。普通聊天切换原生会话时，可交接现有总结或最近五个完整轮次。应用只读取当前会话日志格式。
