# OpenBot 提示词与本轮上下文对比

参考 OpenBot 0.74.0 的固定提交 `c22549534075806abc2231c54c710474bb870afd`。交付行为以该提交的代码和测试断言为准。

- Codex 将稳定规则交给原生 `developerInstructions`，按需把动态上下文作为独立文本块放进 `turn/start`。Claude 将稳定规则追加到系统提示，动态块进入原生用户回合。通用 ACP 通过用户输入前缀兼容稳定规则，测试覆盖首轮、无用量信号时的周期刷新以及压缩或失败后的重发。[Codex 交付](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/app-server-client.ts#L479-L513) · [Claude 系统指令](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/claude-client.ts#L1389-L1401) · [Claude 本轮块](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/claude-client.ts#L1538-L1562) · [ACP 回归](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/scripts/acp-instructions-once-e2e.ts#L89-L151)
- OpenBot 按原生会话跟踪已交付的记忆、队友等上下文。内容未变时省略，核心记忆的增量短于全量才发送增量，压缩后重新建立交付状态。核心记忆预算按脱敏后的 UTF-8 字节计算。附件、选区等代表本轮明确选择的数据仍要按其当轮语义处理。[会话上下文](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/turn-context.ts#L14-L91) · [记忆投影](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/developer-instructions.ts#L222-L276)
- OpenBot 将详细工具指南留给发现调用，按字节检查宿主固定指令、可见工具和目录的增长，另用模型基准观察实际请求。NeuxBot 在会话指令中交付工作流创作启动约定，通过 `workflow.authoring.prepare` 提供创作指南和 SDK。[OpenBot 预算检查](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/scripts/agent-context-budget.ts#L60-L88) · [NeuxBot 创作契约](../architecture/workflows-cn.md#创作与运行)

## NeuxBot 离线对比

使用仓库固定的 `tiktoken-go/tokenizer` 中的 `o200k_base`。上下文样例含实际标签与 JSON，工作流启动段使用同一虚构安装路径及仓库指南内容。只计对应宿主文本。

| 样例 | 修改前字节 / token | 修改后字节 / token |
| --- | ---: | ---: |
| 通用上下文说明 | 409 / 73 | 161 / 31 |
| 无上下文的普通发送 | 0 / 0 | 0 / 0 |
| 选中工作流版本 | 176 / 50 | 137 / 39 |
| 附件、技能、连接器及插件 | 363 / 94 | 273 / 68 |
| 工作流创作启动段 | 约 2,962 token | 约 155 token |

## NeuxBot 真实模型抽测

本机 Codex CLI 0.172.0，`gpt-6.2-sol`，每组使用独立临时数据库及工作区。两组接收同一组自然语言输入，创建并保存工作流，重启 Core 后恢复同一个原生会话，说明当前规则且保留原版本，随后修改默认重复次数并保存新版本。运行旧版本得到 `BOT BOT!`，运行新版本得到 `BOT BOT BOT!`。两组各完成一次，创作期间均未启动运行。

| Codex 原生记录 | 完整指南随会话 | 启动约定 + 按需指南 |
| --- | ---: | ---: |
| 首个模型请求输入，含缓存 | 28,743 | 25,883 |
| 三轮观察到的模型请求数 | 16 | 13 |
| 三轮输入，含缓存 | 746,305 | 625,701 |
| 三轮未缓存输入 | 43,073 | 52,389 |
| 三轮输出 | 4,080 | 3,323 |

两组均调用 `workflow.authoring.prepare`。短版的单次准备结果约 33 KB，之后又查询两次 SDK，其中一次返回输入错误。全量版的准备结果约 31 KB，也包含完整创作指南。这个单样本显示了首发输入下降，同时观测到了不同的工具轨迹和缓存命中。三轮未缓存输入在短版中上升，样本无法推导稳定计费差异。

短版补测了两条路径。画布选中阶段时，真实 Codex 收到原文和独立引用块。Core 重启后清空选择继续发送，原会话只收到用户原文，修改保存成功并按新版本运行。自然语言创建的审批计数流程在拒绝时返回 `count: 0`，没有启动 Worker。明确批准时，独立 Worker 完成计数并提交 `count: 6`。

文件写入与回读用例用真实 Codex `gpt-6.2-sol` 通过。模型保存了通过编译行为比较的版本。人工批准后，两个独立 Worker 分别写入与回读临时文件，最终返回 `written: true`、`verified: true`，文件与回读结果均为 `BOT BOT!`，共 8 字节。另用 Go 回归验证 Core 保存时逐字保留收到的源码，包括缩进和空行。F0 原运行也返回 `BOT BOT!`，本地用例的长度预期已改为按字符串计算，尚未单独重跑 F0 真实模型。

下一轮对照优先针对准备工具，测量重复指南、SDK 补查和失败重试。继续保留现有创作、版本和权限契约，用相同运行时重复用例，分别记录成功率、总输入、未缓存输入、输出和请求次数。
