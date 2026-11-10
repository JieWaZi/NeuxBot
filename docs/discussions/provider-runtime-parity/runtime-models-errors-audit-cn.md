# Runtime 安装、模型能力与结构化错误审计

本文是调研和接口建议，尚未实施。基线：NeuxBot `e18a0d6e377eda9cdc96954321fb1f0788691a5f`、acp-go `a58342da0ddd1e3ec5d022a0f911b82eba02107d`。只读检查源码与已有测试，未启动真实 CLI、模型、安装器或 UI；测试存在不代表本次通过。

## 1. 结论和职责

保留已有 AI Account、Catalog、Manager、Session、Execution 所有权，补全原生协议和能力链。模型与工具循环留给原生 CLI，产品执行生命周期由 NeuxBot 管理。

| 能力        | 已有事实                                                                     | 差距                                                                              |
| ----------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| CLI发现     | Catalog解析程序、枚举路径及版本；RuntimeEnvironment保存探测事实，API可选路径 | 没有受管下载安装、校验、激活、取消和移除接口                                      |
| 动态模型    | Management创建无Prompt临时会话，优先逐模型metadata，否则切模型读setter回执   | 投影只剩model/reasoning/permission，描述、默认、known/empty/unknown及其他配置丢失 |
| Claude Fast | acp-go已有fast目录与setter                                                   | NeuxBot没有选择、保存、配置及生效事实链                                           |
| Codex档位   | 生成协议已有serviceTiers/defaultServiceTier/serviceTier                      | acp-go非生成adapter未提供目录或透传                                               |
| 错误        | Codex/Claude/Kimi已有errorKind；NeuxBot结构优先分类                          | Pi主要仍字符串；billing/quota合并；-32002缺少资源身份判断                         |
| 重试        | Execution已有rate/upstream白名单、无观测副作用、可靠session门禁              | 补submission/effect证据，不新建重试调度器                                         |

[当前Agent所有权](../../../server/internal/agent/README.md)和[工程边界](../../development/backend-cn.md#12-agent-执行边界)继续适用：acp-go拥有通用原生协议、进程、配置回执和厂商错误解码；native保留技术事实；根Agent一次映射产品失败；Catalog/RuntimeEnvironment拥有安装发现与探测；AI Account拥有认证；Execution拥有接纳、持久化及重试；UI消费权威事实，不再保存第二套provider状态。

## 2. 已有链路与精确证据

### 2.1 安装和发现

- `server/internal/agent/catalog.go:840-947`：DiscoverInstallations保留显式及历史路径，再扫官方用户路径及PATH；版本查询总12秒、并发4、单路径2秒。可执行、能读版本、协议可用、认证有效是不同事实。
- `catalog.go:1124-1287`：Detect复用definition/environment/native.Management；已找到CLI但鉴权缺失时仍可ready，并单列auth missing。
- `server/services/runtimeenvironment/service.go:356-410,416-514`：原RuntimeEnvironment持久化探测、默认选择及历史安装；失败可保留上次模型目录。
- `server/apis/runtime_options/list_runtimes.go:49-126,155-212`：已有installed/resolvedBinary/installations/state/auth/defaultModel/defaultReasoning/diagnostics/configHome。installed取resolvedBinary，不等于认证或协议健康。
- `server/apis/runtime_options/router.go:7-13`：现有路由只有list、refresh all/one、update environment；`update_runtime_environment.go:77-109`保存binary/prefixArgs/configHome/defaultAccount后按差异刷新。

新下载必须接回这些事实。下载进度由一次安装操作拥有，激活路径与版本仍写原RuntimeEnvironment；后台下载不能覆盖用户较新的路径选择。

### 2.2 模型与配置

acp-go [pkg/acpmeta/model.go:9-41](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/acpmeta/model.go#L9)已有`acp-go/model-config-options`，返回`(options, known)`区分未知与确认无参数。NeuxBot `native/management.go:127-195`优先读metadata，再用当前会话或setter取得逐模型配置。`catalog.go:1760-1833`明确不把当前模型强度套给未知模型。

但`server/internal/agent/config.go:73-90`的ModelOption只有ID/Name/ReasoningOptions/PermissionLevels，ReasoningOption只有ID/Name；`catalog.go:1813-1825`丢掉额外配置。`native/contract.go:16-23`的Profile只有Model/Reasoning/Thinking/NativeMode/Subject；`native/connection.go:595-695`只配置mode/model/thought_level。补全应沿现有ProbeResult→DetectionResult→RuntimeOption→SessionPlan/Profile→Configuration/InvocationFacts，不另建ModelCatalog缓存。

| Runtime                         | acp-go已有模型能力                                                                                                  | 缺口与归属                                                |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| Codex                           | 原生model/list及逐模型supportedReasoningEfforts；只认mode/model/reasoning_effort                                    | service tier由acp-go补，不在宿主造fast模型ID              |
| Claude                          | 原生models、supportsEffort/supportedEffortLevels/supportsFastMode；fast select/boolean；精确匹配value/resolvedModel | 先贯通NeuxBot，不重写已有adapter                          |
| Pi                              | get_available_models/get_available_thinking_levels；set_model透传provider/modelId；重复裸ID消歧                     | 不制造low/medium/high，不把权限mode当thinking             |
| Grok                            | 旧逐模型metadata投影标准config；当前模型以真实configOptions为准；仅固定旧版有fallback                               | 保留source ID/value/label/default，不能用label作setter值  |
| Kimi                            | configOptions/旧models兼容、专属能力和错误处理                                                                      | 按握手和setter，不按名称猜模型能力                        |
| Gemini                          | NeuxBot的gemini CLI及pkg/gemini                                                                                     | 与 Antigravity 不同，安装/模型/权限不能直接替换           |
| Cursor/OpenCode/Qoder/WorkBuddy | 已有descriptor与adapter；部分允许空模型目录，WorkBuddy过滤别名及思考开关                                            | 保留本项目实际provider集合，不自动增加未经确认的 provider |

acp-go证据：[codex/config.go:170-195,272-311](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/codex/config.go#L170)、[claude/config.go:62-145,177-240,311-374](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/claude/config.go#L62)、[pi/configuration.go:13-113,126-183](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/pi/configuration.go#L13)、[grok/catalog.go](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/grok/catalog.go)。NeuxBot factory/配置ID见`native/registry.go:233-450`。

Codex [generated_protocol.go:753-784](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/codex/protocol/generated_protocol.go#L753)已有additionalSpeedTiers/defaultServiceTier/serviceTiers，[ThreadStartParams:1202-1217](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/codex/protocol/generated_protocol.go#L1202)已有serviceTier；生产ServiceTier搜索仅命中生成协议，非生成adapter没使用。生成字段不是功能已接通的证据。

Fast 按原生服务档位处理：读实际tier，区分未设置、关闭/null、开启/真实tier。参考行为，独立按协议实现。

## 3. 已证实问题

| 编号 | 触发与实际结果                                                            | 证据                                                                                           | 修正范围                                                      |
| ---- | ------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| M1   | A支持high、B确认无配置；选B时因强度长度0而回退runtime的A强度              | `config.go:425-445`；设置页`runtime-environments-section.tsx:163-171`；`catalog.go:1813-1818`  | 保留known/empty/unknown，已知空绝不继承其他模型               |
| M2   | Claude有fast setter，宿主不能表达请求或保存effective fast                 | acp-go`claude/config.go:224-240,362-374`；Neux`native/contract.go:16-23,connection.go:689-694` | 补模型配置全链，复用adapter                                   |
| M3   | Codex生成了服务档位，但adapter无选择和透传                                | acp-go`codex/config.go:170-195`及生成协议                                                      | 先补acp-go，再贯通宿主                                        |
| E1   | billing_error与quota_exhausted均变QUOTA_EXHAUSTED，余额和周期额度丢失区别 | `diagnostic.go:344-347`；文本`205-213`同样合并                                                 | 准确业务失败码由constants生成；没证据不造resetAt              |
| E2   | 任意ACP -32002都判SESSION_NOT_FOUND，不核resource/session ID              | `diagnostic.go:296-301`                                                                        | 只匹配确证session缺失；其他资源不能退休会话                   |
| E3   | deliveryAgent.Prompt在调用adapter.Prompt之前已emit PromptAccepted         | `native/connection.go:275-290`；`contract.go:244`                                              | 拆dispatch/accepted/unknown；进入进程内adapter不证明CLI已接收 |

M1的纯输入反例：Models=[A(high),B([])]、runtime ReasoningOptions=[high]、选择B/high，现有函数仍返回B/high。不能只换一个blanket fallback；需要保留三态。E2已证实分类过宽，本次未断言生产已经错误删除会话；E3已证实证据强度不足，本次未断言已经发生重复执行。

## 4. 结构错误与未知副作用

Codex [request_error.go:37-105](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/codex/request_error.go#L37)按官方enum/HTTP status映射authentication/quota/billing/rate/timeout/network/server/overloaded/context/invalid_request，未知保留；Claude [events.go:608-671](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/claude/events.go#L608)保留is_error与assistant error；Kimi [outcome.go:93-149](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/kimi/outcome.go#L93)按wire code/status分类并保留kimiErrorCode，当前402没有billing分支；Pi [events.go:144-152](https://github.com/JieWaZi/acp-go/blob/a58342da0ddd1e3ec5d022a0f911b82eba02107d/pkg/pi/events.go#L144)仍将errorMessage包普通Go error。

Neux `diagnostic.go:255-358`结构优先再文本fallback；`session.go:1171-1199`构建Failure；`agent.go:467-521`为status_error转换边界。已有`execution/retry.go:20-31`只放行RATE_LIMITED/UPSTREAM_UNAVAILABLE、无观测副作用/未交付载荷、有可靠session；timeout/network不放行。保留该机制，不添加RetryService或ProviderHealthStore。

| 技术事实           | 建议合同                                                 | 唯一归属                       |
| ------------------ | -------------------------------------------------------- | ------------------------------ |
| 类别               | 现有开放errorKind，unknown不猜                           | acp-go依据官方协议             |
| 阶段               | spawn/initialize/session/configure/dispatch/turn/cleanup | native技术Fault                |
| 提交状态           | not_dispatched/rejected/accepted/unknown，有证据才提升   | acp-go生产，native原样归并     |
| 缺失资源           | resource/resourceId，必须与请求session匹配               | 标准RequestError.Data          |
| retryAfter/resetAt | 只保留真实上游值                                         | adapter读取，Execution决定动作 |
| 副作用             | 没观察到不等于已证明未执行；保留text/tool/产出证据       | 唯一Session/Turn               |

沿已有acpmeta命名空间发布技术扩展，native Fault增加具名字段；acp-go不导入产品status_error。根Agent一次映射正确失败码，Execution消费同一Failure。受控厂商文本fallback可用于解释，不能凭任意错误文案授权session替换或重放。

## 5. 下载安装矩阵

本表是源码可见分发形态及实施候选，不声明最新artifact已经下载或全平台验证。Neux现有11类CLI保持；其他 provider 不自动加入。

| Runtime           | 已见分发依据                                    | 首期建议                                       | 验收条件                                         |
| ----------------- | ----------------------------------------------- | ---------------------------------------------- | ------------------------------------------------ |
| Codex             | 官方release完整bundle，含code-mode-host/rg      | 固定完整archive                                | 官方digest、布局、协议版本；不能只复制主binary   |
| Claude            | 官方platform npm包内native executable/license   | 固定platform npm archive，不global npm install | 包名/版本、CLI与SDK版本、binary hash/license     |
| Pi                | 官方GitHub平台archive及asset digest             | 固定archive，后续受控latest                    | 全目录、平台digest；无官方digest不自动升级       |
| OpenCode          | platform npm native包和外层license包            | 固定platform npm artifact                      | 必要sidecar/license，禁postinstall脚本执行       |
| Kimi              | 原生/PyInstaller onedir archive                 | 固定完整目录                                   | 每平台布局/依赖；保留现有factory                 |
| Grok              | 固定版本需提供可验证摘要，latest 的摘要可能缺失 | 仅受审计固定hash版                             | 不照搬TLS-only自动latest                         |
| Cursor            | 多文件platform archive/runtime依赖              | 固定完整archive                                | cursor-agent/agent命令、依赖和平台可用性分别验收 |
| Gemini CLI        | 当前是gemini，非Antigravity                     | 外部优先；若npm则应用私有prefix和固定Node      | 独立审计官方Gemini包；不能下载Antigravity替换    |
| WorkBuddy         | 仅证实本地程序及adapter                         | 外部安装+显式路径                              | 未审计digest、许可和再分发布局，不启用managed    |
| Qoder/Qoder CN    | 各自官方用户目录候选，不扫桌面entry             | 外部安装+已有路径发现                          | 两者config home/品牌分开；未审计包不托管         |
| Profile/自定义CLI | 用户程序及args/env                              | 只管理外部路径                                 | 不成为任意URL下载执行入口                        |

Neux安装候选与factory见`native/registry.go:233-450`。

安装技术职责归Catalog，fetch/filesystem/archive作为注入I/O，RuntimeEnvironment继续唯一保存配置和激活事实。acp-go不拥有宿主安装目录、账号DB、UI进度或自动升级政策。

建议具体接口：

1. Catalog列举InstallOffer：runtimeId/version/os/arch/source/distribution/digest/managedSupported/外部安装指引；来源由构建配置确定，不接任意URL。
2. PrepareInstall/Install拥有一次operationId及download/verify/stage/probe/activate阶段；失败/取消保留旧版。只有确有跨重启恢复需求时才持久operation，不复制RuntimeEnvironment。
3. 下载→digest→拒绝路径逃逸/危险链接→staging→无Prompt版本/initialize验证→同卷原子提交→既有Manager idle门禁切换原binary。busy时等待或明确拒绝，不强关活动Session。
4. RemoveManaged只移除本应用有来源证明且未使用的managed目录；不删除外部安装、config home、账号或会话历史。
5. `/runtimes`下新增offers/install/cancel/managed-remove Operator，进度走既有事件/查询端口；不添加第二条provider websocket。

安装将 stage、verify、activate 分开，并检查活动执行门禁。独立按本项目根生命周期设计，不能照搬其跨实例状态。

## 6. 模型配置接口建议

扩展已有ModelOption，不新建第二份目录：模型级ConfigurationKnown和有序ConfigOptions，保留明确空、choice ID/value/name/description/default。仅支持安全boolean/select，未知扩展不假装支持。

ConfigSelection用option ID及boolean/string联合值；权限、sandbox、credential、auth不进入通用参数。Model/Reasoning继续已有字段，禁止双重来源。Claude fast复用已有fast；Codex serviceTier先由acp-go补目录和原生参数，均不能用换模型ID替代。

SessionPlan保存desired并进入既有兼容摘要；native先选model，再按最新目录串行设置并验证最终currentValue，防后续setter回退前项。Configuration/InvocationFacts保存effective。明确区分未设置、关闭、开启；移除choice保留desired供修正，不静默换第一项后继续计费。

UI读RuntimeOption与当前会话事实；设置、聊天、团队、工作流和自动任务共用解析。账号目录仍由原账号事实限定，无账号catalog能力不等于已获授权。

## 7. 验收矩阵

以下待实施形成证据，本报告没有运行这些验收。

| 领域         | 场景                                  | 必须断言                                              | 最窄归属               |
| ------------ | ------------------------------------- | ----------------------------------------------------- | ---------------------- |
| 目录三态     | A有high/B明确空/C未知                 | B不继承A，C不伪造菜单，序列化不丢三态                 | acpmeta+Catalog/config |
| 刷新         | 旧成功/本次失败/换账号CLI             | 保留旧目录标stale，不假装新账号认证成功               | runtimeenvironment     |
| Claude fast  | 支持/不支持模型，开/关/未设           | 原生准确回执后effective；换模型不保留非法fast         | acp-go假CLI+native     |
| Codex tier   | 新serviceTiers/旧speed tiers          | model ID不变、tier原值、false清除、不支持不发         | acp-go假app-server     |
| 强度         | ID/value/label不同、逐模型差异        | setter用真实值、展示用名称、默认不猜                  | adapter+Catalog        |
| 设置         | grouped select/boolean/伪装权限秘密   | 精确回执，安全过滤，未知值拒绝                        | native                 |
| 配置失败     | setter超时/迟到生效/回退model         | desired/effective准确、不并发交错、不假成功           | Session                |
| 发现         | 多版本/失效路径/version超时           | installed与usable分开、用户路径不被覆盖               | Catalog                |
| 安装拒绝     | digest/包名错、archive逃逸、缺sidecar | 不执行artifact、不激活、旧版保持                      | 注入I/O                |
| 安装中断     | 取消/退出/空间不足/并发/文件占用      | operation收敛、无半激活、不动账号外部安装             | 安装边界               |
| 激活         | busy/idle/用户同时改路径              | 门禁串行、不强杀/重放、不覆盖新选择                   | Manager+环境           |
| 错误         | 401/402/429/5xx/timeout/context       | auth/billing/quota/rate/upstream分别准确，unknown保留 | adapter+Agent          |
| missing      | 匹配session/别的session/file -32002   | 仅匹配session可missing，不错退休                      | adapter/native         |
| 提交未知     | write后断线、迟到accepted/terminal    | unknown不重发，同所有者处理迟到清理                   | adapter+Session        |
| 重试         | 纯拒绝/已有text/tool/network unknown  | 仅合格rate/upstream沿既有入口                         | Execution              |
| 脱敏         | Data/stderr/log/UI含token/URL query   | 不泄露、结构分类仍可用                                | 现有脱敏边界           |
| 真实平台模型 | 授权后连续两轮/取消/重启恢复          | native身份配置、真实执行、静态检查不能替代            | 显式真实验收           |

## 8. 许可和未验证范围

各 CLI artifact再分发仍逐项确认许可和Required Notice，第三方记录归既有THIRD_PARTY_NOTICES。

未验证真实账号、最新厂商分发站点、各平台artifact完整性、设置费用/可用性、取消后工具停止、安装恢复及性能。“建议管理”只是源码分发证据支持的候选，不是已完成的安全或法律结论。没有产品代码修改，没有提交。
