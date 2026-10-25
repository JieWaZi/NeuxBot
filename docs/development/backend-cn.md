# NeuxBot 后端工程开发约定

> 文档状态：开发基线
> 适用范围：Go Core、数据库、HTTP/SSE/WebSocket、执行器和进程生命周期
> 领域词汇：[NeuxBot 领域词汇](../../CONTEXT.md)

本文记录后端实现规则。开始后端改动前，必须先阅读领域词汇，并以用户当前确认的功能范围为准。

## 0. 范围与契约

- 按用户确认的当前范围开发，不从设计设想推导额外功能；
- 数据、协议和界面行为以当前实现及对应架构文档为准；
- 变更公开契约时明确影响、支持范围和验证方式；不为未承诺的使用场景预建兼容层；
- 对外部输入、Runtime 故障和未知扩展值保留必要校验与安全降级。

## 1. 目录和依赖

后端采用以下目录：

```text
server/
  apis/
  services/
  repositories/
    controllers/
    models/
  global/
  constants/
  internal/
  cmd/
  configs/
```

- `apis`：ginx Operator、输入校验、单入口用例编排和必要的 HTTP 输出适配；
- `services`：被两个或以上用例入口共同调用的复用业务方法；
- `repositories/controllers`：Repository 接口、GORM 实现和构造函数；
- `repositories/models`：GORM 持久化模型；
- `global`：DB、Controller、Service 的统一装配，不承载领域规则；
- `constants`：跨层共享的稳定业务常量、枚举、稳定错误和 toolx 生成文件；
- `internal`：不对外暴露的深模块，包括 Codex、Git、进程、事件和诊断等内部适配，以及 Markdown 解析、定向修改、编码和归档等可独立测试的纯能力；
- `cmd`：参数解析、根 Context、启动、信号处理和有序退出；
- `configs`：本地配置结构和默认值。

依赖方向：

```text
apis -> services -> repository interfaces
apis ------------> repository interfaces
repository implementations -> GORM / Dialector
internal adapters ----------> process / filesystem / Git / Codex
cmd / global ---------------> assembly
```

- API 和 Service 不得导入 GORM、原生 SQL 或 SQLite 方言；
- Repository 不得依赖 API DTO；
- 包之间通过公开接口或明确公开类型通信，不访问其他包内部实现。
- 解析、格式转换、Patch、编码、压缩等不负责编排业务用例的纯能力必须放入 `internal` 下的独立深模块，不得作为工具方法堆积在 `services`；
- `services` 只持有被两个或以上 API、后台任务或命令入口共同调用的复用业务方法，可以依赖上述内部能力模块，但不得反向承载通用解析或编码实现。
- 文件按“共同变化的业务目的”组织：一组明显服务于同一目的且会共同演进的规则，例如工具语义短语、展示摘要、协议解析或增量状态机，应放入名称具体的专门文件，不得长期堆积在 `common.go`、`utils.go`、`projection.go` 等宽泛文件中；
- 文件拆分以高内聚、低耦合为准，不按函数数量机械拆分。单个辅助函数不单独成文件；只有规则集合形成清晰职责、可以独立测试或具有独立变化原因时才建立专门文件；
- 文件名必须表达具体能力或变化原因，例如 `tool_phrase.go`、`tool_summary.go`、`message_display_projector.go`，避免仅以 `helper`、`common`、`processor` 等弱语义命名承载大段领域逻辑。

### 1.1 领域归属与文件内聚

- 同一领域的内部能力必须归到一个上层目录，再按真实能力划分子包。例如工作流统一使用 `internal/workflow/{dsl,files,function,input,state}`；软件能力统一通过系统 `ally` skill 与 `allycli` 交付，命令契约归到 `internal/agentcapability/contract`，认证/观察归到 `internal/agentcapability`，客户端归到 `internal/agentcli`；业务存储、编译和调度留在各自领域模块。禁止在 `internal` 顶层继续新增 `workflowxxx` 平级包。上层目录只表达归属，不为了聚合目录新增空接口、转发包或第二套 Service；
- 新建文件前，先检查同包中拥有该职责的文件；依附于既有编译、查询、接纳、恢复或执行规则的辅助函数应放回对应职责文件。禁止按一个函数、一个命令或一次补丁建立文件，导致完成一个用例需要在多个零散文件间跳转；
- 一两个函数可以构成完整且有独立变化原因的公开能力；判断依据是职责边界、依赖和测试，而不是函数或行数。独立 HTTP Operator、平台构建变体及生成文件遵守各自规则，不为减少文件数混合路由、平台或生成边界；
- 文件划分必须说明独立的职责、依赖或变化原因；不能以“这是另一个功能”、新增命令或文件较长作为拆分依据。同一职责允许保留较大的文件，优先让完整规则就近阅读；仅当出现实际独立边界时才拆分。装配与生命周期可以同文件，运行接纳与取消可以同文件，同一执行机制的分派、结果与恢复可以同文件；禁止用无明确职责的 `utils.go` 汇总领域逻辑；
- API 单入口流程优先放在对应 Operator 的职责文件，依赖装配集中管理；禁止为搬移 Service 方法而新增仅有一个 `api.go` 的 `usecases`、`triggers` 包，再由原接口转发。测试随真实职责迁移，不为解决测试导入循环而增加生产包装层；
- 移动或合并时同步更新所有调用、测试样例路径、生成命令、内嵌资源和当前设计文档；保留既有行为、事务和授权边界，生成内容仍只通过对应生成命令更新；
- 交付前审查生产文件的职责清单和包依赖：说明每个保留的小文件为何独立、每组关联规则归到哪里。工作流与自动任务的架构测试检查领域目录归属以及孤立私有辅助函数，禁止添加例外来绕过整理要求。

## 2. ginx API

- 一个接口一个文件，一个文件只定义一个主要 ginx Operator；
- Operator 实现 `Path`、`Validate` 和 `Output`，`Output` 返回 `interface{}`；
- ginx Operator 已提供默认 `Validate` 实现；没有跨字段或自定义校验逻辑时，不得重复声明仅返回 `nil` 的 `Validate`；
- 路由文件只负责注册，不写查询和业务判断；
- 字段确有约束时才写 `validate:"required"`、`max`、`min` 等结构体标签；没有实际校验需求时不写标签；
- toolx 枚举由 ginx 在请求绑定阶段统一校验，API `Validate` 不得再次逐项比较枚举成员；
- `Validate` 只处理框架标签和 toolx 枚举无法表达的数值、跨字段约束和确定性默认值；
- `Validate` 不查询数据库、不读取文件、不调用网络和外部进程；
- 依赖持久化状态的合法性由 API 用例或 Service 判断；
- Model 已经满足接口数据需要时，API 直接将 Model 作为输入或输出，不得机械增加 DTO 和转换层；
- 只有分页、组合多个 Model、隐藏字段或外部协议与 Model 确实不同时，API 才定义语义明确的 `XxxRequest` 或 `XxxResponse`；
- HTTP 专属的 Response 组装留在对应 API 包，不得为此创建 Service View 或 Service DTO；
- `POST`、`PUT` 和 `DELETE` 写接口默认返回 `ginx.CommonSuccessResponse()`，不得为了省略一次刷新而返回完整实体；
- 只有前端下一步交互确实依赖新生成的标识或结果数据时，写接口才返回对应 Model 或必要 Response，并在用例代码中体现该依赖；
- 错误响应使用稳定错误码和参数，由前端本地化。

### 2.1 OpenAPI 注释

`apis` 包及其中供 API 使用的共享类型会被 ginx toolx 扫描并生成 OpenAPI；这些结构体及其字段注释会直接写入 Schema 和 Operation 文案，不得按普通 Go godoc 写法组织。

- Operator 结构体注释写短操作名，例如 `创建对话`、`获取伙伴详情`；
- Request、Response 及共享 API 类型注释写短类型名，例如 `创建对话请求`、`对话列表响应`；
- 字段注释写 2 到 6 个字的精简标签，例如 `伙伴标识`、`项目标识`、`请求体`；不得重复字段名，不得写完整句子；
- 绑定字段使用固定短标签：`请求体`、`路径参数`、`查询参数`；
- 嵌入的 `ginx.MethodGet`、`ginx.MethodPost` 等 HTTP 方法声明不需要注释；
- `Path`、`Validate`、`Output` 等方法注释仍按第 6 节常规 godoc 编写，不受本节约束；`Output` 的自然语言注释只描述用例行为，供 toolx 生成错误响应的 `// [ErrorKey][状态码][说明]` 或 `// @err[...]` 结构化声明必须按真实协议保留；

## 3. Service 和 Repository

只被一个用例入口使用的业务流程，无论是否包含多个 Controller、事务或较多步骤：

```text
API -> Repository Controller
```

同一业务方法被两个或以上 API、后台任务或命令入口共同调用时：

```text
API -> Service -> Repository Controller
```

- Service 业务方法的唯一准入条件是存在两个或以上真实用例入口复用；不得因为流程复杂、跨 Repository、需要事务或希望隐藏 API 编排而创建 Service。启动、关闭等基础设施生命周期必须有明确所有者，不能借其存在把单入口业务用例放进 Service；
- 判断复用必须列出生产调用方；测试调用、同一 API 的转发包装，以及一个用例内的多个内部调用均不算第二个入口。不同用例共用锁、租约、状态提交或输入校验机制，只提取这段真实共享机制，不能据此把各自完整用例全部放入 Service；
- 单入口逻辑留在 API 所属包，API 可以直接依赖 Controller 与内部共享能力；不得把用例搬到新的 Service、Manager、Facade 或纯转发包，留下 API 代理来规避层级约束；
- 单一入口的跨 Controller 用例和事务由 API 直接编排；出现第二个真实入口复用整段逻辑后，再连同事务边界提取到 Service。删除或合并入口后重新核对复用条件，失去复用的用例应迁回 API；
- 禁止创建只转发参数和返回值的 Service；
- Service 返回 Model 或由 Model 组成的最小业务结果，不定义面向 HTTP 的 View、Request 或 Response；
- Repository Controller 必须包含接口、GORM 实现、构造函数和真实临时数据库契约测试；
- Repository 接口表达领域操作，不暴露 `*gorm.DB`、Clause 或方言参数；
- GORM 实现通过 Context 获取当前 DB，以便加入用例事务；
- Service 默认使用具体结构体，依赖 Repository 接口和外部端口；
- Repository 记录数据库失败的技术上下文，Service 翻译领域语义，API 映射外部错误；
- 同一错误不得在 Repository、Service 和 API 重复记录。

## 4. 事务

- 事务由拥有完整用例的 API 或满足复用条件的 Service 开启；
- Repository 通过 Context 加入现有事务；
- 事务统一通过 ginx `dbhelper.TransactionManager` 执行，并在项目数据库事务边界中统一处理返回错误；
- 事务回调返回的 `status_error` 原样保留；begin、commit、rollback 等事务自身产生的非 `status_error` 故障必须记录原始技术错误并统一转换为 `DatabaseError`；
- 事务只包含必须原子完成的数据库读写；
- Codex、Git、文件扫描、网络请求、SSE 等待和用户审批不得在事务内执行；
- 事务回调必须返回原始或包装后的错误，禁止吞掉回滚原因；
- SQLite `busy`（包括扩展错误码）由统一 Repository 事务边界回滚后重新开启完整事务，按 1、2、4 秒最多重试三次；回调只能包含可重放的数据库读写，每次重新查询状态，不复用前一次临时结果。嵌套事务向最外层传递 busy，禁止在旧快照中逐条重试；
- 独立 Controller 单条操作使用同一 busy 退避策略，由统一 GORM 回调在新事务内完成 SQL、结果扫描和提交，禁用包在重试边界外的 ORM 隐式事务；查询使用 `Find` 或 `Count` 完成扫描，不向边界外返回尚未消费的 Rows。多语句原子用例必须使用显式事务。Context 取消立即结束等待，非 busy 错误不重试，耗尽统一返回 `DatabaseBusy`（“正在处理其他数据，请稍后重试”），不增加数据库写锁；
- 事务外副作用需要显式重试、补偿或可恢复状态。

## 5. GORM 和 SQLite

- ORM 统一使用 GORM；
- 业务数据库使用 SQLite 默认模式，不开启 WAL；
- 不设置 `foreign_keys`、`busy_timeout`、`journal_mode`；
- 不设置 `max_open_conns` 和 `max_idle_conns`；
- 不使用 SQLite 外键开关替代业务层不变量检查；
- 独立技术组件确需不同模式时使用独立数据库并新增 ADR。

查询硬规则：

- 默认使用单表查询；JOIN 仅在无法通过单表过滤、存在性判断或有界批量读取合理完成时使用。新增 JOIN 必须给出业务必要性、替代方案对比、真实查询计划和回归测试，禁止仅为少写代码或拼装展示字段联表；
- 只判断关联是否存在时使用 `EXISTS`、`NOT EXISTS` 或索引支持的 `IN` 子查询；读取名称、提示状态和关联详情时先确定候选窗口，再按身份分批查询并在 Go 中组装，禁止逐行查询形成 N+1；
- 禁止将多领域归属、生命周期和 JSON 展开塞进层层嵌套的长 SQL，也不能仅把 JOIN 改成逗号联表或大量相关标量子查询来绕过规则；
- 统计只读取过滤所需的字段和索引，不执行展示补齐；分页必须保留稳定排序、游标和完整总量，不能把有限预览当成总数，也不能加载全部历史后在内存分页；
- 同一次读取涉及多个查询且要求一致版本时使用短读事务；批量读取限制参数数量，查询计划与查询次数回归必须覆盖规模增长；
- `internal/architecture/sql_queries_test.go` 检查生产持久化代码的联表边界。已批准例外仅为 memory 的 `keywordSearch.searchFTS` 与 `vectorStore.search`：虚拟索引排名与原记忆范围、正文在同一次查询返回；其他查询不得借此扩大豁免。

模型和写入规则：

- 模型通过 `dbhelper.RegisterTable` 注册，由 ginx `dbhelper.NewDB()` 和 AutoMigrate 建表；
- 当前不维护 SQL Migration 文件和独立迁移框架；
- 主键使用 32 位无连字符 UUID 字符串；
- `CreatedAt`、`UpdatedAt` 使用 Unix 毫秒 `int64`，可空时间使用 `*int64`；
- 不嵌入 `gorm.Model`，每个模型显式实现 `TableName()`；
- 数据库可能写入 `""` 或 `0` 的枚举必须定义对应零值常量；
- 创建使用 `Create`，局部更新使用明确的 `map[string]any`；
- 用户可见名称由用例入口通过 `internal/util` 的统一方法清理首尾空白并保留大小写，规范化后的名称直接写入 Model；
- 唯一名称直接在实际名称字段建立唯一索引，禁止增加 `NameKey`、`RoleNameKey` 等派生判重字段；
- Controller 使用 ginx `dbhelper.IsDuplicateError` 识别唯一约束冲突，并返回对应的稳定业务冲突错误；
- JSON 列使用 GORM serializer 直接映射为切片或结构体字段，禁止为了持久化 JSON 增加 `XxxJSON string` 字段和手工编解码 DTO；
- 禁止对外部输入反序列化后直接 `Save`；
- 更新和删除必须有明确 `Where`；
- 普通更新和删除的 `RowsAffected == 0` 不由 Controller 视为错误；业务要求资源必须存在时，由用例入口在写入前显式查询；带 revision 等明确并发条件的更新仍使用 `RowsAffected` 判断冲突；
- 关联写入显式执行；
- 并发保护使用 revision、条件更新和 `RowsAffected`；
- 状态转换必须校验当前状态。

## 6. Go 常量、注释、错误和日志

- 稳定业务值、默认值、协议值或跨文件复用值必须集中定义在 `constants`，禁止在 API、Service、Repository 等调用方中直接书写对应字面量；
- 普通稳定业务常量统一定义在 `constants/common.go`，名称必须准确表达业务用途，不得仅复述字面值；局部实现参数和仅用于测试样例或断言的数据不强制集中；
- 所有业务枚举的类型和全部成员必须共同定义在 `constants`，即使枚举当前只供单个 API 使用也不得在 API 包内定义；
- 每个枚举类型使用独立文件，并统一通过 `//go:generate toolx gen enum Xxx` 生成对应实现；生成文件禁止手工修改；
- 所有导出的类型、函数、方法和常量必须有准确中文注释；
- 所有 Go 结构体字段都必须有准确中文注释；
- 字段规则覆盖未导出字段、测试替身字段和适配器内部字段；
- 注释说明业务含义或技术用途，不机械重复字段名；
- 新增或修改结构体时，必须检查该结构体的全部字段；
- 所有项目自有错误必须集中定义在 `constants/status_error` 并通过 toolx 生成，规则覆盖 API、Service、Repository Controller、`internal` 深模块、`global` 装配和进程入口，生成文件禁止手改；
- API、Service、Repository Controller 和 `internal` 可以直接返回 `status_error` 中准确表达当前失败语义的错误，API 只负责通过 `ginx.WithStack(err)` 补充调用堆栈和必要的外部参数；
- 禁止在任何业务或内部模块中使用 `errors.New`、`fmt.Errorf`、包级哨兵或本包自定义错误类型另行定义项目错误；内部错误说明只能使用稳定英文技术文本，禁止返回中文错误文案；只有 `constants/status_error` 可以通过 `@errZH`、`@errEN` 定义中文与英文 i18n 错误；标准库、第三方依赖和系统调用返回的底层错误必须在首次转换处记录技术上下文，再转换为 `status_error`，不得直接暴露给上层或前端；
- 现有 `status_error` 不能准确表达失败语义时，必须在 `constants/status_error/error.go` 新增对应错误并重新生成，禁止为了消除错误字符串而把配置缺失、调用取消、外部服务失败、协议损坏、数据库故障等不同语义粗略折叠为 `BadRequest`、`InternalServerError` 或其他不准确错误；
- 同一失败沿调用链只能转换一次；下层已经返回 `status_error` 时，上层必须原样传递，禁止再次按 `errors.Is`、错误文本或 switch 二次映射；
- API 不得对 Controller、Service 已经返回的 `status_error` 再增加 `errors.go`、错误映射函数、`errors.Is` 分支或 switch 做二次转换；收到此类错误后直接使用 `ginx.WithStack(err)`；
- API 不重复记录下层错误；Controller 负责记录真正的数据库故障，Service 负责记录其拥有的非数据库外部边界故障；
- 所有不可预期的数据库访问故障统一返回 `DatabaseError`，禁止将 GORM、SQL 或驱动原始错误抛给 API；
- `RecordNotFound`、唯一冲突等可识别的正常业务结果返回对应的稳定业务错误，不作为数据库故障记录；
- Controller 或 Service 中会沿调用链向外返回的状态错误不得使用包级 `errors.New` 哨兵，必须使用 `status_error`；
- Repository Controller 保留并记录底层原因，Service 翻译其拥有的非数据库领域语义，API 仅用 ginx 补充调用堆栈；
- 日志字段包含操作、资源类型、资源 ID、执行 ID 等可检索上下文；
- 日志失败不得影响主业务流程。

## 7. 后台任务和外部进程

- 构造函数不得启动 goroutine；
- 后台组件提供显式 `Start`，由应用装配或进程入口调用；
- `cmd` 创建根 Context，处理信号，取消后等待所有后台流程退出；
- goroutine 必须归属于 WaitGroup 或等价生命周期对象；
- 外部命令使用 `exec.CommandContext`；
- WS 客户端断开必须释放订阅资源；传输断开或退订不能取消原业务执行；
- Runtime 实时聊天采用单一 canonical Event Adapter 后的双消费者：Live Consumer 按原始交付顺序生成 `transient` 安全消息快照，Durable Consumer 只在完整执行消息形成后写入权威日志；禁止让 API 层分别解析 Runtime 私有协议；
- `transient` 实时事件 不得伪造 JSONL Sequence 或持久游标；断线恢复必须先以 history Message Snapshot 对账，再从最后持久化 Sequence 续接；
- Runtime Partial、Delta、Raw 心跳和中间 Usage 覆盖不得进入 JSONL；连续 text、commentary、thinking 内容只在内存聚合，工具消息和 Invocation 结束是完整消息边界；
- 最终 Usage 随执行终态只保存一次；取消或失败必须先保存已经完整形成的执行消息，Core 异常退出时尚未形成完整消息的内存内容允许丢失；
- JSONL 使用作用域式信封：Chat ID 由文件名确定，Sequence 由记录行序恢复，Execution、Invocation 和 Message ID 只在对应作用域入口或稳定身份首次出现时写入；
- 终态快照发布前必须完成全部完整执行消息持久化；测试必须证明原始事件实时投影与完整消息日志重放得到相同的 Message Snapshot；
- 测试覆盖启动失败、取消、重复停止和超时退出；

### 7.1 统一通知与提交后事件

通知与实时更新遵守 [当前统一通知契约](../architecture/notifications-and-work-board-cn.md)。Electron 主进程拥有唯一 Core WS，资源事件在提交后发布。

- 新增待办、阅读提醒或可推送的状态变化必须定义原权威事实、稳定身份/版本、有效性、恢复与消费契约，再接入统一事件端口；不得只新增周期状态查询；
- 事务内只登记待发布变化；最外层 commit 成功后发布，失败、回滚、CAS 冲突与 busy 重试的旧尝试不得产生成功通知。JSONL 来源继续遵守持久化、查询投影和终态占位释放边界；
- 全局 EventHub 负责安全事件路由和有界订阅，不执行审批、回答、发送消息、运行或删除，不复制 Runtime 私有协议和领域历史；
- 统一通知聚合复用原请求、交付与确认事实；提示收起、实际阅读、主动清空待查看与业务处理分别表达，内部负责人结果通知不自动计入用户铃铛；
- 状态来源使用提交后唤醒，不在 WS handler、摘要后台任务或每个客户端连接内扫描事件表；心跳只维护连通性；
- 初始快照、重连对账与复杂列表使用有界查询；领域持久游标与连接交付序号分开，不能用 WS 序号替代 JSONL/原事件游标；
- 外部协议轮询与真实时间调度可以保留，必须记录职责、频率、范围、退出条件和无法用事件替代的原因；订阅期限优先使用精确时间边界唤醒；
- 生产者、协议、消费者与测试按同一来源共同维护；每个状态更新保留一个明确通道，不重复广播或扫描；
- 发布不阻塞业务提交；连接、队列、定时器与协程由明确生命周期拥有，缓冲超限触发权威恢复，不静默丢弃持久消息；
- 新接口/枚举/Schema/错误按原生成与稳定错误规则维护，注册 WS 不得绕过本地认证、Origin/来源校验或日志脱敏。

## 8. 测试

- Repository 使用真实临时 SQLite，不 Mock GORM；
- Repository 契约测试允许注入 GORM Dialector；
- Codex、Git、时钟和文件等外部边界使用 Fake；
- 测试事务回滚、乐观锁、错误映射、Context 取消和进程退出；
- 测试替身的每个结构体字段同样需要准确中文注释。

SQL 性能基准位于 `repositories/controllers/sql_performance_test.go`，在 `server` 中运行：

```sh
go test ./repositories/controllers -run '^$' -bench '^BenchmarkSQL' -benchtime=20x -count=1
```

基准使用独立临时文件数据库及默认 SQLite 配置，造数和预热不计入测量；同时校验结果总量与分页，输出 P50/P95、每次分配字节数、SQL 次数及单条查询返回行数。返回行数不等同于数据库内部扫描量，必须结合查询计划判断；本机热缓存的顺序基准不能替代生产数据、冷启动和并发锁竞争测试。

### 8.1 连续聊天输入稳定性

- 新建原生 Session 默认只用有界的最近完整对话轮次播种，默认五轮、最多二十轮，并限制历史文本总量；只保留用户输入和助手最终回复，不注入过程、推理和工具输出。用户选择自动总结时，先在切换前的原生 Session 上生成不进入聊天日志和实时流的交接内容；成功后只把该交接带入新 Session，失败时改用有界近期对话。恢复原生 Session 时只发送本轮新增消息，不得再次把已有历史、伙伴档案、项目指令、MCP 说明或工具定义拼入当前 Prompt；
- 原生 CLI 或模型接口因无状态协议而需要重建上下文时，只允许 Adapter/CLI 单层负责，Host 不得同时重建同一内容；不能仅凭 Provider 报告的输入 Token 较大判断 Host 重复发送，必须分别检查 Host 到 CLI 的请求和 CLI 到 Provider 的真实载荷；
- 同一会话配置未变化时，系统提示、工具名称、MCP 服务名、工具 Schema 及其顺序必须确定且稳定。随机端口、临时路径、Invocation ID、时间戳和鉴权凭据不得进入模型可见名称、描述或 Schema；
- 连接器、技能、权限、模型或指令确实变化时允许重新挂载或改变前缀，但变化必须来自结构化配置事实，不能来自每轮重新生成的随机值；
- 新增或升级 Runtime Adapter 时，必须至少覆盖两个连续回合：断言恢复请求只含第二轮新增消息；对模型可见静态前缀或可观测替代物做规范化摘要，断言无配置变化时摘要一致；同时证明随机端口和凭据仍会安全轮换；
- Turn 输入用量表示本轮所有模型请求处理的输入总和，可能因工具循环多次计算同一上下文；Context 表示最后一次模型请求的当前上下文快照。统计层不得把两者混为累计会话 Token，也不得用展示层修正掩盖重复注入。

## 9. 后端完成清单

- [ ] 术语和对象符合领域词汇及用户当前确认的功能范围；
- [ ] 同领域内部能力归到同一上层目录；文件按共同变化的职责组织，没有单个辅助函数文件、命令碎片或宽泛聚合文件；
- [ ] 没有新增用户确认范围之外的对象、表或依赖；
- [ ] 所有新增或修改的 Go 结构体字段都有准确中文注释；
- [ ] 稳定业务值、默认值、协议值和跨文件复用值已集中到 `constants`，调用方没有散落对应字面量；
- [ ] 业务枚举的类型和成员均按类型独立定义在 `constants`，并通过 toolx 生成实现；
- [ ] API 没有重复校验 ginx 请求绑定阶段已经校验的 toolx 枚举；
- [ ] 暴露给 toolx 的 Operator、Request、Response 及共享 API 类型注释符合 OpenAPI 注释约定；
- [ ] API 没有仅返回 `nil` 的空 `Validate` 方法；
- [ ] Context 和取消沿调用链传递；
- [ ] Service 中的每个业务方法确实被两个或以上用例入口复用，单入口用例由 API 直接编排 Controller；
- [ ] Model 可满足协议时没有机械增加 DTO，必要的 HTTP Response 只定义在 API 包；
- [ ] 写接口默认返回 `ginx.CommonSuccessResponse()`，返回数据的例外确有前端后续交互依赖；
- [ ] Controller 没有向外抛出原始数据库错误，API 没有重复映射或记录下层错误；
- [ ] API、Service、Repository Controller、`internal` 和 `global` 的项目自有错误均来自 `status_error`，没有 `errors.New`、`fmt.Errorf`、包级哨兵或本包自定义错误类型，内部错误说明没有中文文案；
- [ ] 每个失败都返回准确的状态错误，配置缺失、调用取消、外部服务失败、协议损坏和数据库故障没有被粗略折叠为通用错误；
- [ ] 唯一名称直接使用规范化后的名称字段，没有派生判重 Key；
- [ ] 没有盲目 `Save`、无条件更新或事务内外部调用；
- [ ] SQLite 没有启用 WAL 或额外连接参数；
- [ ] 生成文件由命令产生且可重复生成；
- [ ] 修改的 Go 文件已执行 `gofmt -w`，`go vet ./...` 和 `go test ./...` 通过。
- [ ] Runtime 续聊只发送本轮新增内容，模型可见静态前缀在配置未变化时保持稳定；随机执行与鉴权数据没有进入工具身份、说明或 Schema。
