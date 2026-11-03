# CLI 官方安装源核查与受管安装边界

本文是来源调研与实现建议，未实现安装器。访问日期统一为 **2026-11-03（Asia/Shanghai）**。只读取官方文档、npm registry 元数据、GitHub 发布元数据和少量发布源码；读取了 Claude、Grok、OpenCode 的小型顶层 npm 包以核查安装脚本，未下载平台大包、执行脚本、启动 CLI、使用账号或控制浏览器。已有审计基线见 [Runtime 安装、模型能力与结构化错误审计](runtime-models-errors-audit-cn.md)。OpenBot 仅用于定位候选来源，不复制其实现、固定版本或校验值。

## 1. 结论与支持矩阵

**Codex、Claude、OpenCode、Pi、Kimi 有可实施的官方强摘要来源。Grok 有官方 npm SHA-512 来源，但必须处理原始／Brotli 二进制格式；Cursor 暂时只提供 external 安装入口。** 下面的“候选”指来源和平台资产成立，不表示本项目已完成安装、协议或真实平台验收。旧版本和未来版本必须逐次解析，不能沿用本表摘要。

| Runtime | 本次观察到的官方版本 | macOS amd64/arm64 | Linux amd64/arm64 | Windows amd64/arm64 | Go 受管安装候选来源与限制 |
| --- | --- | --- | --- | --- | --- |
| Codex | `rust-v0.166.0` / npm `0.166.0` | 均有 | 均有 musl target | 均有 | GitHub `codex-package-<target>.tar.gz` + asset `digest`；必须保留规范包完整目录 |
| Claude | latest `2.1.317`，stable `2.1.311` | 均有 | glibc、musl 均有 | 均有 | 官方版本 manifest + SHA-256；可进一步验证官方 GPG 签名；npm 原生子包是备用源 |
| OpenCode 1 | `v1.19.5` / npm `1.19.5` | 均有，x64 有 baseline | glibc、musl 均有，x64 有 baseline | 均有，x64 有 baseline | GitHub 精确 CLI asset + SHA-256；不自动迁移 OpenCode 2 |
| Pi | `earendil-works/pi` `v1.5.0` | 均有 | 均有；此处未核定 musl 兼容 | 均有 | GitHub 完整 standalone archive + SHA-256；旧 `@mariozechner` 不是当前发布线 |
| Kimi | `1.52.0` | 均有 | 均有 GNU/glibc target；没有观察到 musl asset | 均有 | GitHub `-onedir` archive + SHA-256；整个 PyInstaller 目录保留 |
| Grok | stable / npm `1.0.70` | 均有原生子包 | 均有原生子包；未核定 musl | 均有原生子包；README 仍只列 Windows x64 | npm 精确平台子包 + SHA-512；核验实际成员与解码能力后才可安装 |
| Cursor | 脚本 `2026.10.22-27dcf5f` | 官方脚本均映射 | 官方脚本均映射；未核定 musl | 官方 PowerShell 脚本均映射 | 本次未发现可信公开强摘要；六个资产 HEAD 均 403，保持 external |
| Gemini / WorkBuddy / Qoder | 本次未核查 | external | external | external | 不因存在 npm 同名包或 OpenBot 相似 runtime 就补成受管安装 |

平台须包含 OS、真实进程架构／原生架构、Linux libc，OpenCode 还要考虑 CPU baseline。`runtime.GOOS=windows` 不能拼成所有厂商的相同字符串：Claude/Grok npm 使用 `win32`，OpenCode asset 使用 `windows`，Codex/Kimi 使用 Rust triple。不存在目标资产时返回 unsupported，不用 amd64、glibc 或其他 provider 的包静默替代。

## 2. 各来源的事实与安装布局

### 2.1 Codex

- [OpenAI CLI 官方说明](https://learn.chatgpt.com/docs/codex/cli)确认独立安装方式；[官方仓库 README](https://github.com/openai/codex/blob/rust-v0.166.0/README.md)列出 GitHub Release 路线。[latest API](https://api.github.com/repos/openai/codex/releases/latest)当前返回 `rust-v0.166.0`，应检查 `draft=false`、`prerelease=false` 和严格 tag 格式，再按精确 asset 名取 URL、size、digest。
- 推荐规范包 `codex-package-<target>.tar.gz`，目标为 `aarch64/x86_64-apple-darwin`、`aarch64/x86_64-unknown-linux-musl`、`aarch64/x86_64-pc-windows-msvc`。本次六平台规范包都有 `sha256:<64 hex>`。例如 [macOS arm64 规范包元数据所在 Release](https://github.com/openai/codex/releases/tag/rust-v0.166.0)对应 SHA-256 `55a440cb579f970abe7c79ab181e54b0ea4c419be99f333bbd868eebac9c21f0`。Windows 规范包同样提供 `.tar.gz`，不必因为 OS 是 Windows 就猜 `.zip`。
- [规范包布局源码](https://github.com/openai/codex/blob/rust-v0.166.0/scripts/codex_package/layout.py)明确创建 `bin/`、`codex-resources/`、`codex-path/`、`codex-package.json`，包括主程序、`codex-code-mode-host`、`codex-exec-server` 和 ripgrep；Linux 有 `bwrap`，Windows 有 command runner、sandbox setup。应验证 metadata 的版本、target 和受限相对入口路径，保留整个包，包括未来同版本新增资源。不能仅复制 `codex`，也不要混合 `-bazel` 和普通规范包的成员。
- [发布封装脚本](https://github.com/openai/codex/blob/rust-v0.166.0/.github/scripts/build-codex-package-archive.sh)也可能装入 voice 等资源，完整树优于本地手列几个文件。这里只以源码确定布局要求，未验证实际平台 archive 内容和启动行为。
- npm [`@openai/codex/0.166.0`](https://registry.npmjs.org/@openai/codex/0.166.0)顶层是 JS launcher、无 scripts；平台依赖使用 **npm alias**，如 `@openai/codex-darwin-arm64` 指向实际包 `@openai/codex@0.166.0-darwin-arm64`。已核查该[原生版本元数据](https://registry.npmjs.org/@openai/codex/0.166.0-darwin-arm64)：无 scripts、无 dependencies、有 SHA-512。不能把 alias 名直接当 registry 中的包名拼接，更不能只装顶层 3 文件包。首期用 GitHub 规范包可减少 npm 模块解析与布局重建。
- 许可为 [Apache-2.0 LICENSE](https://github.com/openai/codex/blob/rust-v0.166.0/LICENSE)，同时保留 [NOTICE](https://github.com/openai/codex/blob/rust-v0.166.0/NOTICE)及包内第三方声明。若 archive 缺许可，按同一 tag 解析到的 commit 取得小型许可文件并记录来源及本地摘要。

### 2.2 Claude Code

- [官方安装说明](https://code.claude.com/docs/en/setup)及本次读取的[POSIX 脚本](https://claude.ai/install.sh)、[PowerShell 脚本](https://claude.ai/install.ps1)共同指向当前 `https://downloads.claude.ai/claude-code-releases`。不要依赖旧 GCS bucket UUID。
- [`latest`](https://downloads.claude.ai/claude-code-releases/latest)为 `2.1.317`，[`stable`](https://downloads.claude.ai/claude-code-releases/stable)为 `2.1.311`；channel 是显式选择，不把 npm latest 冒充 native stable。读取 `/<version>/manifest.json`，要求 `manifest.version` 精确匹配，平台对象含 `binary`、`checksum`、`size`；URL 固定为该版本和平台下的 `claude` 或 `claude.exe`。本次 [2.1.317 manifest](https://downloads.claude.ai/claude-code-releases/2.1.317/manifest.json)覆盖全部 8 个平台／libc 组合；darwin-arm64 原始 binary SHA-256 为 `09f153b46b5df9e8b3454716d682a91901de020cbca630f61dec1a015b32b5e9`。
- [官方签名说明](https://code.claude.com/docs/en/setup#verify-the-manifest-signature)提供 [`manifest.json.sig`](https://downloads.claude.ai/claude-code-releases/2.1.317/manifest.json.sig)与[公钥](https://downloads.claude.ai/keys/claude-code.asc)，本次均成功读取；文档指定 fingerprint `31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`。本次未执行 GPG 验签。实现中若声明 publisher-signed，必须完成固定信任根的真正验签；只有 SHA-256 时只声明 official-TLS digest。
- 优先使用原始 binary；不为节省下载量引入 `.zst` 解码与另一套 manifest。官方安装脚本最后还会执行 binary 的 `install` 命令，所以“脚本这样安装”本身不证明省略 install 后所有运行行为已验收。将原始 binary 放到版本隔离目录后，需完成版本和无 Prompt 协议探测再激活。
- [`@anthropic-ai/claude-code/2.1.317`](https://registry.npmjs.org/@anthropic-ai/claude-code/2.1.317)顶层有 `postinstall: node install.cjs`。本次读到其 `bin/claude.exe` 占位符和脚本：安装脚本从平台子包 `bin/claude(.exe)` 复制 native binary。顶层忽略 scripts 后不能直接作为受管 CLI。
- 本次逐一读取 darwin/linux/win32 的 x64/arm64 共 6 个及两个 Linux musl [原生子包元数据示例](https://registry.npmjs.org/@anthropic-ai/claude-code-darwin-arm64/2.1.317)：全部有 SHA-512、无 scripts、无 dependencies。宿主可在校验 tar 后读取精确 native 成员，无需执行 postinstall 或 npm；这是可实施路径，实际成员与目标平台启动仍需后续验收。
- 顶层包 `LICENSE.md` 为 Anthropic 保留所有权利并链接[Legal and compliance](https://code.claude.com/docs/en/legal-and-compliance)。它不是 MIT/Apache。官方该页允许相应条件下在产品中运行未修改 binary，并要求适用条款、用户自己的认证与直接计费；应原样保留许可入口和所有认证能力，不把“存在下载 URL”写成无限制再分发授权。

### 2.3 OpenCode

- [官方文档](https://opencode.ai/docs/)明确区分 OpenCode 1 与 2；两者都使用 `opencode` 命令，默认安装可能互相覆盖，包管理器渠道也可能跨主版本。本项目现有 adapter 不能仅因名称相同就自动迁移 V2。
- [anomalyco/opencode latest API](https://api.github.com/repos/anomalyco/opencode/releases/latest)本次返回 `v1.19.5`。CLI asset 是 `opencode-darwin-<arch>[-baseline].zip`、`opencode-linux-<arch>[-baseline][-musl].tar.gz`、`opencode-windows-<arch>[-baseline].zip`；只能匹配完整白名单模式，排除 `opencode-desktop-*` 和 desktop updater 的 `latest*.yml`。
- 本次全部 CLI 平台资产均有 SHA-256 digest。例如 `opencode-darwin-arm64.zip` 的值为 `c1c9e62521192192ef4c3ed1355ada2144055bddb61682217e997b03c55203c1`。x64 若无法可靠确认 AVX2，选择有官方 asset 的 baseline；libc 必须独立匹配。无目标组合则失败，不按“文件存在”跨 libc 猜包。
- [官方构建脚本](https://github.com/anomalyco/opencode/blob/v1.19.5/packages/opencode/script/build.ts)把平台 `bin` 目录内容打成发布 archive。完整解压到版本目录，验证 `opencode(.exe)`；即使当前大多是单 binary，也不提前删除同包其他成员。
- npm [`opencode-ai/1.19.5`](https://registry.npmjs.org/opencode-ai/1.19.5)顶层有 `postinstall.mjs`，且 `bin/opencode.exe` 是明确报错的占位脚本；忽略 scripts 后不可启动。本次读到脚本从相同精确版本平台包 `bin/opencode(.exe)` 复制。已读 [`opencode-darwin-arm64/1.19.5`](https://registry.npmjs.org/opencode-darwin-arm64/1.19.5)：有 SHA-512，无 scripts、dependencies，但 metadata 无 license，只有 2 个文件，不能假定该子包含许可。
- 使用 [同版本 MIT LICENSE](https://github.com/anomalyco/opencode/blob/v1.19.5/LICENSE)或经 SHA-512 校验的小型顶层 npm 包的 `LICENSE` 补齐许可。候选安装线约束在已验收的主版本内；如以后选 V2，需独立核定其官方源、许可证及协议。

### 2.4 Pi

- 当前 [官方 README](https://github.com/earendil-works/pi/blob/v1.5.0/README.md)连接 `pi.dev` 与 `@earendil-works/pi-coding-agent`。本次 [`earendil-works/pi/releases/latest`](https://api.github.com/repos/earendil-works/pi/releases/latest)是 `v1.5.0`；[`@earendil-works/pi-coding-agent/latest`](https://registry.npmjs.org/@earendil-works/pi-coding-agent/latest)也为 `1.5.0`。旧 [`@mariozechner/pi-coding-agent/latest`](https://registry.npmjs.org/@mariozechner/pi-coding-agent/latest)仍为 `0.73.1`；旧 scope 不能继续表达“当前官方最新版”。
- 官方 standalone 为 `pi-darwin-{x64,arm64}.tar.gz`、`pi-linux-{x64,arm64}.tar.gz`、`pi-windows-{x64,arm64}.zip`，本次六个均有 GitHub SHA-256；另有 `SHA256SUMS`。例如 macOS arm64 为 `a9bf0d82d58d2a7d337f4ae4171b5fc9ebffe42509527a831364c4b65adf2cdf`。
- [同版本 binary build 脚本](https://github.com/earendil-works/pi/blob/v1.5.0/scripts/build-binaries.sh)把 package.json、README、CHANGELOG、`photon_rs_bg.wasm`、theme、assets、export-html、docs、examples 与 native TUI 平台资源一并装入。POSIX tar 顶层为 `pi/`；Windows zip 内容在根层。解压策略须按来源布局处理，不通用 strip-components=1，更不能只复制 pi binary。
- [当前官方安装脚本](https://pi.dev/install.sh)本次使用 [`pi.dev/api/installer/releases/latest`](https://pi.dev/api/installer/releases/latest)返回的版本及 npm package/lock 走 npm 安装；元数据 `schemaVersion=1`、`version=1.5.0`、`sourceCommit=f9a26365568127461c47a07a37dcb42c140a3ac5`。该路线还涉及 Node runtime、整棵 npm 依赖与各自 integrity，不能只拿顶层 tarball 的 SHA-512 就声称安装完整。Go 首期优先 GitHub standalone。
- 许可为 [同 tag MIT LICENSE](https://github.com/earendil-works/pi/blob/v1.5.0/LICENSE)，保留其版权及全部随包声明。发行迁移与主版本变化不证明本项目 RPC adapter 已兼容；要对待安装的精确版本做协议门禁。

### 2.5 Kimi

- [Kimi 官方入门文档](https://www.kimi.com/code/docs/en/kimi-code-cli/guides/getting-started)与 [MoonshotAI/kimi-cli 发布源](https://github.com/MoonshotAI/kimi-cli/releases)属于同一产品。[latest API](https://api.github.com/repos/MoonshotAI/kimi-cli/releases/latest)本次为 `1.52.0`。
- 六个 Rust target 为 `aarch64/x86_64-apple-darwin`、`aarch64/x86_64-unknown-linux-gnu`、`aarch64/x86_64-pc-windows-msvc`；各有 onefile 与 `-onedir`，以及对应 `.sha256` 小文件。本次全部 archive 也有 GitHub SHA-256 digest。推荐 `kimi-<version>-<target>-onedir.tar.gz` 或 Windows `-onedir.zip`。例如 darwin arm64 onedir 为 `1d87d0c6d39e30593e44571ce6f78b20345735e6e836c1b175283ddabf5b9028`。
- [该 tag release workflow](https://github.com/MoonshotAI/kimi-cli/blob/1.52.0/.github/workflows/release-kimi-cli.yml)明确 PyInstaller onedir 的 `kimi/` 根，入口 `kimi/kimi` 或 `kimi/kimi.exe`，其他库和资源与入口相邻。必须保持完整树；macOS 还包括已签名动态库／framework，不能拆散或改写。来源层面支持 Windows arm64，不应沿用 OpenBot 的较窄 target 列表。
- 许可为 [Apache-2.0 LICENSE](https://github.com/MoonshotAI/kimi-cli/blob/1.52.0/LICENSE)及 [NOTICE](https://github.com/MoonshotAI/kimi-cli/blob/1.52.0/NOTICE)。workflow 的 archive 打包只遍历 runtime 产物，不能假定附带根许可；必要时从固定 tag commit 补齐这两个小文件。未检查 Linux 最低 glibc 或真实平台启动，musl 不在本次候选范围。

### 2.6 Grok

- [官方部署文档](https://docs.x.ai/build/enterprise)同时列出 `x.ai/cli` installer 与 `@xai-official/grok` npm；[`https://x.ai/cli/stable`](https://x.ai/cli/stable)与 [`@xai-official/grok/latest`](https://registry.npmjs.org/@xai-official/grok/latest)本次均为 `1.0.70`，`/latest` 返回 404。不要统一假设所有厂商都发布 `/latest`。
- 本次[官方 shell installer](https://x.ai/cli/install.sh)从 `https://x.ai/cli/grok-<version>-<platform>` 下载原始 binary，fallback 为官方 GCS `grok-build-public-artifacts/cli`，脚本中没有读强摘要的步骤。`xai-org/grok-build/releases/latest` 返回 404；所以不提供基于该 GitHub API 的虚构动态下载路径，也不把直接 binary URL 作为已验证一键源。
- npm 顶层 `1.0.70` 有 `postinstall: node bin/postinstall.js` 与 `@iarna/toml` 依赖；脚本会写用户 Grok 目录、配置及入口，不能整体执行为受管安装。平台子包为 `@xai-official/grok-{darwin,linux,win32}-{x64,arm64}`。本次 6 个精确版本 registry 元数据全部有 SHA-512、无 scripts、无 dependencies；[darwin-arm64 示例](https://registry.npmjs.org/@xai-official/grok-darwin-arm64/1.0.70)为 4 个文件。
- 已读的小型顶层脚本明确从平台包 `bin/grok` / `bin/grok.exe` **或对应 `.br`** 取得 binary；遇 `.br` 使用 `zlib.brotliDecompressSync`。本次未读取平台大包，因此不声称具体平台成员已实测。Go 方案须在校验整个 tgz 后识别白名单成员；原始 binary 可直接落地，Brotli 需内置受限 decoder，输出大小和内存有上限。若无该 decoder、出现未知压缩格式或布局，不得启动 Node/postinstall 兜底，应返回 blocked。解压／解码完成不等于版本、平台或协议验收。
- 顶层 README 仅列 Windows x64，但实际 1.0.70 optionalDependencies、platform metadata 和安装脚本含 win32-arm64；本表仅声明存在官方包，尚无实际 Windows arm64 运行证据。
- npm metadata 标为 Apache-2.0，官方仓库 [LICENSE](https://github.com/xai-org/grok-build/blob/main/LICENSE)与 [THIRD-PARTY-NOTICES](https://github.com/xai-org/grok-build/blob/main/THIRD-PARTY-NOTICES)存在，但本次 `v1.0.70/LICENSE` 为 404。不要把未绑定当前发行的 main 许可文件或 OpenBot 旧 commit 当作当前 binary 的组成证明。安装时要求经校验平台包中的许可／声明；若缺失，须建立当前发行到固定官方源码 commit 的对应关系后补齐，否则 license_unresolved。仅顶层 README 链接不满足开源声明保留。

### 2.7 Cursor

- [当前官方安装页](https://cursor.com/docs/cli/installation)明确支持 macOS、Linux、WSL 和 **原生 Windows**。POSIX [installer](https://cursor.com/install)本次指向 `https://downloads.cursor.com/lab/2026.10.22-27dcf5f/<darwin|linux>/<x64|arm64>/agent-cli-package.tar.gz`；[Windows installer](https://cursor.com/install?win32=true)指向同版本 `windows/<arch>/agent-cli-package.zip`。这些是脚本内读取的事实，不是对未来版本 URL 的稳定 API 承诺。
- 两个脚本都未找到 SHA-256/SHA-512、签名或 manifest 校验；POSIX 脚本完整展开目录并创建 `cursor-agent`／`agent` 链接，Windows 脚本保留整棵版本目录与 `.cmd` launcher。只复制 launcher 不能保留运行时。未下载 archive，所以不列出未经本次验证的 node、sqlite 等成员。
- 六种组合的上述资产 HEAD 请求全部返回 HTTP 403；不能据此认定 CLI 不支持该平台，也不能声称资产已经成功可下载。未找到官方强摘要的来源，ETag 即使存在也不自动等价 SHA-256。
- 首期返回 `external`，链接官方安装页，用户安装后接回原 Catalog 发现流程。以后若厂商提供强摘要 manifest，或建立 Neux 自己审阅、签名且逐版本维护的分发 catalog，再单独设计受管路线。**本地算出下载文件的 hash 只能形成后续完整性记录，不能证明首次拿到的是所期望发布，不能当作缺失官方 hash 的替代。**
- [Cursor Terms of Service](https://cursor.com/terms-of-service)是专有软件条款；不能从第三方 package/源码推断 CLI 有 MIT/Apache 再分发授权。本次不提出 bundle 或镜像。

## 3. Go `InstallOffer` 的来源解析建议

由现有 Catalog／RuntimeEnvironment 管理“安装事实与默认路径”，只给它补安装来源解析与一次安装操作，不另建 provider 状态源。下面是建议字段与流程，不是已经存在的接口。

| 字段／输入 | 含义与约束 |
| --- | --- |
| runtime、sourceID、sourcePolicyVersion | 明确产品与经审阅的官方分发路线；不接受前端任意传入 download URL |
| requestedChannel / requestedVersion、compatibilityPolicy | channel 与具体版本分开；精确版本优先；来源 latest 还须满足 adapter 已核定的主版本范围 |
| platform | OS、arch、libc、CPU 需求；来源 target 由受控映射计算 |
| disposition、reason | `managed` / `external` / `unsupported` / `blocked`；网络失败、缺 digest、未知 layout、许可不明、未支持解码分别表达，不混成“未安装” |
| version、sourceReference、resolvedCommit | 固化 version、release tag/ID 或 npm name+version；小型源码附件也固定 commit |
| assets[] | 每个 URL、size、格式、摘要算法和值、摘要来源；许可证等附件也纳入下载集合 |
| layoutID、entrypoint、requiredPaths | 受控版本布局；入口路径须在安装根内，不能由远端任意指定绝对路径 |
| integrityLevel、signatureEvidence | 区分 official-TLS SHA 摘要、已验证的发布者签名、本地安装后 hash；不能混写成“签名已验证” |
| licenseEvidence、termsURL | 文件位置、官方来源、版本关联及许可类型；下载可行与许可齐备分开判断 |
| resolvedAt、expiresAt、metadataValidator | 有时效的解析快照；用于缓存、重试和重新展示；不是永久相信 `latest` |

推荐 source policy：

1. Codex、OpenCode 1、Pi、Kimi 使用 GitHub release JSON；精确名匹配 asset，严格校验 repo/tag/draft/prerelease、size 和 `sha256:` digest。旧资产的 `digest=null` 是正常可能性，缺失时只允许另一个**本 runtime 官方且有明确对象关联**的 checksum 文件／已验证签名来源；否则 blocked，不复制旧 hash。
2. Claude 使用官方 channel→version→manifest；可加官方 GPG 签名验证。原始 binary 路线没有 npm、Node 或 install 脚本执行依赖。npm native 路线独立解析同精确版本并验证 SHA-512，不跨路线复用摘要。
3. Grok 使用官方顶层 npm manifest 的精确 optionalDependency 选择平台包，再读取该版本 manifest；解码器和许可门禁满足后才形成 managed offer。不得借 shell installer 的无摘要 raw URL绕过门禁。
4. Cursor、Gemini、WorkBuddy、Qoder 保持 external；下载源调研失败不影响已安装外部 CLI 的发现与使用。

`Resolve` 只读、小流量、无执行。请求有 context cancellation、超时、JSON／文本上限、合理缓存和并发限制，支持 ETag/Last-Modified；GitHub 限流不得静默切第三方代理。只允许已审阅 HTTPS origin/path，校验重定向最终 origin，拒绝本机／内网目标、URL 凭据与任意 frontend 指定 URL。

用户接受 offer 后使用其中冻结的版本、URL、摘要和 layout，**不可在执行中再次查询 latest 后换包**。重新解析若产生不同 artifact，应产生新 offer。artifact 获取前后按 offer 校验；只在验证完成后解压到 staging。tar/zip 防穿越、绝对路径、根外链接及重复冲突成员，限制压缩体、展开大小、成员数和解码内存。全树保留不等于允许 archive 写到版本目录之外。

之后验证必需目录／入口／许可，记录整个受管树的文件摘要，定向运行 `--version` 和无 Prompt 协议探测；成功才原子激活，并按原 RuntimeEnvironment 更新规则防止覆盖用户较新的路径选择。旧版本在有使用者时不删除。安装记录的本地文件摘要用于损坏检测，不伪装厂商签名。运行时自动更新策略需要单独核查：版本目录内文件不可被原生 updater 悄悄改写，否则冻结 offer 与后续真实版本会失配。

## 4. SHA-512、签名和完整目录的实施判断

npm `dist.integrity` 是 SRI，例如 `sha512-<base64>`，对象为**整个压缩 tarball 的字节**。Go 可用 `crypto/sha512` 流式计算，将 base64 解码为 64 字节后严格比较；不是对解压 binary 直接算 SHA-512，也不是把 base64 当 hex。拒绝只给 SHA-1 `dist.shasum` 的候选；存在多条 SRI 时按明确支持的强算法规则解析。tarball URL 要与受控 registry、包名、版本一致，并验证下载大小上限。每个额外包都需要自己的 integrity，顶层 hash 不覆盖 optionalDependencies 或依赖树。

registry 的 `dist.signatures`／provenance URL、GitHub asset `digest`／`.sigstore` 是不同证据。本次只验证相关字段和小型签名文件存在，未执行 npm provenance、Sigstore 或 GPG 验签。实现没有验签时，文案仅为摘要校验，不声明供应链证明。GitHub API digest 通过官方 HTTPS 获得，能校验匹配发布元数据，不是独立的厂商签名。

“平台子包无 scripts、无 dependencies”仅说明宿主可**不执行 npm lifecycle**而取得 payload，不证明每个功能对外部程序完全无依赖。Claude/OpenCode/Grok 顶层 postinstall 各自承担复制／解码／入口／用户配置操作，宿主只实现经审阅的文件布局转换，禁止运行这些脚本、隐式 npm fallback 或写用户 shell 配置。Codex/Pi/Kimi 使用规范完整目录，避免丢掉 sidecar、WASM、动态库或平台资源。

## 5. 本次验证结果与剩余边界

- 已直接读取上述官方 API、registry 精确版本元数据、manifest、小型源码／顶层包；所有链接均在本次访问日期核查，404、403 已在相应章节明示。版本、平台存在性和摘要是访问时快照，不作为产品锁版本。
- 已核查 Claude 8 个和 Grok 6 个精确 native 子包 metadata，Codex／OpenCode 各取一个平台 metadata 与官方布局源码；未下载任何平台大包，故 archive 成员、许可证实际随包情况与 executable 行为仍须安装实现阶段验证。
- 未运行安装、真实平台／模型／账号／UI 验收，也未运行产品测试；本报告没有产品代码变更。source metadata 成功不等于某版本已获本项目协议支持。受管发布前最低证据应为目标平台的摘要失败拒绝、布局保持、无脚本执行、无 Prompt 协议探测和激活／回滚验收。
