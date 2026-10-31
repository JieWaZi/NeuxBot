# 软件发行安全与后续签名方案

文档状态：本轮按未做正式发行代码签名的方案交付，正式签名延后到后续用户规模增长后再评估。个人发行身份保留，账号、证书、签名资助与购买不作为本轮前提。本文不表示安装包已经通过 Apple 公证或全部系统安全检测。

资料核验日期：2026-10-16。系统提示及服务条件可能变化；当前需求见[软件升级需求核对](software-update-requirements-cn.md)，用户安装操作统一见[中文 README](../../README.zh-CN.md)与[英文 README](../../README.md)。

## 本轮发行范围

用户选择先不做正式发行签名，后续使用人数增多后再考虑签名。本轮沿用现有打包流程：

- macOS 使用现有[临时签名脚本](../../scripts/sign-desktop.mjs)，保留运行所需签名，不申请 Developer ID，不做 Apple 公证。临时签名不验证发行者身份。
- Windows 不接入 Authenticode，不申请或购买 Certum、SignPath 或其他签名服务。
- GitHub Releases 继续托管 macOS arm64、macOS x64、Windows x64 安装包，附带 SHA256SUMS；下载使用 HTTPS 并保留证书校验。
- 中英文 README 明示当前签名状态，分别说明 macOS 未知开发者提示、应用下载隔离限制、Windows SmartScreen 提示，以及 HTTPS 连接或证书错误。
- 正式签名暂不作为本轮发布前置条件；不能将未签名写成已经完成系统安全认证。

macOS 的单应用打开例外，以及确认来源与摘要后的单应用隔离处理，由用户按 README 操作。Windows 仅对确认来源和摘要的具体安装程序处理 SmartScreen 提示；遇到系统策略限制或明确恶意软件检测时保留错误并反馈，不要求全局关闭防护。HTTPS 证书错误按系统时间、代理与网络问题排查，不与安装包未签名混为一谈。[Apple 安装提示说明](https://support.apple.com/zh-cn/102445)、[Microsoft SmartScreen 说明](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)

## 对软件升级的影响

当前[更新服务](../../desktop/electron/main/application-update.ts)能够探测 GitHub 正式发行、下载安装包并核对 SHA256，最后调用系统打开安装包，版本替换由用户完成。该路径不依赖付费签名账号，但没有实现应用自行安装并重启。

electron-updater 的 macOS 标准更新路径要求代码签名及 ZIP 更新产物。本轮取消正式签名后，不能把它作为已经确定可用的 macOS 一键更新实现。[electron-builder v26 自动更新说明](https://www.electron.build/v26/docs/features/auto-update/)

用户已确认首轮采用“自动检查与下载 → 打开安装包 → 用户完成覆盖安装并重新启动”。应用内自动安装与重启延后评估，本轮不接入自动替换机制。安装前完成工作并真正退出，覆盖原安装位置后沿用独立用户数据目录；下载或打开安装包不主动停止工作。

## 发布与验证边界

1. 在现有原生构建矩阵生成 DMG、EXE 及体积报告，macOS 临时签名保持有效。
2. 对最终发行文件生成 SHA256SUMS，上传到草稿 Release，准备完整后再按已确认的发布流程公开。
3. 发布说明明确当前没有正式发行签名，并链接对应版本的 README 安装说明。摘要核对仅验证下载完整性，不代替签名身份认证。
4. 在默认防护设置下，记录各目标平台的实际安装、系统提示、首次启动和子进程退出结果，不将文档中的排障步骤当作已经完成的安装验收。
5. 验证下载中断、错误摘要和不可信下载地址的处理，继续拒绝损坏下载与不安全传输；验证手动覆盖安装、重新启动、版本核对与用户数据保留。

本轮没有接入新证书或账号，不要求为准备 README 打开或控制运行中的桌面应用。自动化检查、安装包校验和真实平台安装分别记录。

## 后续启用正式签名时的参考

后续决定启用时，优先复用当时成熟框架和开源发布流程，再按个人发行身份核验账号与服务资格：

| 平台    | 可参考的正式发行路线                                                  | 接入条件                                                                                   |
| ------- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| macOS   | Developer ID Application 签名、Hardened Runtime、Apple 公证及票据附加 | Apple Developer Program 账号、证书及安全保管的私钥、公证认证；个人身份可用。               |
| Windows | 公开受信任的 Authenticode 签名、SHA-256 及 RFC 3161 可信时间戳        | 可用的个人签名服务或通过审核的开源资助；资助证书可能显示基金会名称，不能当作自有个人证书。 |

Apple 开发者计划标准年费为 99 美元，中国大陆通过 Developer App 的年费为 688 元，以实际注册页面为准；开源本身不提供普遍费用豁免。Windows 的 SignPath 开源资助需要审核，商业服务需核验个人与地区资格；Microsoft Artifact Signing 的个人 Public Trust 资格目前限美国、加拿大，不能由语言、时区或构建机位置推定资格。这些是后续参考，不是当前采购或注册计划。[Apple 年费说明](https://developer.apple.com/cn/help/account/membership/enrolling-in-the-app/)、[Apple 费用豁免](https://developer.apple.com/help/account/membership/fee-waivers/)、[SignPath 条件](https://signpath.org/terms.html)、[Microsoft 服务资格](https://learn.microsoft.com/en-us/azure/artifact-signing/quickstart)

有效签名也不保证立即获得 Windows SmartScreen 信誉；当前 EV 证书不再自动绕过信誉提示。后续签名方案仍需分别验证签名、公证、恶意软件检测与真实系统安装，不承诺所有设备零提示。[Microsoft 信誉规则](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)

## 开源项目的可参考做法

| 项目            | 从项目自身资料核实的做法                                                                                                                                                                               | 可参考的部分                                                                                                                                                                          |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Electron Fiddle | 打包配置指定 OpenJS Foundation 的 Developer ID，并配置 macOS 公证；当前发布工作流将 Windows 签名接入 Azure Trusted Signing，使用独立 Windows 作业与 OIDC 登录                                          | 两个平台分别接入官方信任链，签名、公证属于 CI 发行流程；签名身份不使用随意填写的产品品牌。                                                                                            |
| Flameshot       | README 明确其 Windows 二进制使用 SignPath.io 与 SignPath Foundation，并说明签名目前需要人工处理，并非每个补丁包都会签名                                                                                | 开源资助是实际存在的路线，但“项目使用签名服务”不等于任意下载产物都已签名；本项目应逐个发行产物验证。                                                                                  |
| OpenBitFun      | 当前桌面发布流程在公开发行时要求 Apple Developer ID 签名、公证资料及 Windows Authenticode 签名；Windows 接入 Certum SimplySign。公开运行 `38772060516` 的 macOS x64 签名配置及签名、公证验证步骤均成功 | 正式信任链确实用于桌面发行；项目另有 minisign 下载签名，不可将其与系统签名混为一谈，也不可用数据迁移器的未签名说明推定桌面产品未签名。                                                |
| Proma           | `v0.19.58` 发布流程导入 macOS 证书，并向打包步骤传入公证所需 Apple 账号及 Team ID；公开运行 `36101031688` 的 arm64、x64 证书导入和打包步骤均成功。所查公开 Windows 发布配置未见独立证书接入            | 工作流头部虽将 macOS 签名、公证资料写为“可选”，具体发行仍应核对实际步骤和运行记录，不能仅凭注释判断未签名。公开记录不能证明账号属于作者个人或公司，也不能代替对最终安装包的签名检查。 |

依据：[Electron Fiddle 打包配置](https://github.com/electron/fiddle/blob/main/forge.config.ts)、[Electron Fiddle 发布工作流](https://github.com/electron/fiddle/blob/main/.github/workflows/release.yml)、[Flameshot 签名政策](https://github.com/flameshot-org/flameshot#code-signing-policy)。

用户指定案例的依据：[OpenBitFun 桌面发布流程](https://github.com/GCWing/OpenBitFun/blob/main/.github/workflows/desktop-package.yml)、[Apple 签名准备脚本](https://github.com/GCWing/OpenBitFun/blob/main/scripts/ci/setup-macos-signing.sh)、[macOS 签名及公证验证运行](https://github.com/GCWing/OpenBitFun/actions/runs/38772060516)、[下载签名说明](https://github.com/GCWing/OpenBitFun/blob/main/docs/verify-downloads.md)、[Proma v0.19.58 发布流程](https://github.com/proma-ai/Proma/blob/v0.19.58/.github/workflows/release.yml)、[Proma v0.19.58 发布运行](https://github.com/proma-ai/Proma/actions/runs/36101031688)。
