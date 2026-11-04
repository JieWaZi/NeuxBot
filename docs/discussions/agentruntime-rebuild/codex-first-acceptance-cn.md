# Codex 优先验收与待完成项

2026-11-04 调整推进顺序：先完成 Codex 的实际运行链路，再补齐其他厂商和管理功能。本记录区分候选运行时实测、产品消费者接线和发布验收，不把其中一层通过当成整个重构完成。

## 已有真实证据

最新两进程复验已经通过实际 `Execution.Start → Thread/Turn → Codex CLI → Chat 订阅和历史`，不再使用测试代写接纳回调。数据保存在临时 SQLite 和共享 ConversationStore 中，产品账号端口验证本机文件型订阅凭据与实际原生进程。两轮使用同一原生会话，重启时故意改变宿主工作目录环境，第二轮只发送新增文本，正确回复第一轮约定的短标记。

| 最新产品复验 | 结果 |
| --- | --- |
| 首发与完成 | 实际 Start 接纳为 running，原生调用完成，业务进入 completed |
| 运行中正文 | 两轮各收到 4 个已提交正文快照，再收到完成事件；未用假事件或改写原生请求 |
| 跨进程恢复 | 一次 thread/start、一次 thread/resume，原生会话 ID 不变；第二轮无历史重播 |
| 分页与重开 | 第一进程读到 2 条消息，第二进程重开后读到 4 条消息、2 页，内容与模型答复一致 |
| 用量 | 两个原生调用各归档一次主快照，均为 last_request、complete=false；公开整轮 Token 字段省略，原始已知计数仍可回读 |
| 原生权限 | 回合 readOnly、禁用网络、on-request，无审批与工具调用；线程初始化策略仍单独记录 |

生产装配已使用同一个 Store、ProviderRuntime 和 ThreadLifecycle，启动不创建旧 Manager。共享前端订阅和消息缓存已经按原生日志的稀疏持久游标接收更新；连接丢帧仍走原有 resync。前端相关 38 个定向用例通过。实际 HTTP 请求、桌面操作和视觉验收尚未执行。

这轮实跑先暴露了重启时完整宿主环境被纳入兼容摘要的问题。现在继承的进程环境与明确声明的原生配置分别处理：前者照常传给子进程，后者才参与持久兼容比较。配置和账号变化仍触发应有替换。原生用量投影也修正为只向既有整轮指标发布完整覆盖，避免丢掉覆盖信息后误导统计。

以下保留此前候选组件阶段的证据，不能与上述产品服务复验混为一层。

使用 Codex CLI 0.163.0 和原生目录返回的 `gpt-6.1-sol`，候选 `ProviderRuntime → ThreadLifecycle → ConversationStore` 已完成真实模型调用、热续聊、新进程恢复、分页和原生调用用量归档。

第一次三轮实跑暴露模式回显仍为 `agent`。原因是 Codex 模式 setter 成功后不发送模式通知，宿主只等待通知更新状态。修订后，成功回执更新会话模式，请求期间到达的更新原生模式通知优先。第二次两个独立进程各跑一轮，均返回预期内容，公开模式为 `read-only`，恢复时只提交本轮新增输入。

第二次实跑的传输观察器逐字节转发，没有改写请求。原生 `turn/start` 使用 `readOnly`、禁用网络和 `on-request`；没有审批请求或工具调用。线程创建和恢复仍使用 `workspace-write/on-request`，不能以回合模式推断线程初始化策略。

这两轮各保存一次主用量，重复提交终态没有增加计量。两轮主快照的覆盖范围均为 `last_request`、`complete=false`：最近请求已知，整轮总量未知。界面和统计不能把它标成完整整轮消费，也不能把未覆盖部分补零。

该次较早实跑使用候选组件和测试提供的 Chat 接纳回调；实际产品服务由本节开头的最新复验单独证明。

## 当前交付顺序

1. 已接通实际 Chat 读写、分页和订阅，共用一个新 Store。
2. 已接通实际 Execution 的新 Thread/Turn，移除旧 Runtime Sink 与重复正文、终态用量写入。
3. 已装配并实测 Codex 本机文件型订阅账号。当前不支持的账号与资源组合明确拒绝，不回退旧执行器。
4. 普通 Codex Chat 已通过开始、正文交付、终态、第二轮、跨进程恢复和唯一用量归档的产品服务复验。
5. 接着完成下表残余项；本轮尚未正式发布替换。

每步按完整所有者提交。公共接口与旧历史兼容仍要验证，不通过保留第二套执行器或伪造旧接口测试替身缩短迁移。

## 固定 OpenBot 依据

参考版本为 `f44dbe116c89a944f8d7d085e092c967b3fce0df`，按实际源码职责独立实现 Go 版本。

| 参考所有者 | 吸收的规则 | 本项目差异 |
| --- | --- | --- |
| [provider-runtime.ts](https://github.com/nightly-labs/openbot/blob/f44dbe116c89a944f8d7d085e092c967b3fce0df/src/backend/agent/provider-runtime.ts) | 初始化、账号确认后发布可用客户端，程序和账号分别管理 | 本地账号服务及其租约需要接入，不能用普通配置 Revision 代替认证代次 |
| [thread-lifecycle.ts](https://github.com/nightly-labs/openbot/blob/f44dbe116c89a944f8d7d085e092c967b3fce0df/src/backend/agent/thread-lifecycle.ts) | 公开聊天和原生会话分别持有身份；恢复、替换、交接各有明确分支 | 保留最近五个完整轮次或旧 Agent 隐藏摘要的产品策略 |
| [drain-scheduler.ts](https://github.com/nightly-labs/openbot/blob/f44dbe116c89a944f8d7d085e092c967b3fce0df/src/backend/agent/drain-scheduler.ts) | 先确认会话、再提交本次输入；普通续聊只送新增输入 | 业务事实和输入关系在同一个耐久提交门禁中写入 |
| [conversation-writer.ts](https://github.com/nightly-labs/openbot/blob/f44dbe116c89a944f8d7d085e092c967b3fce0df/src/backend/database/conversation-writer.ts) | 合并流式变更、更新同一消息投影、终态关联用量 | 保留 JSONL 权威和派生 SQLite 定位库，规范原生正文及用量只保存一份 |

存储取舍和测量口径见 [容量与分页设计依据](conversation-storage-cn.md)。

## 尚未完成

| 范围 | 当前边界与后续验收 |
| --- | --- |
| 产品 Chat/Execution | 普通 Codex 服务链通过；实际 HTTP、桌面和视觉验收未完成。正文暂用最长 2 秒的耐久快照窗口，未达到 OpenBot 的 100 毫秒增量节奏 |
| 账号 | 文件型系统订阅已核对用户和工作区身份及实际启动凭据；受管账号、API Key、仅 Keychain 凭据、主动失效代次和运行中外部换号隔离仍待完成 |
| 模型与管理 | 非默认模型、强度/Fast 切换、服务商探测安装升级、管理界面仍需产品接入和实测 |
| 用量 | 唯一原生结果归档及不完整整轮指标省略已有实跑证据；部分用量的专门呈现、明确零与未知的公开区别、汇总和费用估算、账号额度仍待完成 |
| 资源与独立生成 | 原生插件配置、严格无工具 Codex/Cursor 生成、知识生成和工作流输出消费者待完成 |
| 控制操作 | 产品分叉、完整压缩及其他尚未接线的控制能力，不能沿用旧执行器冒充已迁移 |
| Claude、Cursor | 原生候选和合成测试已有实现；尚未完成同等产品链路和真实账号/CLI 验收 |
| 其他厂商 | Qoder 共享 ACP 核心已有候选；Kimi、WorkBuddy、Grok、Gemini 等仍需厂商差异验收，Pi 保留独立 RPC 路径 |
| 存储已知问题 | 索引换代窗口可能遗漏全局输入归属校验；丢失索引时无关原生桶损坏可能阻塞健康 Chat 重建。先记录，仍属正式切换前阻断，不因 Codex 正常路径通过而关闭 |
| 生命周期与交付 | 账号失效、聊天删除及插件变更的闲置连接回收待接入；Thread 早期修订、Steer 新回执和 Codex 完整正文纠正仍需各自复审。依赖发布 pin、目录替换及旧 Manager 死代码移除未完成 |

本阶段只调整顺序，没有豁免发布前问题，也没有授权推送、发布或修改用户现有聊天数据。
