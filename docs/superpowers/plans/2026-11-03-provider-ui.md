# 服务商、组合模型选择与用量界面 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以唯一服务商设置入口、单浮层执行选择、账号配额和准确用量页面承接已确认的截图行为。

**Architecture:** 服务商界面聚合现有 RuntimeEnvironment 与 AIAccount，不新建账号、安装或连接事实。聊天复用 `useExecutionSelection` 与现有选择回调，一次提交完整候选；统计只消费后端用量聚合，配额只消费账号原生快照。生成 Client、TanStack Query 和全局 WS 是全部服务器交互入口。

**Tech Stack:** React、TypeScript、TanStack Query、React Router、Radix、shared/ui、Tailwind CSS 4、i18next、Vitest/RTL/MSW。

**Spec:** [总体规格](../specs/2026-11-03-provider-runtime-parity-design.md)。实施前同时读[账号与界面审计](../../discussions/provider-runtime-parity/accounts-ui-audit-cn.md)、[模型能力计划](2026-11-03-provider-model-capabilities.md)、[额度计划](2026-11-03-provider-account-quota.md)、[安装计划](2026-11-03-managed-provider-runtimes.md)，用量接口消费[统计计划](2026-11-03-provider-usage-analytics.md)约定的聚合合同，详见本计划的后端交付门槛。

## Global Constraints

- “运行时配置和账号各有唯一权威。服务商页面是两者的聚合视图，不新建一套重复账号/运行时状态。”
- “实时变化复用现有全局连接与查询失效机制，不新增常驻页面轮询。”
- “未知能力不冒充支持。”；“不自动切换账号规避额度限制。”
- “归一化 Token 分量为未缓存输入、缓存读取、缓存写入、输出，推理 Token 作为输出的子集单独记录，绝不再次加到输出上。”
- “字段未知与真实零不同，保留缺失状态。”；“不同币种不直接相加。”
- “订阅 Token 的 API 等价估算不是实际账单。”
- “采用截图的信息结构而不是逐像素复刻”；不复制 OpenBot 代码、样式和资产。
- `Avoid unnecessary secondary labels and descriptions.Let the layout do the explaining`。只在真实错误、时效、计价来源或表单含义需要时增加说明。
- 复用当前账号加密/CRUD/登录、Runtime API、共享语义 Token 与控件；模块遵守 `app -> modules -> shared`，不跨模块内部导入。
- 只在生成 API 完成后写依赖这些类型的产品代码；不手改 generated、不用 `as` 补造接口、不在 UI 开发后端协议。
- 仅定向检查，不运行 `pnpm check`、`pnpm --dir desktop check`、全量前端测试。纯布局不增加镜像测试或快照。
- 本计划阶段不操作运行中的浏览器/桌面，不启动真实模型；真实视觉、账号与平台验收另列证据。计划方向已授权，无须重新请求设计批准。

## Review Focus

- A/B 账号同名模型、迟到请求及 Core 重连：B 始终只见 B 的目录和配额，旧 A 响应不能覆盖；Task 1/4/6。
- 已有账号正在登录/刷新、另一账号可用：行不因临时 checking 从已连接跳走，凭据存在不能单独证明连接；Task 2/3。
- 弹层取消、模型空能力及 Fast 显式关闭：取消不提交，不继承旧强度，false 不丢失；Task 4/5。
- 余额不足与周期耗尽、重置时刻已过、旧配额读取失败：只展示可证明的时间，不自动恢复、不重发 Prompt；Task 6。
- 跨午夜/时区、部分 Token、未定价与混合币种：日期归属以服务端结果为准，未知不显示免费或完整合计；Task 7。

---

## 文件与接口边界

| 文件                                                                                                                            | 职责                                                                     |
| ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `shared/api/provider-queries.ts`（新增）                                                                                        | 账号目录、quota、安装操作的生成 Query 包装及统一 key；无第二套服务器缓存 |
| `shared/lib/provider-presentation.ts`（新增）                                                                                   | 从运行时/账号事实派生列表行、搜索、连接分组；设置与聊天两处使用          |
| `modules/settings/components/provider-settings-section.tsx`（新增）                                                             | 总览/详情装配与 provider URL 选择                                        |
| `modules/settings/components/provider-accounts-section.tsx`（新增）                                                             | 账号引用列表及既有 CRUD/登录装配                                         |
| `modules/settings/components/account-form-dialog.tsx`（新增，迁移）                                                             | 迁移现 `AIAccountsSection` 私有 AccountForm，保持保存/取消/密钥规则      |
| `modules/settings/components/provider-runtime-section.tsx`（新增，迁移）                                                        | 迁移现 Runtime 行配置、默认/启停/探测，接入安装/重启                     |
| `shared/ui/execution-provider-model-control.tsx`（新增）                                                                        | 单 Popover：provider/account rail、model、effort、Fast、完整草稿提交     |
| `shared/lib/execution-selection-draft.ts`（新增）                                                                               | 选择归一化与 capability 校验，禁止跨模型继承未知能力                     |
| `shared/ui/account-quota-popover.tsx`（新增）                                                                                   | 账号额度窗口与时效；供聊天、设置调用                                     |
| `modules/chat/components/chat-quota-banner.tsx`（新增）                                                                         | 当前失败主体与额度快照匹配后的耗尽提示                                   |
| `modules/usage/{index.ts,api/usage-queries.ts,pages/usage-page.tsx,components/usage-report.tsx,model/usage-filters.ts}`（新增） | 独立用量展示领域；查询、URL、费用/Token 表格与每日走势                   |

表中前端路径均相对 `packages/frontend/src/`。测试与实现同目录、同名 `.test.ts(x)`；下面给出精确修改范围。不机械拆单个 Hook；详情普通分组复用 `DetailSection`、`SettingsGroup`、`ReadOnlyField`，不在每层叠 Card。

### 后端交付门槛（消费接口，不假称当前已有）

1. 模型计划提供 `RuntimeModelOption.configurationKnown/fastSupported/defaultFast`、执行与默认配置 `fast/defaultFast`，账号 native models 返回 `options/accountId/runtimeId/detectedAt`。保留 `false | true | undefined`；`undefined` 表示继承原生/默认，不等于关闭。Query identity 必须含 runtime、account、安装/配置修订和认证身份代次；普通 token 轮换不变。
2. 安装计划提供 `GET /runtimes/:runtimeId/install-offers`、`POST .../install`、`GET .../install-operation`、`POST .../install-cancel`、`POST .../restart`。UI 只消费 `managed/external/unsupported/blocked` disposition 和 operation phase，不用 `RefreshOne` 冒充更新/重启。若最终 DTO 未提供实际 maintenance action 能力，先在安装任务补齐窄字段再接按钮，不由页面猜版本/平台。
3. 额度计划提供 `GET /runtimes/:runtimeId/quota?accountId=...`、`POST .../quota/refresh`：主体、status/reason、nullable snapshot、observedAt、stale。该计划只读，没有消耗 credit 的 reset API；本计划的 reset banner 指真实窗口重置时间，仅展示真实额度重置时间，不实现重置/兑换额度动作。
4. 连接状态复用已探测 Runtime 的 `state/auth/diagnostics` 与匹配默认账号；`hasCredential` 只代表保存了凭据。若需要将非默认受管账号标为已连接，账号领域须先提供该主体的已验证 `runtimeId/accountId/state/observedAt/stale` 窄摘要，沿既有查询返回，不新建 provider 状态库。未有该证据的账号显示已保存/待验证，不以一次模型列表或另一个账号探测补证；无凭据也不能推导系统原生账号未连接。旧验证证据必须显示stale，不隐藏时效。
5. 统计交付 `GET /api/v1/usage/summary`（Operator `GetUsageSummary`，SDK `getUsageSummary`）、`GET /api/v1/usage/groups`（Operator `ListUsageGroups`，SDK `listUsageGroups`）。参数 `from/to` 为 Unix 毫秒半开区间、`timezone` 为 IANA、可选 `wakerIds` 最多100；groups 接受 `groupBy=day|model|waker`、`limit`默认50、最大100、稳定 cursor。先取summary返回的snapshot，groups及其后续分页复用该snapshot；筛选变化清空snapshot/cursor，分组变化清空cursor，snapshot过期重新读取summary。消费生成 `getUsageSummaryOptions/getUsageSummaryQueryKey` 与 `listUsageGroupsOptions/listUsageGroupsQueryKey`，生成后核对实际导出；只在 `modules/usage/api/usage-queries.ts` 包装，禁止先伪造生成类型。
6. 统计响应固定顶层 `range/snapshot/coverage/metrics`；groups响应另有 `groups:[{key,label?,metrics}]/totalGroups/nextCursor/hasMore`。金额在 `metrics.amounts[]`，按currency/valuation并保留origin/source分开，amount为decimal string；同消费优先原生，仅以已证实不重叠估算补缺，对照估算在 `metrics.comparisonAmounts[]`，不加到主小计。`metrics.tokens`各分量及 `totalWithCacheRead/totalWithoutCacheRead` 可空，已知小计在 `knownTokens/knownTotal*`；计数 `invocations/missingUsageInvocations/incompleteUsageInvocations/unpricedInvocations`，不能改叫消息数。`coverage` 含state=ready/partial/rebuilding、earliestRetainedAt、legacyBackgroundUnknown、indeterminateInvocations、reasons；priceVersions在amounts项。金额来源与覆盖率是必要信息，以统计计划和生成类型为唯一合同，不另造统计源。

### Task 1: 统一查询身份与服务器状态消费

**Files:** Create `packages/frontend/src/shared/api/provider-queries.ts`、`provider-queries.test.ts`；Modify `shared/api/runtime-queries.ts`、`runtime-queries.test.ts`、`shared/hooks/use-execution-selection.ts`；测试新增 `shared/hooks/use-execution-selection.test.tsx`（以上 Modify/Test 同处 `packages/frontend/src/`）。

**Interfaces:**

- Consumes: 上述模型、quota、install 的最终生成 Options/QueryKey；`withLocalErrorSurface`/`withLocalErrorMutation`；现有 `listAiAccountsOptions()`、`listRuntimesOptions()`。
- Produces: `accountModelCatalogOptions(identity)`、`accountQuotaOptions(identity)`、`runtimeInstallOffersOptions(runtimeId)`、`runtimeInstallOperationOptions(runtimeId)`。`identity` 参数类型从生成请求/响应与已公开修订字段选取，不在 UI 自建认证代次。相同主体共用 Query；错误/后台刷新保留同主体旧值，不跨主体 `placeholderData`。
- Produces: `useExecutionSelection` 直接消费真实 model options，返回逐模型 capability、fast 与目录 loading/stale/error；保留现有调用者字段，不让旧局部 thinking 查询覆盖新版完整目录。

- [ ] **Step 1: RED。** MSW 延迟返回 A 后切换 B，断言 B 只见 B 模型；B 明确空强度不继承 A/global；未知目录不可提交；同账号多个消费者只读同 Query，普通 token 轮换不变 key、认证身份更换变 key。
- [ ] **Step 2: 执行失败测试。** `pnpm --dir packages/frontend test src/shared/api/provider-queries.test.ts src/shared/hooks/use-execution-selection.test.tsx`，预期新导出或行为未实现失败。
- [ ] **Step 3: 实现上述 Options 与 Hook。** Signal 传给生成 Client；删除旧模型空数组的全局 fallback；keys 统一在此构造。只消费实际已生成字段，接口未交付则停止该依赖任务并推进不依赖它的迁移，不能用临时假类型上线。
- [ ] **Step 4: 运行同命令至PASS，加 `src/shared/api/runtime-queries.test.ts`。** 校验取消不报业务错误、同主体失败标 stale 保留已有模型。
- [ ] **Step 5: 自审后提交本任务文件。** Commit `refactor: unify provider query identities`；不包含他人改动或临时 Go workspace。

### Task 2: 唯一服务商导航、列表与旧入口兼容

**Files:** Create `packages/frontend/src/shared/lib/provider-presentation.ts`、`.test.ts`；Create `modules/settings/components/provider-settings-section.tsx`、`.test.tsx`；Modify `modules/settings/settings-navigation.ts`、`.test.ts`、`pages/settings-scene.tsx`、`.test.tsx`、`index.ts`、`shared/i18n/index.ts`；Modify `app/scenes/settings-route.ts`、`.test.ts`、`app/scenes/app-route-scene-model.ts`、`app/layout/app-shell.tsx`（上述相对前端 src）。

**Interfaces:**

- Produces: canonical `/settings/providers?provider=<runtimeId>&q=<search>`；`settingsSectionOf('ai-accounts'|'runtime-environments')` 归一为 providers。保留无效 provider 的未找到/返回列表状态，不悄悄展示另一个详情。
- Produces: `ProviderSettingsSection({providerId, search, onSelectProvider, onSearchChange})`；`onSelectProvider(id?:string)` 与 `onSearchChange(value:string)` 由 app 路由装配，保留单一 Settings Scene/tab/mountKey，不在模块内建第二导航。
- Produces: `providerRows(runtimes, accounts, verifiedConnections)` 的只读派生，row ID 为稳定 runtime ID；账号仍以 ID 引用，跨 Runtime API 账号不复制。自定义 endpoint 显示实际服务身份，不凭协议冒用官方品牌。搜索覆盖 Runtime 显示名/ID和可公开账号 label/issuer，不查密钥或秘密。

- [ ] **Step 1: RED行为测试。** 旧两入口都命中同 providers Scene且返回/前进保留provider/q；空库 Shell与其他服务商可见；仅installed/hasCredential不能成为connected；已验证账号A仍可用、B checking时留在已连接；无匹配与无账号文案不同。
- [ ] **Step 2: 执行失败测试。** `pnpm --dir packages/frontend test src/shared/lib/provider-presentation.test.ts src/modules/settings/settings-navigation.test.ts src/modules/settings/pages/settings-scene.test.tsx src/modules/settings/components/provider-settings-section.test.tsx src/app/scenes/settings-route.test.ts`。
- [ ] **Step 3: 装配列表与路由。** 分组“已连接”“其他服务商”有数据才显示；默认标记只来自Runtime默认；列表显示真实连接状态、必要版本更新标记。复用 ResourcePageToolbar/SearchField与列表行，页面初读Skeleton；后台失败保留行并显示区域错误。旧菜单只留新的“服务商”，旧URL规范化replace后复用同实现，不继续挂两套页面。
- [ ] **Step 4: 重跑同命令至PASS。** RTL验证搜索与导航结果，不断言 CSS/className 或具体 DOM 层数；窄容器布局留 Task 8人工验收。
- [ ] **Step 5: 自审提交。** Commit `feat: unify provider settings navigation`。

### Task 3: 服务商详情复用账号与真实 Runtime 维护

**Files:** Create `packages/frontend/src/modules/settings/components/account-form-dialog.tsx`、`provider-accounts-section.tsx`、`provider-runtime-section.tsx` 及后两者 `.test.tsx`；Modify 同目录 `ai-accounts-section.tsx`、`runtime-environments-section.tsx`、`account-login.tsx`（仅公开必要回调）、`provider-settings-section.tsx`、现有账号/Runtime定向测试、`shared/i18n/index.ts`。

**Interfaces:**

- Produces: `AccountFormDialog({account?,accounts,onSaved:(id?:string)=>void,onClose:()=>void})`，迁移现 `AccountForm`，保存继续调用 `createAiAccountMutation/updateAiAccountMutation`，不重写凭据/登录逻辑。
- Produces: `ProviderAccountsSection({runtime,accounts,onAccountSaved})`；`ProviderRuntimeSection({runtime,accounts})`，复用 `AccountLogin`、delete确认、已有默认账号/模型/权限/路径编辑。`onAccountSaved(id)` 只定位/关闭，不同时失效第二次。
- Consumes: `refreshRuntimeMutation`仅探测；`updateRuntimeEnvironmentMutation`保存 enabled/isDefault/defaultAccountId/defaultModel/defaultReasoning/defaultFast等；安装/取消/重启消费Task 1 Options与最终生成 Mutation。

- [ ] **Step 1: RED。** 添加/重连/重命名/移除仍作用同账号 ID；跨Runtime引用同账号编辑后一起更新；密钥默认脱敏、取消不保存；busy保留正在执行工作与表单；probe不发install/restart；更新失败仍旧版本，restart busy不调用kill或重发；无managed offer显示外部入口与复制命令。
- [ ] **Step 2: 运行失败测试。** `pnpm --dir packages/frontend test src/modules/settings/components/provider-accounts-section.test.tsx src/modules/settings/components/provider-runtime-section.test.tsx src/modules/settings/components/ai-accounts-section.test.tsx src/modules/settings/components/account-login.test.tsx`。
- [ ] **Step 3: 迁移业务片段，装配统一详情。** 页头操作按启用→维护→主CTA；内容依次 Accounts/API Key/Runtime。订阅登录只给后端实际支持provider；API key复用表单。runtime段呈现真实来源/版本/探测/安装operation，支持cancel/retry；explicit路径切换须用户明确确认其实际覆盖范围。默认/启用影响下一轮，不能改当前执行。原两 Section 文件删除重复页逻辑，若仍有内部调用只做同组件装配，不保留第二CRUD/状态源；同步原测试定位。
- [ ] **Step 4: 同命令至PASS，再运行 `src/modules/settings/components/runtime-environments-section.test.tsx`。** 检查旧功能迁移保留默认账号、权限/路径与API自定义模型编辑。删除或迁移过时测试，不并行维护旧页面事实。
- [ ] **Step 5: 自审提交。** Commit `feat: manage accounts and runtimes in provider details`。

### Task 4: 单浮层 provider/account/model/effort/Fast 选择器

**Files:** Create `packages/frontend/src/shared/lib/execution-selection-draft.ts`、`.test.ts`、`shared/ui/execution-provider-model-control.tsx`、`.test.tsx`；Modify `shared/ui/execution-model-control.tsx`（只抽取实际复用的强度/焦点机制）、`shared/i18n/index.ts`。

**Interfaces:**

- Produces: `ExecutionSelectionDraft = {runtimeId:string;accountId:string;model:string;reasoning:string;fast?:boolean}`；`normalizeExecutionSelectionDraft(candidate, catalog): {value:ExecutionSelectionDraft;valid:boolean;reason?:'loading'|'unavailable'|'unsupported'}`。catalog来自Task 1，包含当前主体的逐模型options；不以旧目录填新主体。
- Produces: `ExecutionProviderModelControl({value,candidate,onCandidateChange,catalog,runtimes,accounts,disabled?,onCommit?,onOpenProvider?})`。`value/candidate`为上述draft，`catalog:ExecutionSelectionState`由外层对候选调用Task 1 Hook得到；`onCandidateChange(next:ExecutionSelectionDraft)`只更新本地候选，`onCommit(next:ExecutionSelectionDraft)`每次确认只调用一次。Hook不得在事件回调中调用。
- 外层只持本次候选、组件只持open/search；打开复制value到候选，关闭丢弃未提交候选，禁止复制目录或账号事实。provider/account选择只切候选，模型/effort/Fast更新候选，底部单一“应用”原子提交后关闭，Esc/点击外部不提交；没有onCommit的只读调用隐藏可改Fast/应用操作。

- [ ] **Step 1: RED。** 打开→A切B→选model/effort/Fast→应用只一次完整onCommit；Esc/外部关闭零调用；B空强度清旧值、B不支持Fast清候选；已支持Fast关闭提交false，切不支持模型清除旧true；unknown禁用应用且可刷新；只读调用无onCommit时无可改Fast；搜索中文输入法Enter不误提交。
- [ ] **Step 2: 运行失败测试。** `pnpm --dir packages/frontend test src/shared/lib/execution-selection-draft.test.ts src/shared/ui/execution-provider-model-control.test.tsx`。
- [ ] **Step 3: 实现单Popover。** `PopoverContent width="wide" variant="selection"`限制视口；provider/account rail独立滚动、model区搜索与列表、底部effort/Fast共享层次；窄浮层以自身容器重排为上方rail与下方模型，控件不缩字体/命中区。搜索只搜当前主体模型，空/加载/错误独立；Radix/RadioGroup/Select/Switch复用现件，不复制通用选择器。焦点打开到搜索/当前项，方向键/Tab可遍历，Esc还原触发器；容器自身无整块focus框。
- [ ] **Step 4: 同命令至PASS，加 `src/shared/ui/execution-model-control.test.tsx`。** 检查键盘选项、焦点恢复与控件可访问名称，不测试排版实现细节。
- [ ] **Step 5: 自审提交。** Commit `feat: add unified provider model selection popover`。

### Task 5: 聊天与团队原子选择接入

**Files:** Modify `packages/frontend/src/modules/chat/components/chat-composer-controls.tsx`、`.test.tsx`、`pages/chat-page.tsx`、`pages/team-chat-page.tsx`、`model/use-chat-view-state.ts`、`.test.tsx`；Modify `modules/team/team-chat-context.ts`；Modify `modules/chat/runtime/use-chat-runtime.ts`、`chat-composer.integration.test.tsx` 及当前实际提交DTO映射（追踪本任务新增fast，不重构其他发送逻辑）。

**Interfaces:**

- Consumes: Task 4 `onCommit(next)`→现 `updateExecutionSelection(patch)`。`ChatExecutionSelectionPatch`增加 `fast?:boolean`，整组runtime/account/model/reasoning/fast一次传入；permission沿既有控件保持。
- Produces: 普通聊天草稿 `ChatViewState.fast?:boolean`、团队 `TeamMemberExecutionConfig.fast?:boolean`；提交/排队/重试参数采用模型计划已生成fast合同，不用 `||` 覆盖false。当前执行摘要继续读冻结last/accepted值，下一轮候选只写草稿。合并patch用`Object.hasOwn(patch, "fast")`区分缺字段与显式清继承值，不用`?? current.fast`把旧true留住；完整候选始终包含fast键，跨账号/不支持模型切换时可清成undefined。

- [ ] **Step 1: RED。** 普通新聊天提交完整组合且false不丢；旧聊天切runtime/account仍走现sessionSwitch确认，一次应用无错配中间态；取消sessionSwitch保持原值；团队只更新指定成员一次；切聊天/成员后不串draft；设置改默认不重写已接纳执行。
- [ ] **Step 2: 运行失败测试。** `pnpm --dir packages/frontend test src/modules/chat/components/chat-composer-controls.test.tsx src/modules/chat/model/use-chat-view-state.test.tsx src/modules/chat/runtime/chat-composer.integration.test.tsx`，需要团队入口时定向加入 `src/app/pages/team-thread-page.integration.test.tsx`。
- [ ] **Step 3: 用新控件替换composer两独立入口。** `useChatComposerControls`公开单一组合控件；保留`ExecutionRuntimeControl/ExecutionModelControl`其他真实调用者，不强迫不在范围内的配置表单迁移。移除旧composer按账号/模型分别patch的竞争路径，外层候选目录查询只有一个owner。保持ChatEffortEffects读已选模型真实选项，禁止未知能力触发最大强度效果。
- [ ] **Step 4: 同命令至PASS。** 若触及排队/重试/fork序列化，运行对应既有定向用例，断言false/nil与账号身份实际交付，不只断言控件状态。
- [ ] **Step 5: 自审提交。** Commit `feat: apply provider selection atomically in chat`。

### Task 6: 账号额度浮层与耗尽恢复提示

**Files:** Create `packages/frontend/src/shared/ui/account-quota-popover.tsx`、`.test.tsx`、`modules/chat/components/chat-quota-banner.tsx`、`.test.tsx`；Modify `shared/api/provider-queries.ts`、`modules/settings/components/provider-accounts-section.tsx`、`modules/chat/components/chat-composer-controls.tsx`、`chat-error-card.tsx`、`model/runtime-failure-presentation.ts`及现有tests、`shared/i18n/index.ts`。错误结构若需要新增字段，先由模型/错误计划贯通生成DTO，UI不从stderr解析。

**Interfaces:**

- Produces: `AccountQuotaPopover({identity,modelId,trigger})`，identity与Task 1一致，展开才启用读取，关闭释放该消费者；同账号详情/聊天共享Query。
- Produces: `ChatQuotaBanner({failure,quota,onOpenProvider,onRefresh})`；failure必须含后端冻结runtime/account与稳定code及可证实reset/retryAfter，不使用今天的选择关联历史失败。仅`QUOTA_EXHAUSTED`使用套餐窗口时间；`BILLING_ERROR`进入费用/账号设置；`RATE_LIMITED`只用真实retryAfter。
- Consumes: 模型能力计划Task3具体交付的`FailureRecovery`，来自同次执行/调用并经权威日志、messageview及生成DTO投影。旧日志主体未知只显示通用失败，不绑定当前composer账号。时间缺失时可读同冻结主体的quota证据，不能从错误文本或另一主体补时间。
- Produces: 扩展既有`runtimeFailurePresentation`的稳定action描述与`ChatErrorActions`回调：AUTH_REQUIRED进入冻结服务商账号登录；RUNTIME_UNAVAILABLE/SPAWN_FAILED及协议不兼容进入该运行时维护；未注册RUNTIME_NOT_FOUND进入服务商列表；MODEL_UNAVAILABLE进入同主体目录刷新/重新选择；BILLING_ERROR/套餐限制进入对应账号费用/服务入口。恢复导航不发Prompt、不自动换账号；NETWORK_ERROR/REQUEST_TIMEOUT沿既有安全重试门槛，不新增自动重试。稳定code生成本地化主文案，脱敏detail仅置于可展开细节，未知code用通用文案。

- [ ] **Step 1: RED。** quota nullable与0区分；多额度组/多个窗口全保留，model只选择匹配组；unauthenticated/unsupported/unavailable/stale呈现不同；A迟到不写B；resetAt已过仅标陈旧，刷新失败保留旧快照；billing无套餐重置时间；历史失败与当前账号不同不显示错账号quota；重启重放保留冻结主体及真实时间，无证据/旧日志不关联今天选择。分别验证登录、未安装/启动、协议、模型不可用的恢复入口与未知code降级；主文案不直接展示stderr，导航/刷新不重试Prompt、不切账号，网络错误不放宽安全retry。
- [ ] **Step 2: 运行失败测试。** `pnpm --dir packages/frontend test src/shared/ui/account-quota-popover.test.tsx src/modules/chat/components/chat-quota-banner.test.tsx src/modules/chat/components/chat-error-card.test.tsx src/modules/chat/model/runtime-failure-presentation.test.ts`。
- [ ] **Step 3: 实现额度展示与恢复入口。** 剩余百分比仅在已用百分比已知合法时计算；余额/原生量依unit展示，不强转百分比。显示窗口、真实resetAt和stale读取时间；支持显式刷新。通过已有ChatErrorCard/MessageFailure的恢复位置装配banner，避免同一错误再弹第二toast。quota失效由全局资源事件进入`useQueryEvents`；resetAt本地一次性时钟仅使显示过期、不发轮询、不改服务器snapshot，卸载取消timer。没有reset能力不画credit操作。
- [ ] **Step 4: 同命令至PASS。** fake time验证过期与卸载；无永久refetchInterval或新WS。确认Core离线保留读数且不显示成正常0/100%。
- [ ] **Step 5: 自审提交。** Commit `feat: show scoped account quota and recovery guidance`。

### Task 7: 用量页面、每日费用与完整性

**Files:** Create `packages/frontend/src/modules/usage/index.ts`、`api/usage-queries.ts`、`.test.tsx`、`model/usage-filters.ts`、`.test.ts`、`pages/usage-page.tsx`、`.test.tsx`、`components/usage-report.tsx`、`.test.tsx`；Create `shared/i18n/usage.ts`；Modify `shared/i18n/index.ts`、`modules/settings/settings-navigation.ts`、`pages/settings-scene.tsx`、`app/layout/app-shell.tsx`、`app/scenes/settings-route.ts`及直接受影响tests。

消息展示同任务修改 `packages/frontend/src/modules/chat/runtime/chat-projection.ts`、`chat-projection.test.ts`、`components/chat-continuation.ts`、`chat-continuation.test.ts`、`components/chat-thread.tsx`、`chat-thread.test.tsx`；仅接线既有指标/Tooltip，不创建新统计源。

**Interfaces:**

- Consumes: 统计子项目 summary/groups 生成合同，见前述门槛；waker筛选通过模块公开 `listWakersOptions` 在app装配传入，不从usage直接导入waker内部。
- Produces: `/settings/usage?from=<ms>&to=<ms>&timezone=<IANA>&waker=<id>&groupBy=day|model|waker&metric=cost|tokens`；重复waker参数表示多选。`parseUsageFilters(search,now)`、`usageFiltersSearch(filters)`纯函数，默认最近30个本地日（含今日，to为次日本地零点）；服务端负责按timezone分桶，前端不得用24h毫秒假设替代DST日期边界。
- Produces: `UsagePage({search,onSearchChange,wakerOptions})`；app从`modules/usage`公开入口装配为SettingsScene的`usageContent`，沿IM现模式注入，settings不跨模块内部导入。
- Produces: `usageSummaryOptions(filters)`、`usageGroupsOptions({...filters,snapshot,groupBy,limit:50,cursor?})`包装最终生成Options；`UsageReport({summary,groups,metric,onMetricChange,onLoadMore})`只读后端事实。
- Consumes: [统计计划Task 6](2026-11-03-provider-usage-analytics.md)生成的`MessageRunMetrics.tokenUsage`，含nullable tokens/knownTokens、complete、sources/coverages、missingUsageInvocations/incompleteUsageInvocations与两个完整total/已知total；映射到前端既有RunMetrics时保留全部缺失语义，旧五个数字只供兼容，不能充当已知/完整证据。消息投影来自同execution规范Invocation receipts，不再将execution最后快照视为全部；Context仍独立取最近有效快照。
- Produces: `chat-continuation`按ExecutionID和消息修订先替换重复/迟到快照，再合并不同execution的TokenUsage；完整分量只有所有应计贡献已知且覆盖完整时才给完整和，有未知时只给已知小计和partial。它只呈现后端事实，不写回receipt，也不作为全局summary来源；同Invocation的临时观察与规范终态不能重复相加。

- [ ] **Step 1: RED。** range/时区/waker/groupBy进URL及key；同日期不同timezone不能共cache；费用与Token切换不扫描聊天；fixture混USD/CNY、API估算/原生金额、未知与已知0、partial、missing/unpriced/incomplete计数均按合同展示；reasoning不加到output；API费用估算不能称订阅账单。summary返回snapshot后才查询groups和分页；筛选变化清空snapshot/cursor，分组变化清空cursor，snapshot过期重新获取summary；迟到旧snapshot不能覆盖新筛选。分页后不重算准确总计，后端summary.metrics保持权威。
- [ ] **Step 1a: 消息指标RED。** chat-projection实时/历史fixture映射保留tokenUsage元数据及真实0；Tooltip显示已知0，不用truthy隐藏。续接完整100+未知→完整值unknown、已知小计100且partial；0+0→已知0；两段未知→unknown；非零complete=false仍部分，legacy完整性未声明。两个Invocation合计与后端fixture一致，重复消息修订/同execution续接替换不重加；reasoning是output子集不另加总，Context取末段有效快照且不相加。
- [ ] **Step 2: 运行失败测试。** `pnpm --dir packages/frontend test src/modules/usage/model/usage-filters.test.ts src/modules/usage/api/usage-queries.test.tsx src/modules/usage/pages/usage-page.test.tsx src/modules/usage/components/usage-report.test.tsx`；消息行为单独运行 `pnpm --dir packages/frontend test src/modules/chat/runtime/chat-projection.test.ts src/modules/chat/components/chat-continuation.test.ts src/modules/chat/components/chat-thread.test.tsx`，不运行全量。
- [ ] **Step 3: 实现页面。** ResourcePageToolbar组合DateRangePicker、SearchableMultiSelect伙伴、groupBy/metric共享选择；默认费用视图。展示按currency+valuation区分的金额小计/来源、每日走势、模型/伙伴/日期明细与可展开Token分量。走势用原生SVG+语义Token，不引入图表库；提供同值可访问表格，缺失日期/未知金额不画0消费。只对已知金额格式化，不用浮点把不同来源decimal总和重新计费。未知total展示“已知小计”和不完整含义；reasoning标输出子集；coverage说明历史范围/索引partial及未定价，不追加重复副标签。
- [ ] **Step 3a: 接通消息投影、续接与Tooltip。** chat-projection原样映射生成的tokenUsage，迟到版本遵守已有消息修订；chat-continuation移除将未知经`?? 0`变成普通完整数字的合并，按ExecutionID/修订幂等派生完整和与已知小计。chat-thread的RunMetricsTooltip以presence显示真实0、以一个必要覆盖说明表达partial/source，并展示两个规范total或其已知小计，未知历史不升级完整；不堆叠重复副标签、不从Context推消费。前后端使用相同语义fixture，不增加第三份执行/统计事实。
- [ ] **Step 4: 同命令至PASS。** 模拟服务端时区桶与迟到旧筛选响应，验证新筛选不展示旧分组；首次Skeleton、后台错误保留数据、空结果与无采集覆盖不同。消费统计项目注册的同一全局WS逻辑topic `usage`、resource `{kind:"global"}`、事件 `usage.changed`；由`useQueryEvents`精确失效summary/groups，隐藏只标stale，恢复一次对账，不永久polling。端与服务端topic白名单由统计项目同步，页面不自行绕开白名单。
- [ ] **Step 5: 自审提交。** Commit `feat: add usage costs and completeness report`。

### Task 8: 定向集成、无障碍与交付证据

**Files:** Modify必要的上述行为tests；更新 `docs/architecture/runtimes-and-accounts-cn.md` 已实际落地的页面/查询边界；Create `docs/discussions/provider-runtime-parity/ui-acceptance-cn.md` 记录事实证据。不将未实施能力写进当前架构说明。

**Interfaces:** Consumes Tasks 1–7和总体规格验收矩阵；不增加产品接口。

- [ ] **Step 1: 只补跨边界缺口。** MSW+同QueryClient验证设置改账号→composer目录重读、同resource多个消费者只刷新一次、隐藏Scene恢复不丢首次加载期间事件、install完成刷新真实版本/能力、quota与usage事件互不污染。优先扩展现`shared/api/use-query-events.test.tsx`，不复制事件调度器测试；纯CSS重排无镜像测试。
- [ ] **Step 2: 运行定向集成与静态检查。** 只运行本次实际变更测试路径；`pnpm --dir packages/frontend typecheck`；`pnpm --dir packages/frontend exec eslint <本次TS/TSX相对frontend路径>`；`pnpm exec prettier --check <本次文件>`；`node scripts/check-ui-style-policy.mjs`；后端合同变更已生成后运行 `pnpm api:check`。预期全部退出0；不以该结果宣称真实平台已验收。
- [ ] **Step 3: 记录人工验收矩阵，获得明确UI控制授权后才执行。** 浅/暗主题；Scene内容320/480/960px与应用宽窗窄pane；长中文账号、长endpoint/model、空库/未安装/登录中/错误/离线/取消；首屏Shell/Skeleton稳定；provider rail和列表各自滚动、工具栏换行、无根横裁切；鼠标无整pane焦点框，Tab/方向键/Enter/Space/Esc可完成选择/返回/取消/确认/重试，关闭恢复焦点。无授权时如实标“未运行”，不打开应用代验。
- [ ] **Step 4: 写交付证据并核验文档。** 记录实际commands、通过用例、生成契约差异、真实平台/账号/视觉未验证范围和统计覆盖边界。`pnpm docs:check`、`git diff --check`退出0；仅修本次问题。
- [ ] **Step 5: 审查已确认范围并提交。** Commit `docs: record provider UI integration checks`；执行时按既定方法做独立审查，不推送/发布，不把本计划完成当总体功能验收。

## 依赖顺序与完成定义

模型/账号身份合同、Runtime维护合同先完成生成，再执行Task 1相应包装；Task 2的导航/现有片段迁移可先推进。Task 3依赖安装合同，Task 4依赖模型合同，Task 5依赖Task 4及执行fast贯通，Task 6依赖只读quota，Task 7依赖统计聚合；可用的局部功能不得伪装缺失能力。Task 8汇总真实证据。

实现完成须同时满足：旧入口命中唯一服务商页；同账号跨视图共用权威与Query；组合选择到实际提交无错配；未知/陈旧/不支持真实可见；费用、Token、额度三个口径分离；所有必要定向检查通过。真实视觉、模型、账号与平台的通过状态单独列出，未执行不补写成功。
