import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { loadModule } from "./load-module.mjs";

const chatId = "a".repeat(32);
const executionId = "b".repeat(32);
const requestId = "c".repeat(32);
const messageId = "d".repeat(32);

function setup(
  resolveTarget = async () => ({ valid: true }),
  deliver = (takeTarget, opened) => opened.push(takeTarget()),
) {
  const { installWorkspaceAttention } = loadModule(
    "main/workspace-attention.ts",
  );
  let observer;
  const shown = [];
  const opened = [];
  let focused = false;
  let preferences = { enabled: true, mode: "all", preview: false };
  let stopped = false;
  const stop = installWorkspaceAttention({
    events: {
      subscribe(topic, resource, value) {
        assert.equal(topic, "work-board");
        assert.deepEqual(resource, { kind: "global" });
        observer = value;
        return () => {
          stopped = true;
        };
      },
    },
    isFocused: () => focused,
    preferences: () => preferences,
    resolveTarget,
    showNotification(content) {
      const notification = new EventEmitter();
      notification.show = () => shown.push({ notification, content });
      notification.close = () => {
        notification.closed = true;
        notification.emit("close");
      };
      return notification;
    },
    open: (takeTarget) => deliver(takeTarget, opened),
  });
  return {
    shown,
    opened,
    stop,
    ready() {
      observer.onEvent({ type: "work-board.ready" });
    },
    change() {
      observer.onEvent({ type: "work-board.changed" });
    },
    recheck(kind, id, reason = "requests-changed") {
      observer.onEvent({
        type: "work-board.changed",
        resource: { kind, id },
        payload: { reason },
      });
    },
    reconnect() {
      observer.onState("reconnecting");
    },
    focus(value) {
      focused = value;
    },
    preference(value) {
      preferences = { enabled: true, ...value };
    },
    send(operation, fields = {}) {
      observer.onEvent({
        type: "work-board.live",
        payload: {
          operation,
          work: { kind: "chat", id: chatId },
          chatId,
          executionId,
          ...(operation.startsWith("request")
            ? { requestId, requestKind: "approval" }
            : { messageId }),
          ...fields,
        },
      });
    },
    stopped: () => stopped,
  };
}

const flush = () => new Promise((resolve) => setImmediate(resolve));

test("only live frames after ready can produce a reminder, once per message", async () => {
  const host = setup();
  host.send("completed");
  host.ready();
  host.change();
  host.send("completed");
  host.send("completed");
  await flush();
  assert.equal(host.shown.length, 1);
  host.reconnect();
  host.send("completed", { messageId: "e".repeat(32) });
  host.ready();
  host.change();
  await flush();
  assert.equal(host.shown.length, 1);
  host.stop();
  assert.equal(host.stopped(), true);
});

test("requests resolved during preview lookup stay dismissed", async () => {
  let finish;
  const host = setup(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  host.preference({ mode: "all", preview: true });
  host.ready();
  host.send("request_opened");
  host.send("request_closed", { executionId: "e".repeat(32) });
  host.send("request_closed");
  finish({ valid: true, title: "Request", preview: "Preview" });
  await flush();
  assert.equal(host.shown.length, 0);
  host.stop();
});

test("focus and preferences gate delivery, and reconnect withdraws outstanding requests", async () => {
  const host = setup();
  host.ready();
  host.focus(true);
  host.send("completed");
  host.focus(false);
  host.preference({ mode: "needs-me", preview: false });
  host.send("failed");
  host.send("request_opened");
  await flush();
  assert.equal(host.shown.length, 1);
  host.reconnect();
  assert.equal(host.shown[0].notification.closed, true);
  host.ready();
  host.preference({ mode: "off", preview: false });
  host.send("request_opened", { requestId: "e".repeat(32) });
  await flush();
  assert.equal(host.shown.length, 1);
  host.stop();
});

test("notification click validates the original identity and falls back for a stale request", async () => {
  const checks = [];
  const host = setup(async (target, includePreview) => {
    checks.push({ target, includePreview });
    return { valid: checks.length === 1 };
  });
  host.ready();
  host.send("request_opened");
  await flush();
  host.shown[0].notification.emit("click");
  await flush();
  assert.equal(checks.length, 2);
  assert.equal(checks[1].target.requestId, requestId);
  assert.equal(checks[1].includePreview, false);
  assert.deepEqual(host.opened, [null]);
  host.stop();
});

test("a late preview cannot expose text after previews are disabled", async () => {
  let finish;
  const host = setup(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  host.preference({ mode: "all", preview: true });
  host.ready();
  host.send("completed");
  host.preference({ mode: "all", preview: false });
  finish({
    valid: true,
    title: "Private chat title",
    preview: "Private reply",
  });
  await flush();
  assert.deepEqual(
    host.shown.map(({ content }) => content),
    [{ title: "NeuxBot", body: "有新的工作结果" }],
  );
  host.stop();
});

test("click passes the verified conversation identity without preview text", async () => {
  const wakerId = "f".repeat(32);
  const host = setup(async () => ({
    valid: true,
    wakerId,
    title: "Private chat title",
    preview: "Private reply",
  }));
  host.ready();
  host.send("completed");
  await flush();
  host.shown[0].notification.emit("click");
  await flush();
  assert.equal(host.opened[0].work.id, chatId);
  assert.equal(host.opened[0].messageId, messageId);
  assert.equal(host.opened[0].wakerId, wakerId);
  assert.equal("preview" in host.opened[0], false);
  host.stop();
});

test("a request closed during click verification routes to the work board", async () => {
  let finish;
  let verified = false;
  const host = setup(() => {
    if (!verified) {
      verified = true;
      return Promise.resolve({ valid: true });
    }
    return new Promise((resolve) => {
      finish = resolve;
    });
  });
  host.ready();
  host.send("request_opened", { requestFence: 2 });
  await flush();
  host.shown[0].notification.emit("click");
  host.send("request_closed", { requestFence: 2 });
  finish({ valid: true });
  await flush();
  assert.deepEqual(host.opened, [null]);
  host.stop();
});

test("turning off desktop notifications suppresses events and retains the scope", async () => {
  const host = setup();
  host.ready();
  host.preference({ enabled: false, mode: "needs-me", preview: false });
  host.send("request_opened");
  await flush();
  assert.equal(host.shown.length, 0);
  host.preference({ enabled: true, mode: "needs-me", preview: false });
  host.send("completed");
  host.send("request_opened", { requestId: "e".repeat(32) });
  await flush();
  assert.equal(host.shown.length, 1);
  host.stop();
});

test("notification preferences preserve the legacy scope and persist across restarts", async (t) => {
  const { loadNotificationPreferences } = loadModule(
    "main/notification-preferences.ts",
  );
  const directory = await mkdtemp(path.join(tmpdir(), "ally-attention-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const file = path.join(directory, "preferences.json");
  await writeFile(file, JSON.stringify({ mode: "off", preview: true }));
  const preferences = await loadNotificationPreferences(file);
  assert.deepEqual(preferences.get(), {
    enabled: true,
    mode: "off",
    preview: true,
  });
  const last = { enabled: false, mode: "needs-me", preview: true };
  await Promise.all([
    preferences.set({ enabled: true, mode: "off", preview: false }),
    preferences.set(last),
  ]);
  assert.deepEqual(preferences.get(), last);
  assert.deepEqual(JSON.parse(await readFile(file, "utf8")), last);
  assert.deepEqual((await loadNotificationPreferences(file)).get(), last);
});

test("projection recovery withdraws only the affected stale request", async () => {
  const member = "e".repeat(32);
  const checked = [];
  let valid = true;
  const host = setup(async (event, preview) => {
    checked.push([event.chatId, preview]);
    return { valid };
  });
  host.ready();
  host.send("request_opened");
  host.send("request_opened", { chatId: member });
  await flush();
  checked.length = 0;
  valid = false;
  host.recheck("chat", member, "reading-recovered");
  await flush();
  assert.deepEqual(checked, [[member, false]]);
  assert.equal(host.shown[0].notification.closed, undefined);
  assert.equal(host.shown[1].notification.closed, true);
  assert.equal(host.shown.length, 2);
  host.stop();
});

test("a raw chat request uses the verified work and is withdrawn when that work is deleted", async () => {
  const threadId = "e".repeat(32);
  const channelId = "f".repeat(32);
  const work = { kind: "team_thread", id: threadId };
  let valid = false;
  const host = setup(async () => ({ valid, work, threadId, channelId }));
  host.ready();
  host.send("request_opened");
  await flush();
  assert.equal(host.shown.length, 0, "an unlinked chat request cannot pop up");

  valid = true;
  host.send("request_opened");
  await flush();
  host.shown[0].notification.emit("click");
  await flush();
  assert.deepEqual(host.opened[0].work, work);

  host.send("request_opened", { requestId: "1".repeat(32) });
  await flush();
  valid = false;
  host.recheck("team_thread", threadId);
  await flush();
  assert.equal(host.shown[1].notification.closed, true);
  host.stop();
});

test("a request closed while the window loads uses the board fallback", async () => {
  let send;
  const host = setup(undefined, (takeTarget, opened) => {
    send = () => opened.push(takeTarget());
  });
  host.ready();
  host.send("request_opened");
  await flush();
  host.shown[0].notification.emit("click");
  await flush();
  host.send("request_closed");
  send();
  assert.deepEqual(host.opened, [null]);
  host.stop();
});

test("a binding event arriving during the first lookup is rechecked once", async () => {
  let finish;
  let lookups = 0;
  const host = setup(() => {
    lookups++;
    if (lookups > 1) return Promise.resolve({ valid: true });
    return new Promise((resolve) => {
      finish = resolve;
    });
  });
  host.ready();
  host.send("request_opened");
  host.send("request_opened");
  finish({ valid: false });
  await flush();
  assert.equal(lookups, 2);
  assert.equal(host.shown.length, 1);
  host.stop();
});

test("a result from a chat waits for its visible owner without previews", async () => {
  let finish;
  let lookups = 0;
  const host = setup(() => {
    lookups++;
    if (lookups > 1) return Promise.resolve({ valid: true });
    return new Promise((resolve) => {
      finish = resolve;
    });
  });
  host.ready();
  host.send("completed");
  assert.equal(host.shown.length, 0, "an unverified result cannot pop up");
  host.send("completed");
  finish({ valid: false });
  await flush();
  assert.equal(lookups, 2);
  assert.equal(host.shown.length, 1);
  host.stop();
});
