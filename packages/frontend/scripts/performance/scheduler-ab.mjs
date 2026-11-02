/**
 * 读取真实旧/新批量调度器，用同一虚拟时钟测 enqueue→onBatch；不打开页面。
 * 只说明调度等待，不能代替真实浏览器的 React commit、layout 或 paint。
 * node packages/frontend/scripts/performance/scheduler-ab.mjs [基线提交]
 */
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { cpus } from "node:os";
import { fileURLToPath } from "node:url";
import { runInNewContext } from "node:vm";
import { performance } from "node:perf_hooks";

const root = fileURLToPath(new URL("../../../../", import.meta.url));
const sourcePath =
  "packages/frontend/src/modules/chat/runtime/chat-event-batcher.ts";
const baselineRevision = process.argv[2] ?? "29b609b";
const require = createRequire(
  new URL("../../node_modules/vite/package.json", import.meta.url),
);
const { transformSync } = require("esbuild");
const sources = {
  before: execFileSync("git", ["show", `${baselineRevision}:${sourcePath}`], {
    cwd: root,
    encoding: "utf8",
  }),
  after: readFileSync(
    new URL(`../../../../${sourcePath}`, import.meta.url),
    "utf8",
  ),
  previousThreeFrames: readFileSync(
    new URL(
      "../../../../docs/discussions/chat-presentation-refactor-evidence/performance-review-batcher-before.ts.txt",
      import.meta.url,
    ),
    "utf8",
  ),
};
const variants = Object.keys(sources);
const compiled = Object.fromEntries(
  Object.entries(sources).map(([name, source]) => [
    name,
    transformSync(source, { loader: "ts", format: "cjs", target: "es2023" })
      .code,
  ]),
);
function createClock(hz) {
  let now = 0;
  let nextId = 1;
  const tasks = new Map();
  const add = (at, callback) => {
    const id = nextId++;
    tasks.set(id, { at, callback });
    return id;
  };
  const window = {
    setTimeout: (callback, delay = 0) => add(now + delay, callback),
    clearTimeout: (id) => tasks.delete(id),
    requestAnimationFrame: (callback) => {
      const frameMs = 1000 / hz;
      const at = (Math.floor((now + 1e-7) / frameMs) + 1) * frameMs;
      return add(at, () => callback(now));
    },
    cancelAnimationFrame: (id) => tasks.delete(id),
  };
  return {
    window,
    now: () => now,
    queueMicrotask: (callback) => add(now, callback),
    advanceTo(target) {
      for (;;) {
        let selected;
        for (const [id, task] of tasks)
          if (
            task.at <= target + 1e-7 &&
            (!selected || task.at < selected[1].at)
          )
            selected = [id, task];
        if (!selected) break;
        tasks.delete(selected[0]);
        now = selected[1].at;
        selected[1].callback();
      }
      now = target;
    },
    pending: () => tasks.size,
  };
}
const percentile = (values, fraction) =>
  [...values].sort((a, b) => a - b)[Math.ceil(values.length * fraction) - 1];
function measure(variant, hz, intervalMs, phaseMs) {
  const clock = createClock(hz);
  const module = { exports: {} };
  runInNewContext(compiled[variant], {
    module,
    exports: module.exports,
    window: clock.window,
    queueMicrotask: clock.queueMicrotask,
  });
  const entered = [];
  const latencies = [];
  let covered = 0;
  let batches = 0;
  let terminalDelayMs;
  let terminalAt;
  const batcher = module.exports.createChatEventBatcher((events) => {
    batches++;
    const coverage = Math.max(
      covered,
      ...events.map(
        (event) => event.live?.sequence ?? event.message?.revision ?? 0,
      ),
    );
    for (let index = covered; index < coverage; index++)
      latencies.push(clock.now() - entered[index]);
    covered = coverage;
    if (events.some((event) => event.type === "execution.completed"))
      terminalDelayMs = clock.now() - terminalAt;
  });
  const started = performance.now();
  for (let index = 0; index < 100; index++) {
    clock.advanceTo(phaseMs + index * intervalMs);
    entered.push(clock.now());
    const event =
      variant === "before"
        ? {
            transient: true,
            type: "runtime.event",
            message: {
              id: "message",
              revision: index + 1,
              content: "x".repeat((index + 1) * 512),
            },
          }
        : {
            transient: true,
            type: "runtime.event",
            live: {
              generation: "one",
              sequence: index + 1,
              messageId: "message",
              executionId: "message",
              baseRevision: index,
              revision: index + 1,
              updates: [
                {
                  baseRevision: index,
                  revision: index + 1,
                  updatedAt: clock.now(),
                  parts: [
                    {
                      kind: "text_append",
                      partId: "body",
                      partType: "text",
                      text: "x".repeat(512),
                    },
                  ],
                },
              ],
            },
          };
    batcher.push(event);
  }
  // 在最后一批正文尚未全部交付时插入真实终态，核验原调用方传入的 immediate 边界。
  terminalAt = clock.now() + 2;
  clock.advanceTo(terminalAt);
  batcher.push(
    {
      type: "execution.completed",
      message: { id: "message", revision: 100, content: "x".repeat(51200) },
    },
    true,
  );
  clock.advanceTo(terminalAt + 100);
  batcher.close();
  if (
    covered !== 100 ||
    latencies.length !== 100 ||
    terminalDelayMs !== 0 ||
    clock.pending() !== 0
  )
    throw new Error("调度覆盖、终态或清理校验失败");
  return {
    hz,
    intervalMs,
    phaseMs,
    sourceEvents: 100,
    batches,
    firstPublishMs: latencies[0],
    p50Ms: percentile(latencies, 0.5),
    p95Ms: percentile(latencies, 0.95),
    maxMs: Math.max(...latencies),
    terminalDelayMs,
    simulationWallMs: performance.now() - started,
  };
}
const results = [];
for (const hz of [60, 120])
  for (const intervalMs of [5, 20, 80]) {
    const pairs = [];
    for (const phaseMs of [1, 3, 7, 11, 13])
      pairs.push(
        Object.fromEntries(
          variants.map((variant) => [
            variant,
            measure(variant, hz, intervalMs, phaseMs),
          ]),
        ),
      );
    const summary = (variant) => {
      const all = pairs.map((pair) => pair[variant]);
      return {
        firstPublishMedianMs: percentile(
          all.map((row) => row.firstPublishMs),
          0.5,
        ),
        p50MedianMs: percentile(
          all.map((row) => row.p50Ms),
          0.5,
        ),
        p95WorstPhaseMs: Math.max(...all.map((row) => row.p95Ms)),
        averageBatches:
          all.reduce((sum, row) => sum + row.batches, 0) / all.length,
        terminalDelayMs: 0,
      };
    };
    results.push({
      hz,
      intervalMs,
      ...Object.fromEntries(
        variants.map((variant) => [variant, summary(variant)]),
      ),
      pairs,
    });
  }
process.stdout.write(
  JSON.stringify(
    {
      scope:
        "实际旧/新调度器的受控 enqueue→onBatch 等待；虚拟60/120Hz，不含真实React提交/布局/绘制或旧smooth额外等待",
      baselineRevision,
      comparisonScope:
        "before为旧16ms源码；previousThreeFrames为首次A/B实测三帧源码归档；after为当前单帧产品源码。三者均执行真实实现，无候选替换",
      environment: {
        node: process.version,
        platform: process.platform,
        arch: process.arch,
        cpu: cpus()[0]?.model,
      },
      sourceHashes: Object.fromEntries(
        Object.entries(sources).map(([name, source]) => [
          name,
          createHash("sha256").update(source).digest("hex"),
        ]),
      ),
      workload: {
        inputBatches: 100,
        bytesPerInput: 512,
        phasesMs: [1, 3, 7, 11, 13],
        terminal: "lastInput+2ms, flushImmediately=true",
      },
      results,
    },
    null,
    2,
  ) + "\n",
);
