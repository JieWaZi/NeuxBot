# 后端实时消息实际源码 A/B

此脚本把同一份测试 overlay 分别编译到归档提交 `29b609bc4424e89b68a7f50809add3b1eef5637d` 与当前工作树。overlay 只增加测试入口；不替换生产 `runtimeEventSink`、实时 Adapter、消息投影或日志存储。

主阶段输入固定为 100 个 512 字节正文事件，计划每 20ms 一个，共 50KiB。`plain` 从空执行开始；`tools100` 先通过同一生产路径完成 100 个工具，再输入同样正文。工具建立阶段单列为 `warmup`，不计入后续正文阶段。每个场景、每个版本运行 5 次，交替先后次序。

两边固定 `GOMAXPROCS=1`。计量包含事件折叠、真实 JSONL 写入与 fsync、公开投影、公开事件 JSON 序列化、GC 和只读计数。每次保存 `Flush` 产生的完整消息及执行完成产生的完整消息，两次持久公开事件均不省略。正文有效字节是实际载荷中 `content`、正文 display part 和增量 append 文本的 UTF-8 字节总数；重复的已发送正文照计。完整 payload 编码后立即丢弃，不额外保存所有帧，也不在计量中运行第二份前端 reducer。

`wire_bytes` 表示安全 `PublicEvent` 未压缩的 JSON payload 字节，不含全局 WebSocket 外信封、订阅队列、socket、Electron 或渲染器。序列化时间是各次 `json.Marshal` 墙钟时间总和；CPU 是整个计量阶段的进程 user/system CPU；分配是 `runtime.MemStats.TotalAlloc` 增量。阶段前 GC、fixture 建立、后置语义指纹校验不计入；堆开始/结束值不是峰值堆。计数扫描也计入 CPU，原始结果保留这一边界说明。

最后的完整安全 `Message` 去除创建/更新时间、运行开始/耗时及新增协议覆盖字段 `liveRevision` 后逐字段形成规范 JSON 指纹。正文、部件、身份、状态和其他元数据必须在同场景两版本所有运行中完全相同，否则脚本失败并保留诊断 JSON。

```sh
python3 scripts/performance/backend-ab/run.py build --go-root /absolute/path/to/go
python3 scripts/performance/backend-ab/run.py smoke --output /private/tmp/neux-chat-backend-ab-smoke.json
python3 scripts/performance/backend-ab/run.py run
```

默认旧源码目录是 `/private/tmp/neux-chat-ab-baseline/server`，可通过 `--baseline` 指定。`build` 使用 `go test -c -overlay`，把两个二进制和源码 SHA-256 manifest 保存到 `/private/tmp/neux-chat-backend-ab`。运行前验证生产源码及 harness 未变化；若变化须显式重新编译。`smoke` 的 1ms 节奏仅校验语义，不作为性能结果。

完整原始记录、运行次序、环境、每轮 CPU/分配/序列化/帧数/字节和 median/p95/max 默认写到 `/private/tmp/neux-chat-backend-ab.json`。p95 使用 nearest-rank，5 次时等于该组实测最大值。独立工作区文件捕获不在这一 A/B 中，旧提交没有对应功能，须单列新功能成本。
