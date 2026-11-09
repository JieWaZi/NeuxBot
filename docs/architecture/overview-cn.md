# 架构总览

普通聊天通过 `Execution → ThreadLifecycle/Turn → ProviderRuntime → 原生 Client` 接入 10 个 CLI，Chat 与运行时共用 ConversationStore。独立文本生成与 Wiki 维护使用同一执行底座。文本生成需要原生工具禁用能力，Wiki 维护需要原生受限 MCP，目前由 Claude 提供。各 CLI 的本地验证与原生限制见 [CLI 验收记录](../discussions/agentruntime-rebuild/native-cli-completion-cn.md)。

NeuxBot 是本地优先的伙伴工作台。Web 和 Electron 各自承载宿主能力，共享 React 应用承载界面，Go Core 拥有业务规则、文件、Git、终端执行与持久化，智能体适配隐藏原生 CLI 的协议差异。

下图表示主要调用方向。

```mermaid
flowchart LR
  Web[浏览器入口] --> UI[共享 React 应用]
  Native[原生入口] --> UI
  UI -->|生成 HTTP Client| Core[Go Core 领域用例]
  UI -->|白名单 Bridge| Desktop[Electron 主进程]
  Desktop -->|统一 WS| Events[Core 事件分发]
  Core --> Chat[Chat 与团队及工作流]
  Chat --> Execution[Execution]
  Execution --> Thread[ThreadLifecycle / Turn]
  Thread --> Provider[ProviderRuntime：原生进程与资源租约]
  Provider --> ACP[独立 acp-go 原生 Client]
  Core --> Knowledge[独立知识生成和 Wiki 维护]
  Knowledge --> Thread
  ACP --> CLI[本机智能体]
  Core --> Catalog[agent.Catalog：安装、Profile 与程序定位]
  Core --> Store[SQLite 与权威日志及受管文件]
```

## 所有权

- Chat 保存消息权威日志与原生会话关联；Execution 管理接纳、执行、取消和失败恢复。
- 普通聊天、团队、工作流、自动任务和 IM 共用 Execution 的单一 Runtime 端口；领域能力通过既有授权与资源端口注入，Session 不接管业务编排。
- ThreadLifecycle 拥有公开会话的原生绑定、配置兼容与历史接力；Turn 分开记录输入接纳、原生执行、交付和持久化结果。ProviderRuntime 独占进程与实际租约，关闭失败保留同一所有者供重试。
- 冷准备取得账号和物化资源，原生会话确认后才允许输入提交。热续聊只发送新增输入；原生替换使用现有最近五轮或总结接力。模型 setter 可热更新，进程权限或账号身份改变重新判断兼容。
- `agent.Catalog` 保留安装、Profile 与程序定位。模型及 Thinking 查询通过独立 ProviderRuntime 目录连接，不启动模型 Prompt。软件能力与 allycli 继续使用原有共享模块。
- Channel 管理团队成员、任务与协作；Workflow 管理源码版本、编译产物和运行状态。
- 自动任务和 IM 管理触发、授权及交付关系，复用已有执行入口。
- 伙伴、项目、插件、技能、连接器、账号和记忆各自维护其业务事实。
- 工作看板与通知读取原领域状态，不复制执行生命周期。
- 用量统计以独立于公开聊天的规范原生终态为权威。业务 SQLite 仅保存可重建的 Token 查询投影，按原生调用去重。日期、模型和伙伴分组共用同一快照，区分完整计数、已知小计与缺失数据。账号额度由原生账号接口提供。

前端服务器事实由 TanStack Query 管理，资源事件用于增量更新或精确失效。Native 通过 Electron 主进程、Web 通过浏览器各自持有一条 Core 全局 WebSocket；文件与 Git 的原生文件事件复用该连接。PTY 字节流使用专用受认证连接。普通业务操作继续使用 REST。

## 代码导航

| 路径                              | 职责                                                           |
| --------------------------------- | -------------------------------------------------------------- |
| packages/frontend/src/app         | 路由、Scene 和跨模块装配                                       |
| packages/frontend/src/modules     | 产品领域界面与查询                                             |
| packages/frontend/src/shared      | 组件、API、主题与平台服务                                      |
| desktop/src / desktop/electron    | 原生入口、窗口、preload、系统能力与嵌入浏览器                  |
| web/src                           | 本机会话生命周期与浏览器宿主                                   |
| packages/platform-contracts       | 中立能力接口和事件协议                                         |
| server/internal/agent             | ThreadLifecycle、Turn、ProviderRuntime、原生 Client 和安装目录 |
| server/internal/chat/conversation | 规范会话、输入、原生终态与回放                                 |
| server/internal/agent/capability  | 命令目录、精确授权、有界观察与领域回执                         |
| server/internal/agent/cli         | allycli 参数解析、发现与客户端调用                             |
| packages/terminal-runtime         | Core 管理的 PTY 与终端屏幕运行时                               |
| server/apis                       | HTTP Operator、校验及单入口用例                                |
| server/services                   | 多入口复用业务与后台生命周期                                   |
| server/repositories               | 持久化模型、查询及事务实现                                     |
| server/internal                   | Runtime、日志、文件、编译器等内部能力                          |
| skills/ally                       | 随应用交付的系统任务知识                                       |

当前交付方式包括原生应用和浏览器访问本机回环 Core。Web 包使用 Node.js 22+ 启动器；Native 使用随 Electron 交付的运行时。产品与主仓库名称为 NeuxBot，内部 workspace 包使用 `@neuxbot/*`，Go 模块使用 `github.com/JieWaZi/NeuxBot/server`；整个 Agent 子系统属于该模块，没有独立 `go.mod`。acp-go 继续在独立仓库维护并固定依赖版本。`allycli`、`ALLY_*`、已有 `NEUXALLY_*` 插件环境变量、`neuxally-resource://` 和知识集成的服务身份继续作为稳定技术契约；插件授权使用 `neuxbot-auth/1`，具体边界见[插件资源](plugin-resources-cn.md)。独立插件源位于 `JieWaZi/neux-plugins`。受管文件根目录与程序数据目录分别管理，具体布局见 [工作区与存储](workspace-and-storage-cn.md)。Agent 的调用、并发和清理边界见 [智能体与账号](runtimes-and-accounts-cn.md)。

修改前阅读根目录 AGENTS.md，具体数据边界见 [工作区与存储](workspace-and-storage-cn.md)。

插件资源、伙伴安装、共享授权与版本快照见 [插件资源](plugin-resources-cn.md)。

知识库的独立核心、SQLite 权威、原件与本地／远端执行边界见 [知识库](knowledge-cn.md)。
