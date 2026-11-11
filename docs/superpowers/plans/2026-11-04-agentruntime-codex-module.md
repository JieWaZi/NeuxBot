# AgentRuntime 重建：Codex 原生接入模块

**Goal:** 一次完整重构acp-go的Codex原生接入模块，使其按明确所有权提供原生配置、指令、会话、认证、用量和管理事实；不以零散字段修补或旧对象包装交付。

**Spec:** [云端重建方案](../specs/2026-11-04-agentruntime-rebuild-design.md)，特别是第3节命名/组织/接口/英文注释、第8.2节三家闭环、第9节P1。[P0矩阵](../../discussions/agentruntime-rebuild/p0-provider-matrix-cn.md)与[职责地图](../../discussions/agentruntime-rebuild/p0-ownership-cn.md)是事实和边界输入，不要求沿用旧类型。

## Global Constraints

- 本工作包是同一次重建中的Codex模块，完整实现、定向验证并提交后，审查只聚焦该模块提交；不将wire、helper或单字段分别当模块成果。
- 实现依据为官方协议、锁定依赖版本和本项目实际调用链，测试使用独立样本。
- 用户指定开发使用 GPT-6.1 Sol high；Terra 仅用于简单工作和核对，不能替代产品实现。实现者不能派生子代理，独立 review 由根协调安排。
- 新写/重写代码和测试注释使用英文，公开标识符注释以标识符开头；方案中文，生成声明保留真实来源。
- 文件snake_case；Options、私有状态、状态转换和专属helper就近内聚；按真实协议/资源生命周期拆分，不按类型、方法数或行数机械拆分；没有消费者的interface不增加。
- 实现依据为官方协议、锁定依赖版本和本项目实际调用链，测试使用独立样本。
- 不保留旧设计作为硬约束，不创建包装旧Agent/Session/Client的转发层；已验证语义可以复用，但必须证明新所有权中仍成立。
- acp-go不导入Neux数据库、业务对象、公开Conversation或产品调度。宿主账号持久化、安装、稳定指令内容、技能复制、计价和公开会话管理归新runtime。
- 生成文件只通过既有生成器；仅模块和直接消费者定向检查，GOWORK=off；不调用真实账号/模型、不控制UI、不推送发布。

### Task 1: 完整重构 Codex 原生接入模块

**Read first:** 阅读本项目对应 CLI 的原生适配器、运行时调用链和相关测试，并核对官方协议与锁定依赖版本。

**Scope:** acp-go `pkg/codex`全部手写生产代码的职责与主流程重审；`pkg/codex/protocol`生成入口、`tools/protocolgen`实际必需的变更；`pkg/acpmeta`仅真实生产消费者需要的窄技术合同。不得无关改造其他CLI。

**Architecture and behavior requirements:**

1. 在编码前写出本模块owning文件/类型/唯一状态/真实消费者表与主调用链，附原生协议依据。传输/进程负责JSON-RPC、请求与退出；ACP接入负责原生session/turn身份、顺序和事实；配置所有者集中模型/effort/tier的requested与observed状态。先判定边界再改文件，不围绕旧文件逐项打补丁。保留与替换的每个核心类型必须有理由。
2. 技术声明、版本、initialize能力、session new/load/resume/fork、prompt/cancel/close、工具/审批/输入、历史、MCP和目录加载事实完整且真实。实现当前版本已证实的compact和管理能力；无证据的支持明确unknown/unsupported，不新增猜测RPC。会话缺失必须限定当前资源/ID，其他资源错误不得触发恢复；未知接纳/工具后失败不自动重放。
3. 原生developerInstructions在new/resume及实际支持的fork上真实传递；入口使用明确typed metadata/技术配置，不改SDK生成类型、不写AGENTS.md。缺省不覆盖、明确空清除；初始化能力和请求解析由同一合同定义。原生指令更新边界需要报告，不每轮重复静态内容。
4. model/list分页、精确ID、逐模型参数目录、known/empty/unknown、serviceTiers与明确旧格式兼容完整贯通。Fast映射真实tier ID，不能写死priority或修改模型ID。requested/off/inherit分别为实际ID/null/省略；换模型重新校验，显式失效值可解释拒绝，不能偷偷降档。原生回执更新observed；本地setterACK、turn/start ACK不能冒充实际effective。
5. 真实wire三态通过既有OptionalNullable机制和生成器按DTO/字段作用域维护：ThreadStart/Resume/Fork/TurnStart请求及ThreadStart/Resume/Fork响应的serviceTier，响应缺失不能被当作明确null。当前schema已含这些字段，不因方便手改生成文件或全局字符串替换所有同名字段。
6. 保留并重新核对累计usage的scope、请求/turn身份、可靠基线、缓存互斥、未知presence、失败/取消消费与context分离；原生model/tier归属只取可证明证据。暴露原生费用观察时保留scope/valuation，不能从Token构造已报告金额。归账/计价留runtime。
7. 原生认证read/login/logout/cancel和只读account/rateLimits/read按真实协议接通（schema roots/envelope经生成）；配额无Prompt，保留全部桶/窗、未知/零、reset单位。API key无套餐权益如实报告；账号任务/持久化/读取缓存节流留runtime。
8. 原生错误保留phase、精确code/data、submission强度、可证实retry/reset及输出/副作用事实；清理故障不覆盖原生终态，stderr脱敏有界。消息/工具身份、先终态后start ACK、背压、通知排空、迟到代次隔离、cancel/close竞争要明确，不能只实现“能回答”。

**Steps:**

- [ ] **Step 1:** 阅读参考实现与当前模块，保存职责表、吸收的机制/遗漏点、保留/重写判断到报告；据此一次组织重构。不等待额外设计确认，实质歧义向root提出具体裁决。
- [ ] **Step 2:** 用独立synthetic fake进程/RPC写必要行为RED；已有正确行为复用回归，不人为改坏代码制造RED。覆盖原生指令、Fast wire/目录/回执、quota无Prompt、精确错误与生命周期；无真实CLI证据不能宣称完成平台验收。
- [ ] **Step 3:** 按owning边界完成模块重构及typed合同；相关规则就近、删除被替换重复路径。真实技术消费者保持可编译，不能以空interface或旧实现fallback收尾。
- [ ] **Step 4:** 执行生成、gofmt及模块和直接受影响协议/acpmeta测试；选具体并发场景race。更新模块真实能力说明和未覆盖版本，不更新产品文档为已交付。
- [ ] **Step 5:** 自审完整模块的调用可读性、文件/类型命名、英文注释、所有权与功能；立即创建模块提交。报告命令/结果、变更、API迁移、来源、边界；交独立聚焦review。若工作包确有无法实现的协议证据缺口，返回明确限制而非静默省略。

## 后续模块

Codex提交review关闭后，按同一规则完成Claude接入、标准ACP与Cursor原生单路径模块，再实现新runtime与ConversationStore的真实消费者；不将本模块通过写成三家产品闭环。具体原生公共合同从已产生的真实消费者提炼，不提前建空框架。
