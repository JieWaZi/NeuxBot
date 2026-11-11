# 多 CLI 插件重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. 按以下任务实施，步骤使用 checkbox；用户已明确要求多子代理并行开发。

**Goal:** 用户安装飞书、企微、GitHub 或 Skill 插件后，十类当前 CLI 自动原生加载或交付 Skill/MCP，目录与详情不编进 Go。

**Architecture:** `internal/plugins` 统一解析、生成无秘密目标与打包工件；安装准备和 Execution 消费相同产物，ProviderRuntime 保护资源。目录索引独立发布，详情按需下载，启动保留 ZIP 预取。插件仓库调用同一打包工具输出独立 CLI 工件。

**Tech Stack:** Go、现有 ACP 客户端、Node.js 插件发布脚本、现有文件内容库与租约。

**Spec:** [多 CLI 插件生成、分发与加载设计](../specs/2026-11-09-multi-cli-plugin-delivery-design.md)

## Global Constraints

- 自有 schema 全部为 `1`，`neuxbot-plugin/1`，不做历史兼容、版本升级或迁移。
- 保留当前工作树无关修改。代理按文件所有权并行，不能重置或提交其他代理的文件。
- 原包不可变，派生产物另存；生成不执行包代码、不启动模型，不写入秘密。
- 覆盖 codex、claude、cursor、workbuddy、qoder、kimi、gemini、grok、opencode、pi；原生不可可靠加载则自动 Skill/MCP，不因两端白名单拒绝插件。
- 保持账号、权限、安装版本和租约的唯一所有者；原生与回退不重复加载。
- 不打开、控制或截图运行中的应用；真实安装和业务调用由用户验收。
- 仅定向测试、包检查、契约与文档检查，不运行全量验证。
- 不推送共享分支或发布远端制品；先交付可审阅本地结果及显式开发源。

## Review Focus

- GitHub 远端 MCP 需要真实私密头绑定，不能因原生模板缺授权而匿名连接。
- 三个插件、多伙伴和固定 cwd 并发不能重复工具或覆盖活动技能目录。
- Grok 根清单优先级、各端变量及文件名必须在实际读取根正确，而不只生成隐藏目录。
- 首次无缓存、详情失败和慢 ZIP 不能阻塞轻索引发布或伪造空库。
- 自维护仓库和 SkillBundle 的目标生成、授权约束及独立 CLI 工件必须一致。

## Task 1: 统一插件目标生成与打包工具

**Files:** `server/internal/plugins/` 下的 package、integration、authentication_binding、native_target、native_codex（仅 NativeTarget 描述）、skill_bundle 及对应测试；新增有职责的目标缓存文件和 `server/cmd/pluginpack/`。不编辑 `catalog/`、`services/execution`、`internal/agent`、安装 API。

**Interfaces:** 继续提供 `PrepareNativeTarget(parent, id string, pkg Package, root, cli string, authentication ...AuthenticationBinding) (NativeTarget, error)` 及 WithDigest 变体；扩展 NativeTarget 以声明 Runtime、格式与实际 Skill/MCP 组件。增加 `PrepareTargets(parent, id string, pkg Package, root string) ([]NativeTarget, error)`，按确定顺序覆盖十类 CLI，成功结果无秘密且校验完整。纯 Skill/MCP 不必伪造可执行 JS。既有入口先校验再复用，包内原生额外能力不静默执行。授权声明允许省略 runtime 表达组件共用，显式 runtime 仍须属于当前登记目标；不再强制两端。

- [x] 测试先证明：十目标生成、原包摘要不变、确定性／缓存损坏重建、原生目录可共存、Grok 根清单与 MCP 路径、同组件授权所有 CLI 可取、非法端点／环境仍拒绝、SkillBundle 不继承来源执行能力。
- [x] 运行对应测试并记录缺失行为的 RED；实现并 gofmt，再取得 GREEN。
- [x] 打包命令接收 source、output、profile（支持 all），输出自包含目标目录、ZIP 和 schemaVersion 1 的无秘密工件描述；错误退出非零，不启动 Core。
- [x] 向 Task 2/3 明确 NativeTarget 新字段与打包参数，保持生产接口一致。
- [x] 定向验证 `internal/plugins` 和 `cmd/pluginpack`；报告修改文件、RED/GREEN 及未验收边界。保持本地改动供统一审阅，不提交混合工作树。

## Task 2: 原生运行交付、回退与原生适配

**Files:** `server/services/execution/plugins.go`、`native_turn.go` 及对应测试；`server/internal/agent/provider_runtime.go`、`agent_client.go` 及对应测试；`server/services/plugininstallation/connector_authentication.go` 及对应测试。原生适配工作在独立 acp-go 检出中，保留原库分支。

**Interfaces:** 消费 Task 1 的 NativeTarget；给 ResourceLease 增加有类型的通用原生插件目标／目录，替换 Claude 专用边界。preparedPlugins 携带实际原生目标、回退技能／MCP及来源快照；冻结 key 包含路线和目标身份。保持 ResourceSelection/AcquireResources/CloseFiles 所有权。当前适配器未实现可靠入口的 CLI 使用相同组件的通用投射，不报泛化 unsupported。

- [x] 测试先证明：真实 typed 插件目录传给原生 Client、多个插件／伙伴隔离、授权头和 Skill 环境跨端一致、原生组件不重复 FreezeSkills/MCP、权限限制时回退、失败保留租约、资源变化下一轮替换。
- [x] 记录 RED，按能力实现，定向 GREEN；不能将参数塞进受保护 PrefixArgs。
- [x] 为 Cursor、WorkBuddy、Qoder、Grok 等有可靠目录入口的当前原生客户端补 typed 接线；Codex 原生配置必须在账号租约边界内，不用资源环境覆盖账号根。Kimi/Gemini/Pi/OpenCode 以当前可证实能力选择原生或通用路线。
- [x] acp-go 修改需英文注释与定向协议测试；主仓库以显式本地 Go workspace 验证，报告尚未远端发布的依赖，不伪造已发布版本。
- [x] 保持业务插件账号共用，不引入按 CLI 复制账号；运行准备不把明文秘密写到伙伴目录。
- [x] 报告精确测试、修改文件和实际仍走回退的 CLI，供最终审阅与用户验收。

## Task 3: 独立插件仓库与原生交付工件

**Files:** 独立 `neux-plugins` 仓库的 `plugins/{feishu-office,wecom-office,github}` 中清单／授权入口，`schemas/`、`scripts/build-catalog.mjs`、相关测试／构建脚本和开发说明。不编辑主仓库生产代码。

**Interfaces:** 所有 schemaVersion 维持 1；组件级共享授权不要求列十份 runtime。发布脚本调用 Task 1 的同一个 Go 打包工具，显式 `NEUX_PLUGIN_PACKAGER` 用于本地构建，正式工具版本／摘要具有唯一配置；不另写 manifest 转换器。输出 portable 原包及各 CLI 自包含目标工件，索引只含关键信息和固定下载绑定，详情独立生成。

- [x] 从可信 main 独立检出建立 codex 分支，先核对仓库约定和现有测试。
- [x] 测试先证明：组件声明被所有当前目标消费、三包原生入口／独立 setup 使用同一授权实现、目标包自包含、目录无完整 README／Skill 内容、生成工件来源绑定。
- [x] 记录 RED/GREEN，处理飞书／企微的业务 CLI launcher 和 GitHub 精确 MCP 授权；独立 CLI 不依赖运行中的 Core，不复制宿主凭据。
- [x] 为无可执行扩展需求的 Skill/MCP 包只声明真实能力，不能造空 JS 代码证明原生支持。
- [x] 接入共享打包工具后构建真实三个插件和目录、详情、目标 ZIP，报告稳定输出位置，供 Task 4 显式开发源验收。
- [x] 不推送或发布；保持本地分支／补丁可审阅，报告定向 Node 测试与构建证据。

## Task 4: 分发去内嵌、安装接线与集成

**Files:** `server/internal/plugins/catalog/`、`server/apis/plugins/{prepare_catalog_plugin,create_plugin_from_skills,install_preparation,install_plugin}.go` 及直接相关测试；`scripts/plugin-catalog-snapshot*`、相关构建入口；必要的权威文档。由主代理负责，避免覆盖 Task 1/2 的文件。

**Interfaces:** 保持轻索引／公开资料 schemaVersion 1，删除具体插件数据 Go embed 和兜底；ReadDetails 及其调用者传 Context；索引 flight 与归档后台任务拆分完成边界，Core 退出统一等待。入库与安装调用 Task 1 生成能力，事务只提交固定事实。显式开发源使用 Task 3 的真实工件。

- [x] 写失败行为测试：详情 404 不阻塞列表、慢 ZIP 不阻塞 Refresh、无缓存离线如实失败、已有缓存保留、取消不发布半包；记录 RED。
- [x] 删除 bundled 数据／Go 内嵌和快照脚本；解耦详情和预取，定向 GREEN。
- [x] 安装与 SkillBundle 准备各端目标；安装提交固定关系，实际 CLI／账号在执行接纳后自动构建租约视图，保留批量部分成功及重复安装启停规则。
- [x] 对三包真实开发工件做目录、详情、生成、准备和安装资源测试，不登录模型或打开运行应用。
- [x] 更新架构／构建说明，只写已实现行为；运行相关包测试、gofmt、契约和 docs:check。

## Task 5: 独立审阅与交付

**Files:** 上述任务实际差异、测试报告、生成契约及必要修复。

- [x] 每项任务由独立审阅者检查规格与质量；整个集成完成后审阅跨仓库、并发、权限、数据一致性和构建可复现性。
- [x] 处理所有重要问题并补对应定向测试；不重复运行无变更的全量检查。
- [x] 汇总当前应用与插件仓库位置、开发源启动方式、十类 CLI 实际原生／回退路径，以及用户安装飞书／企微／GitHub 的验收步骤。
- [x] 分别说明本地修改、提交、推送、发布与真实验收状态；不得将自动化通过宣称为用户 CLI 调用已通过。
