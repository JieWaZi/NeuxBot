/**
 * 29b609b 与工作树的真实投影/组摘要算法 A/B；不启动页面或模型。
 * node --expose-gc packages/frontend/scripts/performance/projection-ab.mjs \
 *   --baseline /private/tmp/neux-chat-ab-baseline --out /private/tmp/neux-chat-projection-ab.json
 * --smoke 只跑正确性与一次小量测；正式运行请等待独占 CPU 窗口。
 * WorkProcess 的纯计算块由 TypeScript AST 原样截取，记录源码 hash/行号。
 * React 调度、DOM、Markdown、网络与服务端时间均不包含在本量测中。
 */
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  mkdtemp,
  readFile,
  realpath,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { createRequire } from "node:module";
import { arch, cpus, platform, tmpdir } from "node:os";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../..",
);
const frontend = path.join(root, "packages/frontend");
const require = createRequire(path.join(frontend, "package.json"));
const ts = require("typescript");
const { build } = require("esbuild");
const args = process.argv.slice(2);
const option = (key, fallback) => {
  const index = args.indexOf(key);
  return index < 0 ? fallback : args[index + 1];
};
const baseline = path.resolve(
  option("--baseline", "/private/tmp/neux-chat-ab-baseline"),
);
const output = path.resolve(
  option("--out", "/private/tmp/neux-chat-projection-ab.json"),
);
const smoke = args.includes("--smoke");
const repetitions = smoke ? 1 : Number(option("--runs", "9"));
const cycles = smoke ? 1 : Number(option("--cycles", "20"));
assert(Number.isInteger(repetitions) && repetitions > 0);
assert(Number.isInteger(cycles) && cycles > 0);
const digest = (text) => createHash("sha256").update(text).digest("hex");
const fingerprints = [];
const temp = await mkdtemp(path.join(tmpdir(), "neux-projection-ab-"));
await symlink(
  path.join(frontend, "node_modules"),
  path.join(temp, "node_modules"),
);

function declarationName(statement) {
  return ts.isVariableStatement(statement)
    ? statement.declarationList.declarations[0]?.name.getText()
    : undefined;
}

async function summarySource(base, variant) {
  const filename = path.join(
    base,
    "packages/frontend/src/modules/chat/components/chat-thread.tsx",
  );
  const source = await readFile(filename, "utf8");
  const ast = ts.createSourceFile(
    filename,
    source,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const functions = [
    "toolArgsOf",
    "categoryFromTool",
    "isFileToolCategory",
    "formatToolActivitySummary",
    "statusFromTool",
  ];
  if (variant === "old")
    functions.push(
      "groupChatPart",
      "isChatProcessPart",
      "groupProcessChatPart",
    );
  const constants = ["toolCategoryKeys", "toolActionKeys"];
  const selected = ast.statements.filter(
    (statement) =>
      (ts.isFunctionDeclaration(statement) &&
        functions.includes(statement.name?.text)) ||
      constants.includes(declarationName(statement)),
  );
  for (const name of [...functions, ...constants])
    assert(
      selected.some(
        (statement) =>
          (statement.name?.text ?? declarationName(statement)) === name,
      ),
      `missing source declaration ${name}`,
    );
  const work = ast.statements.find(
    (statement) =>
      (ts.isFunctionDeclaration(statement) &&
        statement.name?.text === "WorkProcess") ||
      declarationName(statement) === "WorkProcess",
  );
  const body = ts.isFunctionDeclaration(work)
    ? work.body
    : work.declarationList.declarations[0].initializer.arguments[0].body;
  const start = body.statements.findIndex(
    (statement) => declarationName(statement) === "tools",
  );
  const end = body.statements.findIndex(
    (statement) => declarationName(statement) === "Icon",
  );
  assert(start >= 0 && end > start);
  const blockStatements = body.statements.slice(start, end);
  assert.deepEqual(blockStatements.map(declarationName).filter(Boolean), [
    "tools",
    "categories",
    "activities",
    "summary",
  ]);
  assert.equal(blockStatements.filter(ts.isForOfStatement).length, 1);
  const block = source.slice(
    body.statements[start].getFullStart(),
    body.statements[end].getFullStart(),
  );
  assert.equal(
    block,
    source.substring(
      body.statements[start].getFullStart(),
      body.statements[end].getFullStart(),
    ),
  );
  let prelude = "";
  if (variant === "old") {
    const indexed = body.statements.find(
      (statement) => declarationName(statement) === "indexedParts",
    );
    const callback =
      indexed.declarationList.declarations[0].initializer.arguments[0];
    assert(ts.isArrowFunction(callback));
    prelude = `const indexedParts = ${callback.body.getText(ast)};`;
  }
  fingerprints.push({
    variant,
    filename,
    sha256: digest(source),
    summaryBlockSha256: digest(block),
    summaryBlockLines: [
      ast.getLineAndCharacterOfPosition(body.statements[start].getStart())
        .line + 1,
      ast.getLineAndCharacterOfPosition(body.statements[end].getStart()).line,
    ],
    sourceDeclarations: selected.map((statement) => ({
      name: statement.name?.text ?? declarationName(statement),
      sha256: digest(statement.getText(ast)),
    })),
    extraction:
      "verbatim AST declaration text and WorkProcess tools..summary statements; indexedParts uses original useMemo callback expression",
  });
  return (
    selected.map((statement) => statement.getText(ast)).join("\n") +
    `\nexport function summarize(${variant === "old" ? "messageContent, indices, messageIsRunning, t" : "group, t"}) {\n${prelude}\n${block}\nreturn { categories, summary };\n}\n` +
    (variant === "old" ? "export { groupProcessChatPart };\n" : "")
  );
}

async function loadVariant(base, variant) {
  const projection = path.join(
    base,
    "packages/frontend/src/modules/chat/runtime/chat-projection.ts",
  );
  if (variant === "old") {
    for (const relative of [
      "packages/frontend/src/modules/chat/runtime/chat-projection.ts",
      "packages/frontend/src/modules/chat/components/chat-thread.tsx",
    ]) {
      const committed = execFileSync("git", ["show", `29b609b:${relative}`], {
        cwd: root,
      });
      assert.equal(
        digest(committed),
        digest(await readFile(path.join(base, relative))),
        `baseline does not match pinned source: ${relative}`,
      );
    }
  } else {
    for (const relative of [
      "packages/frontend/src/modules/chat/runtime/chat-live.ts",
      "packages/frontend/src/modules/chat/components/chat-message-nodes.ts",
    ])
      fingerprints.push({
        variant,
        filename: path.join(base, relative),
        sha256: digest(await readFile(path.join(base, relative))),
      });
  }
  fingerprints.push({
    variant,
    filename: projection,
    sha256: digest(await readFile(projection)),
  });
  const summary = await summarySource(base, variant);
  const entry =
    variant === "old"
      ? `import { CHAT_PROCESS_COMMENTARY_PART } from ${JSON.stringify(projection)}; export { toThreadMessage, applyChatEvent, mergeAuthoritativeMessages } from ${JSON.stringify(projection)};\n${summary}`
      : `export { mergeAuthoritativeMessages } from ${JSON.stringify(projection)}; export { applyChatLiveFrame } from ${JSON.stringify(path.join(base, "packages/frontend/src/modules/chat/runtime/chat-live.ts"))}; export { ChatMessageProjection } from ${JSON.stringify(path.join(base, "packages/frontend/src/modules/chat/components/chat-message-nodes.ts"))};\n${summary}`;
  const outfile = path.join(temp, `${variant}.mjs`);
  await build({
    stdin: {
      contents: entry,
      resolveDir: path.join(base, "packages/frontend"),
      loader: "ts",
    },
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
    target: "node22",
    packages: "external",
    alias: { "@": path.join(base, "packages/frontend/src") },
    plugins: [
      {
        name: "unused-failure-translations",
        setup(builder) {
          builder.onResolve({ filter: /\/shared\/i18n$/ }, () => ({
            path: "i18n",
            namespace: "benchmark",
          }));
          builder.onLoad({ filter: /.*/, namespace: "benchmark" }, () => ({
            contents: "export const i18n = { t: (key) => key };",
            loader: "js",
          }));
        },
      },
    ],
  });
  return import(pathToFileURL(outfile));
}

const words = {
  "toolActivitySummary.completedPrefix": "已",
  "toolActivitySummary.runningPrefix": "正在",
  "toolActivitySummary.file": "文件",
  "toolActivitySummary.verb.file_read": "读取",
  "toolActivitySummary.verb.file_search": "搜索",
  "toolActivitySummary.verb.file_modify": "编辑",
  "toolActivitySummary.verb.command": "执行命令",
  "toolActivitySummary.verb.network": "访问网络",
  "toolActivitySummary.verb.mcp": "调用外部服务",
  "toolActivitySummary.verb.skill": "使用技能",
  "toolActivitySummary.verb.other": "执行操作",
};
const t = (key) => {
  assert(key in words, `unexpected translation ${key}`);
  return words[key];
};
const categories = [
  "file_read",
  "file_search",
  "file_modify",
  "command",
  "network",
  "mcp",
  "skill",
  "other",
];
const actions = [
  "read_file",
  "search_files",
  "modify_file",
  "execute_command",
  "fetch_web",
  "call_service",
  "use_skill",
  "other",
];
const tool = (index, scope) => ({
  id: `${scope}:tool:${index}`,
  type: "tool",
  toolCallId: `native-${index}`,
  toolCategory: categories[index % categories.length],
  toolAction: actions[index % actions.length],
  toolStatus: "completed",
  toolLabel: `${scope}-工具${index}`,
  toolSafeSummary: { kind: "file", path: `/project/file-${index}.ts` },
});

function fixture(historyCount) {
  const history = Array.from({ length: historyCount }, (_, index) =>
    index % 2 === 0
      ? {
          id: `history-${index}`,
          role: "user",
          content: `历史问题${index}：请检查配置。`,
          createdAt: 1000 + index,
        }
      : {
          id: `history-${index}`,
          role: "assistant",
          status: "completed",
          content: `历史答复${index}：已完成检查。`,
          createdAt: 1000 + index,
          displayParts: [
            tool(0, `history-${index}`),
            tool(1, `history-${index}`),
            {
              id: "answer",
              type: "text",
              text: `历史答复${index}：已完成检查。`,
            },
          ],
        },
  );
  const body = "开始\n";
  const active = {
    id: "active",
    executionId: "active",
    role: "assistant",
    status: "running",
    revision: 0,
    createdAt: 2000,
    updatedAt: 2000,
    content: `我来检查工作范围。\n${body}`,
    displayParts: [
      { id: "intro", type: "process_commentary", text: "我来检查工作范围。\n" },
      ...Array.from({ length: 100 }, (_, index) => tool(index, "active")),
      { id: "body", type: "text", text: body },
    ],
  };
  let text = body;
  const oldInputs = [],
    newInputs = [];
  for (let index = 0; index < 100; index++) {
    const prefix = `追加正文${String(index).padStart(3, "0")} `;
    const delta = prefix + "x".repeat(511 - Buffer.byteLength(prefix)) + "\n";
    assert.equal(Buffer.byteLength(delta), 512);
    text += delta;
    const message = {
      ...active,
      revision: index + 1,
      updatedAt: 2001 + index,
      content: `我来检查工作范围。\n${text}`,
      displayParts: [
        ...active.displayParts.slice(0, -1),
        { id: "body", type: "text", text },
      ],
    };
    oldInputs.push(
      JSON.stringify({
        type: "message.updated",
        recordedAt: 2001 + index,
        message,
      }),
    );
    newInputs.push(
      JSON.stringify({
        messageId: "active",
        executionId: "active",
        generation: "benchmark-generation",
        sequence: index + 1,
        baseRevision: index,
        revision: index + 1,
        updates: [
          {
            baseRevision: index,
            revision: index + 1,
            updatedAt: 2001 + index,
            parts: [
              {
                kind: "text_append",
                partId: "body",
                partType: "text",
                text: delta,
              },
            ],
          },
        ],
      }),
    );
  }
  return {
    initial: [...history, active],
    oldInputs,
    newInputs,
    historyCount,
    bodyUtf8Bytes: Buffer.byteLength(text),
    finalText: text,
    appends: 100,
    completedTools: 100,
  };
}

function toolGroups(nodes) {
  return nodes.flatMap((node) =>
    node.kind === "tools" ? [node] : node.nodes ? toolGroups(node.nodes) : [],
  );
}
function legacyGroups(tree) {
  return tree.flatMap((node) =>
    node.type === "group" && node.key === "group-work-process"
      ? [node]
      : node.children
        ? legacyGroups(node.children)
        : [],
  );
}
function leafParts(nodes) {
  return nodes.flatMap((node) =>
    node.part ? [node.part] : leafParts(node.nodes ?? []),
  );
}
function normalizedPart(part, old) {
  if (part.type === "tool-call" || part.type === "tool")
    return {
      type: "tool",
      id: old ? part.toolCallId : part.id,
      category: old ? part.args.category : part.toolCategory,
      action: old ? part.args.action : part.toolAction,
      status: old ? part.args.status : part.toolStatus,
      label: old ? part.args.label : part.toolLabel,
      summary: old ? part.args.safeSummary : part.toolSafeSummary,
    };
  if (part.type === "data")
    return { type: "process_commentary", text: part.data.text };
  return { type: part.type, text: part.text };
}
function normalizeOld(messages) {
  return messages.map((message) => ({
    id: message.id,
    role: message.role,
    status: message.role === "assistant" ? message.status.type : undefined,
    parts: message.content.map((part) => normalizedPart(part, true)),
  }));
}
function normalizeNew(views) {
  return views.map((view) => ({
    id: view.message.id,
    role: view.message.role,
    status:
      view.message.role === "assistant"
        ? view.message.status === "running"
          ? "running"
          : "complete"
        : undefined,
    parts:
      view.message.role === "user"
        ? [{ type: "text", text: view.message.content }]
        : leafParts(view.segments).map((part) => normalizedPart(part, false)),
  }));
}

try {
  const old = await loadVariant(baseline, "old"),
    current = await loadVariant(root, "new");
  const reactPackage = await realpath(
    path.join(
      baseline,
      "packages/frontend/node_modules/@assistant-ui/react/package.json",
    ),
  );
  const baselineRequire = createRequire(reactPackage);
  const corePath = baselineRequire.resolve("@assistant-ui/core/internal");
  const core = await import(pathToFileURL(corePath));
  const groupingPath = path.join(
    path.dirname(corePath),
    "react/utils/groupParts.js",
  );
  const { buildGroupTree } = await import(pathToFileURL(groupingPath));
  for (const filename of [
    path.join(
      path.dirname(corePath),
      "runtimes/external-store/thread-message-converter.js",
    ),
    path.join(
      path.dirname(corePath),
      "runtimes/external-store/external-store-thread-runtime-core.js",
    ),
    groupingPath,
  ])
    fingerprints.push({
      variant: "old-library",
      filename,
      sha256: digest(await readFile(filename)),
    });

  function runVariant(variant, data, counting) {
    let messages = structuredClone(data.initial);
    const authoritative = messages;
    let overlay = [messages.at(-1)];
    const counts = {
      convertedMessages: 0,
      convertedHistoricalMessages: 0,
      rebuiltToolParts: 0,
      rebuiltToolArgs: 0,
      builtPartNodes: 0,
      inspectedParts: 0,
      rebuiltGroups: 0,
      summaryCalls: 0,
      reusedHistoricalMessages: 0,
    };
    let runtime,
      projection,
      views,
      summaries = [],
      previousContent,
      previousToolParts = new Map(),
      previousToolArgs = new Map(),
      previousGroups = new Map();
    const convert = counting
      ? (message) => {
          counts.convertedMessages++;
          if (message.id !== "active") counts.convertedHistoricalMessages++;
          return old.toThreadMessage(message);
        }
      : (message) => old.toThreadMessage(message);
    const adapter = {
      messages,
      convertMessage: convert,
      isRunning: true,
      onNew: async () => {},
    };
    const beforeInit = performance.now();
    if (variant === "old")
      runtime = new core.ExternalStoreThreadRuntimeCore(
        new core.CompositeContextProvider(),
        adapter,
      );
    else {
      projection = new current.ChatMessageProjection(
        counting
          ? {
              node: () => counts.builtPartNodes++,
              group: () => counts.rebuiltGroups++,
              inspect: () => counts.inspectedParts++,
            }
          : undefined,
        "ab",
      );
      views = projection.update(messages);
    }
    const initMs = performance.now() - beforeInit;
    const initialViews =
      variant === "old" ? runtime.messages.slice(0, -1) : views.slice(0, -1);

    function summarizeChanged() {
      if (variant === "old") {
        const message = runtime.messages.at(-1);
        if (message.content !== previousContent) {
          const tree = buildGroupTree(
            message.content.map(old.groupProcessChatPart),
            message.content.map((part) =>
              part.type === "tool-call" ? part.toolCallId : undefined,
            ),
          );
          const groups = legacyGroups(tree);
          summaries = groups.map((group) => {
            if (counting) counts.summaryCalls++;
            return old.summarize(message.content, group.indices, true, t);
          });
          if (counting) counts.rebuiltGroups += countGroups(tree);
          previousContent = message.content;
        }
      } else {
        const groups = toolGroups(views.at(-1).segments);
        summaries = groups.map((group) => {
          const previous = previousGroups.get(group.key);
          if (previous?.group === group) return previous.summary;
          if (counting) counts.summaryCalls++;
          const summary = current.summarize(group, t);
          previousGroups.set(group.key, { group, summary });
          return summary;
        });
      }
    }
    function inspectTools() {
      const nodes =
        variant === "old"
          ? runtime.messages
              .at(-1)
              .content.filter((part) => part.type === "tool-call")
              .map((part) => [part.toolCallId, part, part.args])
          : toolGroups(views.at(-1).segments).flatMap((group) =>
              group.nodes.map((node) => [node.key, node, node.part]),
            );
      const parts = new Map(),
        toolsArgs = new Map();
      for (const [id, part, args] of nodes) {
        if (previousToolParts.has(id) && previousToolParts.get(id) !== part)
          counts.rebuiltToolParts++;
        if (previousToolArgs.has(id) && previousToolArgs.get(id) !== args)
          counts.rebuiltToolArgs++;
        parts.set(id, part);
        toolsArgs.set(id, args);
      }
      previousToolParts = parts;
      previousToolArgs = toolsArgs;
    }
    summarizeChanged();
    if (counting) inspectTools();
    const initialCounts = { ...counts };
    for (const key of Object.keys(counts)) counts[key] = 0;
    const stages = { decodeMs: 0, foldAndProjectionMs: 0, groupSummaryMs: 0 };
    const appendCpuStart = process.cpuUsage(),
      appendStarted = performance.now();
    for (let index = 0; index < data.appends; index++) {
      let started = performance.now();
      const event = JSON.parse(
        (variant === "old" ? data.oldInputs : data.newInputs)[index],
      );
      stages.decodeMs += performance.now() - started;
      started = performance.now();
      if (variant === "old") {
        overlay = old.applyChatEvent(overlay, event);
        messages = old.mergeAuthoritativeMessages(overlay, authoritative);
        runtime.__internal_setAdapter({ ...adapter, messages });
      } else {
        overlay = [current.applyChatLiveFrame(overlay.at(-1), event)];
        messages = current.mergeAuthoritativeMessages(overlay, authoritative);
        views = projection.update(messages);
      }
      stages.foldAndProjectionMs += performance.now() - started;
      started = performance.now();
      summarizeChanged();
      stages.groupSummaryMs += performance.now() - started;
      if (counting) inspectTools();
    }
    const appendWallMs = performance.now() - appendStarted;
    const appendCpu = process.cpuUsage(appendCpuStart);
    if (counting) {
      const finalViews = variant === "old" ? runtime.messages : views;
      counts.reusedHistoricalMessages = initialViews.filter(
        (view, index) => view === finalViews[index],
      ).length;
    }
    return {
      initMs,
      stages,
      appendWallMs,
      appendCpuMs: (appendCpu.user + appendCpu.system) / 1000,
      counts,
      initialCounts,
      semantic: counting
        ? {
            messages:
              variant === "old"
                ? normalizeOld(runtime.messages)
                : normalizeNew(views),
            summaries,
          }
        : undefined,
    };
  }
  function countGroups(tree) {
    return tree.reduce(
      (count, node) =>
        count +
        (node.type === "group" ? 1 : 0) +
        countGroups(node.children ?? []),
      0,
    );
  }
  const workloads = [fixture(0), fixture(500)];
  const correctness = workloads.map((data) => {
    const a = runVariant("old", data, true),
      b = runVariant("new", data, true);
    assert.deepEqual(
      a.semantic,
      b.semantic,
      "old/new final display semantics must match",
    );
    assert.equal(
      a.counts.convertedHistoricalMessages,
      0,
      "actual legacy WeakMap must retain stable history",
    );
    assert.equal(b.counts.rebuiltToolParts, 0);
    assert.equal(b.counts.rebuiltGroups, 0);
    assert.equal(b.counts.summaryCalls, 0);
    return {
      historyMessages: data.historyCount,
      appends: data.appends,
      completedTools: data.completedTools,
      finalBodyUtf8Bytes: data.bodyUtf8Bytes,
      semanticSha256: digest(JSON.stringify(a.semantic)),
      old: a,
      new: b,
    };
  });
  const results = [];
  for (const data of workloads) {
    // 预热与计数 pass 不进入 CPU 量测；每一轮交替旧/新先后，初始化成本另列。
    if (!smoke) {
      runVariant("old", data, false);
      runVariant("new", data, false);
    }
    const runs = { old: [], new: [] };
    for (let round = 0; round < repetitions; round++) {
      for (const variant of round % 2 === 0 ? ["old", "new"] : ["new", "old"]) {
        global.gc?.();
        const cpu = process.cpuUsage(),
          started = performance.now();
        let stages = { decodeMs: 0, foldAndProjectionMs: 0, groupSummaryMs: 0 },
          initMs = 0,
          wallMs = 0,
          cpuMs = 0;
        for (let cycle = 0; cycle < cycles; cycle++) {
          const value = runVariant(variant, data, false);
          for (const key of Object.keys(stages))
            stages[key] += value.stages[key];
          initMs += value.initMs;
          wallMs += value.appendWallMs;
          cpuMs += value.appendCpuMs;
        }
        const cpuUsed = process.cpuUsage(cpu);
        runs[variant].push({
          round: round + 1,
          order: round % 2 === 0 ? "old-new" : "new-old",
          cycles,
          wallMs,
          cpuMs,
          lifecycleWallMs: performance.now() - started,
          lifecycleCpuMs: (cpuUsed.user + cpuUsed.system) / 1000,
          lifecycleCpuUserMs: cpuUsed.user / 1000,
          lifecycleCpuSystemMs: cpuUsed.system / 1000,
          initMs,
          stages,
        });
      }
    }
    results.push({
      historyMessages: data.historyCount,
      appendsPerCycle: data.appends,
      completedToolsInActiveMessage: data.completedTools,
      cyclesPerRun: cycles,
      rawInputBytesPerCycle: {
        old: data.oldInputs.reduce(
          (sum, value) => sum + Buffer.byteLength(value),
          0,
        ),
        new: data.newInputs.reduce(
          (sum, value) => sum + Buffer.byteLength(value),
          0,
        ),
      },
      old: summarizeRuns(runs.old),
      new: summarizeRuns(runs.new),
    });
  }
  const report = {
    baselineRef: "29b609b",
    harnessSha256: digest(await readFile(fileURLToPath(import.meta.url))),
    currentHead: execFileSync("git", ["rev-parse", "HEAD"], {
      cwd: root,
      encoding: "utf8",
    }).trim(),
    currentIncludesWorkingTree: true,
    smoke,
    environment: {
      node: process.version,
      platform: platform(),
      arch: arch(),
      cpu: cpus()[0]?.model,
      logicalCpus: cpus().length,
      gcBeforeEachMeasuredVariant: typeof global.gc === "function",
    },
    scope:
      "Actual old toThreadMessage + assistant-ui ExternalStoreThreadRuntimeCore/WeakMap/repository + GroupedParts buildGroupTree + verbatim WorkProcess summary; current applyChatLiveFrame + ChatMessageProjection + same verbatim summary only when group identity changes. Both variants fold active-only overlay then call their real mergeAuthoritativeMessages for the history window. cpuMs/wallMs and stages cover only steady-state appends including JSON decoding; fixture clone/setup and initialization are separately included in lifecycleCpuMs/lifecycleWallMs and initMs. Excludes React scheduling/render, DOM/layout, Markdown, WS/network, server replay and snapshot precomputation. Only active running process group is summarized, matching collapsed completed history; translation uses the same deterministic words lookup. Counts are a separate real-execution pass, not inferred from loop sizes.",
    unusedFailureTranslationStub:
      "runtime failure i18n is stubbed; fixtures contain no failure and never call it",
    fingerprints,
    correctness,
    results,
  };
  await writeFile(output, `${JSON.stringify(report, null, 2)}\n`);
  process.stdout.write(
    `${JSON.stringify({ output, smoke, results, counts: correctness.map(({ historyMessages, old, new: next }) => ({ historyMessages, old: old.counts, new: next.counts })) }, null, 2)}\n`,
  );
} finally {
  await rm(temp, { recursive: true, force: true });
}

function summarizeRuns(runs) {
  const stats = (key) => {
    const values = runs.map((run) => run[key]).sort((a, b) => a - b);
    return {
      median: values[Math.floor(values.length / 2)],
      p95: values[Math.ceil(values.length * 0.95) - 1],
      max: values.at(-1),
    };
  };
  const perCycle = (key) => {
    const values = runs
      .map((run) => run[key] / run.cycles)
      .sort((a, b) => a - b);
    return {
      median: values[Math.floor(values.length / 2)],
      p95: values[Math.ceil(values.length * 0.95) - 1],
      max: values.at(-1),
    };
  };
  return {
    cpuMs: stats("cpuMs"),
    wallMs: stats("wallMs"),
    per100AppendsCpuMs: perCycle("cpuMs"),
    per100AppendsWallMs: perCycle("wallMs"),
    runs,
  };
}
