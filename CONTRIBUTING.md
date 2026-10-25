# 参与 NeuxBot 开发

## 准备环境

使用 Node.js 22、仓库 packageManager 固定的 pnpm，以及 server/go.mod 指定的 Go 版本。桌面安装包在 macOS 或 Windows 原生环境构建。智能体 CLI 及其登录由开发者自行准备。

```sh
git clone https://github.com/JieWaZi/NeuxBot.git
cd NeuxBot
pnpm install --frozen-lockfile
go -C server mod download
ALLY_TOOLX_MODULE_VERSION=$(go -C server list -m -f '{{.Version}}' github.com/shrewx/ginx)
go install "github.com/shrewx/ginx/pkg/toolx@${ALLY_TOOLX_MODULE_VERSION}"
export PATH="$(go env GOPATH)/bin:$PATH"
pnpm dev:native
```

`pnpm dev` 提供仓库开发脚本入口；只启动 Core 可用 `pnpm dev:server`。服务默认使用回环地址与本地访问令牌。首次使用创建伙伴并选择有效智能体；不在仓库保存真实凭据。

## 修改前

阅读 [开发约定](AGENTS.md)、[领域词汇](CONTEXT.md) 和对应模块文档。先确认功能范围、权威数据与真实调用入口，再选择实现位置。缺陷反馈附上复现步骤、预期与实际行为、平台版本及脱敏日志。

## 生成与检查

| 修改内容               | 对应命令                                                                  |
| ---------------------- | ------------------------------------------------------------------------- |
| Go API、枚举及 OpenAPI | `pnpm api:generate`、`pnpm api:check`                                     |
| 系统技能               | `pnpm skills:generate`、`pnpm skills:check`                               |
| 工作流语言与知识       | `pnpm workflow:sdk:generate`、`pnpm workflow:sdk:check`                   |
| 结构化呈现目录         | `pnpm workflow:presentation:generate`、`pnpm workflow:presentation:check` |
| 伙伴渲染源             | `pnpm companion:generate`、`pnpm companion:check`                         |
| Pi Bridge              | `pnpm pi:bridge:generate`、`pnpm pi:bridge:check`                         |
| 完整验证               | `pnpm check`                                                              |

修改 Go 后先 gofmt。按行为编写必要测试，覆盖身份、权限、事务、恢复或资源生命周期等关键边界。真实登录、计费模型、原生窗口和视觉验收单独说明。

## 提交与评审

一个提交围绕一个完整目的，使用 Conventional Commits，正文说明问题、变化和验证。PR 描述面向首次阅读者，列出实际行为与剩余边界；不提交本机路径、密钥、日志、调研副本或临时输出。

改变行为、协议或领域含义时同步更新对应权威文档。计划、进度和当次验收结果放在 Issue 或 PR 中。构建与发布见 [发布说明](docs/development/build-and-release-cn.md)。提交贡献表示按仓库 MIT 许可提供该贡献；第三方内容仍须保留其适用声明。
