# Agent Runtime 全 CLI 结果调用接入计划

> 执行方式：Sol 6.1 xhigh 并行实施，Terra 6.1 处理简单清点；整阶段完成后统一 review，不逐项反复审查。

**Goal:** 把现有全部 CLI 的知识结果调用接入同一个独立 Runtime，删除旧 GenerateText 执行链，避免后续业务迁移再补厂商适配。

**Architecture:** acp-go 拥有原生协议与消息身份；独立 Runtime 通过公开 ACP 完成一次会话、配置、取消和最终结果收集；runtimehost 解析产品启动事实、账号及各公开工厂。Chat、团队与工作流等后续入口继续按原迁移计划实施。

**Tech Stack:** Go 1.25.8、公开 ACP SDK、独立 acp-go、真实 CLI 加本机假 Provider。

**Spec:** [独立 Runtime 设计](agent-runtime-refactor-spec-cn.md)，以用户最新确认的独立 CLI、最终结果消费及阶段末评审要求为准。

## 范围与约束

- 接入 codex、claude、opencode、gemini、grok、workbuddy、qoder、qoder-cn、kimi、cursor、pi 及其 Profile，不能把地域变体合并成另一身份。
- 每次独立启动 CLI 会话，只向业务返回最后结果；不主动注入 AGENTS.md、Skill 或 MCP，不接管 CLI 原生发现和工具执行，不建设 Runtime HTTP。
- 模块不导入 server；原生解码不复制进主仓库；账号投射与刷新复用现有权威。
- 无自动切回旧引擎、无工具后故障重放；取消和清理仍保留真实所有权，关闭失败只重试关闭。
- 原生最终阶段与公共 Prompt 成功结束的证据分别表达。Codex 有原生阶段时严格使用原生证明；其他适配只在外层成功返回并排空通知后收敛最后有效助手消息，不把工具前的过程文本当结果。
- 长度终止保留原生截断语义与可用末条文本，错误和取消不交付成功正文；未知用量与零值区分。
- Grok 与 Qoder CN 本机未安装；记录协议夹具和真实 CLI 验收的不同覆盖，不能用前者声称原生运行成功。已安装九类优先使用假服务验证，不消耗真实账号。

## 目录与接口

| 所有者 | 文件/职责 | 消费关系 |
| --- | --- | --- |
| 独立 acp-go | pkg/acpmeta 消息事实；nativeacp、codex、claude、cursor、pi 身份与边界投影 | SDK MessageId 为唯一身份；公共元数据表达阶段及来源 |
| 独立 Runtime | driver/acp/session.go、driver.go 及对应测试 | 读取公共事实；CompletionPolicy 区分原生阶段与成功 Prompt 的末条消息；ModeCandidates 按真实目录选择 |
| 宿主 | server/internal/runtimehost，runtimes 的启动解析接口 | SDK.PrepareLaunch 返回 BaseRuntimeID、Executable、PrefixArgs、Environment；不创建旧执行对象 |
| 旧执行清理 | server/internal/runtimes/text.go、Text collector 及相关分支 | 所有知识路由切换后直接删除；Chat/维护共享实现仍保留 |
| 原生验证 | packages/agent-runtime/driver/acp/*_native_test.go | opt-in；临时 HOME/cwd、假凭据、回环 Provider；不读取用户账号 |

## 重点验证

1. 所有内置身份及 Profile 保留二进制、前缀、环境、配置根；Qoder CN 不串国际版。
2. Claude 的 fast 选项不能误认为 model；Pi 的思考模式不能当权限；OpenCode 受管模型转换只做一次。
3. 缺失原生消息 ID 时的投影有稳定身份，工具/过程之后的新答案与旧消息分开；阶段未知不能冒充原生 final。
4. Cursor 临时状态在本次工作区内，不能共享 `/tmp/cursor-state`；Pi 使用已配置 bridge 路径，不额外安装连接器。
5. 多工具循环、无末条答案、失败/取消、长度截断、账号回收失败不重放；保持未迁 Chat 和 Wiki 维护的行为。

### Task 1: 统一公开消息事实

- [ ] 为 acpmeta 增加阶段/完成事实的公开读写接口，原生信息由 owning adapter 提供。
- [ ] 补 Claude、Cursor、Pi 和 nativeacp 稳定消息身份；原生缺 ID 的投影保持有界，工具与思考边界分段。
- [ ] Codex 继续保留旧 Chat 使用的原生元数据，同时提供统一事实；禁止提前在 nativeacp 返回前发最终证明，避免越过 Kimi/Gemini 外层失败检查。
- [ ] 定向执行 acpmeta 和改动适配器的身份/边界测试，提交独立依赖开发分支。

### Task 2: 全部工厂与结果调用

- [ ] 复用现有启动解析，新增 PrepareLaunch；启动事实测试覆盖 11 类及 Profile。
- [ ] runtimehost 使用公开工厂；Codex 单独保留 OAuth broker；其他厂商保留既有账号、模型、设置及状态差异。
- [ ] ACP Driver 统一配置选择与外层 Prompt 末条结果收敛，覆盖模式目录、过程/工具分界、空结果、长度与取消。
- [ ] 所有结果路由使用新内核；删除 Legacy、旧 TextRequest/TextResult/textInvocation/textCollector 及仅为其存在的分支和测试。
- [ ] 对宿主、账号和旧 Chat/维护直接受影响包运行定向测试，不运行全仓验证。

### Task 3: 真实 CLI 验证

- [ ] 保留真实 Codex 最终性和工具循环回归。
- [ ] 对已安装 CLI 使用各自公开工厂与本机假服务验证启动、请求和最终结果，测试环境与真实用户配置分离。
- [ ] 原生登录要求、不可配置假服务或缺失程序分别记录；失败不回退真实账号，不冒充验收通过。

### Task 4: 阶段收尾

- [ ] 固定已推送的真实外部依赖，普通 go.mod 无临时绝对替换。
- [ ] 汇总各 CLI 实现、协议夹具、真实 CLI 证据与剩余边界。
- [ ] 完成一次跨模块 review，一批修复发现的问题，必要回归后提交阶段结果。
