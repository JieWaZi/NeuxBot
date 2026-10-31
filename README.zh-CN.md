<div align="center">

[English](README.md) · **简体中文**

<h1>NeuxBot</h1>

<p><strong>和你的 AI 伙伴，一起把事情做好。</strong><br />
本地优先的桌面工作台，让你与 AI 团队一起工作。</p>

[![MIT License](https://img.shields.io/badge/License-MIT-56b986?style=flat-square)](LICENSE)
![macOS](https://img.shields.io/badge/macOS-Apple%20Silicon%20%7C%20Intel-222822?style=flat-square&logo=apple)
![Windows](https://img.shields.io/badge/Windows-x64-222822?style=flat-square&logo=windows)

[下载安装](https://github.com/JieWaZi/NeuxBot/releases) · [使用文档](docs/README.md) · [参与贡献](CONTRIBUTING.md)

<img src="docs/assets/readme/hero.png" alt="NeuxBot 伙伴形象与桌面工作看板示意。" width="100%" />

<sub>产品示意图 · 示例数据 · 中文界面</sub>

</div>

NeuxBot 把 AI 工具、项目上下文与分工协作放进同一个本地桌面工作台。围绕自己的工作建立伙伴团队，从提出目标，到查看文件、代码改动与报告。

## 为什么用 NeuxBot？

- **让伙伴带着项目知识工作。** 为伙伴配置角色、档案、项目知识及所需的技能与资源，在日常任务、团队分工和工作流中复用这套配置。
- **自由选择工具与账号。** 根据任务选择 CLI、账号和模型，在同一处管理伙伴。既可沿用 CLI 原生登录，也可独立管理 AI 账号。
- **让协作形成可复用的工作。** 由团队负责人协调不同专长的伙伴、汇总成果；把经常做的事情通过对话编排成工作流，随时查看过程、处理审批与检查产出。

**支持的 CLI：** Codex · Claude Code · Cursor · Kimi · Pi · OpenCode · Gemini CLI · Grok Build · WorkBuddy · Qoder

<img src="docs/assets/readme/multi-cli.png" alt="NeuxBot 的现有果冻伙伴形象连接 Codex、Claude Code、Cursor、Kimi、Pi、OpenCode、Gemini CLI、Grok Build、WorkBuddy 和 Qoder，以及表示更多 CLI 的 More… 卡片。" width="100%" />

<sub>多 CLI 支持示意图</sub>

伙伴与工作记录保存在本机，AI 请求使用你选择的服务商；模型与工具能力取决于实际 CLI 和账号。

## 可以交给它什么？

- **推进项目开发。** 让伙伴分工实现需求、审查改动与执行验证，在对话旁查看代码和报告。
- **完成专题研究。** 让不同伙伴从多个角度收集资料，汇总观点与来源，形成研究报告。
- **持续处理例行工作。** 通过对话创建项目周报工作流，按计划运行；也可从授权的 IM 聊天交办任务、接收成果。

## 开始使用

在 [Releases](https://github.com/JieWaZi/NeuxBot/releases) 查找安装包，或按下方步骤从源码运行。连接支持的 CLI 与账号，创建一位伙伴，把项目交给它。

当前版本尚未提供 Apple Developer ID 签名与公证，Windows 安装包也未做代码签名。首次安装可能出现系统安全提示，处理方法见下方说明。

<details>
<summary>安装提示：macOS 无法验证开发者、Windows 已保护你的电脑、HTTPS 连接错误</summary>

请从本仓库的 [Releases](https://github.com/JieWaZi/NeuxBot/releases) 下载安装包，核对同一发行页 `SHA256SUMS` 中对应文件的摘要。摘要可检查下载是否完整，不能代替发行者签名。

**macOS**

1. 选择对应芯片的 DMG：Apple Silicon 使用 `NeuxBot-mac-arm64.dmg`，Intel 使用 `NeuxBot-mac-x64.dmg`，打开后将 `NeuxBot.app` 拖入“应用程序”。
2. 首次打开若提示“无法验证开发者”或“Apple 无法检查其是否包含恶意软件”，关闭提示，进入“系统设置 → 隐私与安全性”，在安全性区域找到 NeuxBot，点击“仍要打开”，再确认“打开”。系统可能要求身份验证。
3. 如果未出现“仍要打开”，或提示应用“已损坏”，先从官方 Release 重新下载并核对摘要。确认来源和摘要后，若仍受下载隔离限制，可在“终端”仅清除本应用的隔离标记，再重新打开：

   ```sh
   xattr -dr com.apple.quarantine "/Applications/NeuxBot.app"
   ```

   此命令会移除 NeuxBot 的下载隔离检查，仅在确认信任该安装包后使用。如果系统明确检测到恶意软件或提示“将损坏你的电脑”，停止安装并[反馈问题](https://github.com/JieWaZi/NeuxBot/issues)。

macOS 摘要核对示例，文件名按实际下载内容替换：

```sh
shasum -a 256 "$HOME/Downloads/NeuxBot-mac-arm64.dmg"
```

**Windows**

1. 下载并打开 `NeuxBot-windows-x64.exe`。
2. 若 SmartScreen 提示“Windows 已保护你的电脑”，在确认安装包来源和摘要后，点击“更多信息”（部分系统显示“详细信息”），再点击“仍要运行”，然后继续安装。
3. 如果没有“仍要运行”，或由 Smart App Control、组织策略或防病毒软件拦截，请联系设备管理员或[反馈问题](https://github.com/JieWaZi/NeuxBot/issues)，保留具体提示；不需要关闭 Defender 或全局安全保护。

Windows 摘要核对示例，在 PowerShell 中运行：

```powershell
Get-FileHash "$env:USERPROFILE\Downloads\NeuxBot-windows-x64.exe" -Algorithm SHA256
```

**HTTPS 连接或证书错误**

浏览器提示“连接不安全”“证书错误”，或下载时报 HTTPS 证书校验失败，属于网络连接问题，与安装包未签名不同。请检查系统日期和时间、代理或 VPN 配置，换用可信网络后重新打开官方 Releases；不要改用 HTTP 或跳过证书校验。仍无法下载时，请反馈错误文字和系统版本。

系统说明：[Apple：在 Mac 上安全地打开 App](https://support.apple.com/zh-cn/102445) · [Microsoft：SmartScreen 信誉与安装提示](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)。

</details>

<details>
<summary>从源码运行</summary>

准备 **Node.js 22**、**pnpm 10.34.4**，以及 [go.mod](server/go.mod) 指定的 Go 版本。

```sh
git clone https://github.com/JieWaZi/NeuxBot.git
cd NeuxBot
pnpm install --frozen-lockfile
go -C server mod download
pnpm dev:native
```

使用本机浏览器版时改为运行 `pnpm dev:web`，按终端地址打开网页即可进入工作台，无需配对码。同一数据目录只运行一个 Core。`pnpm package:web` 生成独立本机 Web 包，`pnpm build:native` 生成桌面安装包；运行要求与启动方式见[构建与发布](docs/development/build-and-release-cn.md)。

</details>

配置与使用见 [使用指南](docs/README.md)，系统命令能力见 [allycli 完整说明](server/cmd/allycli/README.md)，参与开发见 [贡献指南](CONTRIBUTING.md)。这些文档目前提供简体中文版本。

---

[MIT 许可证](LICENSE) · [第三方声明](THIRD_PARTY_NOTICES.md) · [问题反馈](https://github.com/JieWaZi/NeuxBot/issues) · 作者 [Ryan Wu / JieWaZi](https://github.com/JieWaZi)
