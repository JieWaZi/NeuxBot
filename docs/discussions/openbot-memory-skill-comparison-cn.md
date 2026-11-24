# OpenBot 记忆与技能学习源码对照

对照基线为本地 OpenBot 0.74.0，提交 `c22549534075806abc2231c54c710474bb870afd`，日期 2026-11-20。NeuxBot 侧表格记录本轮修订前的设计，作为差异基线。修订后的[功能规格](../superpowers/specs/2026-11-18-partner-memory-spec.md)、[前后端设计](../superpowers/specs/2026-11-18-partner-memory-design.md)已采用共用复盘，支持记忆新增、更新、遗忘，以及技能询问或自动保存、新建或修订。本次对照为静态源码调研。

两边的记忆分类、8 KiB 核心预算、增量复盘与临时无工具调用方向相近。最大的业务偏差在技能学习证据和采纳时机。最大的工程开销在临时调用的完整落盘隔离、跨重启恢复，以及技能发布与安装的一致性。

## 行为对照

| 项目 | OpenBot 0.74.0 | NeuxBot 当前草案 | 工程影响 |
| --- | --- | --- | --- |
| 设置 | 服务端「记忆／学习」区。记忆复盘间隔可设关闭、10、20 或 40 轮，默认 20。技能为自动、询问、关闭，默认自动。 | 智能体设置里两组，分别按伙伴启用且默认关闭。每组可跟随伙伴或选择全局专用 CLI、账号和模型。 | 我们增加配置解析、作用范围、不可用状态、配置切换和迟到结果处理。页面控件本身较轻。 |
| 触发 | 同一个复盘器按伙伴主聊天／侧线程计数，默认 20 轮或至少 3 轮空闲 15 分钟。清空主聊天及关闭有待处理轮次的侧线程也会尝试触发。自动后台技能判断跟随记忆复盘，间隔关闭会停止这条链。 | 记忆和技能独立启用、计数及恢复。关闭聊天标签只是导航。首版只收录未绑定目录的普通伙伴聊天。 | 共用终态入口和调度器即可保留独立开关。配置和游标可以按用途分开，无需各写一套计时、重试及模型执行。 |
| 复盘输入与记忆操作 | 有界聊天摘录包含用户和助手正文，可加入相关已有记忆及浏览动作。允许新增、修订、遗忘，以及选为 essential；保护手工或用户控制项，检查并发更新。 | 记忆只发送脱敏的用户原文和批内标识，只自动新增普通记忆。 | 节省读取和写入状态，但可能保留互相矛盾的旧事实。首版可以维持只新增，在相似事实与更正样例上验证效果。 |
| 核心记忆 | essential／searchable，核心预算 8 KiB。支持轮次上下文的客户端比较已交付快照，发送差量或较短的全量；其他客户端通过初始指令交付。 | 核心／普通，8 KiB。有变化时发送完整快照，成功终态后记录交付版本，原生绑定变化或交付不明时重送。 | 本地版本核对和接续仍需实现。8 KiB 下先发完整快照就足够，暂时无需实现逐条差异。 |
| 技能证据和判定 | 第一次调用在同一 JSON 中输出记忆操作及至多一条技能建议。判定信号是用户纠正、有效技巧或旧技能过时。输入含已安装技能名称及简介、历史建议、聊天和有界工具步骤及错误，优先保留失败及其后继步骤。 | 独立复盘，输入为脱敏用户消息及助手最终答复。排除工具轨迹、已有技能正文及项目聊天，直接生成新技能草稿。 | 当前文本够学习格式偏好和明确纠正，可能错过“先失败、后换方法成功”的经验。是否接纳项目聊天和少量结构化执行证据，比是否同批调用更影响学习质量。 |
| 技能产物 | 自动档命中后进行第二次临时调用，生成或更新本地技能，随即为伙伴安装。询问档先在原聊天展示建议，点保存后才生成和安装。更新读取原文及预期版本，保留归档中的其他资源。自动档还允许前台执行在有证据时修订已有本地技能。 | 先生成持久化、可编辑的 `SKILL.md` 草稿，再由用户采纳并安装，仅支持新建。 | 对比自动档，我们省掉改写既有技能和自动启用的风险。对比询问档，我们多了草稿页面、修订、撤回、存储和发布重放的完整生命周期。 |
| CLI 与恢复 | 侧客户端使用伙伴的 provider 和账号。模型池含伙伴模型时，若同 provider 的默认模型更便宜则选默认模型。临时线程不持久化，清理临时目录。轮次计数和等待在内存，JSON 游标另存。无可用模型或部分失败也会保存已前移的游标。 | CLI 可独立配置；不可用时保留进度。要求重启补齐、切换配置令旧批失效、同批重放幂等。临时会话正文同时避开公开聊天、Core 共享原生 JSONL 和 CLI 原生历史，保留用量。 | 这是最大的后端成本，也是实质语义差异。当前 `GenerateText` 只有 Claude 支持已有无工具边界，独立调用仍走持久原生流。首个适配器需先实测无历史落盘。 |

OpenBot 源码依据：[宿主设置 UI](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/renderer/src/features/servers/ServerMemorySection.tsx#L20-L79) · [记忆默认值](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/packages/contracts/src/ipc-agent-memories.ts#L81-L99) · [技能模式](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/packages/contracts/src/ipc-skills.ts#L295-L313) · [终态条件](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/turn-lifecycle.ts#L2397-L2405) · [复盘调度和执行](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/memory-review.ts#L295-L443) · [复盘输入和技能信号](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/memory-review-prompt.ts#L86-L195) · [记忆操作保护](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/agent-memories.ts#L191-L224) · [核心上下文交付](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/developer-instructions.ts#L227-L276) · [客户端兼容分支](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/thread-lifecycle.ts#L1131-L1190) · [对话内采纳](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/memory-review.ts#L193-L229) · [前台技能修订能力](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/developer-instructions.ts#L448-L463) · [技能版本与安装](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/main/local-skill-learner.ts#L22-L65) · [工具步骤选择](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/skill-suggestion.ts#L43-L71) · [客户端配置来源](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent-service.ts#L735-L768) · [后台模型选择](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/background-model.ts#L9-L25) · [临时线程配置](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/profile-generation.ts#L234-L292) · [游标文件](https://github.com/nightly-labs/openbot/blob/c22549534075806abc2231c54c710474bb870afd/src/backend/agent/memory-review-cursors.ts#L29-L104)。

NeuxBot 源码依据：[独立生成能力检查](../../server/internal/agent/detached_task.go#L70-L113) · [独立执行输入持久化](../../server/internal/chat/conversation/native_stream.go#L968-L984) · [技能发布事务](../../server/apis/skills/import_library_skills.go#L126-L185) · [技能安装事务](../../server/apis/skills/library.go#L65-L103)。

## 首版可以减少的复杂度

1. **一套后台复盘基础设施，保留两个独立开关。** 共用 Chat 成功终态观察、时间窗口、日志读取、领取、退避、重启对账、CLI 解析和临时执行，复用同一配置表单。记忆和技能保留各自的游标、代次和写入事务，以便独立启停和使用不同 CLI。即使选到同一个 CLI，也先分别构造各自授权的输入。只有有效配置、来源区间和外发授权均一致时，才值得评估合并模型调用。
2. **技能先判是否有经验，再决定写正文。** 把用户的明确纠正、已验证的有效方法作为信号，允许无结果，每批至多保留一项。若保留审批前阅读完整技能的产品体验，只对命中的批次生成正文，或由用户点「生成草稿」后再生成并预览。若从 OpenBot 借鉴对话内确认，只需要持久化建议和来源，点击后再生成并展示。是否需要集中待采纳页随实际工作流决定。首版继续只新建，复用技能库的文件准备、发布与安装。
3. **把技能来源与记忆授权分开评估。** 先用三类样例比较产出，分别是用户纠正、项目任务里出现的有效方法、已有技能缺步骤。若实际技能价值主要在项目对话，可先为技能增加明确授权的项目会话来源，给模型有界脱敏正文。后续按效果再考虑提供结构化步骤摘要。附件、原工具输出和工作目录访问不进入这条链。
4. **优先验证一个 CLI 的完整链路。** 验证 Claude 的工具禁用、成功／失败／取消／重启无正文落盘与用量记录，再接入两项功能。用于前台聊天的其他 CLI 仍可选择受支持的全局复盘 CLI。先验证底座比先开发设置页更能早发现阻塞。

调用次数按每个成功处理的逻辑批次、无重试且无文本拆批估算。OpenBot 通常调用一次判断，命中后在自动保存或用户同意时再调用一次写技能。我们草案在两个功能同时开启且对同一窗口各触发时调用两次，其中技能那次直接判断并返回草稿。如果再为技能增加独立写正文阶段，有命中时可能达三次。重试、文本拆批以及不同 CLI 配额会进一步改变次数。两阶段能省去无信号批次的长正文输出，收益应以真实样本测量，调用次数本身不足以估算成本。

静态判断的优先级为临时运行时隔离和恢复（高）、技能采纳及发布一致性（中高）、核心记忆交付（中）、设置布局和表单复用（较低）。本次未运行 OpenBot 或 NeuxBot 的真实模型调用。
