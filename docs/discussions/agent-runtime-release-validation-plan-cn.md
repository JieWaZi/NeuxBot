# Runtime 实际运行与交付收尾

> 历史阶段记录：本文的目录、独立 Go module 方案、迁移顺序和验收结果均以当时为准，不作为当前实现或重新验收的依据。当前执行能力已归入 `server/internal/agent/{core,host,environment}` 并属于 `server` 主模块；会话、启动与认证所有权以[职责收拢 Spec](agent-runtime-boundaries-spec-cn.md)及[当前架构说明](../architecture/runtimes-and-accounts-cn.md)为准。

> 执行方式：Sol 6.1 xhigh 并行验证和修复，Terra 6.1 核对文档；阶段末统一审查一次。

## 开始前核对

基线 `7f1f461`。A–E 的实现迁移及 F 的 CLI 管理拆分已完成：知识生成、独立 Wiki、聊天、团队、工作流、自动任务与 IM 已接入独立 Runtime；旧 runtimes 包已删除。账号配置准入不再隐式探测，探测与分叉走独立管理端口，全部 CLI 共用原生工厂。以上已有自动化证据，不列为待重做开发。

| 尚未完成的证据 | 本阶段处理 | 不得混淆的边界 |
| --- | --- | --- |
| 管理和工厂拆分后的真实 CLI 复验 | 已安装 CLI 使用本地虚构 Provider，验证真实最终结果、取消及可运行的探测 | 历史版本证据不能代替本阶段结果 |
| WorkBuddy、Qoder、Cursor 完整模型回答 | 核对当前可用的隔离配置；如仍有原生门槛，明确记录实际失败位置 | 未登录启动边界不等于最终输出通过 |
| Grok、Qoder CN 真机运行 | 核对安装现状，未安装则明确未验收 | 不为通过测试擅自安装程序 |
| 实际打包产物 | 本机 macOS arm64 构建、资源与许可核对、打包内 Core smoke | Windows 交叉编译不等于 Windows 真机运行 |
| 桌面进程生命周期 | 临时数据目录下验证 Core 启停、父进程退出和外部 Core 所有权，补实际缺口 | 不操作用户运行中的应用和数据库；headless 不等于视觉验收 |
| 文档现状一致性 | 历史记录标明阶段上下文，当前总表只写真实已完成范围 | 不把剩余环境门槛写成全部完成 |

## 任务与所有权

- [x] CLI：核对已安装版本、显式启用真实 CLI 测试，保证验收经过唯一 `native.New`，修复测试或实现中的实际问题。保留工具调用与 CLI 自有配置行为，禁止真实账号 fallback。负责 ACP 原生验收及直接相关工厂文件。
- [x] 交付：执行针对打包及生命周期的定向测试，实际构建 macOS 产物，隔离运行现有 smoke，必要时修复脚本或进程所有权。负责 scripts 与 desktop/electron 相关文件。
- [x] 文档：修正过时当前态描述，核对历史验收记录；已有阶段文档由文档代理维护，本计划由协调者维护。
- [x] 集成：汇总实际命令、结果与未验收边界；阶段末审查一次，集中修复实际发现，完成定向验证和文档检查。

## 约束

主链不增加业务或 CLI 分支，不新增无必要身份、锁或兼容层；acp-go 独立维护且本阶段不随意升级依赖。按实际发现修复，不预建企业或 HTTP 能力。仅验证变更与直接影响范围，不运行全仓检查。不发布、签名、上传产物，不读写真实产品数据或控制用户运行中的 UI。真实账号、模型质量、跨平台与视觉验收分别报告。

## 阶段结果

本轮没有发现需要修改 Runtime 主链的产品缺陷。修正了真实 CLI 测试绕过统一工厂的问题：测试现在通过 `native.New`，Pi 使用实际构建的桥接资源；补管理探测不发送模型请求、Codex 取消与三轮热/冷续聊证据。历史文档与当前实施总表同步更新。阶段末统一审查发现一项 P2：三类未配置 CLI 的管理测试曾接受任意错误。已收紧为握手成功、会话尚未建立、准确匹配已观察到的原生门槛，并拒绝取消和超时；启动测试同步限定 `new_session` 阶段。不增加产品分支或控制逻辑。直接相关六用例复验 6 pass、0 fail/skip；没有其他 P1/P2。`gofmt`、`git diff --check` 与 `pnpm docs:check` 通过。

### 真实 CLI

验收使用已安装 CLI、临时 HOME/工作区、虚构密钥和本地 Provider。实际运行 CLI 与协议，但回答由 fixture 生成，因此不代表真实账号、商业模型质量或全部产品业务验收。测试不限制 CLI 自有工具，不把全局 MCP 行为当成本轮缺陷。

| CLI / 本轮版本 | 最终结果与请求中取消 | 管理探测（无模型 Prompt） |
| --- | --- | --- |
| Codex 0.163.0 | 通过；含工具循环、五类 final 阶段判定 | 初始化、会话与模型确认通过 |
| Claude 2.1.159 | 通过 | 会话可用，目录 5 项 |
| Gemini 0.65.0 | 通过 | 会话可用，目录 6 项 |
| OpenCode 2.0.29 | 通过 | 会话可用，目录 1 项 |
| Kimi 2.0.2 | 通过 | 会话可用，目录 1 项 |
| Pi 0.85.1 | 通过 | 会话可用，目录 1 项 |
| WorkBuddy 2.151.0 | 未进入 | 握手通过；新建会话 `Authentication required` |
| Qoder 1.1.53 | 未进入 | 握手通过；新建会话 `Authentication required` |
| Cursor 2026.09.18-9a7762b | 未进入 | 握手通过；新建会话 `Failed to initialize session services`，不能据此判为认证错误 |
| Grok、Qoder CN | 未安装，未验收 | 未验收 |

显式启用九类已安装程序后，真实 CLI 叶用例 29 pass、0 fail、0 skip。包含三类预期初始化门槛的测试通过，不能解读为九类完整回答通过。11 类工厂构造/参数/失败回收的独立进程夹具亦通过，后者不计入真实 CLI 结果。

Codex 另验证 `Runtime.Run` 两轮热续聊 → `CloseSession` → 原检查点冷恢复第三轮：revision 为 1/2/3、原生句柄保持、无 Seed；每轮新增用户输入恰好一次、旧输入不重复、静态指令不变。强化精确计数后单跑该例通过。这里的冷恢复是同一 Runtime 内重新打开原生连接，不是 Core 重启后的宿主持久化恢复，也未实测原生 Fork。

从 `packages/agent-runtime` 运行以下定向测试。真实用例须显式设置 `NEUX_RUNTIME_<CLI>_CLI` 为本机程序绝对路径，Pi 另需 `NEUX_RUNTIME_PI_BRIDGE` 指向构建后的 `resources/pi-bridge/bridge.mjs`；未配置会跳过，不能以普通测试绿色替代真实验收。

```sh
GOWORK=off go test -json ./driver/acp -run '^TestNative' -count=1 -timeout=8m
GOWORK=off go test ./driver/acp -run '^TestNativeCodexSessionResume$' -count=1
GOWORK=off go test ./driver/acp/native \
  -run '^(TestConstructAgents|TestQoderProcessConfig|TestConstructionFailureReturnsNil|TestEnvironmentValue)$' -count=1
```

### 实际交付和生命周期

- macOS arm64 Core、allycli、Pi bridge、终端 worker/node-pty、Renderer 和 Electron main 均完成实际构建。Renderer 类型与生产构建通过。
- `desktop/release/mac-arm64/NeuxBot.app` 已生成；335.74 MiB，Resources 95.74 MiB，均满足现有 450/215 MiB 预算。没有产品签名、公证或发布，Electron 二进制原有 adhoc 标记不代表发行签名。
- 启动预算为 459210/500000 bytes 与 3847741/4500000 bytes；550 个 Brotli 资源均逐字节还原一致。
- 包内 Electron Node 模式 PTY、Core 认证健康与停止通过。未打包和包内 Core 均验证外部连接者停止不杀所有者、正常及重复停止、父进程正常退出/SIGKILL 后 Core 退出，以及同数据目录重启释放锁。
- 定向 Node 22/22 通过；`server/internal/parentwatch`、`server/cmd/ally-core` 两个 Go 包通过。
- Windows x64 Core 交叉编译得到实际 PE32+ x86-64 文件；没有运行 Windows 二进制，也没有生成安装器。

现有可重复入口如下；本轮另用临时独立父进程驱动器执行上述异常退出检查，没有给产品增加管理逻辑。构建临时依赖链接已清理。

```sh
GOWORK=off pnpm build:sidecar
pnpm terminal:build
NODE_OPTIONS=--no-experimental-webstorage pnpm --dir desktop build
node scripts/check-desktop-entry-budget.mjs
node scripts/package-native-renderer.mjs
node scripts/build-electron.mjs
CSC_IDENTITY_AUTO_DISCOVERY=false pnpm --dir desktop exec electron-builder \
  --mac dir --arm64 --publish never \
  --config.mac.identity=null --config.mac.notarize=false
node scripts/smoke-core.mjs
node scripts/smoke-desktop.mjs
```

以上独立构建命令要求先准备项目依赖。常规 `build:desktop` 还包含签名及 DMG 验证；本轮没有修改或绕过它的发布门槛。

### 仍需外部条件的验收

- WorkBuddy、Qoder、Cursor：原生会话初始化门槛通过后，再验最终回答；Grok、Qoder CN：安装对应 CLI 后再验实际运行。
- 原生 Fork、其他 CLI 热/冷续聊以及 Core 重启后的宿主持久化恢复：尚未真实验收。
- 真实账号刷新、真实模型、独立 Wiki 成果、团队/工作流和外部平台组合：目前是迁移与自动化覆盖，不能用本地 Provider 结果代替业务验收。
- 可见窗口、托盘、激活恢复、首帧、真实睡眠唤醒和系统退出：当前只有契约测试；本轮没有打开或操作用户应用。
- macOS 产品签名、公证、DMG/安装、macOS x64；Windows 真机 Core/ConPTY/生命周期、安装器及签名、Windows arm64：未验。Windows 预算 fixture 不等于真实安装包预算通过。

这些是明确剩余的验收项，不是已完成迁移需要重新开发，也不能标为全部发行就绪。
