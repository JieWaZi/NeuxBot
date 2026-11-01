import { spawn } from "node:child_process";
import { readFile, writeFile, mkdtemp } from "node:fs/promises";
import { tmpdir, cpus, platform, arch, release } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";

const frontend = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const scratch = await mkdtemp(
  path.join(tmpdir(), "neux-markdown-component-ab-"),
);
const output =
  process.env.NEUX_MARKDOWN_AB_OUTPUT ??
  "/private/tmp/neux-chat-markdown-ab.json";
const warmRuns = process.env.NEUX_MARKDOWN_AB_WARM_RUNS ?? "3";
const runs = [];
const baseline =
  process.env.NEUX_MARKDOWN_AB_BASELINE ?? "/private/tmp/neux-chat-ab-baseline";
const entry = "src/modules/chat/components/chat-message-content.tsx";
const sources = {};
for (const [name, root] of [
  ["baseline", path.join(baseline, "packages/frontend")],
  ["current", frontend],
]) {
  const file = path.join(root, entry);
  sources[name] = {
    entry: file,
    sha256: createHash("sha256")
      .update(await readFile(file))
      .digest("hex"),
  };
}
const versions = {};
for (const name of [
  "react",
  "react-dom",
  "streamdown",
  "shiki",
  "vitest",
  "jsdom",
  "@assistant-ui/react-streamdown",
  "@streamdown/code",
]) {
  const root =
    name === "@assistant-ui/react-streamdown" || name === "@streamdown/code"
      ? path.join(baseline, "packages/frontend")
      : frontend;
  versions[name] = JSON.parse(
    await readFile(
      path.join(root, "node_modules", name, "package.json"),
      "utf8",
    ),
  ).version;
}
// Serial children keep the measured renderer off the other implementation's
// module cache and prevent the two CPU measurements from competing.
const workloads = process.env.NEUX_MARKDOWN_AB_WORKLOADS?.split(",") ?? [
  "short-2kib",
  "long-100kib",
  "plain-2kib",
];
for (const workload of workloads) {
  for (const variant of ["baseline", "current"]) {
    const raw = path.join(scratch, `${variant}-${workload}.json`);
    console.log(
      `Markdown A/B ${variant}/${workload}; per-trial raw ${raw}; progress ${raw}.progress.jsonl`,
    );
    await new Promise((resolve, reject) => {
      const child = spawn(
        process.execPath,
        [
          "node_modules/vitest/vitest.mjs",
          "run",
          "--config",
          "scripts/performance/markdown-ab.config.mjs",
        ],
        {
          cwd: frontend,
          stdio: "inherit",
          env: {
            ...process.env,
            NEUX_MARKDOWN_AB_VARIANT: variant,
            NEUX_MARKDOWN_AB_WORKLOAD: workload,
            NEUX_MARKDOWN_AB_WARM_RUNS: warmRuns,
            NEUX_MARKDOWN_AB_OUTPUT: raw,
          },
        },
      );
      child.once("error", reject);
      child.once("exit", (code) =>
        code === 0
          ? resolve()
          : reject(new Error(`${variant}/${workload}: exit ${code}`)),
      );
    });
    runs.push(JSON.parse(await readFile(raw, "utf8")));
    await writeFile(
      output,
      JSON.stringify(
        {
          status: runs.length === workloads.length * 2 ? "complete" : "partial",
          generatedAt: new Date().toISOString(),
          baselineCommit: "29b609b",
          currentSource: "working-tree",
          sources,
          versions,
          warmRuns: Number(warmRuns),
          environment: {
            node: process.version,
            platform: platform(),
            release: release(),
            arch: arch(),
            cpu: cpus()[0]?.model,
          },
          measurement:
            "Actual production React Markdown components in jsdom; CPU/render/commit, no layout or paint",
          runs,
        },
        null,
        2,
      ),
    );
  }
}
console.log(`Raw Markdown component A/B: ${output}`);
