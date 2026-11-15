# Storage Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本次仅编写文档；执行方式在开始实施时由用户选择。

**Goal:** 提供真实可核对的存储统计、缓存清理与默认关闭的闲置会话自动删除，并修复已确认的受管文件回收缺口。

**Architecture:** Core 统一计量、策略、维护状态及多入口编排，各领域继续拥有文件引用、租约和删除。Electron 管理自身缓存与日志；桌面字节附件复用 Core 上传。先建立两个根的独占与恢复边界，再接入资源回收、条件会话删除及共享设置页。

**Tech Stack:** Go、GORM/SQLite、ginx/toolx、Electron、TypeScript、React、TanStack Query、Vitest、Node test；不新增第三方依赖。

**Spec:** [存储管理与受管文件回收设计](../specs/2026-11-15-storage-management-design.md)

## Global Constraints

- 本次仅交付 spec 与实施计划，不实施业务代码或清理真实数据。
- 实施保留无关修改；生成文件只通过对应命令更新，不手改生成文件。
- 不打开、控制或截图运行中的页面、浏览器和桌面应用，真实视觉验收需要用户明确授权。
- 只运行修改点与直接影响范围的定向验证；不运行 pnpm check、pnpm --dir desktop check、全量前端测试或 go test ./...。
- 不新增第三方依赖，不改业务 SQLite 模式，不引入重复状态源或新的业务连接。
- SQL 默认单表；关联存在性使用有索引的 EXISTS/NOT EXISTS 与有界批量补齐，不新增无证据 JOIN。
- 智能体发送、续聊、steering、恢复和分叉不增加账号、额度或模型目录诊断。
- 所有 Go 结构体字段有准确注释；internal/agent 使用英文，其余目录使用中文，API 标签保持简短。
- 未知归属、活动引用和恢复记录保守保留；不以路径名称或文件年龄单独授权删除。

---

## 阅读与执行顺序

执行前阅读根 AGENTS.md、CONTEXT.md、架构总览、工作区与存储、前后端约定、UI 标准、桌面生命周期、通知与工作看板。后端先 gofmt 再运行目标包；前端只检查列出的变更文件和目标测试。新增的 Go 模型/枚举/错误统一遵守项目生成流程。

任务依赖为：1 → 2；1 → 3、4、5、6、7、8；2–8 → 11；9 → 10；10、11 → 12 → 13。按编号执行也成立。每项先补失败回归，再改对应所有者；已有正常路径测试继续保留。所有文件样例都使用临时目录，不指向用户真实数据。

代码块是实现契约和关键分支，不是新的通用框架。测试中的临时路径准备、错误检查和 import 按所在测试文件的现有模式写全。每个任务列出的新接口由该任务定义，后续任务按相同名字使用。

## 文件职责与接口地图

| 任务 | 新增或扩展的职责                           | 后续依赖                    |
| ---- | ------------------------------------------ | --------------------------- |
| 1    | localweb 双根独占、共享启动根解析          | 全部文件写入所有者          |
| 2    | internal/storage 有界测量与根去重          | 3–6、11 的测量结果          |
| 3    | builtin 系统技能无引用回收                 | 11 资源维护                 |
| 4    | capability 授权描述精确释放和恢复          | execution 生命周期          |
| 5    | plugins 派生目标占用与 catalog 清理        | 11 缓存维护                 |
| 6    | installation 已核验旧程序与 staging 回收   | runtimeenvironment 维护围栏 |
| 7    | shared/api 上传字节客户端，Native/Web 共用 | chat 附件                   |
| 8    | Electron 缓存与日志所有者                  | 12 可选宿主操作             |
| 9    | Chat 权威保留选择、活动投影与候选查询      | 10 条件删除                 |
| 10   | Chat 条件删除、storage 策略与后台保留调度  | 11 API、12 UI               |
| 11   | storage 测量快照、清理预览/运行、API/事件  | 12 UI                       |
| 12   | Settings 存储入口与会话保留操作            | 用户交互                    |
| 13   | 故障矩阵、契约生成、当前架构文档           | 交付证据                    |

## Task 1: 统一根解析并锁住两个真实目录

**Files:**

- Modify: `server/internal/localweb/lock.go`, `server/internal/localweb/session_test.go`, `server/cmd/bot-core/main.go`, `server/global/config.go`, `server/global/config_test.go`
- Create: `scripts/core-data-paths.mjs`, `scripts/core-data-paths.test.mjs`
- Modify: `scripts/dev-server.mjs`, `scripts/start-web.mjs`, `scripts/start-web.test.mjs`

**Interfaces:**

- Consumes: `global.Configuration.DataDirectory`, `ManagedFilesDirectory`，现有 `Prepare` 解析和数据锁。
- Produces: `localweb.LockStorageRoots(dataDirectory, managedDirectory string) (func() error, error)`；`defaultCoreDataDirectory(inherited, platform, home)` 移入共享启动模块并保持现有调用语义。

- [ ] **Step 1: 添加会失败的双根与别名回归。**

```go
func TestStorageRootsRejectAnotherDataRootSharingManagedRoot(t *testing.T) {
    data, other, managed := t.TempDir(), t.TempDir(), t.TempDir()
    release, err := LockStorageRoots(data, managed)
    if err != nil { t.Fatal(err) }
    defer release()
    second, err := LockStorageRoots(other, managed)
    if err == nil {
        _ = second()
        t.Fatal("shared managed root admitted a second writer")
    }
}
```

另测两个参数相同、根别名、第二把锁失败后第一把锁释放、初始化失败/退出幂等释放。Node 测试覆盖 darwin/win32/linux 默认值及显式环境覆盖。

- [ ] **Step 2: 运行目标回归，确认缺少双根 API 或现有行为允许第二个写入者。**

Run in `server`: `go test ./internal/localweb -run 'TestStorageRoots' -count=1`。

- [ ] **Step 3: 实现规范化、独立锁和默认路径复用。**

```go
// LockStorageRoots 在每个真实存储根获取同名进程锁，重复根仅获取一次。
func LockStorageRoots(dataDirectory, managedDirectory string) (func() error, error)
```

按规范路径排序后获取每根的 `core.lock`，失败逆序释放；不要只锁两根组合的哈希，否则共享其中一根仍能并发。Core 在恢复、上传、数据库装配前持锁，最后释放。dev-server 使用共享默认程序根，不改变显式配置，不移动相对旧库。

- [ ] **Step 4: gofmt 后验证目标范围。**

Run in `server`: `go test ./internal/localweb ./global -run 'TestStorageRoots|TestPrepare|TestRunLoadSequence' -count=1`。

Run at root: `node --test scripts/core-data-paths.test.mjs scripts/start-web.test.mjs`。

- [ ] **Step 5: 仅提交本任务文件。**

Suggested commit: `fix(storage): enforce ownership of both storage roots`。

## Task 2: 有界测量、分类、去重和磁盘身份

**Files:**

- Create: `server/internal/storage/filesystem.go`, `server/internal/storage/filesystem_test.go`
- Create: `server/internal/storage/disk_unix.go`, `server/internal/storage/disk_windows.go`
- Modify: `server/constants/common.go`, `server/constants/status_error/error.go`
- Create: `server/constants/storage_category.go`

**Interfaces:**

- Produces: `storage.Roots`、`storage.Limits`、`storage.FileEntry`、`storage.Measurement`，`Measure(ctx context.Context, roots Roots, limits Limits) (Measurement, error)`。
- `Roots` 字段为 `Data`、`Managed`；`Limits` 字段为 `MaxEntries`、`MaxDepth`、`Timeout`。
- `FileEntry` 字段为 `Path`、`OwnerRoot`、`Bytes`、`Category`；路径仅供 Core 所有者与 API 授权后的展示使用。
- `Measurement` 字段为 `Bytes`、`Files`、`Complete`、`Entries`、`Problems`、`Disks`；问题包含真实操作、稳定错误和有界相对位置，不返回凭据或文件正文。

- [ ] **Step 1: 添加总量与根重叠的失败测试。**

```go
func TestMeasureDeduplicatesNestedRoots(t *testing.T) {
    root := t.TempDir()
    managed := filepath.Join(root, "managed")
    if err := os.Mkdir(managed, 0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(managed, "item"), []byte("abc"), 0600); err != nil { t.Fatal(err) }
    got, err := Measure(t.Context(), Roots{Data: root, Managed: managed}, Limits{MaxEntries: 100000, MaxDepth: 64, Timeout: time.Second})
    if err != nil { t.Fatal(err) }
    if !got.Complete || got.Bytes != 3 || got.Files != 1 { t.Fatalf("measurement=%+v", got) }
}
```

再测内部软链接/环、硬链接、特殊文件、权限拒绝、扫描中消失、共享总预算、64 层和 30 秒取消；不支持的 OS 能力采用专门平台测试，不能以空成功代替。

- [ ] **Step 2: 运行 `go test ./internal/storage -run 'TestMeasure' -count=1`，确认红灯。**
- [ ] **Step 3: 实现遍历与分类。**

```go
if entry.Type()&os.ModeSymlink != 0 { continue }
if ctx.Err() != nil { return result, status_error.OperationCanceled }
```

使用按批 ReadDir、Lstat 和真实文件身份，全扫描共享计数。根规范化并合并重叠遍历，分类使用实际归属；未知文件计 other。文件存在性和读取失败分开，超限保留小计。平台磁盘读取只报告本卷 free/total，不新增数据库模式或依赖。系统技能物化分类读取既有 manifest 并核对路径身份，不能把所有 `.cursor` 文件直接排除为程序资源。

- [ ] **Step 4: 生成枚举/错误并验证。**

新增 `StorageCategory` 及稳定错误时使用项目 toolx 流程；统一在 Task 11 执行 `pnpm api:generate`，此前需要类型时使用仓库锁定 toolx 的对应 go generate，禁止手写 generated。

Run in `server`: `go test ./internal/storage -run 'TestMeasure|TestDisk' -count=1`。

- [ ] **Step 5: 提交本任务文件。** Suggested commit: `feat(storage): measure managed roots with bounded scans`。

## Task 3: 回收旧系统技能摘要，保护当前版本

**Files:**

- Modify: `server/internal/skills/builtin/system.go`, `server/internal/skills/builtin/system_test.go`, `server/services/execution/service.go`, `server/global/config.go`

**Interfaces:**

- Produces: `builtin.CollectSystemDirectories(dataDirectory, currentDirectory string, protectedDirectories []string) error`。
- Consumes: `SystemDirectory` 当前发布路径；`protectedDirectories` 来自真实使用者，不持久化第二套引用事实。

- [ ] **Step 1: 扩展现有 SystemDirectory 测试，确认两次不同 CLI 会保留两个不可变版本，再增加显式回收断言。**

```go
if err := CollectSystemDirectories(data, second, []string{first}); err != nil { t.Fatal(err) }
if _, err := os.Stat(CLIPath(first)); err != nil { t.Fatal("active version removed", err) }
if err := CollectSystemDirectories(data, second, nil); err != nil { t.Fatal(err) }
if _, err := os.Stat(first); !os.IsNotExist(err) { t.Fatalf("old version remains: %v", err) }
if _, err := os.Stat(CLIPath(second)); err != nil { t.Fatal(err) }
```

同一测试中 `first`、`second` 沿用 `TestSystemSkillVersionsCLIAndRejectsTampering` 的临时二进制构建方式；不要删除其篡改检测用例。再覆盖同 CLI 重启不新建版本、未知目录与软链接保留、staging 仅在启动无所有者时回收。

- [ ] **Step 2: 运行 `go test ./internal/skills/builtin -run 'TestSystemSkill' -count=1`，观察新回归失败。**
- [ ] **Step 3: 添加归属核验和保护集。**

```go
keep := map[string]bool{filepath.Clean(currentDirectory): true}
for _, directory := range protectedDirectories { keep[filepath.Clean(directory)] = true }
```

只回收根内摘要合法、内容核验及内部布局符合本应用系统技能的直接目录；遇到未知/改指向内容保留并记录稳定诊断。当前路径发布并验证后、Execution.Open 前执行一次；手动维护在同一当前版本保护下调用。不要把「最后修改时间最新」当成当前版本。

- [ ] **Step 4: gofmt 后运行目标包与当前初始化回归。**

Run in `server`: `go test ./internal/skills/builtin ./global -run 'TestSystemSkill|TestLoadSystemSkill' -count=1`。

- [ ] **Step 5: 提交本任务文件。** Suggested commit: `fix(skills): reclaim unused system skill versions`。

## Task 4: 授权上下文精确释放与异常退出恢复

**Files:**

- Modify: `server/internal/agent/capability/capability.go`, `server/internal/agent/capability/capability_test.go`, `server/services/execution/agent.go`, `server/global/config.go`
- Modify: `server/services/execution/native_turn_test.go`

**Interfaces:**

- Produces: `capability.RemoveConnection(path string, expected Connection) error`、`capability.RecoverConnections(directory, currentEpoch string) error`。
- Consumes: 现有 `WriteConnection`、GrantID/CoreEpoch 与 Registry 的实际生命周期。

- [ ] **Step 1: 添加精确释放、旧回调和旧 epoch 测试。**

```go
path, err := WriteConnection(directory, "connection.json", original)
if err != nil { t.Fatal(err) }
if _, err := WriteConnection(directory, "connection.json", next); err != nil { t.Fatal(err) }
if err := RemoveConnection(path, original); err != nil { t.Fatal(err) }
got, err := ReadConnection(path)
if err != nil || got.GrantID != next.GrantID { t.Fatal("old release removed next grant", err) }
```

`original` 和 `next` 使用既有 Connection 测试的有效回环地址及协议字段，GrantID 不同。再测自身释放后空父目录消失、旁文件保留、描述被改为链接、旧 epoch 与当前 epoch 混合、强制退出后重启。

- [ ] **Step 2: 运行 `go test ./internal/agent/capability -run 'TestConnection' -count=1`，确认新语义未满足。**
- [ ] **Step 3: 用 grant/epoch 比对替代盲删，撤销后精确 Remove 空父目录。**

```go
if actual.GrantID != expected.GrantID || actual.CoreEpoch != expected.CoreEpoch { return nil }
```

恢复只在双根锁持有且新授权尚未建立时执行。正常关闭失败保留原能力/文件所有者；失败返回与重试不丢失待清理身份。internal/agent 内所有新增字段与注释使用英文。

- [ ] **Step 4: 验证能力和连续回合回归。**

Run in `server`: `go test ./internal/agent/capability ./services/execution -run 'TestConnection|TestNativeProduct' -count=1`。仅选此次实际影响的 NativeProduct 子用例；不得为目录回收增加 status/模型探测。

- [ ] **Step 5: 提交本任务文件。** Suggested commit: `fix(execution): release owned capability context directories`。

## Task 5: 插件派生缓存和分发缓存回收

**Files:**

- Modify: `server/internal/plugins/content_store.go`, `server/internal/plugins/content_store_test.go`, `server/internal/plugins/target_cache.go`, `server/internal/plugins/target_cache_review_test.go`
- Modify: `server/internal/plugins/catalog/index_cache.go`, `server/internal/plugins/catalog/catalog.go`, `server/internal/plugins/catalog/details.go`, `server/internal/plugins/catalog/catalog_test.go`
- Modify: `server/services/execution/plugins.go`, `server/apis/plugins/install_preparation.go`, `server/apis/plugins/prepare_catalog_plugin.go`

**Interfaces:**

- Produces: `plugins.TargetRequest`，字段 `Parent`、`PluginID`、`SourceRoot`、`Runtime`、`SourceDigest`、`Package`。
- Produces: `(*ContentStore).PrepareTarget(ctx context.Context, request TargetRequest) (NativeTarget, func(), error)`、`(*ContentStore).CollectTargets(ctx context.Context) error`。
- Produces: `catalog.ClearCachedArtifacts(ctx context.Context, source string) error`、`catalog.PruneUnreferencedArtifacts(ctx context.Context, source string) error`。
- Release 由现有 ProviderRuntime 资源 CloseFiles 持有；API 安装准备结束或失败释放。无秘密 targets 不增加数据库安装状态。

- [ ] **Step 1: 写活动目标、准备窗口及 repair 的失败回归。**

```go
target, release, err := store.PrepareTarget(t.Context(), request)
if err != nil { t.Fatal(err) }
if err := store.CollectTargets(t.Context()); err != nil { t.Fatal(err) }
if _, err := os.Stat(target.Root); err != nil { t.Fatal("leased target deleted", err) }
release()
release()
if err := store.CollectTargets(t.Context()); err != nil { t.Fatal(err) }
if _, err := os.Stat(target.Root); !os.IsNotExist(err) { t.Fatalf("unused target remains: %v", err) }
```

沿用 `standardInput` 创建完整插件 fixture，store 使用 NewContentStore，request 指向临时原包。Catalog 使用现有 httptest 分发 fixture：清理与预取/安装并发、当前索引保留、清理后无立即下载循环、失效历史归档可回收。

- [ ] **Step 2: 运行 `go test ./internal/plugins ./internal/plugins/catalog -run 'TestTarget|Test.*CachedArtifacts|Test.*Prune' -count=1` 并确认失败原因。**
- [ ] **Step 3: 将派生发布到实际交付的占用窗口接入同一保护。**

```go
target, release, err := contents.PrepareTarget(ctx, request)
if err != nil { return result, err }
releases = append(releases, release)
result.targets = append(result.targets, target)
```

所有当前 PrepareTargets/PrepareNativeTarget 调用迁入受保护入口；内容 Guard 内不能递归获取同一 mutex。使用私有 under-lock 辅助方法完成登记，公开释放幂等。归属未核验目录保留，锁文件不按年龄删除。正常 release 仅解除占用，不立即删除可复用缓存。

- [ ] **Step 4: 在 catalog 原所有者中暂停预取、等待在途、清理并恢复可用性。**

```go
if index.archiveFlight != nil {
    select { case <-index.archiveFlight.done: case <-ctx.Done(): return status_error.OperationCanceled }
}
```

等待发生在锁外；失效历史包按当前索引引用判断；手动清理保留轻索引。当前 Core 不立即重新预取，下一次启动、显式刷新或实际安装恢复下载。变更目录时使旧 artifacts 列表失效，不取消已接纳业务。

- [ ] **Step 5: gofmt 后验证插件目标、安装准备和实际执行资源释放。**

Run in `server`: `go test ./internal/plugins ./internal/plugins/catalog ./apis/plugins ./services/execution -run 'TestTarget|TestContentStore|Test.*CachedArtifacts|Test.*Prune|Test.*Preparation|TestNativeProductAcquiresReservedPluginSkills' -count=1`。

- [ ] **Step 6: 提交本任务文件。** Suggested commit: `fix(plugins): reclaim unused derived and distribution caches`。

## Task 6: 受管 CLI 旧版本及安装中断残留

**Files:**

- Modify: `server/internal/agent/installation/maintenance.go`, `server/internal/agent/installation/maintenance_test.go`, `server/internal/agent/installation/installer.go`, `server/internal/agent/installation/installer_test.go`
- Modify: `server/services/runtimeenvironment/installation.go`, `server/services/runtimeenvironment/installation_test.go`, `server/services/runtimeenvironment/service.go`

**Interfaces:**

- Produces: `(*Installer).CollectVersions(ctx context.Context, runtimeID string, selected []Result) error`、`(*Installer).RecoverStaging(ctx context.Context, runtimeID string) error`。
- Produces: `runtimeenvironment.Service.CollectProgramFiles(ctx context.Context) error`，内部逐基础 Runtime 取得已有维护围栏。
- Consumes: 现有 `BeforeRuntimeMaintenance`、`managedFixture`、全部环境配置实际 Binary/PrefixArgs。

- [ ] **Step 1: 用已有 managedFixture 创建三个版本，保留选中旧版与最新有效版，断言仅中间版可回收。**

```go
installer, install := managedFixture(t)
selected := install("1.2.3")
unused := install("1.2.4")
latest := install("1.2.5")
if err := installer.CollectVersions(t.Context(), "codex", []Result{selected}); err != nil { t.Fatal(err) }
if _, err := os.Stat(selected.Binary); err != nil { t.Fatal(err) }
if _, err := os.Stat(latest.Binary); err != nil { t.Fatal(err) }
if _, err := os.Stat(unused.Binary); !os.IsNotExist(err) { t.Fatalf("unused version remains: %v", err) }
```

另测多个环境引用、损坏 receipt、外部安装、仍忙碌的程序、关闭失败、安装成功但配置提交失败。子进程测试在 staging 写入后强制退出，再启动恢复，确认未知目录与卸载日志保留。

- [ ] **Step 2: 运行 `go test ./internal/agent/installation -run 'Test.*CollectVersions|Test.*RecoverStaging' -count=1`，确认红灯。**
- [ ] **Step 3: 在安装器所有权核验和自身维护锁内回收。**

```go
release, err := s.beforeRuntimeMaintenance(ctx, base)
if err != nil { return err }
defer release()
```

服务在同一围栏内重新读取全部环境配置，传入实际选中版本。发布新版本且数据库配置提交成功后调用；提交失败不动旧引用。创建 staging 时保存操作身份并持有操作锁；恢复必须核对仍存活的安装子进程与操作锁，未标记的历史残留仅诊断。启动无活动操作时恢复已核验 staging；不以扫描结果替代操作所有者。无需访问官方网络或启动 CLI。

- [ ] **Step 4: 验证安装、恢复与账号保护。**

Run in `server`: `go test ./internal/agent/installation ./services/runtimeenvironment ./services/aiaccount -run 'Test.*CollectVersions|Test.*RecoverStaging|TestManagedUninstall|Test.*Installation|TestProgramMaintenance' -count=1`。

- [ ] **Step 5: 提交本任务文件。** Suggested commit: `fix(runtime): collect unreferenced managed program files`。

## Task 7: 桌面字节附件复用 Core 上传租约

**Files:**

- Create: `packages/frontend/src/shared/api/uploads.ts`, `packages/frontend/src/shared/api/uploads.test.ts`
- Modify: `desktop/src/platform/index.ts`, `web/src/platform/host.ts`, `packages/frontend/src/shared/platform/index.ts`, `packages/platform-contracts/src/services.ts`
- Modify: `desktop/electron/shared/contracts.ts`, `desktop/electron/shared/validation.ts`, `desktop/electron/preload/index.ts`, `desktop/electron/main/desktop-services.ts`, `desktop/electron/tests/bridge.test.mjs`
- Remove: `desktop/electron/main/attachment-staging.ts`, `desktop/electron/tests/attachment-staging.test.mjs`，其字节、名称、并发与释放用例迁移到共享客户端和 Core Store 测试。
- Modify: `server/internal/uploads/store.go`, `server/internal/uploads/store_test.go`, `packages/frontend/src/modules/chat/runtime/chat-attachment-adapter.test.ts`
- Create: `desktop/src/platform/uploads.test.ts`

**Interfaces:**

- Produces: `stageCoreUpload(name: string, bytes: Uint8Array): Promise<string>`、`discardCoreUpload(sourcePath: string): Promise<void>`。
- Consumes: 生成的 UploadResponse 类型、DeleteUpload 客户端和同一已配置 client 的原始二进制 POST，现有运行时认证、语言头、CSRF 与 ApiError 适配；当前 StageUpload 的 OpenAPI 未声明二进制 body，不伪造它的生成参数类型。
- HostService 保留业务层 stage_attachment/discard_attachment；DesktopBridge 不再暴露字节暂存命令，真实文件选择能力不变。

- [ ] **Step 1: 添加桌面上传接纳与移除失败回归。**

```ts
it("原生文件路径不会进入上传删除接口", async () => {
  const request = vi.spyOn(client, "delete");
  await discardCoreUpload("/workspace/report.txt");
  expect(request).not.toHaveBeenCalled();
});
```

共享客户端测试通过现有 MSW 模式模拟 StageUpload/DeleteUpload，断言返回 upload 引用、Bearer 或 CSRF、语言头、取消与响应失败。桌面适配测试断言不再调用 bridge.stage_attachment，移除 upload 引用调用 DELETE。

- [ ] **Step 2: 运行 `pnpm --dir packages/frontend exec vitest run src/shared/api/uploads.test.ts src/modules/chat/runtime/chat-attachment-adapter.test.ts`，确认桌面释放缺口。**
- [ ] **Step 3: 提取 Web 上传逻辑到共享 API 适配器，并接入两个宿主。**

```ts
host: {
  stage_attachment: ({ name, bytes }) => stageCoreUpload(name, bytes),
  discard_attachment: ({ sourcePath }) => discardCoreUpload(sourcePath),
}
```

使用同一已配置 client 的二进制 post 和生成响应类型，Delete 使用生成 SDK；不新增裸 fetch 的第二份连接状态。真实本机路径和拖入文件继续原授权流程，不能在 discard 中删除。DesktopCommands 使用 `Omit<HostCommands, "stage_attachment">`，HostService 仍保留 stage_attachment，由各宿主注入共享上传实现。移除字节暂存 IPC、Schema、preload 和窗口暂存对象，检查所有引用后删除文件。API 包目录要与 frontend exports 显式出口一致，不能从 shared 导入宿主实现。

- [ ] **Step 4: 补充 Core 删除失败可重试证据。**

给 Store 注入仅用于文件删除的窄端口与 Fake 时钟，删除失败保留待清理身份/预算并可再次释放或由过期/Close 重试，不提前丢掉记录。旧根启动清理失败可诊断；Claim 忙碌不删，接纳后相同幂等键仍保留原路径回执。不得通过长于原租约的全局保留掩盖失败。

- [ ] **Step 5: 定向验证跨端上传。**

Run in `server`: `go test ./internal/uploads ./apis/uploads -count=1`。

Run at root: `pnpm --dir packages/frontend exec vitest run src/shared/api/uploads.test.ts src/modules/chat/runtime/chat-attachment-adapter.test.ts`；`pnpm --dir desktop exec vitest run src/platform/uploads.test.ts`；`node --test desktop/electron/tests/bridge.test.mjs`。

- [ ] **Step 6: 提交本任务文件。** Suggested commit: `fix(uploads): share managed attachment lifetime across hosts`。

## Task 8: 桌面缓存与日志的真实所有者

**Files:**

- Create: `desktop/electron/main/storage-maintenance.ts`, `desktop/electron/tests/storage-maintenance.test.mjs`
- Modify: `desktop/electron/main/index.ts`, `desktop/electron/main/desktop-services.ts`, `desktop/electron/shared/contracts.ts`, `desktop/electron/shared/validation.ts`, `desktop/electron/preload/index.ts`, `desktop/src/platform/index.ts`, `packages/platform-contracts/src/services.ts`
- Modify: `desktop/electron/tests/bridge.test.mjs`, `desktop/electron/tests/core.test.mjs`

**Interfaces:**

- Produces: `createStorageMaintenance(options)` 返回 `capabilities()`、`clearCache()`、`writeLog(message: string)`、`clearLogs()`、`close()`。
- `options` 包含固定 `dataDirectory`、受信 `coreConnection`、`session` 端口、时钟与文件操作测试端口。
- Bridge 新增 `storage_capabilities(): Promise<{ cache: boolean; logs: boolean }>`、`clear_storage_cache(): Promise<void>`、`clear_storage_logs(): Promise<void>`；均无路径输入。
- 前端按已确认类别分别调用 Core 维护与固定宿主能力，无主进程所有者时只测量桌面日志，不从 Go 与写入者竞争删除；没有 Core 到 Electron 的反向 RPC。

- [ ] **Step 1: 在临时根添加轮转与并发清理失败回归。**

```ts
test("轮转保留份数与时间，清理后仍能写入", async () => {
  await owner.writeLog("before");
  await owner.clearLogs();
  await owner.writeLog("after");
  const text = await readFile(path.join(root, "logs", "desktop.log"), "utf8");
  assert.match(text, /after/);
  assert.doesNotMatch(text, /before/);
});
```

测试内用 createStorageMaintenance 创建 owner，Core root 和 Session 使用 Fake；时钟可控。再覆盖 10 MB 阈值、最多 5 份、超过 15 天、同一时刻多次写入、一次 I/O 失败后恢复、退出等待队列。

- [ ] **Step 2: 运行 `node --test desktop/electron/tests/storage-maintenance.test.mjs` 并确认红灯。**
- [ ] **Step 3: 将 index.ts 日志入口迁入串行所有者。**

```ts
const queue = previous.then(operation, operation);
```

实际队列实现分别保留当前操作 Promise 和下一次继续所需恢复链，失败不能吞掉当前调用错误，也不能永久毒化后续队列。轮转数量和保留期是独立上限；清理删除已核验历史并截短活动文件，不移除整个 logs 目录。

- [ ] **Step 4: 通过 Session 清 HTTP/代码缓存，主进程核对当前 Core 根。**

```ts
await activeSession.clearCache();
await activeSession.clearCodeCaches({ urls: [] });
```

只调用受信主窗口 Session；不调用清 Cookie/Local Storage/IndexedDB 的接口。对照 Core `/storage/usage` 返回根和主进程固定根的真实身份；不一致时 capabilities 返回不可用，写操作返回明确错误。调用失败不能伪造成功。日志清理能力只在持有实际写入所有者时可执行。

- [ ] **Step 5: 验证白名单、外部 Core 与关闭边界。**

Run at root: `node --test desktop/electron/tests/storage-maintenance.test.mjs desktop/electron/tests/bridge.test.mjs desktop/electron/tests/core.test.mjs`。

- [ ] **Step 6: 提交本任务文件。** Suggested commit: `feat(storage): manage desktop caches and bounded logs`。

## Task 9: 会话保留事实、工作活动投影与候选查询

**Files:**

- Modify: `server/constants/chat_event_type.go`, `server/repositories/models/chat.go`, `server/internal/chat/eventlog/types.go`, `server/internal/chat/conversation/store.go`
- Modify: `server/services/chat/chat.go`, `server/services/chat/recovery.go`, `server/services/chat/live_projection.go`, `server/services/chat/service.go`, `server/services/chat/fork.go`
- Modify: `server/repositories/controllers/chat_controller.go`, `server/repositories/controllers/chat_controller_test.go`
- Create: `server/services/chat/retention_test.go`, `server/repositories/controllers/chat_retention_test.go`

**Interfaces:**

- Produces: `models.Chat.RetentionProtected bool`、`LastActivityAt int64`；`chat.Service.SetRetentionProtection(ctx context.Context, chatID string, protected bool) error`。
- Produces: `controllers.ChatRetentionCandidate{ChatID string, Sequence int64, LastActivityAt int64}`；`ListRetentionCandidates(ctx context.Context, cutoff int64, afterActivity int64, afterID string, limit int) ([]ChatRetentionCandidate, error)`；`CountRetentionCandidates(ctx context.Context, cutoff int64) (int64, error)`。
- 新权威事件为 `chat.retention_protection_updated`，加入既有 ChatEventType 并生成。字段只投影，不新增第二套会话配置表。

- [ ] **Step 1: 通过原日志创建临时会话，增加活动/元数据区别的失败测试。**

```go
before := summary.Chat.LastActivityAt
if err := chats.SetRetentionProtection(t.Context(), chatID, true); err != nil { t.Fatal(err) }
after, err := chats.GetChatSummary(t.Context(), chatID)
if err != nil { t.Fatal(err) }
if after.Chat.LastActivityAt != before { t.Fatal("metadata renewed work activity") }
if !after.Chat.RetentionProtected { t.Fatal("protection not projected") }
```

测试沿用 newDeleteTestService 和已有 Chat 创建 fixture，ChatSummary.Chat 是现有元数据字段；断言重建数据库投影后保留选择、标题编辑/阅读不续期、原生 checkpoint/终态推进、分叉子例外为 false，不为测试另建生产包装器。

- [ ] **Step 2: 用真实 SQLite 写候选查询回归。**

```go
cutoff := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC).UnixMilli()
rows := []models.Chat{
    {ID: "old", WakerID: "partner", LastActivityAt: cutoff-1},
    {ID: "boundary", WakerID: "partner", LastActivityAt: cutoff},
    {ID: "protected", WakerID: "partner", LastActivityAt: cutoff-1, RetentionProtected: true},
    {ID: "busy", WakerID: "partner", LastActivityAt: cutoff-1, ActiveExecutionID: "active"},
}
if err := db.Create(&rows).Error; err != nil { t.Fatal(err) }
```

使用 newTestDB，补入团队、创作、自动任务、IM 对应表记录；期望只有 old。覆盖 hidden=false 但有领域引用、零活动时间、稳定游标与准确计数，规模增长验证查询计划与查询次数，不逐行补齐。

- [ ] **Step 3: 运行 `go test ./services/chat ./repositories/controllers -run 'Test.*Retention|Test.*Activity' -count=1`，确认红灯。**
- [ ] **Step 4: 增加权威事实及索引重建。**

```go
if contributesWorkActivity(record) {
    projection.chat.LastActivityAt = max(projection.chat.LastActivityAt, record.RecordedAt)
}
```

`contributesWorkActivity` 就近放在 Chat 投影职责文件，明确覆盖实际输入、执行、消息/原生推进及审批回答，排除保护、标题等元数据。正常投影与恢复共用同一规则。候选使用 LastActivityAt 的合适索引、单表条件和 NOT EXISTS。新增布尔缺省 false，老日志活动可重建，无法核验的会话不进候选。

- [ ] **Step 5: gofmt、生成枚举后运行目标回归。**

Run in `server`: `go test ./services/chat ./internal/chat/conversation ./repositories/controllers -run 'Test.*Retention|Test.*Activity|Test.*Recovery|Test.*Fork' -count=1`；选择直接受新增事件与字段影响的已有用例。

- [ ] **Step 6: 提交本任务文件。** Suggested commit: `feat(chat): persist retention protection and work activity`。

## Task 10: 条件删除、策略存储和可取消的后台保留任务

**Files:**

- Modify: `server/services/chat/chat.go`, `server/services/chat/service.go`, `server/services/chat/retention_test.go`, `server/internal/chatstorage/operations.go`, `server/internal/chatstorage/operations_test.go`
- Modify: `server/services/im/inbox.go`, `server/apis/im/select_im_conversation.go`, `server/services/channel/thread.go`, `server/apis/workflows/send_creation_message.go`, `server/services/schedule/runs.go`；修改 IM 的 SaveLink/目标更新和其他领域接纳的实际引用提交边界，禁止只修改查询端。
- Create: `server/services/storage/service.go`, `server/services/storage/retention.go`, `server/services/storage/retention_test.go`
- Modify: `server/global/config.go`, `server/constants/common.go`, `server/constants/status_error/error.go`

**Interfaces:**

- Produces: `chat.Service.DeleteChatIfExpired(ctx context.Context, candidate controllers.ChatRetentionCandidate, cutoff int64, unreferenced func(context.Context, string) (bool, error)) (bool, error)`。
- Produces: `storage.Policy{Enabled bool, RetentionDays int, Revision int64}`；`storage.Service.ReadPolicy() (Policy, error)`、`UpdatePolicy(ctx context.Context, expectedRevision int64, enabled bool, days int) error`、`Start(ctx context.Context) error`、`Close() error`。
- Produces: `storage.RetentionBatchResult`，字段 `Deleted`、`Skipped`、`Failed` 和有界 `Reasons`；`RunRetention(ctx context.Context, candidates []controllers.ChatRetentionCandidate, cutoff int64) (RetentionBatchResult, error)`。
- 新 Service 通过 Config 接收原 Chat、候选 Controller、原子文件写、事件、时钟/定时器和资源端口；后台与多个 API 复用同一用例。

- [ ] **Step 1: 添加预览之后新活动、保护、绑定与删除交错测试。**

```go
deleted, err := chats.DeleteChatIfExpired(ctx, preview, cutoff, unreferenced)
if err != nil { t.Fatal(err) }
if deleted { t.Fatal("changed chat was deleted") }
if _, err := chats.GetChatSummary(ctx, preview.ChatID); err != nil { t.Fatal(err) }
```

每个用例在预览与删除之间分别提交新输入、保护选择或领域绑定；通过通道控制持有排他门的时机。断言原 CancelAndWait 从未被调用、伙伴 cwd/真实项目保留、共享原生与分叉引用保留。重用原删除事务失败与 staging 恢复 fixture。

- [ ] **Step 2: 添加策略和调度的 Fake 时钟失败测试。**

```go
if err := service.UpdatePolicy(ctx, 0, false, 15); err != nil { t.Fatal(err) }
clock.Advance(48*time.Hour)
if queries.Load() != 0 { t.Fatal("disabled policy scanned candidates") }
```

在同文件实现 fakeClock，提供 Now、After、Advance，测试等待通过通道屏障，不能真实 sleep 24 小时。覆盖默认关闭、1/3650 边界、损坏配置不覆盖、revision 冲突、开启时预览、缩短周期确认、重启只补一次、关闭策略取消下一批、Core Close 等待安全提交、手动/自动互斥。

- [ ] **Step 3: 运行 `go test ./services/chat ./services/storage -run 'Test.*Retention|Test.*Expired|Test.*Policy' -count=1`，确认红灯。**
- [ ] **Step 4: 扩展原删除内部围栏，在 staging 前做权威条件核验。**

```go
if state.chat.RetentionProtected || state.chat.ActiveExecutionID != "" ||
   state.chat.LastActivityAt >= cutoff || state.chat.LatestSequence != candidate.Sequence {
    return false, nil
}
```

同时检查查询投影完整、LastActivityAt 有效和当前领域引用。把现有 deleteChatsAtomically 的内部 prepare/校验职责扩展为持门回调，复用后续 staging/事务/提交机制；不能外层拿门再调用会重复拿门的方法。引用创建、切换、RenameChat 与保护等会话元数据入口也持同门，并发冲突按稳定错误处理。标题不续工作活动时间，但仍不能在文件已暂存后向旧目录写新事件。

- [ ] **Step 5: 原子保存唯一策略并调度。**

```go
cutoff := now.Add(-time.Duration(policy.RetentionDays)*24*time.Hour).UnixMilli()
```

策略文件保留 revision 与最近结果，采用临时文件、Sync、原子替换，失败保留旧文件。启动在领域恢复之后；每 24 小时检查，按稳定游标每批最多 256 个，不因跳过而陷入同一页死循环。每批前核验 policy revision；最后结果与原删除日志职责分开。Close 在 Chat/DB 之前取消并等待。

- [ ] **Step 6: 验证条件删除及恢复。**

Run in `server`: `go test ./services/storage ./services/chat ./internal/chatstorage -run 'Test.*Retention|Test.*Expired|Test.*Policy|TestDeleteChat|TestSharedAdmissions' -count=1`。

- [ ] **Step 7: 提交本任务文件。** Suggested commit: `feat(storage): clean expired chats through guarded deletion`。

## Task 11: 快照、清理预览、维护 API 与事件

**Files:**

- Create: `server/services/storage/usage.go`, `server/services/storage/maintenance.go`, `server/services/storage/usage_test.go`, `server/services/storage/maintenance_test.go`
- Modify: `server/services/storage/service.go`, `server/global/config.go`, `server/apis/root.go`
- Create: `server/apis/storage/router.go`, `get_usage.go`, `get_entities.go`, `preview_cleanup.go`, `start_cleanup.go`, `get_maintenance.go`, `get_policy.go`, `update_policy.go`, `list_expired_chats.go`, `cleanup_expired_chats.go`, `update_chat_protection.go`, `storage_test.go`，均位于 `server/apis/storage/`
- Modify: `server/apis/events/protocol.go`, `server/apis/events/stream_test.go`, `packages/platform-contracts/src/core-event-protocol.ts`
- Create: `server/constants/storage_cleanup_kind.go`，必要稳定错误加入原 status_error。

**Interfaces:**

- Produces: Service 的 `Usage(ctx context.Context, force bool) (Snapshot, error)`、`PreviewCleanup(ctx context.Context, kinds []constants.StorageCleanupKind) (CleanupPreview, error)`、`StartCleanup(ctx context.Context, previewID string) (string, error)`、`Maintenance() MaintenanceState`。
- `Snapshot` 字段为 `Generation`、`MeasuredAt`、`Measurement`，测量数据引用 Task 2 类型。实体排名由已读路径和有界批量元数据补齐，不反复读全部消息。
- `CleanupPreview` 字段为 `ID`、`ExpiresAt`、`Generation`、`Items`；Items 包含固定种类、已知大小、可执行性和稳定跳过原因。
- `MaintenanceState` 字段为 `ID`、`Revision`、`StartedAt`、`FinishedAt`、`CompletedKinds`、`FailedKinds`、`RetentionResult`；HTTP 分页 DTO 在 API 包定义。
- `storage` 事件只允许 global 资源；前端按运行 revision 与快照代次对账。

- [ ] **Step 1: 添加缓存代次和多请求合并失败回归。**

```go
first := make(chan struct{})
release := make(chan struct{})
```

在 Fake 测量端口中用 first/release 屏障挂起旧代扫描，清理推进代次再释放，断言旧结果没有回填。并发 20 个读请求只执行一次；60 秒内命中、过期或 force 重测；单个等待者取消不取消其他等待者；Close 释放全部 goroutine。

- [ ] **Step 2: 添加清理预览与部分成功失败测试。**

```go
preview, err := service.PreviewCleanup(ctx, requestedKinds)
if err != nil { t.Fatal(err) }
runID, err := service.StartCleanup(ctx, preview.ID)
if err != nil || runID == "" { t.Fatal("cleanup not admitted", err) }
```

端口使用明确 Fake：插件成功、程序忙碌、日志能力不可用；断言按类结果、失败后统计失效、同预览重复提交返回原运行或明确失效、过期/重启预览不可用、无任意路径入口、活动项执行前重新校验。

- [ ] **Step 3: 运行 `go test ./services/storage ./apis/storage -count=1`，确认新契约失败。**
- [ ] **Step 4: 实现共享快照和所有者编排。**

```go
generation := s.generation
measured, err := s.measure(ctx)
if err == nil && generation == s.generation { s.cached = measured }
```

实际实现必须在同一同步边界比较和写缓存；大扫描/文件删除放锁外。Core 类别端口调用 Tasks 3–7 的所有者，Task 8 桌面类别由前端通过固定宿主能力组合；Core 直接收到桌面清理请求时返回不可用。保持 policy/清理的运行席位和请求幂等；页面断线不取消已接纳任务，Core Close 取消。

- [ ] **Step 5: 按 spec 路径添加 Operator、稳定错误和事件白名单。**

GET 输出包括完整性和分页信息；POST 清理只因后续进度展示返回运行身份，PUT 策略/保护用 CommonSuccessResponse。预览保存 5 分钟，仅 Core 内有界保存，成功或到期释放。请求体使用固定 enum，不接受 path。policy 原子写成功、维护阶段真实变化后发布事件；不逐文件广播。

- [ ] **Step 6: 生成并验证完整协议链。**

Run at root: `pnpm api:generate`，然后 `pnpm api:check`。这属于本次新增协议的生成与差异检查，不运行项目全量 check。

Run in `server`: `go test ./services/storage ./apis/storage ./apis/events -run 'Test.*Storage|Test.*Cleanup|Test.*Policy|Test.*Retention|Test.*Control' -count=1`。

- [ ] **Step 7: 提交本任务代码与由命令生成的实际差异。** Suggested commit: `feat(storage): expose measured usage and maintenance operations`。

## Task 12: 设置存储页、保留预览与会话操作

**Files:**

- Create: `packages/frontend/src/modules/settings/components/storage-settings.tsx`, `storage-settings.test.tsx`, `storage-cleanup-dialog.tsx`, `storage-retention-settings.tsx`，均位于同一 components 目录
- Create: `packages/frontend/src/modules/settings/api/storage.ts`, `packages/frontend/src/shared/i18n/storage.ts`
- Modify: `packages/frontend/src/modules/settings/settings-navigation.ts`, `settings-navigation.test.ts`, `pages/settings-scene.tsx`, `pages/settings-scene.test.tsx`
- Modify: `packages/frontend/src/modules/chat/components/chat-history-navigation.tsx`, `packages/frontend/src/modules/chat/api/chat-queries.ts`, `packages/frontend/src/shared/i18n/index.ts`, `base-en.ts`, `packages/frontend/src/shared/api/global-events.ts`
- Modify: `packages/frontend/src/app/scenes/settings-route.test.ts`, `packages/frontend/src/shared/i18n/translations.test.ts`

**Interfaces:**

- Consumes: Task 11 生成的 Query/Mutation 与 storage 事件；Task 8 可选宿主能力。
- Produces: `StorageSettings()`，SettingsSection 新值 `storage`，惰性加载；`storageOptions()`、`storagePolicyOptions()`、`storageMaintenanceOptions()` 在 settings/api 定义统一 QueryKey 适配。
- 清理和保留弹窗各自处理预览、确认、提交和错误；不把服务器快照复制进 Context 或第二套全局 Store。

- [ ] **Step 1: 添加导航与默认策略测试。**

```ts
expect(settingsSectionOf("storage")).toBe("storage");
expect(
  settingsNavigation.filter((item) => item.key === "storage"),
).toHaveLength(1);
```

组件测试使用 QueryClient 与现有 MSW/生成客户端替身：初始显示骨架；返回 enabled=false、days=15 后保持关闭；开启、缩短周期均先展示候选预览，取消不写策略。

- [ ] **Step 2: 增加部分数据和危险动作测试。**

```ts
expect(screen.getByText(/至少/)).toBeInTheDocument();
expect(
  screen.queryByRole("button", { name: /清理桌面缓存/ }),
).not.toBeInTheDocument();
```

模拟 incomplete 和不可用宿主；再覆盖后台刷新保留旧数据、离线重试、长路径、多卷、同路径不重复、内部资源单列、Top 5 及更多分页、预览失效、部分成功仅重试失败类、保护例外与导航删除后对账。

- [ ] **Step 3: 运行对应新测试，确认红灯。**

Run: `pnpm --dir packages/frontend exec vitest run src/modules/settings/settings-navigation.test.ts src/modules/settings/components/storage-settings.test.tsx src/app/scenes/settings-route.test.ts`。

- [ ] **Step 4: 复用 Settings 组件实现信息结构与 Query。**

```tsx
<SettingsGroup title={t("cleanup.title")}>
  <SettingsCard>{cleanupRows}</SettingsCard>
</SettingsGroup>
```

补齐概览、分类、伙伴、会话私有占用、清理和保留六区。单位和时间用 Intl；不把 shared native 重复相加。清理确认按固定类别组合 Core 预览和真实宿主能力，分别提交、保留完成/失败状态，宿主完成后主动失效 Core usage；不向 Core 传任意本机路径，不重复累加桌面目录大小。自动删除放独立危险确认，缓存一键清理不包含聊天。聊天菜单和过期行使用同一保护 Mutation。页内无永久 refetchInterval，新 storage 订阅隐藏时释放，resync 精确失效。

- [ ] **Step 5: 验证定向测试、类型与变更文件格式/样式。**

Run: `pnpm --dir packages/frontend exec vitest run src/modules/settings/settings-navigation.test.ts src/modules/settings/components/storage-settings.test.tsx src/modules/settings/pages/settings-scene.test.tsx src/app/scenes/settings-route.test.ts src/shared/i18n/translations.test.ts`。

Run: `pnpm --dir packages/frontend typecheck`；`pnpm --dir desktop exec tsc --noEmit`；`pnpm --dir desktop exec tsc -p electron/tsconfig.json`；`pnpm --dir web typecheck`。ESLint/Prettier 仅传入本任务和宿主变更文件；UI policy 运行 `node scripts/check-ui-style-policy.mjs`，不得放宽例外。

- [ ] **Step 6: 提交本任务文件。** Suggested commit: `feat(settings): add storage usage cleanup and retention controls`。

## Task 13: 故障验收、契约收尾与当前架构同步

**Files:**

- Modify: `docs/architecture/workspace-and-storage-cn.md`, `docs/architecture/desktop-lifecycle-cn.md`, `docs/architecture/notifications-and-work-board-cn.md`
- Modify: 各前述任务对应的真实故障测试，新增 `server/services/storage/maintenance_integration_test.go`
- Modify: 本计划勾选记录；spec 保留设计说明，不冒充验收结果。

**Interfaces:**

- Consumes: ST01–ST10 全部实现和前述 Fake/临时目录 fixture。
- Produces: 每条验收的命令、实际结果、未运行的平台范围；没有新的生产抽象或额外数据目录。

- [ ] **Step 1: 补齐端到端维护的崩溃/恢复回归。**

```go
if _, err := os.Stat(partnerFile); err != nil { t.Fatal("partner work removed", err) }
if _, err := os.Stat(sharedNativePath); err != nil { t.Fatal("shared native shard removed", err) }
```

使用真实临时 SQLite、JSONL、附件和共享 native 引用：测量 → 预览 → 新活动 → 跳过 → 再次过期 → 条件删除 → 提交中断 → 重启恢复。验证 policy 文件损坏禁止自动清理、程序/插件忙碌不误删、停止调度先于关闭数据库。

- [ ] **Step 2: 整理每个候选的实际结论。**

对 V01 的每类 staging 运行强制退出测试；若证明现有机制已正确处理，保留回归并记录「现有机制覆盖」，不加入新的重复清理。没有证明归属的旧账号/系统临时目录只报告受保护，不把它计为已修复垃圾。

- [ ] **Step 3: 只补跑因最后变更而直接受影响的验证。**

Run in `server`: `go test ./services/storage -run 'TestStorageMaintenanceIntegration' -count=1`。其他目标包仅在本任务改动后按前述确切用例再跑；不例行重复全部定向套件。

Run at root: `pnpm api:check`，仅在 Task 12 后契约有变化或尚有未解决差异时运行。

- [ ] **Step 4: 更新现有架构章节与验证边界。**

记录双根锁、分类口径、唯一策略文件、保留与条件删除、各资源回收所有者、桌面日志轮转、共享上传和 storage 事件。只描述已实现并有证据的行为，保留厂商历史与真实项目边界。

Run at root: `pnpm docs:check`；对实际变更文档运行 Prettier 检查。

- [ ] **Step 5: 按验收矩阵逐条对照，输出可审阅结果。**

检查 ST01–ST10 没有未覆盖要求，记录是否已完成真实平台磁盘和视觉验收。没有用户明确授权时不打开应用，不把自动化等同于视觉通过。报告实际新增/回收的文件类型、清理失败/保护边界及仍需平台验证的项目。

- [ ] **Step 6: 仅提交本任务文档及真实新增测试。** Suggested commit: `docs(storage): document lifecycle ownership and verified cleanup behavior`。

## Spec 覆盖与计划自审

| Spec 要求           | 负责任务   |
| ------------------- | ---------- |
| ST01 根一致性       | 1、2、13   |
| ST02 统计           | 2、11、12  |
| ST03 快照           | 11、12     |
| ST04 系统资源       | 3、4       |
| ST05 插件与程序     | 5、6、13   |
| ST06 上传           | 7          |
| ST07 桌面缓存与日志 | 8、12      |
| ST08 会话候选       | 9、10      |
| ST09 删除与调度     | 10、13     |
| ST10 UI 与契约      | 11、12、13 |

逐项执行时保持红绿回归与实际证据，不把本文所有未勾选任务当成已经通过。所有建议 commit 都只暂存该任务文件；遇到其他工作者改动同一文件时先对照 diff，不能整工作树提交。
