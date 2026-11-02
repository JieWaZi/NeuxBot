# 聊天展示首次迁移性能 A/B

本报告的“新版”固定指首次迁移时受测的三帧工作树，原始数据及结论保留。后续单帧发布、排版和图片接线的当前实现与验证见[展示整改记录](chat-presentation-followup-cn.md)，不能把这里的受测源码与后续工作树混为同一版本。

本次对照确认了部分 CPU、分配字节和公开事件载荷收益，也测到了退化。**不能据此宣布聊天整体体验优化完成。** 普通短回复的组件完成时间几乎不变，三帧聚合增加了消息发布等待，长代码的热态进程 CPU 也没有下降。

旧侧固定为 `29b609b` 的完整源码归档，新侧为首次迁移时已保存源码指纹的工作树。不是以新解析器的“全量模式”冒充旧版，也没有为取得更好数字关闭旧平滑队列、旧语法高亮或旧版已有的历史缓存。该轮量测期间产品代码保持不变；当时的单帧候选只是调度实验。

## 1. 直接结论

以下 CPU 数字是对应测试进程的 CPU 时间，不是模型耗时。百分比为新相对旧的变化；普通场景、压力场景与纯计算阶段分开阅读。

| 场景及计量范围                                  |        旧版 |       新版 | 判定                                        |
| ----------------------------------------------- | ----------: | ---------: | ------------------------------------------- |
| 普通纯文本 2 KiB，组件热态 CPU 中位数           |  1517.181ms |  298.518ms | 减少 80.32%，处理成本改善                   |
| 同上，固定约 4.8 秒输入的组件完成墙钟           |  4823.734ms | 4820.265ms | 仅减少 3.469ms / 0.07%，不能称回复明显更快  |
| 短代码 2 KiB / 24 行，组件热态 CPU 中位数       |  2827.528ms |  760.310ms | 减少 73.11%                                 |
| 100 KiB / 2000 行代码，组件热态 CPU 中位数      |  5104.812ms | 5595.703ms | 增加 9.62%，本轮观察到退化                  |
| 同上，终态输入到完整着色代码 DOM 就绪           |  1647.418ms |  345.718ms | 减少 79.01%；这是收尾阶段，不是整个回复时间 |
| 同上，整个组件输入及完成过程墙钟                |  6498.718ms | 5786.072ms | 减少 10.97%；输入规模和速率见下文           |
| 100 完成工具后追加 100 批，前端投影/摘要 CPU    |    79.657ms |    9.054ms | 减少 88.63%，不包含 React 或 DOM            |
| 同上另有 500 条稳定历史，投影/摘要 CPU          |   108.419ms |   40.032ms | 减少 63.08%，历史扫描没有消失               |
| 50 KiB / 100 批，公开事件 JSON 载荷             | 5,422,569 B |  301,089 B | 减少 94.45%，不含 WS 外信封与 socket        |
| 100 完成工具后同样正文，公开 JSON 载荷          | 8,118,561 B |  356,018 B | 减少 95.61%                                 |
| 60Hz、每 20ms 一批，调度等待 p95 的最差输入相位 |        16ms |   49.667ms | 增加约 33.7ms，三帧聚合明确退化             |

最明显的收益在重复工作：普通/短代码组件的内部提交、重复工具对象构建、累计正文传输。**这些收益没有自动变成普通短回复的完成时间收益。** 长代码也不是全面变快：结束后更早出现完整代码、流中显示更完整，但新侧的热态总 CPU 和单次更新预算仍有问题。

## 2. 方法与等价条件

环境为 macOS Darwin amd64、Intel Core i9-9880H 2.30GHz、16 逻辑 CPU；Node 22.20.0、React 19.2.8 development、Go 1.25.8。长量测按 Markdown、投影、后端依次运行，避免这些基准彼此竞争 CPU；不宣称操作系统或其他应用完全空闲。

### 实际 Markdown 组件

旧入口为原 `AssistantText`、`TextMessagePartProvider`、`@assistant-ui/react-streamdown@0.3.8`、`@streamdown/code@1.1.1`；新入口为 `AssistantMarkdown`。两侧采用相同默认 Vite React 插件，无单侧移除 React Compiler 或其他旧优化。

三个 fixture 的输入字节、SHA-256、100 个前缀与 48ms 输入节奏在两侧完全相同：纯段落/列表 2 KiB、含 24 行 TypeScript 的 2 KiB、含 2000 行 TypeScript 的 100 KiB。所有代码块都模拟进入视区，并要求真实高亮完成。长场景约 21 KiB/s，是高吞吐压力输入，不能当成日常模型输出速率。

每个 fixture/版本独立进程，测一次首次组件挂载及三次热态重新挂载，共 24 轮。每轮恰好创建一个 React root、提交 102 次外部来源更新，最终校验完整代码及换行、表格、链接、最终标记和高亮。旧侧额外提交来自实际组件内的 smooth/deferred/Streamdown 工作，不是重复挂载；普通短文热态提交中位数为 1322→102，短代码 1404→104，长代码 1447→104。

CPU 包含相同的输入驱动、Profiler 记录、流内 DOM 覆盖采样和少量阶段落盘。两侧 DOM 大小不同，不能假设仪器开销相同，也不能未经测量就用这一点解释掉退化。React Profiler 记录的是 render duration 与提交次数，**未测到真实 DOM commit duration、layout 或 paint**。模块导入/测试转换耗时单列，不作为生产启动时间。

旧平滑队列在长场景结束前只显示了 1647～2023 / 93999 个代码字符（约 1.75%～2.15%），新侧四轮都显示完整代码和最终标记。因此需要同时阅读 CPU、显示覆盖和完成时间，不能只摘一个指标。

新长场景的热态输入更新 p95 为 66.7～72.3ms，逐提交 React render p95 为 26.8～31.0ms；旧侧对应约 10.0～12.2ms / 4.4～5.1ms，但仅显示约 2% 代码。新实现仍不能声称长代码每帧预算达标。

早期 `await act` 驱动会等待内部队列，产生的长场景超时已排除，不当成旧版产品耗时。最终驱动通过 `flushSync` 提交每次外部来源更新，输入间由真实计时器调度组件内工作；阶段进度和输入时钟迟到量保存在原始记录中。

### 实际消息投影与工具摘要

旧侧运行原 `applyChatEvent`、`toThreadMessage`、assistant-ui `ExternalStoreThreadRuntimeCore`、真实 WeakMap 缓存、`buildGroupTree` 和原 WorkProcess 摘要计算。相关纯计算语句由 TypeScript AST 原样提取并保存源文件、语句块 SHA 和行号；没有手写一个更慢的旧算法。新侧运行实际增量折叠及 `ChatMessageProjection`。

两个场景都包含 100 个完成工具、后续 100 次 512 字节追加；另一场景另有 500 条历史。两边在活动消息叠层折叠后调用各自的原历史合并函数。正式 9 轮交替先后，每轮 20 个完整循环，计数单独运行；CPU 表按每 100 次追加归一。经验 p95 在 9 个样本时等于本组最大值。

| 每 100 次追加的实测计数    |               旧版 |                    新版 |
| -------------------------- | -----------------: | ----------------------: |
| 工具 part 对象重建         |             10,000 |                       0 |
| 工具参数对象重建           |             10,000 |                       0 |
| 组构建                     |                200 |                       0 |
| 工具摘要调用               |                100 |                       0 |
| 正文节点构建/检查          | 不按新节点模型计数 |                  各 100 |
| 500 历史场景的历史消息转换 |                  0 | 不走 ThreadMessage 转换 |
| 500 历史引用复用           |                500 |                     500 |

旧版本来就有稳定历史缓存，不能把历史转换为零全部算成这轮新收益。500 历史场景里，折叠/投影阶段 wall 中位数仍为 33.273→31.896ms；总收益更多来自 JSON 解码和摘要复用。源码装载、初始化、fixture clone 和第一次摘要不在 steady append 内，初始化及完整生命周期成本另列于 raw；没有把它们当成不存在。

### 实际后端公开链路

同一 Go 测试 overlay 分别编译进两份真实源码，执行 `runtimeEventSink → JSONL/fsync → Adapter → PublicEvent JSON`。两个场景各版本各五轮，共 20 轮；每轮 100 个 512 字节正文事件，计划每 20ms 一个。工具场景先真实完成 100 个工具，建立阶段单列。固定 GOMAXPROCS=1，最终完整 Message 语义在同场景全部运行中一致，只排除时间/运行耗时和新增 liveRevision 元数据。

| 指标（中位数）      |                      纯正文旧→新 |               100 工具后正文旧→新 |
| ------------------- | -------------------------------: | --------------------------------: |
| 进程 CPU            |       43.965→31.905ms（-27.43%） |        60.311→27.996ms（-53.58%） |
| 分配字节            | 8,196,256→2,746,048 B（-66.50%） | 12,745,808→2,694,992 B（-78.86%） |
| 分配次数            |         5941→6937（**+16.76%**） |            44,695→5790（-87.05%） |
| JSON Marshal 总墙钟 |                   15.317→3.763ms |                    29.197→3.681ms |
| 含固定输入节奏墙钟  |              2092.810→2093.580ms |               2092.709→2094.897ms |

公开载荷减少没有让这个固定约 2.09 秒的输入过程变快。完整工具建立阶段墙钟也仅约 8.921→8.821 秒，不应将后续正文阶段的载荷收益冒充整个任务的加速。

**撤回原先完整正文只传 3 倍的口径。** 原微测试只计一次持久检查点；真实公开路径还包含随后执行终态的第二次完整消息，每份同时有 content 与 displayParts。新链路实际为 live 1 倍＋两次持久公开消息各 2 倍＝**5 倍**；旧纯正文中位数为 105 倍。相对旧版明显减少，但原先“不超过 3 倍”的完整链路目标仍未达到。

### 调度等待与单帧候选

直接读取旧、新批量调度器的真实源码，在同一虚拟时钟运行 60/120Hz、5/20/80ms 三种输入间隔，每种测五个输入相位。指标为 enqueue→onBatch，不含 React、旧 smooth 额外等待、模型、网络或真实 paint。

| 场景           | 旧 16ms timer p95 | 受测三帧 p95 | 单帧候选 p95 |
| -------------- | ----------------: | -----------: | -----------: |
| 60Hz、20ms/批  |              16ms |     49.667ms |     16.333ms |
| 60Hz、80ms/批  |              16ms |     49.667ms |     16.333ms |
| 120Hz、20ms/批 |              16ms |     24.667ms |      8.000ms |

60Hz、20ms/批的提交次数由 100 降到 40，代价是额外等待；80ms/批时提交次数没有减少，等待却仍增加。终态两侧都立即交付。该轮单帧候选只在实验中将 `schedule(3)` 换成 `schedule(1)`，当时尚未用于产品，也没有测它在完整页面里的 CPU/长代码取舍；后续产品整改见[展示整改记录](chat-presentation-followup-cn.md)。

## 3. 没有证据支持的结论

本轮没有启动或操作真实浏览器/桌面，没有调用真实模型，不能给当前安装版本的首字、交互帧率、paint、峰值堆或滚动卡顿的前后百分比。

独立快照是新增功能成本。此前万文件/50MiB测量中，基线中位数 Git 3.163 秒、非 Git 3.891 秒；代码会在 Run 前等待基线完成，因此有新增首字等待风险。旧路径没有同等独立采集步骤，不能把这项写成性能收益；结束快照在真实模型终态之后继续完成，不代表基线成本也消失。1 秒阶段目标未达到，详见[采集原始记录](chat-presentation-refactor-evidence/workspace-changes-performance.log)。

完整文件 diff 打开链路尚未形成等功能 A/B。旧工具历史重放、旧端带语法高亮 diff、新端服务端预计算和新 hunk 渲染不能只取各自较快的一段拼成收益。当前不把它列为已经证实的速度提升。

该轮整体性能验收为**未通过**：有资源效率收益，同时有三帧等待、长代码更新预算、5 倍正文预算及前置采集成本需要处理。现有结构和正确性测试通过不能替代这份性能判断。

## 4. 原始证据与复现

- [Markdown 热态/冷态摘要](chat-presentation-refactor-evidence/performance-ab-markdown-summary.json)、[完整 24 轮及逐提交记录](chat-presentation-refactor-evidence/performance-ab-markdown.json.gz)
- [投影完整 9 轮、实际计数与语义指纹](chat-presentation-refactor-evidence/performance-ab-projection.json.gz)
- [后端完整 20 轮、字节/CPU/分配/输入迟到量](chat-presentation-refactor-evidence/performance-ab-backend.json.gz)
- [调度全部相位及候选](chat-presentation-refactor-evidence/performance-ab-scheduler.json)
- [归档内容与原始输出哈希](chat-presentation-refactor-evidence/performance-ab-artifacts.json)、[投影 runner 执行版本证明](chat-presentation-refactor-evidence/performance-ab-runner-provenance.json)、[实际执行 runner 字节副本](chat-presentation-refactor-evidence/performance-ab-projection-measured-runner.mjs.gz)

目录前缀用 `<baseline>` / `<working-tree>` 等占位符脱敏，全部计量数字、fixture、生产源文件和提取块 hash 保持不变；完整逐轮数据用 gzip 保存，避免在仓库中展开约 6MiB 日志。`gzip -dc <artifact.json.gz>` 可读取 JSON。p95 为样本经验分位数，不提供虚假的统计精度。

从仓库根准备新的基线临时目录（需要当前安装缓存中有精确旧依赖；不修改当前依赖、不联网安装）：

```sh
node packages/frontend/scripts/performance/prepare-ab-baseline.mjs --out /tmp/neux-chat-ab-repeat
```

然后串行运行，避免相互争抢 CPU：

```sh
# 旧真实组件/新真实组件，共24轮。完整条件见各runner README。
NEUX_MARKDOWN_AB_BASELINE=/tmp/neux-chat-ab-repeat node packages/frontend/scripts/performance/markdown-ab.mjs

# 真实消息投影，9轮交替，每轮20×100追加。
node --expose-gc packages/frontend/scripts/performance/projection-ab.mjs --baseline /tmp/neux-chat-ab-repeat --runs 9 --cycles 20

# 真实后端。Go版本须符合仓库go.mod。
python3 scripts/performance/backend-ab/run.py build --baseline /tmp/neux-chat-ab-repeat/server
python3 scripts/performance/backend-ab/run.py smoke
python3 scripts/performance/backend-ab/run.py run

# 调度源码对照与候选；只使用虚拟时钟。
node packages/frontend/scripts/performance/scheduler-ab.mjs 29b609b
```

调度脚本现同时执行旧 16ms、已归档三帧源码和当前单帧源码；三帧归档的 SHA 与本报告原始记录一致，旧结果可重放。

详细边界分别见 [Markdown 方法](../../packages/frontend/scripts/performance/markdown-ab.README.md)、[投影方法](../../packages/frontend/scripts/performance/projection-ab.README.md)、[后端方法](../../scripts/performance/backend-ab/README.md)。普通测试不会自动运行长时性能 fixture。复现时新的 CPU 数值可能变化，应保留全部轮次与 source hash，不按单次更好的结果替换本记录。
