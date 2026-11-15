import { spawn } from "node:child_process";
import { mkdtemp, mkdir, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);

test(
  "Electron 正常退出时不会把 Vite 的受控关闭报告为 143 错误",
  { skip: process.platform !== "darwin", timeout: 15_000 },
  async () => {
    const fixture = await mkdtemp(path.join(tmpdir(), "bot-electron-fixture-"));
    const executable = path.join(
      fixture,
      "Electron.app/Contents/MacOS/Electron",
    );
    await mkdir(path.dirname(executable), { recursive: true });
    await symlink("/usr/bin/true", executable);
    try {
      const child = spawn(process.execPath, ["scripts/dev-electron.mjs"], {
        cwd: root,
        env: { ...process.env, ELECTRON_OVERRIDE_DIST_PATH: fixture },
        stdio: ["ignore", "pipe", "pipe"],
      });
      let output = "";
      child.stdout.on("data", (chunk) => (output += chunk));
      child.stderr.on("data", (chunk) => (output += chunk));
      const { code, signal } = await new Promise((resolve) =>
        child.once("exit", (code, signal) => resolve({ code, signal })),
      );

      assert.equal(signal, null, output);
      assert.equal(code, 0, output);
      assert.doesNotMatch(output, /ELIFECYCLE|exit code 143/, output);
    } finally {
      await rm(fixture, { recursive: true, force: true });
    }
  },
);
