# Cursor 原生 ACP 用量采集实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**目标：** 保留原生 ACP 实时执行，每轮结束后独立采集并核对用量，复用现有聊天和统计链路。

**架构：** 官方 ACP 继续独占文本流、审批、取消、恢复和终态。Cursor 专有 Node 加载钩子只采集原生 `turnEnded` 计数，通过私有文件交给已有 `CapturePromptJournal`。原生会话、请求和输入身份核实后补充 `NativeOutcome`，不新增执行账本。

**技术：** Go、Cursor bundled Node、版本与模块摘要校验、私有用量文件、原生 ACP 会话 SQLite 只读身份。

**需求来源：** 用户确认用量在每轮结束时处理，不能影响原有增量能力；宿主边界见 [智能体与账号](../../architecture/runtimes-and-accounts-cn.md)。

## 统一约束

- 保留现有 ACP 执行；不引入 PTY、屏幕解析、项目 Hook 配置或每轮新进程。
- 不改安装文件；钩子只作用于本客户端拥有的子进程，限定已核对版本、精确路径、完整模块摘要和唯一锚点。
- 不保存提示、回复、凭据；计数缺失及身份不明保持未知，重复或冲突不重复计费。
- 用量采集失败不能改变流式、权限或原生终态；未知版本沿用 ACP。
- 构造器只冻结启动计划；资源由 Initialize 与 Close 持有，失败清理保留原所有者供重试。
- 保留无关修改，不提交、不重启运行中的 Core，不控制桌面界面。

## 审查重点

- nonce、会话、原生请求与输入身份必须共同匹配，旧轮及晚到记录不能串轮。
- 自动更新不能让管理、版本探测和执行使用不同安装；固定解析后的启动路径。
- 采集只在原生结束事件后发生，不积攒或替换文本增量。
- 摘要不匹配、私有目录不可用、缺失计数时保持正常 ACP 执行。
- 去重、缓存输入拆分、读取大小和调用次数均有明确边界。

### 任务 1：版本固定的采集钩子

**文件：** `acp-go/pkg/cursor/acp_usage_preload.cjs`、`usage_launch.go` 与测试。

**接口：** `cursorUsageEnvironment(executable, directory string, environment []string) []string`；只为原生子进程追加内联 Node preload 与私有路径。

- [x] 先验证实际 ACP 长回复持续增量与结束后的计数可以同时取得。
- [x] 校验版本、摘要、锚点；只发布白名单字段，重叠或无身份不发布。
- [x] 定向测试正常、未知、篡改、重复、缺失和可选失败路径。

### 任务 2：用量核实与归属

**文件：** `acp-go/pkg/cursor/usage_journal.go`、`acp_usage_store.go` 与测试。

**接口：** `captureCursorUsageJournal(ctx, scope, configRoot, directory, nonce)` 返回现有 `PromptJournalEvidence` 结算回调。

- [x] 测试 nonce、会话、版本、原生请求／输入与提交前基线不匹配时拒收。
- [x] 单表键查询读取 ACP 权威 root 与 turn 身份，不扫描全部历史消息。
- [x] 计数按已证明口径拆分缓存；重复去重，缺失保留未知；不改变终态。

### 任务 3：客户端与宿主接入

**文件：** `acp-go/pkg/cursor/client.go`、`usage_client.go`、`flavor.go`；宿主 Cursor 构造入口。

- [x] 保留共享 ACP 所有者，增加可选 `UsageHook`，每次同步预留轮次冻结独立 nonce。
- [x] 一次性 continuation 结算后清理本轮文件，重复 continuation 不干扰活动轮次。
- [x] Close 先回收原生进程，再清理私有文件；固定实际安装路径。
- [x] 移除未交付的 PTY 执行代码及其依赖，同步权威文档。

### 任务 4：真实验收与审查

- [x] 真实长回复验证多次原生文本增量及单轮末用量。
- [x] 同一会话两轮验证原生进程复用、不同请求身份、聊天与统计投影及重复查询去重。
- [x] 定向覆盖取消、模型参数切换、恢复、采集失败与资源回收；区分真实与夹具证据。
- [x] 定向测试、race、vet、文档检查和最终审查通过；报告本地与运行中 Core 的状态边界。
