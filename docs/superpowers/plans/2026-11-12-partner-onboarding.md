# 伙伴创建与对话建档实施计划

> **执行技能** 使用 subagent-driven-development。前端和后端各由一个实现子代理负责。

**目标** 新伙伴通过四项表单创建并在首个对话建档，伙伴概览直接显示一份长期指令。

**架构** 共享 React Scene 复用现有选择器和 Chat。Go Core 使用既有 Chat/Execution、档案存储和 allycli 授权，保存已确认的一份档案。

**技术栈** React、TanStack Query、Go、SQLite、ginx/OpenAPI、Vitest。

**设计** [伙伴创建与对话建档设计](../specs/2026-11-12-partner-onboarding-design.md)

## 全局约束

- 在当前 `codex/openbot-ui-alignment` 分支开发，按文件职责并行。
- 前后端分别阅读 AGENTS.md 指定文档。发送、续聊和恢复使用现有本地配置和实际调用。生成文件只用仓库命令更新。
- 只运行直接受影响的定向验证。真实页面与应用验收由用户明确授权。
- 共享接口以设计文档为准。Go、系统技能、Go/OpenAPI 与生成 TS 由后端负责。前端负责手写 React、i18n 与测试。契约落地后双方做一次接入检查。
- 两位实现者只提交各自文件。任务控制者负责权威文档、交叉检查和最终验证。

## 审查重点

- 首发网络重试不能新增第二个伙伴或聊天。
- 已选模型失效时，当前表单仍可调整，已创建伙伴仍可恢复。
- 旧伙伴有用户覆盖与未覆盖内置 seed 时，运行与档案展示一致。
- 拒绝、跳过、跨对话与过期草稿不能变成已发布档案。
- 绑定模型进入新聊天，历史和显式选择按原有优先级保留。

### Task 1 后端创建、引导与档案

**主要文件** `server/apis/wakers/`、`server/services/execution/`、`server/repositories/{models,controllers}/`、`server/internal/{profile,builtinrolepreset,agent/capability}/`、`server/services/channel/team_md.go`、`skills/ally/`。可按现有装配方式新增伙伴服务。负责生成契约及 CLI 资源。

**接口** 实现设计中 `createWaker`、`startWakerOnboarding`、伙伴默认模型字段、`agents` 文档类型、CLI 提议与保存、伙伴事件。先提供可生成的 HTTP 类型和操作标识，通知前端接入。

- [x] 在现有 waker API 测试覆盖同 requestId 重试只创建一次、修改输入返回冲突、已持久但首发失败后重试。运行 `cd server && go test ./apis/wakers -run 'Test.*(Create|Onboarding)'`，确认新用例因当前行为失败。
- [x] 实现基础创建与首发。通过同一稳定请求建立普通 Chat，用 Core 侧上下文发起欢迎，正常续聊从绑定 Chat 恢复引导。优先复用现有先接纳后派发和配置默认解析能力。生成第一版共享契约后通知前端。
- [x] 在伙伴档案或执行测试覆盖 `agents` 优先、三份正文顺序合并、内置覆盖和原文完整、展示字段不会进入执行指令。定向验证旧行为失败后实现统一正文读取及保存，并更新团队指令。
- [x] 增加交互建档回归测试，覆盖伙伴生成单选项和「其他」、缺少实际回答、无效选项、拒绝、跳过、草稿变更、跨对话和相同请求重放。在失败测试的基础上实现受限 CLI 和事务，提交后发伙伴资源事件。待确认的内容不能成为当前档案。
- [x] 对改动 Go 文件运行 gofmt；定向测试 wakers、profile、capability、execution 及直接修改的 team 用例。运行 `pnpm api:generate` 和需要的系统技能生成命令，检查变更范围，提交后端与生成文件。

### Task 2 前端创建页、模型选择与档案正文

**主要文件** `packages/frontend/src/app/layout/app-shell.tsx`、`packages/frontend/src/app/scenes/`、`packages/frontend/src/modules/waker/{api,components,pages,model}/`、`packages/frontend/src/modules/chat/pages/chat-page.tsx`、`packages/frontend/src/shared/i18n/`。复用 `CompanionPicker`、共享模型选择控件、`StaticMarkdown` 或同职责共享组件。

**接口** 消费 Task 1 生成的 API。创建 Scene 收到 `chatId` 时跳转该伙伴的普通聊天；只有 `wakerId` 时能展示原入口和重试。缓存用现有伙伴资源事件定向刷新。等待生成契约期间使用测试 fixture 覆盖表单和交互。

- [x] 在现有侧栏或伙伴测试覆盖「新建伙伴」打开右侧四项表单，创建请求带模型和稳定 requestId，成功直接进入返回的聊天，启动失败可恢复同一伙伴。运行对应 Vitest 文件，确认新增断言失败。
- [x] 实现约 420px 的居中 Scene。头像、色点、形状依次居中，表单采用两行概要、同一行显示的默认模型和紧接模型行的居中按钮。维护有效值、提交状态、局部错误和重试。清理创建路径中的预设控件及无调用组件，伙伴编辑保留姓名、头像、概要、默认模型及既有概览编辑能力。
- [x] 在现有聊天定向测试覆盖新聊天使用伙伴默认模型、历史聊天沿用已保存模型、用户显式选择优先、不可用绑定可由用户更换。验证失败后接入共享配置解析。
- [x] 在伙伴概览测试覆盖 `agents` Markdown 正文、无三文件列表、待建档继续入口、保存或资源事件更新查询。验证失败后实现档案区及单一编辑动作。
- [x] 运行定向 Vitest、定向 ESLint/Prettier 和前端 typecheck。检查生成契约接入并提交前端文件。

### Task 3 联调验证

**负责人** 控制者。核对两位实现者的变更，检查新建、确认、恢复、模型默认、团队摘要与旧数据主路径。根据审查结果派回对应实现者。更新 [领域词汇](../../../CONTEXT.md) 和 [资源与记忆](../../architecture/resources-and-memory-cn.md) 等直接受影响的现行说明。运行 `pnpm docs:check`、`pnpm api:check` 与前后端定向验证，检查 `git diff --check`。
