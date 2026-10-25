# 知识库 HTTP MCP 实施计划

规格：[HTTP MCP 接入规格](../specs/2026-10-24-knowledge-http-mcp-design.md)。基于 main 的独立 worktree，分支 codex/knowledge-http-mcp，开发过程与原工作区隔离。

## 1. 分工

主代理负责规格、WeKnora 对比、权限/协议审查、跨端集成和最终验收。前后端分别由用户指定 GPT-6.1 high 子代理开发，共享此 worktree，分开文件职责，不覆盖他人改动、不自行提交或合并。

- 后端：server/ 共享知识调用、MCP 传输、发布装配与定向测试。生成合同变化须按原命令执行并通知主代理。
- 前端：`packages/frontend/src` 中的 settings 模块、必要导航/i18n和测试，新增 MCP 接入页，复用已有服务查询，开关存入共享服务配置。
- 主代理：spec/plan、架构与使用文档、环境依赖协调、评审和集成验证；代理开发期间不编辑其负责文件。

## 2. 后端步骤

1. 核对全部知识合同/Binding、Key 授权、同步与后台提交屏障。
2. 提取入口无关 Wiki 命令流程，读取复用原绑定；内部 allycli 行为保持。
3. 接入外部身份/能力映射，保证撤权、跨库、幂等、任务原授权与锁顺序。
4. SDK 无状态 HTTP MCP，工具 Schema 同源，挂载 `/mcp`，实现请求限制、稳定错误、Origin/Key 校验。
5. 真实临时存储、SDK 端到端和内部 CLI 定向回归；gofmt、受影响包测试和必要 vet。

## 3. 前端步骤

1. 阅读完整 UI 标准，沿发布集成增加 MCP 集成导航，保持 Shell 即时挂载。
2. 复用服务查询，默认关闭，提供独立开启／关闭按钮；开启后仅保留占位 Key 的 HTTP MCP 配置复制。
3. 页面不展开独立 URL、权限及运行边界说明，复用语义组件与 i18n。
4. 定向验证导航、地址推导、复制、加载/禁用/失败，运行 Vitest/ESLint/格式和类型检查。

## 4. 集成验收

1. 主代理审查真正复用、合同覆盖、权限和授权范围，重点检查撤权并发、任务重试、幂等及错误路径。
2. 反馈具体问题由负责代理修复；运行必要跨入口验证，确认管理 API 未暴露。
3. 更新权威知识/能力架构与发布指南，运行 docs:check，核对文件职责和依赖。
4. 交付 spec、plan、worktree、改动和验证结果，声明真实客户端/视觉边界；按用户授权，独立复审通过后提交并合并 main。

## 5. 实现与验证结果

前后端实施及主代理评审已完成。MCP HTTP 适配位于 `server/apis/integration_mcp`，复用知识 Service、命令合同和唯一集成授权；设置页使用共享服务中的 MCP 开关并派生连接信息。同步写入与后台提交沿原业务屏障，重试配置依据核心回执恢复，提交同时核对当前尝试号。没有新增依赖、独立凭据表或监听服务。

已通过以下定向验证：

- 后端相关包：`services/knowledge`、`services/integration`、`apis/integration_mcp`、`apis/integrations`、`apis/integration_knowledge`、`internal/knowledge`、`internal/agentcapability`、`internal/agentcli`、`internal/agentcapability/contract`，使用 `go test`、`-count=1` 和 `-timeout=90s`。
- Repository：`TestJobRetryAuthorityUsesJobIndex`，以真实 SQLite 查询计划确认通过 `idx_knowledge_retry_authority` 按任务检索。
- SDK 客户端连接真实临时 HTTP 服务，覆盖全部知识工具合同、读写、幂等重放、权限隔离、真实跨库对象和内部绑定互读；服务层覆盖失败候选隔离、累积授权、重启及原接纳重放、撤权和旧尝试竞争。
- 主代理对上述新增 MCP、后台授权及提交竞争用例运行 `GOWORK=off go test -race`，三个相关包通过；五个直接受影响包的 `go vet` 通过。
- 前端五个定向 Vitest 文件共 45 项测试、修改文件 ESLint/Prettier、Renderer 类型检查与 UI 规范检查通过。
- `pnpm docs:check`、公开 OpenAPI 选取测试及 `git diff --check` 通过。

独立 GPT-6.1 high 子代理完成提交前复审，重点核对共享业务逻辑、真实跨库对象、能力隔离、主体回执隔离、累计撤权与锁顺序，无剩余阻断问题。复审发现的重试恢复缺陷已在共用绑定中修复：接纳后请求取消时，调用重放和回执查询恢复原尝试，重新核对原始及累计授权，不重复重试或追加授权。新增 Call/Receipt 与授权有效/原 Key 撤权四分支测试先失败后通过，独立复跑三个相关后端包与该回归通过；主代理对新增回归运行 race，并对知识 Service 运行 vet，均通过。

整合最新 main 的 Web／Native 拆分后，MCP 设置位于共享前端包，发布监听继续独立于浏览器会话与管理路由。`apis/integration_mcp`、`apis/integrations`、`global` 的定向验证通过。

未运行全量测试或启动运行中界面；真实第三方 Agent、远程网络部署与桌面视觉验收尚未执行。独立 MCP 开关与页面精简的后续验证覆盖默认关闭、旧配置、重启、失败回滚、API／MCP 路由隔离及并发配置保留；相关监听和路由测试、race 与 vet 通过，生成管理合同未进入公开合同。按用户授权完成提交前复核后合并 main，保留原工作区无关改动。
