<div align="center">

**English** · [简体中文](README.zh-CN.md)

<h1>NeuxBot</h1>

<p><strong>Your AI partners, working together.</strong><br />
A local-first desktop workspace for you and your AI team.</p>

[![MIT License](https://img.shields.io/badge/License-MIT-56b986?style=flat-square)](LICENSE)
![macOS](https://img.shields.io/badge/macOS-Apple%20Silicon%20%7C%20Intel-222822?style=flat-square&logo=apple)
![Windows](https://img.shields.io/badge/Windows-x64-222822?style=flat-square&logo=windows)

[Releases](https://github.com/JieWaZi/NeuxBot/releases) · [Documentation](docs/README.md) · [Contributing](CONTRIBUTING.md)

<img src="docs/assets/readme/hero.png" alt="NeuxBot's companion character alongside an illustrated desktop work board." width="100%" />

<sub>Product illustration · sample data · Chinese interface</sub>

</div>

NeuxBot brings your AI tools, project context, and collaborators into one local desktop workspace. Build a team of partners around the way you work, and follow their work through to files, code changes, and reports.

## Why NeuxBot?

- **Partners you can keep working with.** Give each partner a role, profile, project knowledge, and the skills and resources it needs. Reuse that setup across tasks, team assignments, and workflows.
- **Your choice of tools and accounts.** Choose a CLI, account, and model for the work at hand while managing your partners in one place. Use existing CLI logins or separately managed AI accounts.
- **From delegation to repeatable work.** Let a team leader coordinate specialists and collect their results. Turn recurring work into workflows through conversation, with execution progress, approvals, and outputs available for review.

**Supported CLIs:** Codex · Claude Code · Cursor · Kimi · Pi · OpenCode · Gemini CLI · Grok Build · WorkBuddy · Qoder

<img src="docs/assets/readme/multi-cli.png" alt="NeuxBot's existing jelly companion connects Codex, Claude Code, Cursor, Kimi, Pi, OpenCode, Gemini CLI, Grok Build, WorkBuddy and Qoder, plus a More card, in one workspace." width="100%" />

<sub>CLI support overview · illustration</sub>

Partners and work records are stored locally. AI requests use your chosen providers; available models and tools depend on the CLI and account.

## What can you put to work?

- **Build and review a project.** Have partners implement a change, review it, and verify it; inspect their code and reports alongside the conversation.
- **Research a topic together.** Assign different angles to research partners, then bring their findings and sources into a shared report.
- **Make routine work repeatable.** Create a project-update workflow through conversation and schedule it. Use an authorized IM chat to ask your partners for work and receive results.

## Get started

Find an installer in [Releases](https://github.com/JieWaZi/NeuxBot/releases), or run from source below. Connect a supported CLI and account, create a partner, and give it a project to work on.

Current macOS releases have no Apple Developer ID signature or notarization, and Windows installers are not code signed. You may see a security prompt during installation; see the instructions below.

<details>
<summary>Installation help: unidentified macOS developer, Windows protected your PC, or HTTPS errors</summary>

Download installers from this repository's [Releases](https://github.com/JieWaZi/NeuxBot/releases) and compare the file's SHA-256 hash with its entry in that release's `SHA256SUMS`. A matching hash checks download integrity; it does not replace a publisher signature.

**macOS**

1. Choose `NeuxBot-mac-arm64.dmg` for Apple Silicon or `NeuxBot-mac-x64.dmg` for Intel, open the DMG, and drag `NeuxBot.app` into Applications.
2. If macOS cannot verify the developer or check the app for malicious software, dismiss the prompt. Open **System Settings → Privacy & Security**, find NeuxBot in the Security section, click **Open Anyway**, then confirm **Open**. Authentication may be required.
3. If **Open Anyway** is unavailable, or the app is reported as damaged, download it again from the official release and check its hash. If you trust the source, the hash matches, and download quarantine still prevents opening it, remove quarantine for this app only in Terminal, then open it again:

   ```sh
   xattr -dr com.apple.quarantine "/Applications/NeuxBot.app"
   ```

   This removes NeuxBot's download quarantine check; use it only for an installer you have verified and trust. If macOS detects malware or says the app **will damage your computer**, stop installation and [report the issue](https://github.com/JieWaZi/NeuxBot/issues).

To check a macOS download's hash, replace the filename below with your actual download:

```sh
shasum -a 256 "$HOME/Downloads/NeuxBot-mac-arm64.dmg"
```

**Windows**

1. Download and open `NeuxBot-windows-x64.exe`.
2. If SmartScreen shows **Windows protected your PC**, verify the download source and hash, then choose **More info → Run anyway** and continue installation.
3. If **Run anyway** is unavailable, or Smart App Control, an organization policy, or antivirus software blocks installation, contact your device administrator or [report the issue](https://github.com/JieWaZi/NeuxBot/issues) with the exact message. Keep Defender and system-wide protections enabled.

To check the Windows installer's hash, run this in PowerShell:

```powershell
Get-FileHash "$env:USERPROFILE\Downloads\NeuxBot-windows-x64.exe" -Algorithm SHA256
```

**HTTPS connection or certificate errors**

A browser's insecure-connection or certificate warning, or an HTTPS certificate validation failure during download, is a network connection issue separate from an unsigned installer. Check your system date and time and proxy or VPN settings, then retry the official Releases page on a trusted network. Keep HTTPS and certificate validation enabled. If downloading still fails, report the error text and operating system version.

System guidance: [Apple: Open apps safely on your Mac](https://support.apple.com/en-us/102445) · [Microsoft: SmartScreen reputation and installation warnings](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).

</details>

<details>
<summary>Run from source</summary>

Requires **Node.js 22**, **pnpm 10.34.4**, and the Go version in [go.mod](server/go.mod).

```sh
git clone https://github.com/JieWaZi/NeuxBot.git
cd NeuxBot
pnpm install --frozen-lockfile
go -C server mod download
pnpm dev:native
```

For the local browser version, run `pnpm dev:web` instead and open the address printed in the terminal; no pairing code is required. Run only one Core against a data directory. Use `pnpm package:web` for a standalone local Web bundle and `pnpm build:native` for a desktop installer. See [build and release](docs/development/build-and-release-cn.md) for runtime requirements and startup.

</details>

See the [user guides](docs/README.md) for setup and usage, the [allycli capability reference](server/cmd/allycli/README.md) for available system operations, or [CONTRIBUTING.md](CONTRIBUTING.md) to help build NeuxBot. These documents are currently in Simplified Chinese.

---

[MIT License](LICENSE) · [Third-Party Notices](THIRD_PARTY_NOTICES.md) · [Issues](https://github.com/JieWaZi/NeuxBot/issues) · By [Ryan Wu / JieWaZi](https://github.com/JieWaZi)
