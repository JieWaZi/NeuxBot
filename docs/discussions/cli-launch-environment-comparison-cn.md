# 跨平台 CLI 启动环境源码对照

2026 年 11 月 25 日，NeuxBot macOS 桌面 Core 使用 `/usr/bin:/bin:/usr/sbin:/sbin`。安装探测能读到 Codex 0.172.0，内置账号检查和聊天首发均返回 `RuntimeUnavailable`。使用相同的 `PATH` 直接启动选中的 npm Codex 入口，得到 `env: node: No such file or directory`。加入 `/opt/homebrew/bin` 后，同一入口可以读出版本。这个复现说明，找到 CLI 文件和找到脚本解释器是两个条件。

## NeuxBot 修复前

发现安装与检查版本使用 `buildPreparedEnvironment`，保留已有路径顺序并追加平台常见目录。macOS 有 Homebrew，Linux 有 Linuxbrew、Snap 和用户工具目录，Windows 有 npm、pnpm、Winget、Scoop 等目录，并按 `PATHEXT` 查找入口。账号检查与聊天启动则从 Core 的 `os.Environ()` 构造 `ProviderLaunch.BaseEnvironment`。共享 `nativeLaunchEnvironment` 按账号和资源组装子进程，未复用探测时的路径补全。账号检查也进入同一个原生客户端构造入口。 [探测和路径规则](../../server/internal/agent/runtime_installation.go#L62) · [账号检查](../../server/services/runtimeenvironment/authentication.go#L260) · [聊天启动](../../server/services/execution/native_turn.go#L53) · [共享原生客户端](../../server/internal/agent/agent_client.go#L645)

## OpenBot

- **macOS 与 Linux**。OpenBot #2713 修过同样的 npm Codex 退出 127。其 CLI 解析器限时从登录 shell 读取 `PATH`，缓存结果并合入进程的路径。解析器为系统安装追加选中 CLI 的目录，把 `launchPath` 交给版本检查和真正的子进程。Codex 的 `app-server` 启动直接消费这份环境。上游回归用精简 `PATH` 和只有登录 shell 能找到的假解释器，同时检查版本和协议初始化。 [修复 PR](https://github.com/nightly-labs/openbot/pull/2713) · [PATH 与版本检查](https://github.com/nightly-labs/openbot/blob/2bb74f4f47f229fe0f0f47908e62609aed8dbf75/src/backend/cli.ts#L1062-L1175) · [Codex 客户端装配](https://github.com/nightly-labs/openbot/blob/2bb74f4f47f229fe0f0f47908e62609aed8dbf75/src/backend/agent/provider-runtime.ts#L472-L484) · [回归用例](https://github.com/nightly-labs/openbot/blob/2bb74f4f47f229fe0f0f47908e62609aed8dbf75/scripts/codex-login-path-e2e.ts#L98-L120)
- **Windows**。查找阶段读取用户和机器注册表 `Path`，展开变量并合入进程路径，用 `where.exe` 搜索并按 `PATHEXT` 过滤。`.cmd` 和 `.bat` 由 `cmd.exe` 执行。该版本的 Windows `cliLaunchPath` 返回空，注册表补全只送给这里的 `where.exe` 查询。因此，找到批处理文件仍不能证明其依赖的解释器可以由子进程找到。 [注册表与查找](https://github.com/nightly-labs/openbot/blob/2bb74f4f47f229fe0f0f47908e62609aed8dbf75/src/backend/cli.ts#L908-L1005) · [启动环境](https://github.com/nightly-labs/openbot/blob/2bb74f4f47f229fe0f0f47908e62609aed8dbf75/src/backend/cli.ts#L1162-L1175) · [批处理启动](https://github.com/nightly-labs/openbot/blob/2bb74f4f47f229fe0f0f47908e62609aed8dbf75/src/backend/cli.ts#L1223-L1238)

## 对 NeuxBot 的建议

在 Go Core 的共享 CLI 启动边界复用现有的跨平台路径规则。发现、版本检查、内置账号检查、模型目录管理和原生进程使用相同的程序选择及 `PATH` 基线。保持已有有效路径的优先级及账号、资源各自的环境合并规则。此处的工作是构造本地进程环境。账号、额度和目录探测仍由各自的管理入口发起。 [现有路径规则](../../server/internal/agent/runtime_installation.go#L77) · [运行边界](../development/backend-cn.md#82-执行关键路径禁止诊断探测)

macOS 和 Linux 先复用已实现的常见目录补全。需要覆盖依赖登录 shell 动态选版本的安装时，可在启动或显式刷新时限时读取 `PATH`，将结果用于后续进程。Windows 应基于继承的 `Path` 补入用户和机器注册表中缺失的目录，并将同一结果交给查找和子进程。继续使用现有 `PATHEXT` 解析与批处理启动机制。WSL 仅在 Core 实际运行于目标发行版时按该发行版的 Linux 环境处理。

三平台回归应从精简宿主 `PATH` 开始，分别覆盖原生程序和需要第二个解释器的脚本或 shim，验证版本读取、原生握手和实际进程对同一解释器的解析。Windows 加上已有进程未继承新注册表路径及含空格的 `.cmd` 入口。本文记录 macOS 缺 Node 的本机复现及 OpenBot 源码证据，Linux、Windows 和 WSL 的 NeuxBot 启动行为仍待实机验证。
