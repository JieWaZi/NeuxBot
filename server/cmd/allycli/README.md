# allycli 系统能力

`allycli` 是 NeuxBot 随应用交付的能力命令入口，供智能体在当前执行的授权范围内调用知识库、IM、工作流、团队、记忆和结构化问答。本文列出当前实现的完整操作；某次聊天实际能调用哪些操作，以该执行的 `capabilities` 返回为准。

命令与参数的权威定义是 [contract](../../internal/agentcapability/contract/catalog.go)，命令树直接由该目录建立。本文说明用途和完成语义，完整参数、枚举、限制与输出结构通过 `describe` 获取，避免维护第二套参数表。目录测试检查下列能力是否遗漏、重复，以及影响和完成语义是否与代码一致。

## 开始使用

应用向受管执行提供 CLI 路径 `ALLYCLI_BINARY_PATH` 和执行上下文 `ALLYCLI_CONTEXT_FILE`。系统 [ally 技能](../../../skills/ally/SKILL.md) 也给出技能目录内的实际可执行文件路径。使用该路径，不依赖全局安装或当前工作目录。

以下为应用已提供执行上下文时的 POSIX Shell 示例：

```sh
"$ALLYCLI_BINARY_PATH" capabilities
"$ALLYCLI_BINARY_PATH" describe knowledge.pages.search
"$ALLYCLI_BINARY_PATH" knowledge pages search --library-id library-example --query "部署流程"
```

Windows 使用当前 Shell 的环境变量语法及应用提供的 `allycli.exe` 路径。普通外部终端没有执行上下文时，不能直接获得应用的业务能力。开发时可在仓库根目录运行 `go -C server run ./cmd/allycli --help` 查看命令树，但这不签发业务授权。

操作标识中的点对应命令路径中的空格，例如 `workflow.runs.get` 对应 `allycli workflow runs get`。短标量可用命令行参数，复杂对象或长正文用 `--input request.json`；`--input -` 从标准输入读取 JSON。同一字段不要同时从文件和命令行提供。

## 通用命令

| 命令 | 用途 |
| --- | --- |
| `version` | 查看应用和 CLI 协议版本，不要求业务执行上下文。 |
| `capabilities` | 查看当前执行获授权的操作摘要、影响、完成语义和关联指南；完整 Schema 用 `describe` 读取。 |
| `describe <operation-id>` | 读取当前获授权操作的输入与输出 Schema、影响和完成语义。 |
| `guide <guide-id>` | 按需读取应用内置方法指南；默认 JSON，可用 `--output text` 获取正文。 |
| `doctor` | 检查当前上下文与 Core 是否可用；不代表账号、模型或外部平台已通过验证。 |
| `receipts get --operation <operation-id> --request-id <original-id>` | 查询原写操作的持久回执，不重新执行写入。 |
| `--help` | 查看根命令或具体子命令帮助，不要求业务执行上下文。 |

`capabilities` 是当前授权范围，不是全系统管理接口。操作不在当前执行的能力列表中时，知道其名称也不能调用。

## 影响与完成语义

| 字段值 | 含义 |
| --- | --- |
| `read` | 读取已有事实。 |
| `mutation` | 修改所属领域的内部事实。 |
| `external` | 接纳可能产生外部效果的工作，例如 IM 交付或工作流运行。 |
| `immediate` | 本次同步调用完成；读取操作返回本次观察到的事实。 |
| `accepted` | 工作或请求已接纳，实际结果需按所属资源继续查询。 |
| `candidate` | 候选结果或报告已提交，不代表所属执行已经成功结束或通过验收。 |

写操作必须携带稳定的 `--request-id`。响应丢失或结果未知时，使用原操作和原请求 ID 查询 `receipts get`，不更换身份重复提交。业务失败、权限失效和未知结果分别处理。

成功默认输出结构化 JSON，失败输出结构化错误并返回非零退出码。结果中的 `data` 由具体操作合同定义；先判断 `ok`、`completion` 和真实业务状态，再报告成功。只读查询及有界 `wait` 的超时不取消业务工作。

## 全部领域操作

以下是静态实现目录；每组说明其适用场景。完整参数请调用 `describe <operation-id>`，对象身份应来自实际发现结果。

<!-- allycli-operations:start -->

### 知识库

普通聊天的工具按当前执行可见的完整资源范围签发；模型只能执行用户明确要求的修改。页面AI修订使用同一CLI工具，额外固定当前库和页面，不获得其他库的写权限。

| 操作标识 | 用途 | 影响 | 完成语义 |
| --- | --- | --- | --- |
| `knowledge.libraries.list` | 列出当前执行可见知识库及完整资源身份。 | `read` | `immediate` |
| `knowledge.libraries.get` | 读取知识库配置、当前页面结构和权限版本。 | `read` | `immediate` |
| `knowledge.sources.list` | 按文件名、状态和排序分页查询实际来源。 | `read` | `immediate` |
| `knowledge.sources.get` | 读取指定不可变来源版本元数据。 | `read` | `immediate` |
| `knowledge.sources.read` | 分页读取持久原文分块，支持文内搜索或块定位，保留真实来源版本和位置；不启动解析或模型。 | `read` | `immediate` |
| `knowledge.pages.search` | 搜索已发布Wiki；本地首屏可附带原文检索命中。 | `read` | `immediate` |
| `knowledge.pages.get` | 按页面ID或slug二选一读取真实页面。 | `read` | `immediate` |
| `knowledge.pages.list` | 分页查询实际Wiki页面元数据。 | `read` | `immediate` |
| `knowledge.themes.list` | 读取Wiki真实目录结构。 | `read` | `immediate` |
| `knowledge.index.get` | 读取持久索引导言与动态目录；远端无合同则准确报不支持。 | `read` | `immediate` |
| `knowledge.revisions.list` | 分页查看页面历史版本。 | `read` | `immediate` |
| `knowledge.revisions.get` | 读取具体历史正文与证据。 | `read` | `immediate` |
| `knowledge.jobs.list` | 分页读取整理任务，区分接纳和真实终态。 | `read` | `immediate` |
| `knowledge.jobs.get` | 查询指定整理任务的实际状态、结果和失败事实。 | `read` | `immediate` |
| `knowledge.audit.get` | 复用既有孤儿页、断链、短正文和失效来源只读检查。 | `read` | `immediate` |
| `knowledge.activity.list` | 按实际游标和条件读取知识库活动。 | `read` | `immediate` |
| `knowledge.export.get` | 获取一致快照中的Markdown导出；副本修改不回写知识库。 | `read` | `immediate` |
| `knowledge.pages.write` | 以Agent来源创建或整页修订Wiki，保留有效证据与人工保护。 | `mutation` | `immediate` |
| `knowledge.pages.replace` | 按当前正文精确替换全部匹配内容，保留其他内容和版本历史。 | `mutation` | `immediate` |
| `knowledge.pages.rename` | 原子更新页面slug与入链，保留稳定页面身份和历史。 | `mutation` | `immediate` |
| `knowledge.pages.delete` | 按明确删除要求归档页面并清理入链，保留历史。 | `mutation` | `immediate` |
| `knowledge.revisions.restore` | 将历史内容恢复成新版本，保留当前目录位置。 | `mutation` | `immediate` |
| `knowledge.themes.write` | 新建或调整目录，或按明确要求删除空目录。 | `mutation` | `immediate` |
| `knowledge.sources.import` | 导入明确提供的原件字节，不自动启动整理；JSON字节字段为base64。 | `mutation` | `immediate` |
| `knowledge.jobs.start` | 按用户指定目标和附件／来源接纳本地后台整理。 | `mutation` | `accepted` |
| `knowledge.jobs.retry` | 按原冻结来源和配置显式重试失败任务；当前仅本地支持。 | `mutation` | `accepted` |

### IM 交付

目标和文件范围由当前执行绑定。交付接纳后需查询真实发送状态；平台接受消息也不等于收件人已阅读。

| 操作标识 | 用途 | 影响 | 完成语义 |
| --- | --- | --- | --- |
| `im.targets.list` | 发现当前执行获授权的消息交付目标。 | `read` | `immediate` |
| `im.deliveries.create` | 将指定消息和真实文件登记到持久交付队列。 | `external` | `accepted` |
| `im.deliveries.get` | 查看原交付的实际状态和失败事实。 | `read` | `immediate` |
| `im.deliveries.wait` | 在有界时间内观察原交付，超时不取消发送。 | `read` | `immediate` |

### 工作流

创作操作绑定当前工作流；运行操作使用当前执行获授权的工作流定义和版本；结果提交只供实际工作流 worker 使用。

| 操作标识 | 用途 | 影响 | 完成语义 |
| --- | --- | --- | --- |
| `workflow.authoring.prepare` | 按实际 DSL 原语准备所需指南、SDK、当前源码、待处理批注及保存合同。 | `read` | `immediate` |
| `workflow.source.get` | 读取绑定工作流的当前或指定修订源码。 | `read` | `immediate` |
| `workflow.source.validate` | 对完整源码执行可选预检，返回诊断和摘要。 | `read` | `immediate` |
| `workflow.source.save` | 编译并按期望修订保存完整源码，防止覆盖并发修改。 | `mutation` | `immediate` |
| `workflow.sdk.get` | 读取一个 SDK 主题或示例；合法名称由发现合同返回。 | `read` | `immediate` |
| `workflow.resources.list` | 分页发现当前创作可用的一类资源。 | `read` | `immediate` |
| `workflow.annotations.list` | 读取绑定工作流的创作批注。 | `read` | `immediate` |
| `workflow.definitions.list` | 发现当前执行获授权的工作流定义。 | `read` | `immediate` |
| `workflow.definitions.describe` | 读取一个定义的版本和输入输出合同。 | `read` | `immediate` |
| `workflow.runs.start` | 按明确版本及冻结输入接纳一次运行。 | `external` | `accepted` |
| `workflow.runs.get` | 读取原运行、实际请求与业务状态。 | `read` | `immediate` |
| `workflow.runs.wait` | 在有界时间内观察原运行，超时不取消执行。 | `read` | `immediate` |
| `workflow.runs.cancel` | 对获授权运行请求取消，不撤销已经发生的外部效果。 | `mutation` | `accepted` |
| `workflow.evidence.list` | 读取运行及外部效果的原始执行证据。 | `read` | `immediate` |
| `workflow.results.schema` | 获取当前 worker 尝试的结果 Schema。 | `read` | `immediate` |
| `workflow.results.submit` | 按 Schema 提交当前 worker 的实际候选结果。 | `mutation` | `candidate` |

### 团队协作

负责人获得当前团队对话的成员和任务操作；成员只提交所属任务尝试的真实报告。任务不能跨团队对话或冒用其他成员身份。

| 操作标识 | 用途 | 影响 | 完成语义 |
| --- | --- | --- | --- |
| `team.members.list` | 读取当前团队对话成员身份与职责。 | `read` | `immediate` |
| `team.tasks.list` | 查询当前团队对话的任务看板。 | `read` | `immediate` |
| `team.tasks.get` | 读取当前团队对话内一个任务的实际状态和报告。 | `read` | `immediate` |
| `team.tasks.create` | 向非负责人成员分配正式任务及依赖。 | `mutation` | `accepted` |
| `team.tasks.update` | 修改状态允许的待处理任务字段。 | `mutation` | `immediate` |
| `team.tasks.follow-up` | 向原任务追加指令，保留任务与尝试身份。 | `mutation` | `accepted` |
| `team.tasks.retry` | 在真实状态和重试额度允许时显式重试失败任务。 | `mutation` | `accepted` |
| `team.tasks.cancel` | 在当前状态允许时取消任务。 | `mutation` | `accepted` |
| `team.reports.submit` | 提交当前成员尝试的事实报告及是否可交用户查看。 | `mutation` | `candidate` |

### 记忆

读写范围由当前伙伴和实际绑定项目决定。搜索默认覆盖全部可见范围；创建时必须明确伙伴或项目归属，不能创建到 `all` 范围。

| 操作标识 | 用途 | 影响 | 完成语义 |
| --- | --- | --- | --- |
| `memory.entries.search` | 有界搜索可见记忆，返回实际使用的语义或关键词检索模式。 | `read` | `immediate` |
| `memory.entries.get` | 读取一条可见记忆。 | `read` | `immediate` |
| `memory.entries.create` | 将一条稳定事实保存到明确的伙伴或项目范围。 | `mutation` | `immediate` |
| `memory.entries.update` | 替换已知记忆的内容，保留原归属。 | `mutation` | `immediate` |
| `memory.entries.forget` | 遗忘一条当前记忆，保留其变更历史。 | `mutation` | `immediate` |

### 结构化问答

问题属于当前执行，用户通过应用回答。待回答、跳过、取消或失效均不是同意；业务问答也不代替真实工具授权或用户验收。

| 操作标识 | 用途 | 影响 | 完成语义 |
| --- | --- | --- | --- |
| `interaction.questions.create` | 按发现合同支持的 Schema 向用户提出结构化问题。 | `mutation` | `accepted` |
| `interaction.questions.get` | 读取当前执行问题的实际回答状态。 | `read` | `immediate` |
| `interaction.questions.wait` | 在有界时间内等待实际回答，超时不关闭问题。 | `read` | `immediate` |

<!-- allycli-operations:end -->

## 调用和核对示例

先发现目标和操作，再提交用户要求的工作。以下示例中的 `request.json` 是调用者准备的真实 UTF-8 JSON 文件，字段以对应 `describe` 为准：

```sh
"$ALLYCLI_BINARY_PATH" describe im.deliveries.create
"$ALLYCLI_BINARY_PATH" im deliveries create --input request.json --request-id delivery-example-01
"$ALLYCLI_BINARY_PATH" receipts get --operation im.deliveries.create --request-id delivery-example-01
```

已知交付身份后，可在最长 60 秒的观察窗口内等待：

```sh
"$ALLYCLI_BINARY_PATH" im deliveries wait --delivery-id delivery-example --timeout 30s
```

已有明确的工作流创作需求时，优先读取所需上下文，避免遍历全部指南：

```sh
"$ALLYCLI_BINARY_PATH" workflow authoring prepare --primitives functionTask
```

## 当前边界

- 本文不是全系统管理权限清单。伙伴、项目、插件、账号和自动任务等应用管理界面，不会因为存在 HTTP API 就自动变成 CLI 能力。
- 页面修订要求当前可写范围及真实版本；整页写入保留人工保护，明确删除内容优先用精确替换。来源字节和原文分块仍由独立核心保存。
- `knowledge.jobs.start` 和 `knowledge.jobs.retry` 的接纳回执不证明 Wiki 已发布；用 `knowledge.jobs.get` 查询原任务的真实结果。当前没有知识任务专用 `wait`，不要把超时或接纳当成完成。
- 本地、自有远端和官方 WeKnora 的实际能力不同。明确返回不支持或权限不足时，不回退同名本地库或更换凭据。
- `allycli` 不提供任意 HTTP 透传、连接文件编辑、执行上下文自助续期或通过数据库路径扩大范围的入口。

## 方法指南与实现入口

| 场景 | 指南 |
| --- | --- |
| 知识检索、整理与Wiki维护 | [knowledge](../../../skills/ally/references/knowledge.md)、[knowledge.maintenance](../../../skills/ally/references/wiki-maintenance.md) |
| 消息和文件交付 | [im.delivery](../../../skills/ally/references/im-delivery.md) |
| 工作流创作与运行 | [workflow.authoring](../../../skills/ally/references/workflow-authoring.md)、[workflow.execution](../../../skills/ally/references/workflow-execution.md) |
| 工作流结果、交付与 API 查询 | [workflow.results](../../../skills/ally/references/workflow-results.md)、[workflow.output-delivery](../../../skills/ally/references/output-delivery.md)、[workflow.api-retrieval](../../../skills/ally/references/api-retrieval.md) |
| 团队负责人和成员 | [team.lead](../../../skills/ally/references/team-lead.md)、[team.member](../../../skills/ally/references/team-member.md) |
| 记忆与用户问题 | [memory](../../../skills/ally/references/memory.md)、[interaction.questions](../../../skills/ally/references/user-questions.md) |

授权、回执和执行生命周期见[系统技能与软件能力](../../../docs/architecture/agent-capabilities-cn.md)。修改命令时同时更新能力目录、真实领域绑定、本 README 及相关指南；运行 `go -C server test ./internal/agentcapability/contract ./internal/agentcli` 和 `pnpm docs:check` 核对合同与文档。
