# Desktop Size Performance Implementation Plan

> **For agentic workers:** Use subagent-driven-development for the independent tasks below; preserve unrelated working-tree changes.

**Goal:** 按实际交付链减少不必要安装资源，并消除欢迎阶段 JS 超预算。

**Architecture:** 保持 Electron、Core、Renderer 和终端资源边界；原生构建与 Web 显式区分资源兼容策略。以同一版本、平台、压缩链度量，不关闭功能凑体积。

**Tech Stack:** Electron、Vite、TypeScript、Node.js、Shiki、Vitest。

**Spec:** ../specs/2026-11-09-desktop-size-performance-design.md

## Global Constraints

- 当前分支开发，保留无关修改，不提交整个工作树，不发布安装包。
- 不打开、控制或截图运行中的页面、浏览器和桌面应用。
- 不运行全量检查，不放宽启动 JS 预算。
- 只有无消费资源和多余依赖入口允许移除；PDF、Excel、代码预览、终端和工作流能力保留。

## Review Focus

- 工作台初始化前的文案消费者、英文及未知语言回退。
- Vue／Svelte 内嵌语法、未知文件名、快速切换与高亮加载失败。
- Web 字体回退不随原生优化丢失；PDF 的非沙箱资源保持完整。
- PTY 的平台、架构和 Electron ABI 优先于文件体积。
- 压缩安装包、展开资源与启动 JS 必须分别报告；旧包不可作为当前源码的冒充基线。

## Task 1: 制品与依赖清单

- [x] 核对本地旧制品及公开 release 元数据，按组件分类必要性和体积。
- [x] 测量同输入原生交付和安装包；记录仍未取得的约 200MB 文件来源，未冒充其版本。

## Task 2: 收窄文件代码预览

- [x] 在 `selectable-code-viewer.tsx` 引入专属高亮加载器，保留原语言映射与两主题。
- [x] 以构建依赖图证明不再引入无消费语言／主题，并对比现有高亮 token。
- [x] 定向验证失败、快速切换和实际 Vite 产物差异；补回全部可达惰性嵌入语法并冷实例对照。

## Task 3: 原生资源与启动入口

- [x] 将 workflow／automaticTask 文案移至 `app/workspace-i18n.ts`，验证真实导入顺序。
- [x] 显式区分原生字体与 PDF 资源策略，验证 Native 精简与 Web 保留。
- [x] 删除无消费欢迎图并限定 node-pty 的 source map／测试交付。
- [x] 粒子材质增加独立视觉尺寸补偿，保持资源交接与动画。

## Task 4: 集成验证与交付

- [x] 统一 Vite、Electron 编译和预算检查，记录原始与 Brotli 资源前后值；完整生产检查边界另行标明。
- [x] 定向测试、类型、样式、格式和生成一致性检查通过。
- [x] 离线安装包内容与体积核对；独立审查修复高亮嵌入语法的重要回归。
- [x] 更新[交付体积证据](../../discussions/desktop-size-performance-cn.md)，报告本地修改、fork 与发布状态的边界。

## 结果

首屏静态 JS 509449 → 458878 bytes，原生前端交付 17248739 → 12225709 bytes。同 Electron／侧车／宿主输入 DMG 124119342 → 119834987 bytes，减少 3.45%；包含本轮离线输入对齐标准 CI 的示例包清理，未把旧 release 当作基线。

完整 `desktop build` 执行时被并发设置页的 `Button ref` 类型错误阻塞，该无关文件未修改。本轮 Vite、Electron 编译、定向回归、离线签名与 DMG 校验通过，没有执行真实应用 UI 验收或发布；侧车复用本地已有构建。
