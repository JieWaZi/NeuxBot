# Kimi 内置账号登录修复计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 内置 Kimi 账号提供真实登录、授权进度、取消及可信的连接状态。

**Architecture:** 复用 RuntimeAuthentication 的任务、准入围栏、事件与弹窗。Kimi 原生认证适配器调用所选 CLI 的 `login`，成功后通过已有 ACP 隔离模型发现的原生认证门禁检查已有凭据；模型探测使用同一回执确认认证，保持系统与受管账号隔离。

**Tech Stack:** Go、原生 Kimi CLI、ACP、React、Vitest。

**Spec:** 用户在本次会话确认完整修复 Kimi 登录；既有架构以 `docs/architecture/runtimes-and-accounts-cn.md` 为准。

## Global Constraints

- 保留现有工作区修改，不提交无关文件、不增加数据库迁移。
- 不打开或控制运行中的应用；自动验证使用临时原生目录及替身进程。
- 只发布原生返回的授权链接和验证码，不传输凭据；取消等待进程退出后再释放任务所有权。
- 系统账号使用所选配置目录，受管账号维持独立原生目录。

## Review Focus

- 分片输出、ANSI 控制符、恶意或不完整授权地址。
- 登录非零退出、取消、超时不能显示成功。
- 登录后原生鉴权失败不能显示已连接。
- 旧任务取消不能影响新任务；关闭失败保留同一进程所有者。
- 仅有模型列表或凭据文件不能作为登录成功证据。

### Task 1: 原生 Kimi 登录适配与连接证据

- [x] 添加 CLI 替身进程测试：只读能力检查、系统配置目录、分片授权输出、成功、失败与取消。
- [x] 实现 Kimi 原生登录适配；扩展已有认证端口的可选进度回调，复用任务快照事件。
- [x] 模型探测通过 Kimi ACP `login` 进行只读认证核验；补定向测试。
- [x] 对相关 Go 文件 gofmt，运行受影响包的定向测试及 race 检查。

### Task 2: 登录界面与文档

- [x] 验证 Kimi 内置账号显示登录按钮，授权信息跟随准确任务，成功后刷新账号状态，取消不误显成功。
- [x] 更新账号架构与指南中的 Kimi 登录说明。
- [x] 运行定向前端测试、ESLint、格式及文档检查，复核 diff。

## 执行记录

- 当前分支 `codex/openbot-ui-alignment` 有既存功能修改；按用户连续修复请求原处执行。
- 本机 `kimi login --help` 确认设备码流程及 `--region`，未执行真实用户登录或打开浏览器。

- Ruling: 本机 Kimi 2.0.2 使用 terminal 认证广告，现有 acp-go 不允许通过 Authenticate 调用 terminal 方法。改用已有隔离模型发现路径，官方 Kimi 2.x `session/new` 在创建会话前调用 `ensureAuthed`，作为实际认证回执。

- 验证：Kimi 原生适配及运行时认证定向测试／race 通过；前端 65 项定向测试、ESLint／格式、UI 规范与文档检查通过。本机 2.0.2 在临时原生目录完成 help 与未登录门禁验证。真实用户授权未执行。
- 独立审查：修复 terminal 广告不能通过 agent Authenticate 调用的问题，以及设备码续期更新问题；复核未发现其他确定功能缺陷。
