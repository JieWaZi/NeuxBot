# Markdown 真实组件 A/B

该基准挂载两套实际产品入口：`29b609b` 完整源快照中的 `AssistantText`（原始 `TextMessagePartProvider`、`@assistant-ui/react-streamdown@0.3.8`、`@streamdown/code@1.1.1`），以及当前工作树的 `AssistantMarkdown`。不会用当前解析器的全量模式代替旧实现，也不会关闭任一侧的代码高亮、表格组件或旧平滑队列。

从 `packages/frontend` 运行：

```sh
/usr/local/bin/node scripts/performance/markdown-ab.mjs
```

默认读取 `/private/tmp/neux-chat-ab-baseline` 中的完整旧源快照。该目录须保留原有相对源码结构，并准备可解析的相同依赖，包含上面两个旧版精确依赖。`NEUX_MARKDOWN_AB_BASELINE` 可覆盖快照位置，`NEUX_MARKDOWN_AB_OUTPUT` 可覆盖原始结果文件位置，`NEUX_MARKDOWN_AB_WARM_RUNS` 可覆盖默认三次预热后测量。默认结果为 `/private/tmp/neux-chat-markdown-ab.json`。

六个独立子进程串行运行：三种工作负载分别测旧、新实现，每个进程先测一次首次挂载，再测三次重新挂载。两侧输入字节、SHA-256、分片边界、100 次输入和 48ms 实时时钟间隔完全相同。普通聊天场景恰好 2KiB，仅有段落与列表；短代码场景恰好 2KiB、24 行 TypeScript；长代码场景恰好 100KiB、2000 行 TypeScript。两个代码场景都包含真实表格、外链、内联代码与最终标记。所有代码面模拟为位于视区，要求实际着色结果完成，避免通过跳过高亮降低计算量。

计时边界：

- 两侧都使用 React 19.2.8 development、Vitest jsdom、相同 `@vitejs/plugin-react` 默认设置，不启用 StrictMode。仓库正常 Vite 配置也没有 React Compiler 设置，基准没有单侧移除编译优化。
- jsdom 启动、宿主 API 模拟与组件依赖导入在轮次外；导入的墙钟与 CPU 单独记录，包含 Vite 测试转换开销，不代表生产启动时间。
- 轮次包含空内容挂载、实际解析与高亮初始化、流式输入、旧平滑与 deferred 更新，以及最终完整着色代码呈现。卸载和最终正确性断言在计时外；流内覆盖采样属于相同驱动开销。
- `process.cpuUsage` 记录进程 CPU；`perf_hooks` 记录实际墙钟。React Profiler 记录逐提交的 render duration 与提交次数，不能据此声称测到了浏览器 DOM commit、布局或绘制耗时。
- 总 CPU 包含相同的驱动、Profiler 记录、流内 DOM 覆盖采样与少量阶段落盘，没有扣除仪器开销。两侧 DOM 大小不同，不能推定这部分开销相同，也不据此解释或抹除测得的性能回退。
- 每轮结束核验完整代码及换行、表格、外链、最终标记与着色。Streamdown 的行包装没有文本换行节点，读取时按直接行包装重建换行，再与同一原文比较。
- 驱动使用 `flushSync` 提交每次外部来源更新，真实计时器在输入之间自由调度，保留组件内原有 smooth/deferred 工作。不会使用 `await act` 等待组件内部队列排空；早期采用该驱动的长场景超时数据已经排除，不能当作旧版产品延迟。

原始结果保留每次输入耗时、逐提交记录、四个流内覆盖采样、完成前覆盖、流内与完成阶段 CPU、总 CPU 和墙钟。必须结合完成前覆盖读取 CPU 结果：旧平滑队列可能只显示部分长内容，在完成时才跳到完整正文。重新挂载允许复用产品已有语法或 token 缓存，这也是两侧真实行为的一部分。

本基准不运行真实浏览器，不测量原生平台、布局、paint、合成器、交互帧率、堆增长或图片与 Mermaid 渲染。单次冷测量和三次预热后测量只用于同机诊断，不能替代更多设备与真实页面验收。普通测试命令不会启动该性能工作负载。
