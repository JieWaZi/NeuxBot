# Bloub Avatar Implementation Plan

> **For agentic workers:** Use subagent-driven-development or executing-plans; implement the following scoped tasks and preserve unrelated changes.

**Goal:** 用通用形状、多材质和状态动画替换旧头像系统。

**Architecture:** 固定公开 Bloub 核心和毛绒纯函数，自维护 fork 提供框架无关库，主仓库负责 React 集成；NeuxBot 通过 `@neuxbot/bloub` `main` 分支依赖，根锁文件固定解析提交。既有 avatar 字段持久化配置，既有聊天活动派生驱动动画。

**Tech Stack:** React、TypeScript、SVG、Bloub、Vitest。

**Spec:** ../specs/2026-11-09-bloub-avatar-design.md

## Global Constraints

- 当前分支开发；保留无关修改，不提交整个工作树。
- 不新增 Vue／Solid 运行时，不保留旧专属形象与聊天浮动召唤；保留侧栏双击资料卡和单击表情互动。
- 只有毛绒与粒子，使用原创完整立体身体图集；保留 MIT 与固定来源。
- 只进行定向验证，不打开运行中的应用。

## Review Focus

- 快速切换材质／颜色不能提交迟到纹理。
- 失败、等待、取消不能被悬停或延迟表情遮盖。
- 静态与减少动态模式必须保留状态且不启动循环。
- 历史身份在改名后稳定，旧标识不加载旧资源。
- 欢迎页需保留 ready／error 交接，不因移除图片而卡住。

## Task 1: 来源、配置与工作状态

- [x] Fork 上游，记录来源提交与许可证，以固定 Git 依赖接入框架无关核心和毛绒纯函数。
- [x] 先测试新 avatar 配置往返、旧身份稳定映射、无效输入回退和全部状态映射；确认旧实现不满足后实现。
- [x] 接口：companionAppearance(avatar) 返回 shape、material、color、expression；companionAvatar(value, identity) 返回合法标识；randomCompanionAvatar() 随机配置；encodeCompanionAppearance(config) 保存配置。

## Task 2: React 展示与材质

- [x] 测试固定帧不启动循环、动态生命周期与快速更新，然后实现同一展示层。
- [x] CompanionAvatar 保留 avatar/activity/animated/onReady/onError props；不保留旧命令式引擎。
- [x] 复用换色和状态辅助函数，两种原创立体身体按元数据裁格显示，不再平铺纹理。

## Task 3: 选择与集成

- [x] 选择器先覆盖单项变更保留其他属性与重新打开后还原，再接入材质／形状／眼睛／颜色。
- [x] 删除聊天悬浮形象，保留侧栏双击资料卡，补足单击表情互动及工作表情循环，移除空白会话中央头像与问候，欢迎页接入新头像。
- [x] 删除旧引擎、生成脚本和 Studio，更新来源与权威文档。

## Task 4: 验证与审查

- [x] 运行配置、动画、选择器、欢迎页和聊天相关定向测试，类型和格式检查。
- [x] 独立审查本次变更，修复重要问题；报告真实视觉验收、fork 推送与本地修改的边界。

## 最终范围

- 默认毛绒，只保留毛绒与 particles；毛绒为原创完整立体身体，粒子为三维微光尘投影的可重复生成 SVG 图集。
- 13 种原生动作由会话状态驱动，取消产品睡眠；选择器不展示动画或播放控制。
- 24 种默认表情和全池点击互动；空闲与工作自动轮换实际眼形，原生动作穿插表情段，悬停反馈只播放一次；欢迎页固定玫粉 `#e152b0`。
- 眼睛统一放大 15%，保留外侧余量和眨眼；粒子覆盖眼周并复用绘图时钟，单独补偿 20% 视觉体积；侧栏伙伴头像使用 40px 规格。
- 选择器移除全部装饰勾；纹理完成换色与解码后完整交接，正常准备期间不绘平面过渡帧；就绪缓存最多保留最近使用的 12 项。
- 侧栏双击资料卡保留，聊天悬浮角色及空白页中央头像／问候移除。

## 验证记录

- 本轮头像、选择器、侧栏、资料卡、表单、欢迎页及工作状态相关 18 个测试文件共 137 项通过；Renderer 的 TypeScript 生产检查与 Vite 构建通过。
- UI 规范、定向 ESLint／格式、文档检查及差异检查通过。
- 自维护源码位于 `JieWaZi/bloub` 的 `main` 分支；消费方通过 `@neuxbot/bloub` 公开入口引用 `main` 分支，根锁文件固定解析提交，不保留源码副本。
- 启动入口和工作台预算分别为 500000／4500000 bytes，没有放宽限制；当前体积指标见[交付体积分析](../../discussions/desktop-size-performance-cn.md)。
- 独立审查修复了长悬停永久覆盖表情，以及同请求跨实例恢复后因缓存淘汰退回平面的问题；静态、减少动态效果及工作状态下的指针进入不增加悬停定时器。
- 粒子生成命令与产物一致，橙、玫粉、紫在浅暗背景的 32／56／128px 离线预览经过实际图集、换色及 WebP92 编码链路。全量测试、测试文件整体类型检查、安装包发布及真实应用视觉验收未执行。
