import { readFile, writeFile } from "node:fs/promises";

const input = process.argv[2] ?? "/private/tmp/neux-chat-markdown-ab.json";
const output =
  process.argv[3] ?? "/private/tmp/neux-chat-markdown-ab-summary.json";
const raw = JSON.parse(await readFile(input, "utf8"));
const median = (values) => {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
};
const range = (values) => ({
  min: Math.min(...values),
  median: median(values),
  max: Math.max(...values),
});
const p95 = (values) =>
  [...values].sort((a, b) => a - b)[Math.ceil(values.length * 0.95) - 1];
const rows = raw.runs.map((run) => {
  const cold = run.trials[0];
  const warm = run.trials.slice(1);
  return {
    variant: run.variant,
    workload: run.workload,
    cold: {
      cpuMs: cold.totalCpuMs,
      streamCpuMs: cold.streamCpuMs,
      completionCpuMs: cold.completionCpuMs,
      totalWallMs: cold.totalWallMs,
      completionWallMs: cold.completionWallMs,
      commits: cold.commits,
      renderMs: cold.profilerActualRenderMs,
      preCompletion: cold.preCompletion,
      inputUpdateP95Ms: p95(cold.inputUpdateWallMs),
      profilerRenderP95Ms: p95(
        cold.profilerEvents.map((event) => event.actualRenderMs),
      ),
    },
    warm: {
      runs: warm.length,
      cpuMs: range(warm.map((trial) => trial.totalCpuMs)),
      streamCpuMs: range(warm.map((trial) => trial.streamCpuMs)),
      completionCpuMs: range(warm.map((trial) => trial.completionCpuMs)),
      totalWallMs: range(warm.map((trial) => trial.totalWallMs)),
      completionWallMs: range(warm.map((trial) => trial.completionWallMs)),
      commits: range(warm.map((trial) => trial.commits)),
      renderMs: range(warm.map((trial) => trial.profilerActualRenderMs)),
      preCompletion: warm.map((trial) => trial.preCompletion),
      inputUpdateP95Ms: range(
        warm.map((trial) => p95(trial.inputUpdateWallMs)),
      ),
      profilerRenderP95Ms: range(
        warm.map((trial) =>
          p95(trial.profilerEvents.map((event) => event.actualRenderMs)),
        ),
      ),
    },
    audit: run.trials.map((trial) => ({
      run: trial.run,
      driverRenderCalls: trial.driverRenderCalls,
      rootCreateCount: trial.rootCreateCount,
      mountCommits: trial.profilerEvents.filter(
        (event) => event.phase === "mount",
      ).length,
      animationFrameCallbacks: trial.animationFrameCallbacks,
    })),
  };
});
await writeFile(
  output,
  JSON.stringify(
    { input, environment: raw.environment, measurement: raw.measurement, rows },
    null,
    2,
  ),
);
process.stdout.write(
  "| workload | variant | cold CPU ms | warm CPU median ms | cold completion ms | warm completion median ms | cold commits | warm commits median |\n| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |\n",
);
for (const row of rows)
  process.stdout.write(
    `| ${row.workload.name} | ${row.variant} | ${row.cold.cpuMs.toFixed(1)} | ${row.warm.cpuMs.median.toFixed(1)} | ${row.cold.completionWallMs.toFixed(1)} | ${row.warm.completionWallMs.median.toFixed(1)} | ${row.cold.commits} | ${row.warm.commits.median} |\n`,
  );
process.stdout.write(`Summary: ${output}\n`);
