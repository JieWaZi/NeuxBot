import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const runtime = fileURLToPath(
  new URL("../../resources/terminal-runtime/worker.cjs", import.meta.url),
);
test(
  "真实 PTY 输出、分离后屏幕恢复、输入身份与父进程退出清理",
  { timeout: 15000 },
  async () => {
    const root = await mkdtemp(path.join(tmpdir(), "ally-pty-test-"));
    const worker = spawn(process.execPath, [runtime], {
      env: { ...process.env, SHELL: "/bin/sh" },
      stdio: ["pipe", "pipe", "pipe"],
    });
    const pending = new Map();
    const events = [];
    let errors = "";
    worker.stderr.on("data", (data) => {
      errors += data;
    });
    const lines = createInterface({ input: worker.stdout });
    let ready;
    const readiness = new Promise((resolve) => {
      ready = resolve;
    });
    lines.on("line", (line) => {
      const value = JSON.parse(line);
      if (value.event) {
        events.push(value);
        if (value.event === "ready") ready();
      } else {
        const result = pending.get(value.id);
        pending.delete(value.id);
        value.error
          ? result?.reject(new Error(value.error))
          : result?.resolve(value.result);
      }
    });
    let next = 0;
    const request = (method, args) =>
      new Promise((resolve, reject) => {
        const id = String(++next);
        pending.set(id, { resolve, reject });
        worker.stdin.write(JSON.stringify({ id, method, args }) + "\n");
      });
    const waitFor = async (condition) => {
      const deadline = Date.now() + 5000;
      while (!condition()) {
        if (Date.now() > deadline) throw new Error("等待 PTY 超时: " + errors);
        await new Promise((resolve) => setTimeout(resolve, 20));
      }
    };
    try {
      await readiness;
      const started = await request("start", {
        root,
        ownerKey: "test",
        createWhenMissing: true,
      });
      const snapshot = await request("attach", {
        sessionId: started.sessionId,
      });
      assert.equal(snapshot.state, "running");
      await request("write", {
        sessionId: started.sessionId,
        attachmentId: snapshot.attachmentId,
        data: "printf '\\101\\114\\114\\131_PTY_OK\\n'\r",
      });
      await waitFor(() =>
        events.some(
          (event) =>
            event.event === "terminal-output" &&
            event.payload.data.includes("ALLY_PTY_OK"),
        ),
      );
      const output = events.filter(
        (event) => event.event === "terminal-output",
      );
      assert.ok(
        output.every(
          (event, i) =>
            i === 0 || event.payload.sequence > output[i - 1].payload.sequence,
        ),
      );
      await request("detach", {
        sessionId: started.sessionId,
        attachmentId: snapshot.attachmentId,
      });
      const restored = await request("attach", {
        sessionId: started.sessionId,
      });
      assert.match(restored.screen, /ALLY_PTY_OK/);
      assert.notEqual(restored.attachmentId, snapshot.attachmentId);
      await assert.rejects(
        request("write", {
          sessionId: started.sessionId,
          attachmentId: snapshot.attachmentId,
          data: "bad\r",
        }),
      );
      const exit = new Promise((resolve) => worker.once("exit", resolve));
      worker.stdin.end();
      await exit;
      assert.equal(worker.exitCode, 0);
    } finally {
      worker.kill();
      lines.close();
      await rm(root, { recursive: true, force: true });
    }
  },
);
