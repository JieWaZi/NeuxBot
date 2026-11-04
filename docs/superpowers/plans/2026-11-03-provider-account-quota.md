# 账号额度与重置时间实施计划

> 历史增量方案：后续实施以 [AgentRuntime 重建方案](../specs/2026-11-04-agentruntime-rebuild-design.md) 为准。本文件不再约束保留旧 Manager、Session、Catalog；已实现部分仅作为行为与回归证据。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以当前选中账号读取原生额度窗口和重置时间，准确区分未知、失效与耗尽，供服务商详情和聊天提示消费。

**Architecture:** acp-go 增加独立只读 quota extension；Neux Management 沿现有 LaunchLease 调用，账号领域合并查询并保留同主体旧快照。额度与 Token/费用的存储、含义分离，不增加进程池或 Prompt。

**Tech Stack:** Go、ACP extension、厂商原生 control/只读 HTTP、现有 Query/WS。

**Spec:** `docs/superpowers/specs/2026-11-03-provider-runtime-parity-design.md`

## Global Constraints

- 原生协议依据[核验记录](../../discussions/provider-runtime-parity/quota-protocol-cn.md)，不得复制OpenBot实现。
- GPT‑6.1 high子代理，定向测试，禁止继续派生、真实账号查询和UI控制。
- 配额请求不接受token、任意URL、原生method或账号切换参数；身份只能来自当前LaunchLease。
- 不创建模型Prompt、不自动消耗reset credit、不充值、不更换账号。
- 未知不等于0；过了reset时刻只表示旧读数过期，不表示额度已恢复。
- Go结构字段中文注释；生成类型只经生成器；Neux对外时间统一Unix毫秒。

## Review Focus

- Claude旧版本未实现get_usage，与新版本认证/服务失败必须分开。
- 两个账号并发查询、旧账号迟到响应、凭据轮换不能串快照。
- API key或自定义代理没有订阅权益，不能借系统登录向官方端点查询。
- primary/secondary/model桶/三个窗口可同时约束，不能只保留最先一项。
- HTTP重定向、错误响应和日志不能泄露Authorization或响应中的秘密。

## 审查后固定的边界

认证身份代次由账号领域拥有：显式登录或认证配置替换使其变化，正常token刷新不变；不使用token哈希或普通UpdatedAt。清理先关闭临时session/Management，成功后释放LaunchLease；失败保留同一owner重试。无会话quota读取只提取存活检查，不放宽原Probe/Fork的workspace校验。

### Task 1: acp-go quota合同及Codex/Claude/Grok读取

**Files:** 新建acp-go `pkg/quota/{quota,quota_test}.go`；各provider新增 `quota.go`/`quota_test.go`；修改对应agent extension dispatch/initialize metadata；Codex protocol roots/envelope与生成文件；Claude protocol/control DTO与transport的必要typed error。

**Interfaces:** 使用核验记录第3节完整typed合同：`quota.ReadMethod="_account/quota/read"`、`ReadRequest{Version int,SessionID *string}`、`ReadResponse{Version,Status,Reason,Snapshot}`、`Snapshot`及`Window`；Version=1，所有可缺失数值为指针。Initialize metadata为`accountQuota={version:1,read:true,requiresSession:<bool>}`。Codex/Grok false，Claude true。Status取available/not_applicable/unsupported/unauthenticated/unavailable，Reason为有限枚举。

- [ ] **Step 1: RED协议fixture。** Codex多桶去重、reset秒、nullable0、API key不适用；Claude get_usage/skip_behaviors精确control消息、无Prompt、所有窗口/旧版不支持；Grok真实扩展名、缺百分比不是0、Cent={}只在对象存在时为0。
- [ ] **Step 2: 运行新用例记录失败。** 对quota/codex/claude/grok实际修改包使用具体测试正则。
- [ ] **Step 3: 实现typed请求与解码。** Codex纳入本地已有schema roots后generate，保留封闭envelope；Claude复用transport，最低支持证据CLI2.1.309并处理未知方法；Grok截获统一扩展再转发_x.ai/billing，其他既有扩展保持原行为。不得发送空user message。
- [ ] **Step 4: gofmt与涉及包测试。** 覆盖ctx取消、迟到响应、不可用与非法百分比；结果无Prompt计数。
- [ ] **Step 5: 自审提交acp-go，记录原生版本与fixture出处。**

### Task 2: Cursor与OpenCode条件额度读取

**Files:** acp-go `pkg/cursor/quota.go`、`pkg/opencode/quota.go` 和对应测试；各自Config、agent extension与初始化metadata；必要共享HTTP边界放`pkg/quota/http.go`。

**Interfaces:** 消费Task1合同。新增显式 `QuotaTokenSource func(context.Context) (string,error)` 注入当前账号凭据，仅用于此只读能力，不接收调用者URL；来源无法确认则unsupported。HTTP client由构造配置注入用于fixture，生产固定官方端点；不得把任意自定义API Key当官方订阅token。

- [ ] **Step 1: RED身份和HTTP边界。** 两个source不同token、无source不读取真实HOME；跨域重定向拒绝；401/403/429/5xx/超大body；Cursor数字或字符串毫秒和unknown merged状态；OpenCode三个窗口保留且无Go权益返回not_applicable。
- [ ] **Step 2: 运行新测试记录失败。** 仅httptest与临时目录，不访问真实quota服务。
- [ ] **Step 3: 实现只读来源。** Cursor两固定POST、OpenCode Go固定GET，10秒上限，1MiB响应上限，禁跨origin认证跳转。未知schema保持partial/unavailable；不声称私有接口稳定。
- [ ] **Step 4: 定向验证并自审。** 生产认证provider在Task3由当前lease注入；测试不依赖开发机登录。
- [ ] **Step 5: 提交并记录条件支持，其他OpenCode服务保持unsupported。**

### Task 3: Neux Management、账号缓存和quota API

**Files:** `server/internal/agent/native/management.go`及测试、`launch.go`/`registry.go`必要注入、`server/internal/agent/catalog.go`窄管理端口；`server/services/aiaccount/quota.go`及测试；`server/apis/runtime_options/quota.go`、router与测试；`server/global/config.go`装配；生成契约。

**Interfaces:**
- `native.Management.ReadQuota(ctx context.Context, request QuotaRequest) (quota.ReadResponse,error)`；QuotaRequest只含WorkspaceScope。按metadata决定是否临时NewSession，所有关闭沿原Management/LaunchLease可重试清理。
- `AIAccountService.ReadQuota(ctx, runtimeID, accountID string, refresh bool)` 的实际环境/配置由装配端口读取，不让API重建租约逻辑。accountID使用现有`system`哨兵或受管32位ID，不以空值猜另一个账号。
- `GET /runtimes/:runtimeId/quota?accountId=...` 读缓存或首次有界读取；`POST /runtimes/:runtimeId/quota/refresh`显式刷新。返回主体、status/reason、可空snapshot、observedAt、stale；HTTP/厂商失败不清空同主体旧snapshot。
- 缓存key含runtime/account/启动配置指纹/认证身份代次，60秒新鲜窗口；并发请求合并，Core退出取消。普通token轮换不使身份变化。仅系统认证选择可读取固定平台CLI认证源；受管选择不得fallback系统Keychain/auth.json。
- 全局已有资源事件使查询失效，无常驻页面轮询。

- [ ] **Step 1: RED身份/生命周期。** A/B隔离、同主体请求合并、60秒过期、reset过期只stale、刷新失败保留旧值、账号删除使缓存失效；Claude临时session无Prompt且关闭失败保留owner。
- [ ] **Step 2: 运行native/aiaccount/runtime_options对应新测试记录失败。**
- [ ] **Step 3: 实现租约端口、缓存与API。** 系统Cursor认证reader使用既有平台安全工具/指定home，仅查询选中系统身份；OpenCode只允许明确官方Go key，不扫描其他账号。不能证明身份则unsupported。
- [ ] **Step 4: 生成契约和定向验证。** gofmt、涉及Go测试、`pnpm api:generate`；正文与日志无凭据，时间单位只转换一次。
- [ ] **Step 5: 自审提交并更新支持矩阵。** UI消费由后续服务商界面计划负责；真实账号quota仍须单列验收。
