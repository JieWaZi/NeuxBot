# NeuxBot Pi bridge

NeuxBot 在此工作区固定 `pi-mcp-adapter@2.32.1`、`pi-auto-approval@0.1.0` 的指定提交及 Pi 0.84.1 对应依赖。`pnpm pi:bridge:generate` 从 `entry.mjs` 生成 `desktop/resources/pi-bridge/bridge.mjs` 和第三方许可；`pnpm pi:bridge:check` 验证提交的产物与锁文件安装结果一致。请勿手工修改生成文件。

桌面打包把两个产物放在 `process.resourcesPath/pi-bridge/`。Electron 主进程将 `bridge.mjs` 的绝对路径作为 `PI_MCP_MODULE_PATH` 传给 NeuxBot Core，Core 再传给内嵌的 `pi.Config.MCPModulePath`。开发模式从 `desktop/resources/pi-bridge/` 读取。独立启动 Core 或 `acp-agent` 的宿主必须自行提供该环境变量，且路径需指向 Pi 可读取的真实文件。

该文件同时导出 `createMcpAdapter`、`classifyAction`、`createReviewSubject`、`completeSimple`；它由 Pi 官方扩展加载器导入，不由 Electron Renderer 执行。所有 Pi 会话都要求 bridge，包括没有用户 MCP 连接器的会话。Pi CLI 及模型登录仍由用户准备，版本兼容由 NeuxBot 的固定依赖和发布验收保证。
