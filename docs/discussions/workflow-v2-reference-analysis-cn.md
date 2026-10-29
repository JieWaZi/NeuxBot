# Workflow V2 参考流程分析

本文是一次参考产品调查记录，用于支持 [Workflow V2 设计](workflow-v2-design-cn.md)，不描述 NeuxBot 已有能力。调查范围为用户指定的 QoderWake 工作流编辑页面、页面内生成与修正对话、工具卡参数及响应、脚本和画布。后续独立实验另列，不能把历史工具记录当成本次重新执行。

原始记录单独交付；下列示例替换工作流、运行、伙伴身份及摘要值，保留调用结构。没有获得参考产品服务端源码，MCP 的内部实现和 DSL 执行器只能作有边界的推断。

## 1. 已观察的调用契约

### E1：工作流上下文

页面是工作流内部的“对话编辑 WakerFlow”。历史中以下两种读取均取得当前工作流：

```json
{
  "tool": "mcp__wake__workflow_get_script",
  "arguments": { "workflowId": "workflow-example" }
}
```

```json
{ "tool": "mcp__wake__workflow_get_script", "arguments": {} }
```

响应数据包括 `workflowId`、`name`、`script`、`digest`、`meta`、`updatedAt`。可确认读取工具存在当前对象的隐式绑定；绑定来自会话、请求上下文还是其他宿主配置，未能确认。

### E2：修改与保存

观察到四次 `mcp__wake__workflow_upsert`，**四次都显式提供了 `workflowId`**：

```json
{
  "workflowId": "workflow-example",
  "baseDigest": "previous-source-digest",
  "patches": [
    {
      "search": "  args: ['--data-dir', DATA_DIR, 'status'],",
      "replace": "  args: [TOOL_PATH, '--data-dir', DATA_DIR, 'status'],"
    }
  ]
}
```

成功响应提供 `id`、`digest`、`meta` 和 `version`，其中 `meta` 包括 `phases`、`callSites`、Schema 和 `actions`。版本按 2、3、4、5 推进，每次使用前一次摘要，补丁数分别为 7、1、4、9。

可推断其对外采用“读取基准—提交修改—返回新摘要及投影”的闭环。未观察摘要冲突、多匹配补丁或失败保存响应，不能宣称其 CAS、补丁原子性及冲突恢复已经验证。

### E3：运行诊断

```json
{
  "tool": "mcp__wake__get_workflow_run_context",
  "arguments": { "runId": "run-example", "detailLevel": "events" }
}
```

响应包含 `run`、`final`、`events`、`pendingConfirmations`，并提供事件数量及截断信息。同一次运行的完整事件模式有 41 条事件且 `truncated:false`；摘要模式仍报告原事件总数，但 `truncated:true`。摘要不是完整执行轨迹。

两次真实历史故障分别为：

- 调用 Python 时遗漏工具路径，错误为 `ACTION_SCRIPT_ERROR`、`Unknown option: --data-dir`。
- 脚本路径中的 `~/` 没有展开，被 Python 当作字面文件路径。

诊断对话先读取实际运行，再修改源码。可借鉴的是错误到调用点的可追踪关系，不是让 Agent 凭最后一段报错重写流程。

## 2. 三轮修正的真实顺序

| 对话轮次 | 已观察步骤                                                                                                                        | 可以得出的结论                                                    |
| -------- | --------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| 第一轮   | 读取失败运行事件 → 读源码 → Bash/Read 修改与本地 harness → upsert 至版本 2 → 再改至版本 3 → 读取比对                              | 参数错误进入源码修复，保存有明确响应；本地测试与真实运行分开      |
| 第二轮   | 读取另一失败运行 → 发现字面 `~` → upsert 至版本 4 → 读回并比对                                                                    | 用脚本解析绝对路径，修复执行目录假设                              |
| 第三轮   | 读取运行成功但含大量业务错误的结果 → `{}` 读取源码 → 本地模拟、历史记录中的单页 API 验证 → upsert 至版本 5 → 读回、mock、摘要读取 | 运行终态不是业务质量结论；未见版本 5 保存后真实全量流程成功的证据 |

历史有 `node --check` 因顶层 `return` 报错，之后改为 `AsyncFunction` 包装的本地 harness。它说明普通 Node 文件语法检查不适配这份脚本的宿主执行形态；不能据此证明参考产品后端也使用 `AsyncFunction`。

当前历史未见独立的工作流 `check`/`validate` MCP 调用，也未观察保存校验拒绝。**这不证明产品没有这些能力，更不证明参考 Agent 严格遵守了所有检查。**

## 3. 脚本如何组成工作流

经只读核对，取得的本地脚本副本 SHA-256 与历史版本 5 的摘要相同；脚本约 61.8 KB。以下是结构摘要，不是可直接运行的兼容源码：

```text
meta
  inputSchema / outputSchema
  phases：准备工具 → 令牌 → 选择空间与目录 → 增量同步 → 汇总
  actions：check-sync-tool / install-sync-tool / run-sync-tool
常量
  数据目录输入、工具版本、内嵌 Python 程序
编排
  phase → action 检查工具
  条件 action 安装工具
  action 读取配置、鉴权
  条件 askUser 补充初次凭据
  action 获取空间和目录树
  条件 askUser 选择空间、多个目录
  action 保存配置、增量同步
  条件 worker 校验文件并写中文摘要
  return 业务结果
```

### E4：本地脚本

内嵌 Python 提供 `status`、`auth`、`spaces`、`tree`、`configure`、`sync` 等子命令。工作流先检查文件和版本，缺失或过期时进行语法编译检查，再以临时文件及替换方式写入工具文件；后续通过 `python3`、工具路径和 argv 调用。

Python 程序约定 stdout 输出 JSON，日志写 stderr。编排读取 `exitCode`、`stdout`、`stderr`，解析业务字段并决定后续动作。大部分同步工作由确定性程序完成，仅最后的阅读、抽查和摘要需要 Agent。

这是一份生成出来的业务工具生命周期，不足以证明参考宿主具有通用脚本资产管理、完整目录隔离或效果幂等机制。

### E5：用户决策和动态数据

`askUser` 接受问题、`label`、`phase`，可带 `options`、`multiSelect`；答案使用 `value`、`skipped`。

- 凭据已经存在时不再追问；缺失时在运行中询问。
- 空间由脚本查询，选项从真实返回的空间数组生成。
- 页面树由脚本查询，目录选项带层级缩进，支持多选；之后转换回页面 ID。
- 样本只展示前 40 个选项，剩余项允许用户提供 URL 或 ID。它有明确的覆盖限制，不能把 40 项视作完整目录。

此样本在部分位置通过显示文案找回 ID；同名、改名和本地化容易影响解析。NeuxBot 已有 `{value,label}` 选项，应保留业务键，不能照搬字符串反查。

### E6：Agent 执行

`worker` 的任务包括读实际文件、核对同步报告、抽查 Markdown 和图片、写摘要；选项包含 `label`、`phase`、`inputs`、`outputs`、`schema`、`resolve:{kind:'waker',wakerId:...}` 和 `mcpServers`。

这里同时存在伙伴身份、节点职责和节点工具选择。所谓“用户角色”还需要分清真实用户的决策行为与 Agent 扮演的业务角色；样本并未证明有多人角色权限系统。

### E7：画布

画布呈现五个阶段及本地脚本、用户决策、员工节点。节点详情分别包含：

| 节点     | 已观察详情                                  |
| -------- | ------------------------------------------- |
| 本地脚本 | 标签、行为说明、类型、风险、命令、Action ID |
| 用户决策 | Prompt                                      |
| 员工     | 标题、指派 Waker、Auto 模型、连接器、Prompt |

条件分支中的节点也静态显示，画布与源码中的声明及调用位置对应。`callSites` 是静态位置，不能当作执行日志。未观察其内部 AST、布局算法或页面组件生成机制。

### E8：执行成功与业务质量

一条真实历史运行的状态为 `succeeded`，业务结果同时报告 56 项新增、350 项错误、0 张图片和 1 个附件。末尾源码返回 `ok:true`，校验结果可以为 false 或 null；没有变化时会跳过摘要 Agent。

因此 V2 不能只学习它的外观：必须区分执行终态、覆盖率、校验结论和交付状态，且不能让 `ok:true` 覆盖实际问题。

## 4. 逆向结论的可信边界

| 问题                           | 当前结论                                                         |
| ------------------------------ | ---------------------------------------------------------------- |
| 工具叫什么、参数和响应如何组织 | 有页面工具历史证据                                               |
| 读取如何取得当前工作流         | 确认空参数读取有效；隐式绑定的内部实现未知                       |
| 修改是否基于上一份源码         | 确认 `baseDigest` 加补丁的成功序列；失败原子性未知               |
| DSL 能否调用脚本、用户和 Agent | 已确认样本包含对应声明和调用；执行器内部未知                     |
| 用户选项能否由脚本结果生成     | 已确认样本源码如此组织；每条交互是否在最新版本真实执行未全部验证 |
| 画布是否依据源码               | 字段和调用位置吻合，属于有证据的推断；内部投影算法未知           |
| 是否所有检查都被强制遵守       | 未确认                                                           |
| 新版本是否真实全量成功         | 未确认                                                           |

## 5. 对 V2 的直接启示

1. 工作流编辑上下文应由宿主绑定；ID 用于定位和校验，不能让模型猜测当前对象。
2. 生成、修正都要有“读取基准—提交—诊断—保存回执—画布更新”的完整闭环。
3. 脚本负责可重复的数据获取和处理，用户负责不可推导的业务选择，Agent 负责推理与表达。
4. 动态候选必须保留业务 ID、数据快照、分页和真实选择来源。
5. 画布节点使用稳定源码身份；运行实例、尝试和工具调用另有身份。
6. 不能以工具返回成功、运行终态成功或模型自报检查通过代替业务完成标准。

## 6. 独立新建与实际交互实验

用户追加授权后，通过 computer use 新建了独立实验工作流并实际运行。实验使用虚构项目、三个固定候选和本地 Python 纯数据处理，未运行原业务同步。原始调查记录保留具体工作流地址、工具参数、版本摘要和 runId，另行交付。

### E9：生成阶段的保存拒绝与恢复

首次生成在此历史中直接调用 `mcp__wake__workflow_upsert`，参数为 `{workflowId,script}`，未观察到此前另调 discover、SDK 或 prepare。完整源码首次被拒绝：

```json
{
  "code": "SYSTEM_WAKER_DIRECT_USE_ONLY",
  "message": "Waker system-example can only be used in a direct local conversation."
}
```

此时没有可运行画布，运行按钮禁用。生成 Agent 更换为可供工作流使用的实际伙伴后，同形 upsert 成功，响应返回 version 1、digest、meta 和 callSites。这次观察证明参考产品至少有伙伴可用性的保存门禁及一次自动修复路径；不能将其扩大为所有业务质量检查都受强制。

### E10：缺参、脚本候选、用户选择和 Agent 确实串联

实际运行的链路为：

```mermaid
flowchart LR
  Missing["项目输入为空"] --> Project["askUser 补充项目"]
  Project --> Candidates["Python 返回三个候选"]
  Candidates --> Pick["askUser 选择候选"]
  Pick --> Agent["worker 分析"]
  Agent --> Check["Python 核对项目与候选"]
  Check --> Result["返回结果"]
```

两次用户请求都实际进入 pending，再由 UI 回答。脚本候选响应包含 id/label，编排读取 `genResult.data.candidates` 后构造 askUser 选项；实际选择使用演示项目和第二个候选。该实验支持“先查询事实，再让用户决策”的主链路，不只是静态源码截图。

### E11：三轮生成、修正、运行的结果

| 版本 | 修正与实际现象                                                                                                                                             | 实际终态                                                                            |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| 1    | worker 返回 `{content:"..."}`；编排执行 `String(analysisResult)`，变成 `[object Object]`；Python 检查 `valid:false`、项目和候选均未匹配；源码仍正常 return | `succeeded`，但业务 `validated:false`                                               |
| 2    | 基于前次 digest 提交两个精确补丁：读取 content，校验失败 throw；本轮 worker 返回 `{analysis:"..."}`，字段仍不匹配，校验失败                                | `failed`，存在 fatal.raised；外层错误为 `WORKER_BACKEND_ERROR`                      |
| 3    | 读当前源码后用一个补丁添加必填 `analysisText:string` 的 worker Schema，并只读取该字段，缺失抛错                                                            | worker 返回 analysisText，Python `valid:true`，最终 `succeeded` 且 `validated:true` |

诊断通过 `mcp__wake__get_workflow_run_context` 的 `detailLevel:"full"` 取得。三轮分别有 29、30、29 条事件，均报告 `truncated:false`。运行由 UI 启动，未观察到 MCP 启动工具调用。

结论是：自由文本或不稳定对象输出不能作为可靠节点接口；单纯正常 return 也不会自动把业务布尔值解释成运行失败。显式 Schema、字段读取和失败分支共同形成闭环。一次正向通过不能证明自动 Schema 重试、多选、大候选集、断线或重启恢复已通过。

### E12：没有取得的证据

实验调用过 `Skill({skill:"workflow-authoring"})`，工具卡实际内容仅为 `Launching skill: workflow-authoring`。仅靠页面调查时没有取得完整 Skill；后续 JSONL 调查已取得其注入正文，见 E13。服务端源码和 Skill 的原始加载文件仍未确认，Agent 自述不能替代原始契约。

当前可确认的节点/原语包括本地 script action、askUser 和 worker，原流程另有 phase、分支与数据处理。未对其他节点类型进行穷举，也没有证据证明其耐久执行、数据库结构或 MCP 上下文绑定内部算法；V2 中的 AST/IR/检查点方案来自 NeuxBot 当前源码。

## 7. JSONL 中的 Skill 注入证据

### E13：Skill 正文位于独立元消息

根据本机服务进程定位到 QoderWake 数据目录后，找到了原参考流程的一份创作会话 JSONL，以及独立实验的 JSONL。只检查对应会话，不将其他会话作为调查材料。

实验日志中的调用关系为：

| JSONL 行 | 记录                | 内容                                                          |
| -------- | ------------------- | ------------------------------------------------------------- |
| 62       | assistant/tool_use  | `Skill({skill:"workflow-authoring"})`                         |
| 66       | user/tool_result    | `Launching skill: workflow-authoring`                         |
| 68       | user，`isMeta:true` | 完整 `message.content`，标题为 `Workflow authoring reference` |

第 68 行正文为 16,768 字符、16,941 UTF-8 字节，已逐字提取并与 JSON 字段复核；SHA-256 为 `10731dde99756f423381f81fe7bed9c15b9f7479a41eff4f6ba5ca5f56d3fecb`。原始文件位置和提取件另行交付，避免在公开设计中写入个人路径及完整会话内容。

该正文说明 `agent()`、`pipeline()`、`parallel()`、`phase()`、`log()`、`workflow()`、结构化输出和基于 journal 的恢复；它描述的是 `Workflow` 工具契约，不是已经验证的 wake MCP `worker()/action()/askUser()` 完整 SDK。因此，关于并行失败转 null、缓存恢复及并发上限等说明只能归属于这份工具文档，不能推广成 WakerFlow 的运行保证。

这一证据揭示两个可用于后续研究的边界：工具卡未必展示实际注入模型的全部上下文；原生 CLI 编排工具和产品持久工作流可能同时存在，必须分别确认契约。日志能还原这次注入的 Skill 正文，不证明全部 Skill、系统指令和 MCP 定义都完整落盘。

## 8. 失败恢复、跳过与终止的追加实测

本轮继续使用独立实验流程，未修改原真实同步流程。实际运行和画布点击由 computer use 完成，原始 runId、事件及响应另行保存。

### E14：重试使用旧版本并选择性复用

点击历史失败运行的“重试”后创建新的运行，源失败记录继续保留。此时当前保存版本为 3，但新 Run 的事件 correlationId 绑定失败时版本 2 的 digest，而不是版本 3。

完整事件 28 条、`truncated:false`，其中两个 `ask_user.cache_hit` 以 `system:cache_replay` 回答，`worker.cache_hit` 复用版本 2 的结果；两个 Python action 均重新出现 dispatched/completed。最终再次因同一输出字段问题失败。

所以本例不是直接把失败 Run 改回 running，也不是只执行最后一个失败点。它是旧版本的新 Run，复用部分结果并重执行脚本；不能推广成所有节点和所有版本的缓存策略。

### E15：优化入口带有可删除的运行引用

“基于此次运行优化工作流”返回编辑画布，预填原运行批注及分析/修改要求，未自动发送。批注是独立的类型化 token；只替换普通可见文本不会删除它，必须点删除批注。

发送内容包含原运行身份和修改要求；完整事件是否随消息注入，必须看后续真实工具调用，不能由批注文案推断。实验中识别到遗留 token 后立即停止相关请求，未观察到其修改工具调用。

### E16：主动跳过与事件来源存在不一致

两次在画布点击“跳过”分别触发了 `ask_user.timeout`；事件均标 `system:timeout`，但 timeoutMs 为 null，操作分别在待答后约 3 秒和 13 秒发生。其中一项采用默认项目，另一项未返回值，源码自行回退到第一个候选；后续伙伴和检查仍成功。

这是主动操作和记录来源不一致的实证，不能解释成用户一直未回答而自然超时，也不能据此认为所有跳过都必须使用默认值。V2 设计应保留 answer/skip/timeout/cancel 的真实来源。

### E17：等待时终止

在项目待答时点击运行停止，UI 从等待输入变为已终止，问题表单消失、后续节点没有执行。完整事件 6 条，Run 和 final 均 cancelled，pendingConfirmations 为空；中间仍出现 payload 为空的 ask_user.timeout。

可以确认这次运行终止和待答清理，不能从该 timeout 事件声称用户回答了问题或选择跳过；执行中进程取消和 Core 重启后的恢复尚需独立实验。

### E18：脚本错误没有在本例自动重试

实验加入最早执行的本地 Python 动作，仅向 stderr 写实验标记并 `exit(7)`。第一次保存含语法错误，被 `PARSE_ERROR` 拒绝；Agent 修正后保存为版本 4，成功版本 3 的历史仍在。

实际运行完整 7 条事件，只有一次 action.dispatched，随后 action.failed，错误为 `ACTION_SCRIPT_ERROR`、`Script exited with code 7: FAULT_EXPERIMENT`、`retryable:false`；最终 failed，后续阶段未执行。失败详情可优化、复制技术信息，未观察节点级重试按钮；运行历史有重试。

当前独立实验停留在用于证明故障处理的版本 4，并非最后一个版本已经业务成功。此前成功的版本 3 仍可在历史中查看。不能把此次单次失败推广成参考产品一律不重试。

### E19：本地日志仍未给出全部产品契约

实验会话的精确原生日志段包含模型请求的消息数、工具 Schema 数量、工具名称及实参，但未找到 systemPrompt 或完整 Schema 内容；会话 hook 上下文只有身份和通用平台路由。未从这些记录取得产品完整 SDK。

已安装 CLI 文档另外明确：run-retry 从 failed/cancelled 创建新 Run；运行更新和 rollback 追加版本；confirmation 用 runId 与 confirmationId 定位。这些文档与部分实测吻合，但不能替代未测的子流程、并行错误和结构化输出修正策略。

### E20：进一步核查 MCP 定义的边界

目标实验会话的 tool namespace 文件只记录了相关 Skill 身份；对应的运行配置没有找到可验证的 Wake Workflow MCP 服务地址或 tools/list。工作流编辑工作区配置中可见的内置 memory SSE 端点属于记忆服务，不能拿它当作工作流工具服务。

因此，现有证据可以引用实际发生的工具名、调用载荷和响应，但不能声称已经取得其完整参数 Schema。V2 的 MCP 定义以本产品 Operation Catalog 为权威生成，具体方案见[契约详细设计](workflow-v2-contracts-cn.md)，不猜测参考产品内部实现。
