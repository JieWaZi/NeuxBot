# 知识核心独立 Go 模块维护计划

> 执行方式：当前会话实施；英文注释按互不重叠目录并行整理，交付前独立审查。

**目标：** 沿用 `github.com/JieWaZi/neux-knowledge`，以 NeuxBot 当前源码为基线独立维护，取消宿主源码快照。

**方案依据：** 用户已确认 README 明确说明基于 WeKnora 精简和微调、直接复用 Wiki 能力，保留必要许可，代码注释英文且不写来源 commit，宿主通过 Go 模块接入。

**架构：** 独立库保留现有 SDK、SQLite 与模型端口；宿主只管理模块版本和分发所需许可。本地联合开发使用不提交的 Go workspace，宿主固定引用已发布的 `v0.1.0`。

## 全局约束

- 不改运行行为、提示词、协议身份、测试数据和数据库结构。
- 当前 `third_party/neux-knowledge` 比同级独立目录新，以当前宿主源码迁出；先备份旧独立目录。
- 用户已补充授权创建公开独立仓库、提交并推送首版与发布 `v0.1.0`；用户已补充授权提交 NeuxBot 全部当前改动，不推送宿主分支。
- 独立库注释使用英文；必要版权原文保留，来源汇总到 README 与 THIRD_PARTY_NOTICES。
- 不打开运行中的应用或浏览器，不修改其他功能。

## 任务 1：整理独立库

- [x] 备份原独立目录，在临时工作区以当前宿主副本为基线整理。
- [x] README 中英文对应，说明 WeKnora 来源、官网、Wiki 复用、Go 安装与 SDK 入口。
- [x] 将原始许可全文合并进 THIRD_PARTY_NOTICES，删除 upstream、快照清单与开发过程文件。
- [x] 翻译 Go 注释，去除冗长来源头、迁移摘要和来源 commit；比较非注释 token 保证行为不变。
- [x] 更新并验证独立目录，运行 gofmt、go vet、go test、go test -race。

## 任务 2：宿主通过 Go 模块引用

- [x] 删除 go.mod 的快照 replace，同步构建检查和当前架构说明。
- [x] 宿主 THIRD_PARTY_NOTICES 仅声明 neux-knowledge 依赖；sidecar 构建从模块复制完整许可并随包交付。
- [x] 删除同步脚本、对应测试和 third_party 源码副本。
- [x] 本地 Go workspace 使用独立目录，不把个人路径写入受版本管理的配置。
- [x] 关闭 workspace 后从远程下载并校验真实 `v0.1.0`，更新宿主 go.mod/go.sum。

## 任务 3：验收与交付

- [x] 核对源代码非注释 token、嵌入资源和许可原文未丢失。
- [x] 运行文档与完整检查链：API可重复生成、前端全部检查、1926项前端测试、构建与71项Electron测试通过；整链后端曾受并行错误码生成及运行时探测超时影响，关闭workspace补跑完整go vet和go test通过。
- [x] 独立审查模块接入、许可随包交付和残留引用，未发现额外问题。
- [x] 获得公开发布授权，独立仓库 main 与正式 `v0.1.0` Release 已发布。

## 交付核对

- 正式仓库与 Release：`JieWaZi/neux-knowledge`，`v0.1.0`。
- 独立库 go vet、go test、go test -race 全部通过；发布包230个Go文件的非注释token与迁出基线一致。
- 宿主关闭 workspace 后从 Go 模块缓存读取远程 v0.1.0，go.mod/go.sum 固定真实版本与校验值。
- 临时 go.work 已删除；旧独立目录的完整源码备份保留在本地忽略的诊断目录。
- 并行出现的目录删除错误码与映射改动按用户补充授权一并提交，保持源码、错误枚举与 API 生成文件一致。

- 提交前 `NODE_OPTIONS=--no-experimental-webstorage pnpm check` 整链通过；许可复制已验证干净模块缓存和重复构建，实际 sidecar 构建通过。
