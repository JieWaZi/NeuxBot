# 桌面软件升级首轮实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans 完成核对与修复，使用 superpowers:requesting-code-review 启动独立审查。步骤以复选框记录实际状态。

**Goal:** 交付未签名版本的检查、下载与手动覆盖升级能力，审查通过后推送功能分支并向 main 提交 PR。

**Architecture:** Electron 主进程持有唯一更新服务，经受限 Bridge 和 revision 快照向共享 Query Cache 交付状态。设置及 Shell 复用状态，系统安装器完成用户手动安装。

**Tech Stack:** Electron 44.2.0、React 19、TypeScript、TanStack Query、Node fetch、Zod、现有 electron-builder DMG/NSIS 与 GitHub Releases。

**Spec:** [桌面软件升级首轮 SPEC](../specs/2026-10-16-software-update-design.md)。

## 全局约束

- 固定仓库 JieWaZi/NeuxBot；平台为 macOS arm64、macOS x64、Windows x64；保持应用标识与数据目录。
- 本轮未做正式签名，安装由用户手动完成，不新增自动替换、镜像、差分、自动回滚、账号或付费服务。
- 只在独立 worktree 的 codex/software-update 分支工作；用户已选择独立子代理审查、修复后推送并向 main 提交 PR，无需再次选择执行或集成方式。
- 项目文档与业务文案使用中文；英文 README 按项目约定同步。修改前遵守 AGENTS.md 阅读路由，保留无关修改。
- 不打开或控制运行中的页面与桌面应用；自动化与真实平台验收分别报告。

## 审查重点

1. 铃铛被更新操作替换后，审批、问答及工作看板是否仍有可达入口；既有快照不能因更新而改变业务已读。
2. 检查失败、用户重试、已有安装包及新发行撤回等状态下，界面操作是否仍能恢复，不被缓存锁死。
3. 恶意或损坏的元数据、重定向、安装包路径与实际文件大小是否突破主进程信任边界。
4. 慢速响应、流中断、磁盘写入失败及退出期间，句柄、临时文件、订阅与定时器是否完整释放。
5. 快照与事件竞态、多个消费者及 Renderer 重新加载是否导致旧状态覆盖、重复下载或无限订阅。

## 执行说明

本轮开始前已有未提交实现和测试。以下先核对已有代码，再复验，不补写或声称不存在的测试先失败历史。独立审查发现的功能缺陷使用新增回归测试先复现，再修复；修复后更新本文及 SPEC。实现状态与验证状态分开记录。

### 任务 1：主进程升级服务与 Bridge

**Files:**

- 新增：`desktop/electron/main/application-update.ts`、`desktop/electron/shared/application-update.ts`。
- 修改：`desktop/electron/main/index.ts`、`desktop-services.ts`、`desktop/electron/preload/index.ts`、`desktop/electron/shared/contracts.ts`、`validation.ts`。
- 测试：`desktop/electron/tests/application-update.test.mjs`、`bridge.test.mjs`。

**Interfaces:** 消费已打包版本、平台、架构、独立缓存目录、fetch 及系统打开文件能力；提供 `snapshot()`、`subscribe()`、`start()`、`check()`、`download()`、`openInstaller()`、`dispose()` 及 SPEC 中的四个白名单命令。

- [x] 核对已有实现的版本筛选、下载校验、缓存、退出与来源边界，禁止扩展为自动替换。
- [x] 运行 `node --test desktop/electron/tests/application-update.test.mjs desktop/electron/tests/bridge.test.mjs`，21/21 通过。
- [x] 审查发现缓存重检和失败流清理缺陷，新增回归测试先失败再修复；主进程与 Bridge 定向复验 26/26 通过。
- [x] 主进程、Bridge 及相应测试已提交为 `17acee0`；21/21 定向测试及全量检查通过。

### 任务 2：关于软件、Shell 入口及状态同步

**Files:**

- 新增：`packages/frontend/src/shared/platform/application-update.ts`、`packages/frontend/src/modules/settings/components/about-software-section.tsx`、`packages/frontend/src/app/layout/desktop-attention-action.tsx` 及各自测试。
- 修改：`packages/frontend/src/app/layout/app-shell.tsx`、`packages/frontend/src/modules/settings/pages/settings-scene.tsx`、`settings-navigation.ts`、`packages/frontend/src/shared/i18n/index.ts`。

**Interfaces:** 消费任务 1 的更新快照、命令及事件；`useApplicationUpdate()` 提供共享状态与操作，设置和 Shell 不维护第二份状态。`DesktopAttentionAction` 复用原工作看板导航回调和更新页导航回调。

- [x] 核对已有设置入口、平台安装文案、共享订阅与 revision 合并，下载完成只提供打开安装包。
- [x] 运行 `NODE_OPTIONS=--no-experimental-webstorage pnpm --dir desktop exec vitest run src/shared/desktop/application-update.test.tsx src/modules/settings/components/about-software-section.test.tsx src/app/layout/desktop-attention-action.test.tsx`，3 个文件、9/9 通过。
- [x] 通知入口和已下载版本重新检查的回归测试先失败再修复；前端定向复验 13/13 通过，使用真实通知组件与路由导航验证看板可达。
- [x] 设置、Shell 与状态适配及测试已提交为 `9df1181`；前端定向 9/9 及全量检查通过。

### 任务 3：未签名发行、指南与设计文档

**Files:** `.github/workflows/release.yml`、`README.md`、`README.zh-CN.md`、`docs/development/build-and-release-cn.md`、`docs/architecture/desktop-lifecycle-cn.md`、`notifications-and-work-board-cn.md`、`docs/guide/getting-started-cn.md`、相关讨论及本 SPEC、计划。

**Interfaces:** 消费既有 DMG/EXE 文件名及 SHA256SUMS；发布任务准备全部资产后公开 Release，说明当前未签名并链接对应版本 README。

- [x] README 和发行说明明确未签名、macOS 单应用处理、Windows SmartScreen 与 HTTPS 错误区别；不关闭全局防护或证书校验。
- [x] 补齐入门指南的手动升级路径，把讨论中的建议与最终实现边界归入 SPEC，删除已失效的待确认描述。
- [x] 运行 `pnpm docs:check`、`git diff --check` 及发布工作流 5 个多行 Bash 语法检查，全部通过。
- [x] 发行、指南、SPEC 与计划一并提交；不打 tag、不发布安装包。

### 任务 4：完整验证、独立审查与 PR

- [x] 运行 `NODE_OPTIONS=--no-experimental-webstorage pnpm check`，退出码 0；前端 253 个文件、1595 项测试，Electron 84 项测试，生成内容、API、构建、Go vet 与全部 Go 测试通过，完整日志本地保留。
- [x] 独立子代理以 base `4fb33f8` 与内容快照 tree `6f8d724` 完成审查；SPEC 无重要缺口，代码审查发现 3 个 Important、无 Critical，审查只读且不继承作者会话。
- [x] 三项重要问题均经回归测试先失败再修复通过，SPEC 与计划已同步；不采纳理由及真实平台验收边界记录于本文。
- [x] 最终代码再次运行 `NODE_OPTIONS=--no-experimental-webstorage pnpm check`，退出码 0；前端 253 个文件、1599 项测试，Electron 89 项测试及全部 Go 检查通过。生成内容与锁文件无额外修改，完整分支空白检查通过。
- [x] `git push -u origin codex/software-update` 成功；已创建目标为 main 的 [PR #1](https://github.com/JieWaZi/NeuxBot/pull/1) 并附到当前任务，未合并 PR。

## 实施与验证结果

| 项目           | 状态与证据                                                                                                                                                                                                  |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 实现提交       | 主进程与 Bridge：`17acee0`；设置、Shell 与共享状态：`9df1181`；发行与文档：`6f04aef`；三项审查修复及同步文档：`3ff3b8a`。                                                                                   |
| 需求与设计     | 已收敛为未正式签名发行、手动覆盖安装，代码核对后补齐本 SPEC 与计划。                                                                                                                                        |
| 自动化验证     | 修复后定向主进程与 Bridge 26/26、前端 13/13；最终完整检查退出码 0，前端 253 个文件、1599 项测试、Electron 89 项测试及全部 Go 检查通过；文档、完整分支空白和 5 个发布脚本语法检查通过。                      |
| 独立审查       | SPEC 无重要缺口；正式代码审查发现 3 个 Important，无 Critical，结论为需修复后再合入。三项均以回归测试先失败后通过完成修复，最终完整复验通过；未另行声称已做第二轮独立审查。                                 |
| 真实平台与视觉 | 未执行；缺少运行中应用操作授权，Windows 真机覆盖安装也未验收。                                                                                                                                              |
| 推送与 PR      | 功能分支 `codex/software-update` 已推送；[PR #1](https://github.com/JieWaZi/NeuxBot/pull/1) 目标为 main，已附到当前任务，保持未合并。GitHub CI 状态以 PR 当前检查结果为准，本表的通过记录来自本地完整检查。 |

首次定向测试因独立 worktree 未安装依赖而在加载阶段退出；按现有锁文件安装后复验通过，未改依赖版本或锁文件。已有测试未补记为测试先失败开发；审查修复的回归验证另行记录。

## 独立审查与修复记录

审查 base 为 `4fb33f8`，固定内容快照为 tree `6f8d724`，与首轮提交 `6f04aef` 的生产源码一致。子代理只读，未继承作者会话。

| 问题                     | 复现证据                                                                             | 修复与定向结果                                                                                                              |
| ------------------------ | ------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| P1：有更新时看板入口消失 | 真实通知组件与 MemoryRouter 的 available、downloading、downloaded 三种导航回归均失败 | 下载操作旁保留原通知按钮、计数及导航；三种状态均可进入看板，相关文件 7/7 通过。                                             |
| P2：缓存使后续检查短路   | 主进程 3 个回归失败，设置页下载完成后的检查按钮禁用回归失败                          | 允许重新检查；相同身份和摘要保留缓存，新版或撤回废止旧入口；检查失败仍可使用此前缓存及重试，主进程 15/15、设置页 2/2 通过。 |
| P2：提前失败未释放响应体 | 非成功 HTTP 响应及真实文件创建冲突两个回归均失败，取消计数为 0                       | 失败 HTTP 响应取消响应体；先取得读取器并将文件创建纳入 finally，取消和释放均验证通过，主进程与 Bridge 26/26 通过。          |
| Minor：新文档尾部空行    | 独立审查固定快照的完整范围空白检查失败                                               | 文档末尾规范为单一换行，与 SPEC 同步编辑一起修正，完整分支范围再次检查。                                                    |

审查设置之外事项的裁定：正式签名、公证、自动替换及自动回滚遵守用户已明确的延期范围，后续接入需重新验证；真实平台安装、数据保留及视觉效果没有验收证据，PR 保留待验收边界；后续签名调查中的费用、资助资格与第三方历史运行不能替代本次实现证据。没有将这些事项描述为已经完成，没有延期未处理的代码风格问题。
