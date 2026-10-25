import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { loadModule } from "./load-manager.mjs";

test("终端按标签身份复用，屏幕快照恢复输出并限制输入控制权", async (t) => {
  const root = await realpath(
    await mkdtemp(path.join(tmpdir(), "ally-terminal-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  const children = [];
  const { TerminalManager } = loadModule("src/manager.ts", {
    "node-pty": {
      spawn() {
        const child = {
          killed: false,
          writes: [],
          sizes: [],
          kill() {
            this.killed = true;
          },
          onData(handler) {
            this.emitData = handler;
            return { dispose() {} };
          },
          onExit(handler) {
            this.emitExit = handler;
            return { dispose() {} };
          },
          write(data) {
            this.writes.push(data);
          },
          resize(cols, rows) {
            this.sizes.push({ cols, rows });
          },
        };
        children.push(child);
        return child;
      },
    },
  });
  const events = [];
  const manager = new TerminalManager((name, payload) =>
    events.push({ name, payload }),
  );
  t.after(() => manager.dispose());
  const [first, same] = await Promise.all([
    manager.start(root, "tab-a", true),
    manager.start(root, "tab-a", true),
  ]);
  assert.equal(same.sessionId, first.sessionId);
  assert.equal(children.length, 1);
  children[0].emitData("hello\r\n");
  const attached = await manager.attach(first.sessionId);
  assert.match(attached.screen, /hello/);
  assert.equal(attached.sequence, 1);
  manager.write(first.sessionId, attached.attachmentId, "ls\r");
  assert.deepEqual(children[0].writes, ["ls\r"]);
  manager.detach(first.sessionId, attached.attachmentId);
  children[0].emitData("离线输出\r\n");
  const resumed = await manager.attach(first.sessionId);
  assert.match(resumed.screen, /离线输出/);
  assert.equal(resumed.sequence, 2);
  assert.throws(
    () => manager.write(first.sessionId, attached.attachmentId, "stale"),
    /控制权/,
  );
  manager.resize(first.sessionId, resumed.attachmentId, {
    cols: 80,
    rows: 30,
  });
  assert.deepEqual(children[0].sizes, [{ cols: 80, rows: 30 }]);
  children[0].emitData("live\r\n");
  await manager.attach(first.sessionId);
  assert.ok(
    events.some(
      (event) =>
        event.name === "terminal-output" &&
        event.payload.sequence === 3 &&
        event.payload.data === "live\r\n",
    ),
  );
  children[0].emitExit({ exitCode: 0 });
  const exited = await manager.attach(first.sessionId);
  assert.equal(exited.state, "exited");
  assert.equal(exited.exitCode, 0);
  assert.equal(await manager.close("tab-a"), true);
  assert.equal(children[0].killed, false);
  await assert.rejects(manager.start(root, "tab-a", true), /已关闭/);
  await assert.rejects(manager.start(root, "missing", false), /手动新建/);
});

test("标签关闭与启动并发时不遗留 PTY", async (t) => {
  const root = await realpath(
    await mkdtemp(path.join(tmpdir(), "ally-terminal-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  let allocated = 0;
  const { TerminalManager } = loadModule("src/manager.ts", {
    "node-pty": {
      spawn() {
        allocated++;
        throw new Error("不应创建已关闭标签的进程");
      },
    },
  });
  const manager = new TerminalManager(() => {});
  const opening = manager.start(root, "tab-race", true);
  await manager.close("tab-race");
  await assert.rejects(opening, /已关闭/);
  assert.equal(allocated, 0);
});

test("屏幕写入积压时暂停 PTY，排空后恢复且退出后不再恢复", async (t) => {
  const root = await realpath(
    await mkdtemp(path.join(tmpdir(), "ally-terminal-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  const children = [];
  const screens = [];
  const { TerminalManager } = loadModule("src/manager.ts", {
    "node-pty": {
      spawn() {
        const child = {
          paused: 0,
          resumed: 0,
          pause() {
            this.paused++;
          },
          resume() {
            this.resumed++;
          },
          kill() {},
          onData(handler) {
            this.emitData = handler;
            return { dispose() {} };
          },
          onExit(handler) {
            this.emitExit = handler;
            return { dispose() {} };
          },
        };
        children.push(child);
        return child;
      },
    },
    "@xterm/headless": {
      Terminal: class {
        constructor() {
          this.writes = [];
          screens.push(this);
        }
        loadAddon() {}
        write(_data, done) {
          this.writes.push(done);
        }
        dispose() {}
      },
    },
    "@xterm/addon-serialize": {
      SerializeAddon: class {
        serialize() {
          return "";
        }
      },
    },
  });
  const manager = new TerminalManager(() => {});
  t.after(() => manager.dispose());
  const first = await manager.start(root, "slow", true);
  children[0].emitData("x".repeat(256 * 1024));
  assert.equal(children[0].paused, 1);
  await Promise.resolve();
  screens[0].writes.shift()();
  await manager.attach(first.sessionId);
  assert.equal(children[0].resumed, 1);

  const second = await manager.start(root, "exiting", true);
  children[1].emitData("x".repeat(256 * 1024));
  children[1].emitExit({ exitCode: 0 });
  await Promise.resolve();
  screens[1].writes.shift()();
  await manager.attach(second.sessionId);
  assert.equal(children[1].paused, 1);
  assert.equal(children[1].resumed, 0);
});
