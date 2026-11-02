# 聊天消息展示重构实施验收

本记录对应[Spec](chat-presentation-refactor-spec-cn.md)及[实施计划](chat-presentation-refactor-plan-cn.md)。初始核对基线为主仓库 `da871f8`，交付工作树 HEAD 为 `29b609b`（期间已有的能力测试与 Core 冒烟提交保留），上游为 DeepSeek Harness `ce7073d51254c26cd63b22a60835b8f948deb447`。以实际源码与定向测试为准，真实平台与模型验收不由单元测试代替。

**整体性能验收未通过。** 补充的[真实旧版/新版 A/B 报告](chat-presentation-performance-ab-cn.md)测到了资源效率收益，也测到了首次迁移三帧发布等待和长代码热态 CPU 的退化。[后续展示整改](chat-presentation-followup-cn.md)已把发布改为单帧并补齐排版、图片和链接；真实 paint、长代码 CPU 与其他未达目标仍不视为通过。以下性能表区分两轮测量；原先单检查点微测试的“正文合计 3 倍”不代表完整公开链路，完整链路实际为 5 倍。

## 实现与行为证据

| 范围                                                                           | 实现位置                                                                                                                                             | 定向证据                                                                                                                                                                                                                                       |
| ------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| UI-01～UI-08：耗时、过程说明、网络组、具体动作、折叠、最终答复、操作与阅读位置 | `packages/frontend/src/modules/chat/components/chat-thread.tsx`、`use-chat-viewport-scroll.ts`、`chat-message-nodes.ts`                              | `chat-thread.test.tsx`、`chat-message-spacing.integration.test.tsx`、`chat-send-refresh.integration.test.tsx`、`chat-composer.integration.test.tsx`、`use-chat-viewport-scroll.test.tsx` 与 `use-chat-history-scroll.test.tsx`；真实视觉待验收 |
| NODE-01～NODE-04：稳定消息、部件、工具组与增量失效                             | `chat-message-nodes.ts`、`runtime/chat-live.ts`                                                                                                      | `chat-message-nodes.test.ts`；100 工具后 100 次正文追加仅检查/构建正文节点，工具组重建为零；合并多批只构建最终受影响节点                                                                                                                       |
| EVT-01～EVT-09：顺序、覆盖、恢复、终态与唯一订阅                               | `server/internal/chat/{eventstream,messageview}`、`services/execution/runtime_events.go`、前端 `shared/api/events.ts`、`runtime/use-chat-runtime.ts` | `live_adapter_test.go`、`live_facts_test.go`、`runtime_live_test.go`、`runtime_events_test.go`、`chat-live.test.ts`、`chat-event-batcher.test.ts`、`use-chat-runtime.test.ts`、普通/团队 Query 测试                                            |
| MD-01～MD-06：增量语法、稳定块、围栏、公式、非追加替换与高亮                   | `packages/frontend/src/shared/markdown/`、`chat-message-content.tsx`                                                                                 | 上游移植的增量与 viewport 测试；`chat-markdown.test.tsx`、`read-only-markdown.test.tsx`、`chat-markdown-export.test.ts`、`chat-project-link.test.tsx`、工作区 Markdown 测试                                                                    |
| FILE-01～FILE-13：独立采集、范围、状态、持久、恢复、分叉与删除                 | `server/internal/chat/changes/`、`services/chat/file_changes.go`、`services/execution/lifecycle.go`、`internal/chat/eventlog/file_changes.go`        | changes 包真实 Git/非 Git 临时目录与 race；共享 Execution 内真实 shell 成功/失败/取消；引用提交、重启、分叉删源、晚到阅读版本测试                                                                                                              |
| PATH-01～PATH-05：原始路径与标签、长路径、按需解析及有界 diff                  | `shared/workspace/file-reference*`、`execution-file-changes-card.tsx`、`snapshot-diff-view.tsx`、`workspace-file-viewer.tsx`                         | 路径与 Markdown 点击/复制回归、长路径后端测试、卡片及 hunk 渲染测试；主题和单双栏只改变布局，隐藏 diff 不启动读取                                                                                                                              |

上述范围映射说明自动化覆盖位置，不代表真实模型或视觉验收已经完成。第三方来源、具体移植文件、依赖与适配差异集中在[第三方声明](../../THIRD_PARTY_NOTICES.md)。

## 旧链路退出

| 原链路                                           | 当前边界                                                                                                                                           |
| ------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| ACP 工具状态、oldText/newText 合成为执行文件成果 | `messageFileChanges`、`messageFileChangeDiffs` 已删除；单工具安全摘要仍用于说明该工具，不产生执行成果                                              |
| 文件详情重放完整工具历史                         | API 先按索引读取创建范围与最后成果引用，再读取私有不可变比较计划；晚到引用无需旧正文前缀                                                           |
| 前端收到全文再计算执行 diff                      | 独立成果只返回 `comparison.hunks`，前端直接渲染；既有工作区当前 Git diff 不属于历史成果协议                                                        |
| 高频累计完整消息与逐字队列                       | 后端 12ms/操作预算仅打包新增操作；前端在下一次 rAF 合并发布，持久与交互边界立即冲刷；`chatSmoothStream` 与 `@assistant-ui/react-streamdown` 已退出 |
| 每轮全量 ThreadMessage/Part 转译和工具组扫描     | 列表直接消费领域消息与稳定节点；assistant-ui 只负责输入/队列/附件；正文脏部件快速路径不扫描完成工具                                                |
| 文件成果依附最后工具组                           | 成果卡片位于消息结果区域，独立于过程折叠                                                                                                           |
| 为短标签改写路径与提前探测文件                   | 删除路径缩写函数及 120 字符路径清空；作者标签与完整路径保留；打开或明确预览才解析，行列号共享文件定位缓存                                          |
| 旧公开字段                                       | `Message.fileChanges`、执行 diff 的 `oldContent/newContent` 不再进入公开合同；旧内部 Go 字段仅兼容内部历史形状，不被新结果消费                     |

忽略覆盖限定于仓库/祖先 `.gitignore`、`.git/info/exclude` 及显式策略；不读取宿主全局/系统配置或本地 include，不能宣称全局 Git 忽略已经导入。已跟踪文件遵循 Git 跟踪语义。

没有独立前后快照的旧执行仍可阅读正文、工具、审批与状态，不能重新获得准确的历史文件卡片或 diff。该边界属于有意兼容范围，不声称旧文件成果已迁移。

晚到文件成果的已读确认同时绑定 `fileChangeRecordId` 与 `fileChangeRecordVersion`；原正文只能确认原成果。摘要首次进入视区才读取，读取失败保留未读并提供重试，零净改动不新增文件成果阅读身份。该约束由真实阅读组件与通知、工作看板服务的拒绝旧引用测试覆盖。

64 KiB 是新增操作的聚合打包目标，单个完整安全工具替换可超过该目标；它不保证每个 JSON 帧的硬字节上限。文本新增仍按 UTF-8 边界分段，受控正文预算与原子事实不丢失分别验证。

## 性能口径与结果

环境为 macOS/Darwin amd64、Intel Core i9-9880H 2.30GHz；前端 Node 22.20.0；后端 Go 1.25.8。早期阶段基准期间有并行编译和测试；补充 A/B 的 Markdown、投影、后端依次运行，避免这三组彼此竞争 CPU。原始数据须连同工作负载阅读，不能外推为整体页面提速比例。

| 场景                                | 已验证内容                                                                                                                   | 状态与限制                                                                                                                                   |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| PERF-01，50 KiB/100 批              | 完整公开链路正文旧 105 倍→新 5 倍；公开 JSON 中位数 5,422,569→301,089 B（-94.45%）；后端 CPU 43.965→31.905ms                 | 资源成本改善，但完整链路 3 倍目标未达；含固定输入节奏墙钟约 2.09 秒不变，网络与原生模型总延迟未测                                            |
| PERF-02，100 完成工具/100 正文批    | 实际旧/新工具 part 重建 10,000→0、组构建 200→0、摘要调用 100→0；投影/摘要 CPU 79.657→9.054ms（-88.63%）                      | 同工作负载 9 轮交替对照；正文检查/构建各 100。CPU 不含 React/DOM，不能据此称整个页面加速 88.63%                                              |
| PERF-03，100 KiB、2,000 行代码      | 真实组件热态 CPU 5104.812→5595.703ms（+9.62%）；终态到完整着色 DOM 1647.418→345.718ms（-79.01%）；总墙钟 6498.718→5786.072ms | 长代码结束更快，但热态 CPU 退化、单次更新 p95 66.7～72.3ms；React development/jsdom，不含 layout/paint。原同语法解析微基准只作为算法补充     |
| PERF-04，发布延迟                   | 真实旧 16ms/首次三帧/当前单帧调度器在 60Hz、每 20ms 一批的虚拟时钟下，最差输入相位 p95 为 16→49.667→16.333ms；终态立即冲刷   | 已修正三帧额外等待，整改后比三帧减少约 33.3ms；60/120Hz 真实可见 p95 仍未测                                                                  |
| PERF-05，10,000 文件/50 MiB/修改 20 | 固定覆盖、私有对象去重及阶段预算的真实目录基准                                                                               | 非 Git 基线/收尾 median 3.891/3.563 秒，p95 4.114/4.065 秒；Git median 3.163/3.489 秒，p95 4.155/3.525 秒。1 秒初始目标未通过，30 秒预算未变 |
| PERF-06，2 MiB/侧与超限             | 服务端有界比较、coarse/截断状态及 5,000 行上限，前端不重算                                                                   | 2 MiB/侧有界 coarse 比较均值 31.988ms/op、p95 39.091ms、5,000 行；算法与超限测试通过，实际 UI 帧与内存待测                                   |
| PERF-07，生命周期                   | 定向 observer/订阅/帧释放和停用行为测试                                                                                      | 强制 GC 与 20 次真实场景堆曲线未测                                                                                                           |
| PERF-08，500 条历史窗口             | 同样 100 工具/100 追加，投影/摘要 CPU 108.419→40.032ms（-63.08%）；旧版本已缓存历史，双方均复用 500 个历史引用               | 历史扫描仍存在，折叠/投影 wall 33.273→31.896ms；500 条真实页面 DOM/滚动/峰值堆未测，未新增虚拟化                                             |

普通 2 KiB 文本的真实组件热态 CPU 1517.181→298.518ms（-80.32%），但固定输入节奏下的完成墙钟 4823.734→4820.265ms（仅减少 3.469ms）。这份对照支持减少重复工作，不支持普通短回复已明显更快。全部方法、轮次、等价校验、原始记录和复现脚本见 [A/B 报告](chat-presentation-performance-ab-cn.md)。

快照最终 5 次测量及预热原始记录见[完整日志](chat-presentation-refactor-evidence/workspace-changes-performance.log)。每份基线保存 10,001 个对象、52,848,800 逻辑字节（包含 tree）及 834,626 编码字节；修改 20 个文件后只增加 21 个对象、524,860 逻辑字节及 248,923 编码字节，未变化内容去重。最初逐 blob 同步写入曾触发 30 秒超时，改为私有 Git 批量 pack 写入并同步对象后，固定覆盖数据集完成；没有通过缩小覆盖或加大超时获得结果。

1 秒目标与当前 3～5 秒阶段耗时的差距仍保留，当前不适合据此宣告大项目启动耗时已优化完成。后续若要调整产品采集预算，须以此日志为依据另行评估；本次没有修改验收目标。

真实 Markdown A/B 入口为 `packages/frontend/scripts/performance/markdown-ab.mjs`；原 `packages/frontend/scripts/benchmark-markdown.mjs` 仅比较相同语法解析器的全量/增量模式，不是旧产品对照，其[原始数据](chat-presentation-refactor-evidence/markdown-parsing.json)保留为算法补充。快照基准为 `go test ./internal/chat/changes -run '^$' -bench 'BenchmarkCapture10000|BenchmarkTextDiffAtLimit' -benchtime=5x -count=1 -benchmem`；普通测试不使用不稳定的墙钟时间断言。单检查点用例已改名为 `TestRuntimeCheckpointBodyBudget`，不再用其结果推断完整公开链路预算。

## 验证命令与剩余边界

只运行本次相关文件和 Go 包，不运行全量前端测试、`pnpm check`、桌面全量检查或 `go test ./...`。以下按实际执行分组记录，重复覆盖的测试数量不相加。早期接线、类型与旧断言失败已修正并在对应最终组重跑；单元用例等待调整不作为产品延迟结论。

| 检查                     | 实际入口与结果                                                                                                                                                                                                                                                                                        |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Markdown                 | 在前端目录用锁定 Vitest 运行 `src/shared/markdown` 及 Markdown、只读、导出、项目链接和工作区预览九文件组，88 用例通过；公共 observer 提取后高亮两文件 11 用例再次通过                                                                                                                                 |
| 稳定节点与现有交互       | 纯消息、耗时、组、分叉、输入、历史刷新、工作区、阅读和滚动十四文件组 212 用例通过；滚动最终两文件 11 用例通过，包含 100 次追加的零历史 DOM 扫描断言                                                                                                                                                   |
| 增量、卡片、路径与 Query | 最终七文件组 `use-chat-runtime`、`chat-live`、独立卡片、服务端 hunk 渲染、工作区文件预览、真实阅读组件、文件引用，103 用例通过；此前十一文件组含普通/团队 Query 与事件传输，157 用例通过                                                                                                              |
| 日志与消息               | `go test ./internal/chat/eventlog ./internal/chat/messageview -count=1` 通过；`eventstream`、`chat` 整包定向通过                                                                                                                                                                                      |
| 共享执行与并发           | Execution 按修改文件的测试名字集合验证；真实 shell 成功/失败/真实 Context 取消、删除等待及恢复/分叉/增量的相关 race 通过。完整 Execution 包中的无关 httptest 监听端口受沙箱限制，未宣称全包通过                                                                                                       |
| 精确已读                 | chat 六状态、notification 三 owner、团队转交、通知和工作看板 HTTP API 的定向测试均通过；旧正文引用、错误记录 ID 与错误版本均被拒绝                                                                                                                                                                    |
| 快照                     | changes 包真实 Git/非 Git、预算、不可读、过滤器/Hook/LFS/索引隔离、重启/分叉和 race 定向验证；最终持久边界与 Discard 定向 race 通过（引擎 3.927s、未接纳执行清理 12.177s）；未接纳新 Chat 的两次失败后存储根为空，已有 Chat 的三次失败不新增记录/对象或消耗采集配额；其他活动采集及已提交历史保持可读 |
| 类型、风格与合同         | 前端 `tsc --noEmit` 通过；修改文件 ESLint 无错误/警告，Prettier 与 `git diff --check` 通过；`scripts/generate-api.sh` 与 `scripts/check-generated-api.sh` 均退出 0；生成工具/公开合同的四项 Node 测试通过。生成文件仅通过原生成脚本更新                                                               |
| 文档                     | `node --test scripts/check-docs.test.mjs` 四项通过；`node scripts/check-docs.mjs` 链接、图片与个人路径检查通过                                                                                                                                                                                        |

默认 Go 缓存与包管理器路径不可用时使用相同工具版本的临时缓存/固定 CLI，未修改检查规则。

本次没有打开、控制或截图运行中的浏览器或桌面。尚未执行真实浅暗主题、窄窗口、键盘、滚动与两种原生 CLI/模型端到端验收。真实 shell 临时目录测试只证明采集边界与生命周期，不证明所有 Agent 原生来源均已验收。未推送、发布或合并。
