# NeuxBot 开发约定

## 必读路由

- 所有代码和开发文档修改前阅读 [CONTEXT.md](CONTEXT.md) 与 [架构总览](docs/architecture/overview-cn.md)。
- 后端修改阅读 [后端约定](docs/development/backend-cn.md)。
- 前端修改阅读 [前端约定](docs/development/frontend-cn.md)；页面、组件、样式或交互修改还必须完整阅读 [UI 标准](docs/development/ui-cn.md)。
- 跨端协议、OpenAPI 和桌面生命周期修改同时阅读前后端约定及 [桌面边界](docs/architecture/desktop-lifecycle-cn.md)。
- 通知、实时状态、事件订阅和轮询修改阅读 [通知与工作看板](docs/architecture/notifications-and-work-board-cn.md)。
- 文档编写遵守 [文档约定](docs/development/documentation-cn.md)。

## 实现约束

- 按已确认范围实现，以当前可运行代码为基线；保留无关修改。
- 复用已有模块、共享组件和语义 Token；不增加重复的业务事实、连接或状态源。
- SQL 默认单表查询，JOIN 仅限确有必要且提供查询计划与测试证据的情况；完整硬规则见 [后端约定](docs/development/backend-cn.md#5-gorm-和-sqlite)。
- 普通 Card 使用语义背景、排版和留白分层，无常驻边框与阴影；轮廓用于选中、焦点、错误等明确状态。
- 新增或修改 Go 结构体时检查全部字段，每个字段须有准确中文注释。OpenAPI 字段采用简短业务标签。
- 生成文件只通过对应命令更新，不手工编辑或扩大检查例外。
- 注释解释当前职责、约束和设计理由；第三方版权与许可声明准确保留。
- 未经用户明确同意，不打开、控制或截图运行中的页面、浏览器和桌面应用。

## 验证

- 默认只验证本次修改点及直接受影响的行为，选择对应测试文件、用例或 Go 包；修复问题不例行运行全量测试。
- 前端按修改范围运行定向测试、类型、样式或格式检查；协议变化时校验生成契约。后端先对修改文件运行 `gofmt -w`，再定向运行相关包的检查与测试。
- 只有用户明确要求时才运行 `pnpm check`、`pnpm --dir desktop check`、全量前端测试或 `go test ./...` 等全量验证。
- 静态和自动化检查不能替代真实平台、模型与视觉验收；无法运行时说明原因与剩余边界。
