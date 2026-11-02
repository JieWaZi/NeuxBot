# 消息投影 A/B 量测

`projection-ab.mjs` 比较固定提交 `29b609b` 与当前工作树的实际消息投影路径。它只使用虚构消息，不启动页面、外部智能体或模型。

先将 `29b609b` 的完整源码准备到独立临时目录，并让该目录能解析对应版本的前端依赖。不要用不同版本的 assistant-ui 替代旧依赖。runner 会将旧投影与旧 `chat-thread.tsx` 的 SHA-256 与 `git show 29b609b:<path>` 核对；基线目录需要保留原文件布局。

从仓库根运行正确性检查，再在没有其他编译、测试或基准进程竞争 CPU 的窗口运行正式量测：

```sh
node --expose-gc packages/frontend/scripts/performance/projection-ab.mjs \
  --baseline /tmp/neux-chat-ab-baseline \
  --smoke --out /tmp/neux-chat-projection-ab-smoke.json

node --expose-gc packages/frontend/scripts/performance/projection-ab.mjs \
  --baseline /tmp/neux-chat-ab-baseline \
  --runs 9 --cycles 20 --out /tmp/neux-chat-projection-ab.json
```

正式运行先预热，再交替执行旧→新、新→旧。每轮每个变体执行 20 个独立循环，每个循环包含 100 次追加；统计来自 9 轮，经验 p95 在此样本量下等于最大值。每个变体量测前显式 GC；是否启用写入 raw。脚本需要当前前端已有的 TypeScript、esbuild 和基线 assistant-ui 依赖，不安装包。

两个输入场景都是 100 个已完成工具后继续追加 100 批正文，每批 512 UTF-8 字节；另一场景另有 500 条稳定历史消息，用户和助手各半，历史助手消息各有两个完成工具及正文。旧输入是完整安全消息，新输入是对应正文增量。两边先在只含活动消息的叠层中折叠，再调用各自真实 `mergeAuthoritativeMessages` 合并历史窗口。

旧路径执行原 `applyChatEvent`、`toThreadMessage`、assistant-ui 的 `ExternalStoreThreadRuntimeCore` 与其真实 WeakMap 缓存、消息仓库、原 `buildGroupTree` 及原工具组摘要。新路径执行原 `applyChatLiveFrame` 与 `ChatMessageProjection`；摘要是否重算按实际组对象身份判断，模拟当前 `memo(WorkProcess)` 的稳定 props 边界。这里不挂载 React，也不把该判断描述为实际组件 render 次数。

工具组摘要相关声明由 TypeScript AST 原样提取；旧 `indexedParts` 使用原 `useMemo` 回调表达式，`tools` 至 `summary` 的语句块不改算法。提取器核对声明名称、语句顺序及循环结构，raw 保存整文件、声明和计算块的 SHA-256 与行号。翻译使用双方相同的固定文字查表；未触及的失败文案依赖使用 stub，输入不进入失败分支。

raw 的口径如下：

| 字段                                         | 含义                                                                                                  |
| -------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `cpuMs` / `wallMs`                           | 当前轮所有循环中，正文追加阶段的进程 CPU / wall 时间，含 JSON 解码、叠层合并、投影和组摘要判断        |
| `per100AppendsCpuMs` / `per100AppendsWallMs` | 按每 100 次追加归一后的 median、p95、max                                                              |
| `stages`                                     | 追加阶段内的 JSON 解码、折叠与投影、组摘要 wall 时间；各阶段统计不能当成 CPU 时间相加                 |
| `initMs`                                     | 旧 Core constructor 或新 Projection 初次 update 的 wall 时间，不包含初次组摘要                        |
| `lifecycleCpuMs` / `lifecycleWallMs`         | 另含 fixture clone、初始化和初次组摘要的循环总成本                                                    |
| `correctness`                                | 独立实际执行得到的转换、对象重建、组构建、摘要调用及历史引用复用次数，不在 CPU 量测中执行这些计数遍历 |
| `semanticSha256`                             | 旧新显示语义深比较通过后的同一结果摘要                                                                |
| `fingerprints` / `harnessSha256`             | 实际源文件、抽取块、旧库实现和 runner 的内容指纹                                                      |

显示语义检查覆盖消息身份、角色、状态、部件顺序、正文、过程说明、工具身份、类别、动作、状态、标签、安全摘要及工具组文字。它排除内部 live revision 等传输元数据；不据此宣称其他未出现在 fixture 中的业务字段已验收。

旧版已具备稳定历史缓存。runner 实际检查旧历史 `toThreadMessage` 调用为零、双方历史消息引用得到复用；不会把旧版重建工作量臆算为 `500 × 100` 次消息转换。历史窗口的合并、扫描及旧消息仓库处理仍计入实际量测。

进程 CPU 使用 `process.cpuUsage`，包含 V8 的进程内辅助线程工作，因此可能高于 wall 时间。模块装载、打包、预热、计数与最终语义检查均在 steady append 时间之外。raw 保留初始化与生命周期成本，不能只摘取较快的一个阶段作为完整聊天成本。

本量测不包含 React 调度、DOM、浏览器样式与布局、绘制、Markdown/高亮、真实 WebSocket 网络、服务端投影、Git 采集或模型耗时。原始输入字节数只描述 fixture，不作为完整网络流量。完整文件 diff 打开 A/B 未测；旧工具历史重放、旧带语法高亮 diff、新 Go 预计算和新 hunk 渲染尚未形成同功能对照，不能列为本量测的收益。

本轮实测环境和每次原始结果由 JSON 保存。归档 raw 时可以统一替换绝对目录前缀以去除个人路径，需保留相对文件身份、源码内容 hash、行号、输入规模、全部轮次及上述成本边界。
