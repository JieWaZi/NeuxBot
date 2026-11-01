/**
 * 受控语法算法比较：累计全文 vs 相同语法的增量尾部。
 * 这不是旧版 Streamdown 的完整渲染或真实平台 PERF-03 结论。
 * 在仓库根运行 node --experimental-transform-types packages/frontend/scripts/benchmark-markdown.mjs。
 */
import { performance } from "node:perf_hooks";
import { cpus, platform, arch } from "node:os";
import { parseGfm, parseGfmWithMath } from "../src/shared/markdown/parse.ts";
import { IncrementalMarkdownParser } from "../src/shared/markdown/incremental.ts";

const paragraphs = Array.from(
  { length: 100 },
  (_, index) =>
    `段落 ${index} **中文强调。**后续说明。\n\n- 步骤一\n  - 嵌套说明\n\n| 名称 | 值 |\n| --- | ---: |\n| item | ${index} |`,
).join("\n\n");
const code = Array.from(
  { length: 2000 },
  (_, index) => `const value${index} = ${index};`,
).join("\n");
const base = `${paragraphs}\n\n\`\`\`ts\n${code}\n\`\`\`\n\n`;
const remaining = Math.max(0, 100 * 1024 - Buffer.byteLength(base));
const filler = "Closing paragraph.\n\n";
const source = `${base}${filler.repeat(Math.ceil(remaining / filler.length)).slice(0, remaining)}`;
const prefixes = Array.from({ length: 100 }, (_, index) =>
  source.slice(0, Math.ceil((source.length * (index + 1)) / 100)),
);

function measure(incremental) {
  let parsedUnits = 0;
  let parseCalls = 0;
  let finalBlocks = 0;
  const grammar = (text) => {
    parsedUnits += text.length;
    parseCalls += 1;
    return parseGfm(text);
  };
  const parser = incremental
    ? new IncrementalMarkdownParser(grammar)
    : undefined;
  const start = performance.now();
  for (const prefix of prefixes) {
    if (parser) {
      const result = parser.update(prefix);
      finalBlocks = result.frozen.length + result.tail.length;
    } else finalBlocks = grammar(prefix).children.length;
  }
  const streamingMs = performance.now() - start;
  const settleStart = performance.now();
  const settledBlocks = parseGfmWithMath(source).children.length;
  return {
    streamingMs,
    settledMs: performance.now() - settleStart,
    parsedUnits,
    parseCalls,
    finalBlocks,
    settledBlocks,
  };
}
// 独立预热；两种算法按轮交替先后次序。
measure(false);
measure(true);
const full = [],
  incremental = [];
for (let run = 0; run < 5; run += 1) {
  if (run % 2 === 0) {
    full.push(measure(false));
    incremental.push(measure(true));
  } else {
    incremental.push(measure(true));
    full.push(measure(false));
  }
}
function summarize(runs) {
  const sorted = runs.map((run) => run.streamingMs).sort((a, b) => a - b);
  return { medianMs: sorted[2], p95Ms: sorted[4], maxMs: sorted[4], runs };
}
process.stdout.write(
  `${JSON.stringify(
    {
      scope:
        "same-grammar parsing only; excludes React/DOM/highlighting, transport and old Streamdown baseline",
      environment: {
        node: process.version,
        platform: platform(),
        arch: arch(),
        cpu: cpus()[0]?.model,
      },
      workload: {
        utf16Units: source.length,
        utf8Bytes: Buffer.byteLength(source),
        batches: prefixes.length,
        codeLines: 2000,
        measuredRuns: 5,
        warmed: true,
      },
      fullParse: summarize(full),
      incrementalParse: summarize(incremental),
    },
    null,
    2,
  )}\n`,
);
