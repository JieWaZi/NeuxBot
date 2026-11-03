# 受管 CLI 安装、更新与重启实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 服务商详情可发现、下载、校验、激活和更新有可靠官方来源的CLI，并保留外部安装及正在执行的旧版本。

**Architecture:** 新建独立文件/网络技术模块 `server/internal/runtimeinstall`，运行时服务拥有一次安装任务并复用Catalog探测。RuntimeEnvironment仍唯一保存用户选择和激活路径，Manager仍唯一管理会话，不新增provider进程池。

**Tech Stack:** Go net/http、tar/zip、SHA-256/SHA-512、现有Catalog/RuntimeEnvironment/事件系统。

**Spec:** `docs/superpowers/specs/2026-11-03-provider-runtime-parity-design.md`

## Global Constraints

- 来源依据[官方安装源核查](../../discussions/provider-runtime-parity/installer-sources-cn.md)，不用OpenBot旧锁版本或复制其实现。
- GPT‑6.1 high子代理；不派生子代理；Go结构字段中文注释；定向验证，不控制UI。
- 只写应用私有runtime目录，不写系统PATH、shell配置、CLI原生HOME；不执行npm lifecycle/install脚本。
- 解析offer和下载执行分离，任务使用冻结版本/URL/digest，不在安装途中重新选择latest。
- 下载、摘要、布局、许可、版本/协议探测都通过才激活；失败保留旧版，活动会话使用的版本不删除。
- 默认元数据请求10秒、2MiB上限；artifact单个512MiB、展开总量2GiB、最多20000成员；失败不截断为成功。
- 无可靠hash、许可、布局或目标平台能力时返回明确blocked/external，不伪造可安装。

## Review Focus

- 自定义路径在下载过程中被用户修改，激活不能覆盖较新选择。
- archive绝对路径、Windows drive/backslash、重复冲突、symlink/hardlink逃逸与解压炸弹。
- 版本相同但asset摘要或平台不同，缓存不能误复用；nonce staging不构成真实激活。
- GitHub/npm元数据变动、缺digest、主版本迁移及许可缺失不能绕过门槛。
- restart只关闭闲置会话；存在活动工作时明确busy，不终止或重放任务。

### Task 1: 官方安装offer解析

**Files:** 新建 `server/internal/runtimeinstall/{contract,sources,github,npm,claude,platform}.go` 和对应fixture测试。

**Interfaces:**

- `Resolve(ctx context.Context, request ResolveRequest) (Offer,error)`；输入仅RuntimeID、Version/Channel和Platform，HTTPClient在构造时注入，调用者不能传下载URL。
- `Offer`包含RuntimeID/Version/Platform、Disposition(managed/external/unsupported/blocked)、Reason、SourceReference、Assets、LayoutID、Entrypoint、RequiredPaths、TermsURL、ResolvedAt。`Asset`包含固定URL、Size、Format、DigestAlgorithm、Digest、Destination及用途；摘要是原压缩字节的摘要。
- GitHub路线：Codex规范完整package、OpenCode 1精确CLI资产、Pi earendil新官方standalone、Kimi onedir。Claude native固定版本manifest/raw binary。Grok精确npm native子包SHA512。其余external。
- 新主版本必须经过当前adapter协议门槛，OpenCode不能因同命令自动跨到2；支持平台按核查矩阵，不做跨架构/libc猜测。

- [ ] **Step 1: RED解析fixture。** 六类来源精确asset、npm alias/SRI、缺hash、错误tag/version、无平台、GitHub429、Claude manifest mismatch、Grok未知许可、Pi新repo与旧scope分离。
- [ ] **Step 2: 运行 `go test ./internal/runtimeinstall -run '<新增用例>' -count=1` 记录失败。**
- [ ] **Step 3: 实现受控resolver。** 仅官方HTTPS白名单，受控发布CDN重定向，metadata有界；不访问token/keychain。许可附件固定到同版本可追溯来源，不能把main许可充当特定binary许可证明。
- [ ] **Step 4: gofmt及runtimeinstall定向测试。** 所有网络fixture用httptest/注入transport，不实际下载平台大包。
- [ ] **Step 5: 自审提交，记录支持矩阵与未验证布局。**

### Task 2: 校验、完整解包与原子版本目录

**Files:** 新建 `server/internal/runtimeinstall/{store,download,extract,verify}.go` 及对应测试；如Grok .br确需支持，添加受限Brotli依赖并记录许可。

**Interfaces:**

- `Store.Prepare(ctx, offer, progress) (Installation,error)`；Store构造时给私有Root、HTTPClient与`Probe(context.Context,string) error`。
- `Installation`冻结runtime/version/platform/root/entrypoint/asset摘要/安装时间，不含凭据；版本目录内写受管manifest，外部目录不被当作受管。
- `Store.List(ctx,runtimeID,platform)`只读枚举manifest、入口和必要文件仍有效的完整正式安装，排除staging；它不保存当前选择，也不自动激活未选中版本。
- 正式目录只在验证结束后同卷rename；staging可清理，部分版本不得出现在可用安装列表。progress只报告phase与有界计数字段，不透出signedURL。

- [ ] **Step 1: RED文件/网络边界。** 合法tar/zip完整树、SHA256/SHA512对象正确、checksum失败、cancel、过大、路径穿越/drive/backslash/冲突、内部链接合法与越界链接拒绝、probe失败保留旧目录、重复相同artifact幂等。
- [ ] **Step 2: 运行新增fixture记录失败。** 临时目录与假可执行probe，不操作用户真实安装。
- [ ] **Step 3: 实现下载和安全解包。** 先校验artifact后展开；文件成员写入前不得经archive创建的链接逃出staging，链接目标最终必须仍在根内。限制成员/展开大小，保留完整资源树与许可。Grok raw/.br分别处理，无decoder不执行外部脚本兜底。
- [ ] **Step 4: gofmt与本包回归。** 验证已安装manifest与真实入口一致、激活后源数据不可被未完成任务覆盖；rename后进程重启可枚举完整版本，staging和入口缺失版本不可见。
- [ ] **Step 5: 自审提交并记录各布局fixture证据，真实平台验收单列。**

### Task 3: RuntimeEnvironment操作与API

**Files:** `server/services/runtimeenvironment/{service,install}.go` 与测试；`server/repositories/models/runtime_environment.go`、`controllers/runtime_environment_controller.go` 选择语义与条件更新；`server/internal/agent/{agent,session,catalog}.go` 闲置Runtime关闭及安装枚举与测试；`server/apis/runtime_options` 新offer/install/cancel/restart/operation路由及安装DTO；`server/global/config.go`；相关枚举、事件与生成契约。

**Interfaces:**

- 服务方法 `InstallOffers(ctx,runtimeID)`、`StartInstall(ctx,runtimeID,offerID)`、`InstallationOperation(ctx,runtimeID)`、`CancelInstall(ctx,runtimeID,operationID)`、`Restart(ctx,runtimeID)`。operation由服务根Context拥有，不依赖页面连接；同runtime并发请求合并。
- offerId由服务端生成并绑定冻结offer和当前选择指纹；响应给出当前/目标选择、目标版本、disposition/reason、外部入口/可复制命令及服务端判定的maintenance能力。UI不得自行比较任意版本字符串决定可更新。提交offerId表示请求安装并激活该目标；显式路径替换范围在提交前可供界面确认。
- API：`GET /runtimes/:runtimeId/install-offers`、`POST /runtimes/:runtimeId/install`（offerId）、`GET /runtimes/:runtimeId/install-operation`、`POST /runtimes/:runtimeId/install-cancel`（operationId）、`POST /runtimes/:runtimeId/restart`。
- operation包含id/runtimeId/version/phase/receivedBytes/totalBytes/startedAt/finishedAt/errorCode；phase为preparing/downloading/verifying/installing/probing/activating/completed/failed/cancelled/interrupted。completed仅表示激活已成功提交；CAS冲突phase=failed、errorCode=selection_changed并保留preparedInstallation用于显示“已下载、未启用”。保存最近任务的小型安全状态，Core重启时未完成任务标interrupted，不能伪称完成。
- 激活对比开始时Binary/PrefixArgs/ConfigHome的冻结指纹，以条件更新拒绝覆盖用户新配置。显式路径需要明确由安装动作切换，不自动后台替换。通过现有RefreshOne回读版本与能力，认证缺失不等于binary下载失败。
- RuntimeEnvironment新增`BinarySelection=auto|explicit|managed`表明选择意图；与安装项的来源分开。旧空值迁移时默认命令视auto，已保存非默认路径保守视explicit；用户保存路径为explicit，安装激活为managed。auto分支先选有效受管安装再系统；explicit/managed尊重冻结路径，失效不偷偷回退。检测到的绝对路径不能再无条件改写为用户显式选择。
- RuntimeEnvironment把Store.List合入现有Catalog安装清单，复用版本/能力探测；安装项DTO增加实际来源。完整但未激活的版本可见，当前选择仍只来自RuntimeEnvironment。CAS失败后重启仍能找到已下载版本，不能因此覆盖新选择。
- 所有探测提交与激活使用同一选择指纹条件，包括BinarySelection；探测开始后配置变更，旧结果不得写回Binary、版本、能力或派生默认值，必要时重新刷新。网络/进程探测仍在数据库事务外。
- `Manager.CloseIdleRuntimeSessions(ctx,runtimeID)`按实际Runtime ID（非底层CLI家族）匹配同一owner的已冻结目标、资源目标与pending目标；冷准备增加与pendingAccount等价的pendingRuntime字段。匹配的活动turn或清理中owner返回busy；闲置清理失败保留owner重试，不维护第二份活跃会话表。

- [ ] **Step 1: RED生命周期。** 双请求同operation、用户改路径拒绝激活且重启后仍可枚举未激活版本、失败旧版可用、进程重启interrupted、取消收尾、install完成只做无Prompt探测。屏障用例让旧RefreshOne在新激活/路径保存后返回，断言旧版本/能力不能覆盖；auto/explicit/managed及legacy迁移优先级贯通API。restart覆盖idle匹配、不相关Runtime、active busy、A→B冷准备中B busy、清理失败同owner重试。
- [ ] **Step 2: 运行runtimeenvironment/controller/agent/API新增用例记录失败。**
- [ ] **Step 3: 实现服务、条件提交与事件。** 数据目录由应用装配提供；操作状态通过既有Runtime资源事件失效查询，禁另起WS/常驻前端轮询。
- [ ] **Step 4: 生成契约与定向验证。** gofmt；受影响测试；`pnpm api:generate`；更新桌面生命周期文档原“仅安装指引”描述为实际支持矩阵。
- [ ] **Step 5: 自审提交与阶段审查。** 页面按钮在UI计划接入，功能支持与实际平台通过分别记录。
