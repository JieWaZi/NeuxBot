# 知识库 HTTP MCP 接入规格

> 状态：实现完成，定向自动化验证通过；尚未执行真实第三方 Agent 接入与桌面视觉验收。基于 main 的独立 worktree，由 GPT-6.1 high 子代理分别实施前后端，主代理完成架构审查、集成和并发验证。

## 1. 目标

内部 Agent 通过 allycli，外部 Agent 通过 Streamable HTTP MCP 调用同一套知识操作。MCP 是传输入口，不建立第二套业务、索引、授权、任务状态或聊天会话。复用现有发布服务和 API Key，提供 allycli 对应的知识读取、编辑、导入和整理能力。不新增外部 CLI、Skill 或 stdio 服务。

参考 WeKnora 提交 `0d7516ea0`：`internal/mcpserver` 提供 HTTP 服务，`frontend/src/views/integrations/McpServerIntegrationSettings.vue` 提供独立配置页面。参考协议和交互，不复制其知识业务。

## 2. 独立 MCP 集成页

在设置“发布集成”分组增加“MCP 集成”，与 API 集成并列。此页是同一服务的接入视图，不是新的端点管理系统。

- 复用 integrationServiceOptions 查询，MCP 默认关闭，页头只提供开启／关闭动作，开启后展示可复制的 HTTP MCP JSON 配置。
- URL 使用已保存的 publicBaseUrl，移除末尾 `/api/v1` 后追加 `/mcp`，保留代理路径前缀。无确定访问主机时不生成伪可用 URL，不使用未保存草稿。
- 配置使用 `X-API-Key: <YOUR_API_KEY>` 占位符。不读取或保存已有 Key 明文；页面不提供 API 集成跳转。
- 移除独立 URL 区块与底部权限、连接边界说明。禁用或绑定失败不显示连接成功。
- 复用共享 UI、语义 Token、i18n、加载/失败/复制反馈；MCP 与 API 各有开关，保存在同一设置文件；任一开启时复用唯一监听，仅接纳已开启入口的请求。不增加独立端口、Token、工具白名单、Endpoint CRUD 或密钥表。

WeKnora 的端点本身是另一种授权主体，我们已有 API Key；只借鉴独立接入说明，不照搬其持久模型。

## 3. 协议与命令

唯一发布监听在 `/mcp` 挂载已有 modelcontextprotocol/go-sdk 的 Streamable HTTP handler，优先无状态模式，初始化/通知/版本/方法语义交给 SDK。请求体有界，Origin 沿用服务间发布策略。默认认证 X-API-Key；若兼容 Bearer，必须使用同一 Key 并拒绝歧义双凭据，不增加 OAuth 或另一类 Token。

工具名、Schema、说明和执行效果来自 `server/internal/agentcapability/contract` 的知识命令，稳定映射为下划线名，如 `knowledge_pages_get`。禁止第二份业务 Schema，禁止泛化任意读写工具或暴露非知识管理操作。写操作的 requestId 等同 allycli 幂等身份，不是 JSON-RPC id。结果提供结构化内容和文本兼容，错误使用稳定错误码和 isError，不泄露底层错误及凭据。

合同对齐测试覆盖全部知识操作：发现、检索、页面/来源/目录/索引/历史/任务/审计/活动/导出读取，页面写入/替换/重命名/归档、版本恢复、目录维护、来源导入、任务开始与重试。保留 CLI 的 CAS、历史、来源及完成语义。

远端没有内部 Execution 附件序号：整理必须明确 libraryId，选择已导入 sourceVersionIds 或 pending 来源；非空 attachmentIndexes 拒绝并指引先导入，不读取宿主路径或伪造 Chat。外部 backend/connectionId 只允许发布服务器本地库，不转授宿主保存的上游凭据。方法一致、身份与资源范围由入口授权。

同输入和 requestId 可恢复原回执；若提供 receipt 工具，复用 Binding.Receipt 并限定主体、操作与库，不接受任意内部回执身份。

## 4. 业务复用与授权

读取复用 ProviderReadBindings、LocalReadProvider 和现有原文检索。Wiki 写入从 wikiBindings 提取真实共用流程，Execution 与 API Key 仅注入身份、范围、提交屏障和活动来源，保留同一 applyWikiCommand、事务、版本、持久回执及 knowledge.changed 事件。任务复用 Organize/Retry，不创建外部聊天或虚假 Execution。

权限映射：读取 retrieve；内容编辑/归档/恢复/目录维护/原件导入 retrieve + ingest；任务开始/重试 retrieve + organize。整理权限不赋予任意编辑。每次 tools/call 校验当前 Key 和真实目标，工具发现不代替授权。列表不可泄露未授权库名称/计数，空范围拒绝。页面、来源、历史和任务必须核对真实所属库。

写请求身份隔离主体与入口；同键同输入重放，同键异输入冲突，跨 Key 不共享回执。同步写入和撤权沿现有提交屏障；锁顺序一致，避免授权读锁嵌套导致有等待写者时死锁。后台任务冻结 Key、凭据代次、授权版本和库策略，每次提交复查；撤权或重启不能重新赋权。

任务重试保持原任务冻结授权，不能用新 Key/内部入口恢复已撤权外部任务，外部重试也不能脱离当前 Key 的后台提交约束。后端必须具体核对方案交主代理审查，不得静默删掉重试能力。查询不调用回答模型；向量检索保留真实 Embedding 和失败。写入成功沿原资源事件通知，冲突不发成功事件。

## 5. 验收

1. SDK 客户端 initialize/tools/list/tools/call 通过；无状态 GET/DELETE/通知正确；缺 Key、无效 Origin 拒绝。
2. 工具逐项覆盖 allycli 知识合同，Schema 同源，无非知识工具泄漏。
3. 真实临时数据库下验证读、建页、替换、重命名、恢复、归档、目录修改、原件导入；内部入口可读外部结果、历史与引用正确。
4. 只读 Key 拒绝写；ingest/organize 隔离；跨库 ID、撤销、过期、缩权和远端身份拒绝。
5. 重放无二次版本、异内容冲突、旧页面/目录/更新时间不能覆盖新内容，回执不能跨主体。
6. 整理/重试保留原授权，撤权阻止后台后续提交，接纳不伪报完成。
7. 前端测试导航、URL/代理前缀/IPv6/回环、配置复制、禁用/失败/加载、占位凭据；不复制服务器状态。
8. 定向测试、类型/样式/格式、docs:check；不运行全量 check/go test ./...。未经授权不打开、控制或截图运行中 UI。未完成的真实客户端/视觉验收明确说明。
