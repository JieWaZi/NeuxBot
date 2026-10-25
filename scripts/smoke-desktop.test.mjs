import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  access,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("./smoke-desktop.cjs", import.meta.url));

async function runSmoke(t, overrides = {}) {
  const resources = await mkdtemp(
    path.join(os.tmpdir(), "neuxbot-smoke-test-"),
  );
  t.after(() => rm(resources, { recursive: true, force: true }));
  const moduleDirectory = path.join(
    resources,
    "terminal-runtime/node_modules/node-pty",
  );
  const record = path.join(resources, "pty-cwd.txt");
  await mkdir(moduleDirectory, { recursive: true });
  await writeFile(
    path.join(moduleDirectory, "index.js"),
    `
const { Worker } = require("node:worker_threads");
const { writeFileSync } = require("node:fs");
exports.spawn = (_shell, _args, options) => {
  writeFileSync(${JSON.stringify(record)}, options.cwd);
  // 模拟 Windows ConPTY：Shell 退出后，worker 继续维持事件循环。
  new Worker("setInterval(() => {}, 1000)", { eval: true });
  let onData;
  return {
    onData(handler) { onData = handler; },
    onExit(handler) {
      if (process.env.TEST_PTY_NEVER_EXIT === "1") return;
      setImmediate(() => {
        onData(process.env.TEST_PTY_OUTPUT ?? "ally-pty-ready");
        handler({ exitCode: Number(process.env.TEST_PTY_EXIT_CODE ?? "0") });
      });
    },
    kill() {},
  };
};
`,
  );
  const result = spawnSync(process.execPath, [script, resources], {
    env: { ...process.env, ...overrides },
    encoding: "utf8",
    timeout: 8000,
  });
  assert.equal(result.error, undefined, "残留 worker 不应导致冒烟进程超时");
  assert.equal(result.signal, null);
  const directory = await readFile(record, "utf8");
  await assert.rejects(access(directory), { code: "ENOENT" });
  return result;
}

test("PTY 断言通过后清理目录并退出，即使原生模块留下 worker", async (t) => {
  const result = await runSmoke(t);
  assert.equal(result.status, 0);
  assert.equal(result.stdout, "Packaged Electron PTY: passed\n");
  assert.equal(result.stderr, "");
});

test("Shell 失败或缺少预期输出时返回非零，不把残留 worker 当成成功", async (t) => {
  for (const overrides of [
    { TEST_PTY_EXIT_CODE: "7" },
    { TEST_PTY_OUTPUT: "unexpected-output" },
  ]) {
    const result = await runSmoke(t, overrides);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, "");
    assert.match(result.stderr, /AssertionError/);
  }
});

test("Shell 未退出时保留 PTY 超时失败并清理目录", async (t) => {
  const result = await runSmoke(t, { TEST_PTY_NEVER_EXIT: "1" });
  assert.equal(result.status, 1);
  assert.equal(result.stdout, "");
  assert.match(result.stderr, /终端输出超时/);
});
