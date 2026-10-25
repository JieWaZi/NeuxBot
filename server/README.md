# NeuxBot Core

Go Core 维护业务、执行和持久化，使用 ginx、GORM 与 SQLite，按 apis、共享 services、repositories 和 internal 能力组织。开始修改前阅读根目录 AGENTS.md 与后端开发约定。

本地运行：在仓库根执行 `pnpm dev:server`；完整桌面开发执行 `pnpm dev:native`。默认接口监听回环地址，业务访问使用本地令牌。

协议由 `pnpm api:generate` 统一生成，并以 `pnpm api:check` 核验。完整环境、生成入口及验证要求见根目录 CONTRIBUTING.md。
