# Agent / ACP 重构收尾

依据：`docs/superpowers/specs/2026-11-04-agentruntime-rebuild-design.md` 与 `docs/discussions/agentruntime-rebuild/module-review-cn.md`。当前正式目录已完成替换，本工作包基于用户工作区当前代码收尾，不重新创建候选执行器，不覆盖并行修改。

## 范围和约束

保留 ThreadLifecycle、Turn、ProviderRuntime 和 ConversationStore 的唯一所有权。acp-go 保留原生协议；不增加公共接口或第二份状态。保留真实生产调用的 Catalog 能力与原生账号、插件环境。应用启动器定位打包 allycli 的环境覆盖属于宿主装配，保留；删除向模型执行子进程注入 CLI 路径、上下文与 PATH 的链路，以及 allycli 读取上下文环境变量的回退。

工作在当前非主分支，保留未提交基线；不提交混合工作区，不运行真实模型或控制桌面。基线副本与验证记录放在 `.superpowers/sdd/agent-acp-cleanup/`。

## Task 1: 显式 CLI 连接上下文

- 回归：真实 CLI 在空 PATH、遗留错误上下文环境下，显式 `--context` 成功，无该参数则拒绝；两轮授权轮换、撤销、MCP 工具入口保持。
- 删除 TurnInput.LaunchEnvironment 与资源获取时的 CLI 环境合并、PATH 补齐。CLI 只从参数取得连接文件；测试改从已公布的实际命令读取路径。
- 定向运行 execution 的 Agent 准备测试、agent/cli、plugininstallation 相关测试。

## Task 2: 按职责收敛正式代码

- Catalog 保留注册、Profile、能力声明；安装发现、程序定位、探测环境放入 runtime_installation.go；配置投影类型归 runtime_options.go；宿主冻结输入归 execution_plan.go。
- developer_instructions.go 只处理稳定指令组合；独立文本及任务调用归 detached_task.go；会话摘要与历史交接归 history_handoff.go；原生结果持久化归 native_outcomes.go。
- 删除 resources.go 的旧指令文件写入与技能软链接实现。技能复制、摘要清单、暂存发布及仅清理受管内容归 skills.go；仍通过唯一连接资源获取入口调用。稳定指令交付实际原生 Session，不另写 AGENTS.md / CLAUDE.md。
- 先写回归证明用户指令文件和非受管技能保留、技能复制/更新/禁用不依赖源路径链接；核对生产调用后才删除无消费者声明。
- 定向验证 Agent 包、Execution 直接消费者和 Catalog 安装探测；检查依赖方向与英文注释。

## Task 3: 文档与审查

- 更新 Agent README、CLI 使用说明和当前架构说明；系统技能由 `pnpm skills:generate` 更新生成包。
- 运行定向 Go 测试/vet、技能生成检查及文档检查，不运行全仓检查。
- 独立审查工作包相对于保存基线的改动，核对接纳、续聊、资源租约、原生指令与 CLI 授权边界；修正重要问题。

## Review Focus

并行修改保留；CLI 显式上下文在热会话中正确轮换且不接受旧环境；原生账号/插件环境保留；技能安装不越界、不删用户内容；指令只进入原生入口且不重复用户历史；目录整理不引入转发层或新状态源。

## 源码对照与边界结论

本地原正式链路只把指令写到工作区文件，`nativeThreadRequest` 没有交付 DeveloperInstructions。按本项目方案 4.2 节，当前改由相同原生 Session 入口消费稳定文本，历史接力保持原所有者。原生协议降级继续由 acp-go 决定。

同版本 `src/main/managed-skill-service.ts` 在安装边界拒绝软链接并使用临时文件后 rename 发布。旧 `resources.go` 创建链接、清空整个发现目录，无法满足本项目方案 4.3 节。当前通过既有技能复制和摘要函数、安装清单及暂存回滚实现受管安装，不复制上游 Effect 状态或引入新连接对象。跨技能集合的清单与回滚属于本项目文件布局的实现约束，不声称是上游的原样算法。

旧链接没有安装清单。首次准备只认领指向当前授权源或宿主明确给出的应用专有源根（system-skills、skills/versions、plugins/content）的链接，覆盖版本变化、Runtime 切换及停用。目录和无关用户链接不认领；内容源本身不被迁移删除。源根来自既有程序数据目录，不由模型或 HTTP 参数提供。以后只依据安装清单更新和删除，不重复扫描认领。

CLI 授权协议由本项目维护，兼容规则以真实 CLI 为准；真实 CLI 回归证明无 `--context` 的隐式环境回退确实存在，再移除回退和注入。原生进程边界过滤继承的 `ALLYCLI_` 宿主配置，继续保留账号、插件及普通工具环境。

未发现需在本工作包改写的 acp-go 生命周期；NeuxBot 通过现有直接 Client 合同消费它。未改独立仓库中的并行工作，发布版本和平台／真实模型验收不属于本次代码清理完成证明。
