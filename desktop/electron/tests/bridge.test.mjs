import { test } from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { loadModule } from "./load-module.mjs";
test("下载保存允许原文件扩展名，并原样写入文件字节", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "ally-download-test-"));
  const registered = new Map();
  let fileName;
  const { installDesktopServices } = loadModule("main/desktop-services.ts", {
    electron: {
      ipcMain: {
        handle: (name, handler) => registered.set(name, handler),
        removeHandler: (name) => registered.delete(name),
      },
      Notification: { isSupported: () => false },
      dialog: {
        showSaveDialog: async (_window, options) => {
          assert.equal(options.defaultPath, fileName);
          const extension = path.extname(fileName).slice(1);
          assert.ok(
            !options.filters ||
              options.filters.some(
                (filter) =>
                  filter.extensions.includes(extension) ||
                  filter.extensions.includes("*"),
              ),
            `保存对话框必须允许原始扩展名 ${extension}`,
          );
          return { canceled: false, filePath: path.join(directory, fileName) };
        },
      },
    },
    "node-pty": {},
  });
  const contents = new EventEmitter();
  contents.mainFrame = { url: "ally://app/index.html" };
  contents.isDestroyed = () => false;
  const dispose = installDesktopServices(
    { webContents: contents, isDestroyed: () => false },
    {},
    contents.mainFrame.url,
  );
  try {
    const saveFile = registered.get("ally:save_file");
    for (fileName of [
      "报告.png",
      "报告.html",
      "预算.xlsx",
      "档案.zip",
      "README",
    ]) {
      const bytes = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0, 255]);
      assert.equal(
        await saveFile(
          { sender: contents, senderFrame: contents.mainFrame },
          { fileName, bytes: Array.from(bytes) },
        ),
        true,
      );
      assert.deepEqual(
        await readFile(path.join(directory, fileName)),
        Buffer.from(bytes),
      );
    }
  } finally {
    dispose();
    await rm(directory, { recursive: true, force: true });
  }
});
test("预加载只暴露白名单方法，事件注销幂等且不泄露 Electron 事件", () => {
  const ipc = new EventEmitter();
  ipc.invoke = async (...args) => args;
  let bridge;
  const window = { addEventListener() {} };
  loadModule(
    "preload/index.ts",
    {
      electron: {
        ipcRenderer: ipc,
        contextBridge: {
          exposeInMainWorld(name, value) {
            assert.equal(name, "allyDesktop");
            bridge = value;
          },
        },
        webUtils: {},
      },
    },
    window,
  );
  assert.equal(bridge.invoke, undefined);
  assert.equal(bridge.send, undefined);
  assert.equal(typeof bridge.stage_attachment, "function");
  assert.equal(bridge.terminal_start, undefined);
  assert.equal(bridge.open_workspace_scope, undefined);
  const received = [];
  const stop = bridge.onBrowserNavigation((event) => received.push(event));
  ipc.emit(
    "ally:browser-navigation",
    { sender: "privileged" },
    { sessionId: "a", data: "hello" },
  );
  assert.deepEqual(received, [{ sessionId: "a", data: "hello" }]);
  stop();
  stop();
  assert.equal(ipc.listenerCount("ally:browser-navigation"), 0);
});
test("文件拖放在预加载层解析路径，注销后不再交付", () => {
  let bridge;
  const listeners = new Map();
  loadModule(
    "preload/index.ts",
    {
      electron: {
        ipcRenderer: new EventEmitter(),
        contextBridge: {
          exposeInMainWorld(_name, value) {
            bridge = value;
          },
        },
        webUtils: {
          getPathForFile(file) {
            return file.path;
          },
        },
      },
    },
    {
      addEventListener(name, callback) {
        listeners.set(name, callback);
      },
    },
  );
  const received = [];
  const stop = bridge.onFileDrop((event) => received.push(event));
  const drop = {
    dataTransfer: { types: ["Files"], files: [{ path: "/work/a.txt" }] },
    preventDefault() {},
    clientX: 10,
    clientY: 20,
  };
  listeners.get("drop")(drop);
  stop();
  listeners.get("drop")(drop);
  assert.deepEqual(received, [
    { type: "drop", paths: ["/work/a.txt"], position: { x: 10, y: 20 } },
  ]);
});

test("软件更新 IPC 只允许可信主框架的无参数调用，窗口销毁释放状态订阅", async () => {
  const registered = new Map();
  const { installDesktopServices } = loadModule("main/desktop-services.ts", {
    electron: {
      ipcMain: {
        handle: (name, handler) => registered.set(name, handler),
        removeHandler: (name) => registered.delete(name),
      },
      Notification: { isSupported: () => false },
    },
    "node-pty": {},
  });
  const contents = new EventEmitter();
  contents.mainFrame = { url: "ally://app/index.html" };
  contents.isDestroyed = () => false;
  const sent = [];
  contents.send = (...args) => sent.push(args);
  const state = { revision: 1, status: "available" };
  const called = [];
  let listener;
  let stopped = false;
  const updates = Object.fromEntries(
    ["snapshot", "check", "download", "openInstaller"].map((name) => [
      name,
      () => {
        called.push(name);
        return state;
      },
    ]),
  );
  updates.subscribe = (callback) => {
    listener = callback;
    return () => {
      stopped = true;
    };
  };
  const dispose = installDesktopServices(
    { webContents: contents, isDestroyed: () => false },
    {},
    contents.mainFrame.url,
    undefined,
    updates,
  );
  for (const command of [
    "application_update_state",
    "check_application_update",
    "download_application_update",
    "open_application_installer",
  ]) {
    const handler = registered.get(`ally:${command}`);
    assert.throws(
      () => handler({ sender: {}, senderFrame: contents.mainFrame }),
      /来源无效/,
    );
    assert.throws(
      () =>
        handler({
          sender: contents,
          senderFrame: { url: contents.mainFrame.url },
        }),
      /来源无效/,
    );
    const event = { sender: contents, senderFrame: contents.mainFrame };
    assert.throws(() =>
      handler(event, {
        url: "https://attacker.example/installer",
        path: "/tmp/a.exe",
      }),
    );
    assert.deepEqual(await handler(event, undefined), state);
  }
  assert.deepEqual(called, ["snapshot", "check", "download", "openInstaller"]);
  listener(state);
  assert.deepEqual(sent, [["ally:application-update", state]]);
  dispose();
  assert.equal(stopped, true);
  assert.equal(registered.has("ally:download_application_update"), false);
});
test("IPC 拒绝外部网页及子框架，并在执行前校验参数", async () => {
  const registered = new Map();
  const electron = {
    ipcMain: {
      handle: (name, handler) => registered.set(name, handler),
      removeHandler: (name) => registered.delete(name),
    },
    Notification: { isSupported: () => false },
  };
  const { installDesktopServices } = loadModule("main/desktop-services.ts", {
    electron,
    "node-pty": {},
  });
  const contents = new EventEmitter();
  contents.mainFrame = { url: "ally://app/index.html" };
  contents.isDestroyed = () => false;
  const window = { webContents: contents, isDestroyed: () => false };
  let calls = 0;
  const dispose = installDesktopServices(
    window,
    {
      connection() {
        calls++;
        return { baseUrl: "http://127.0.0.1:8432", token: "secret" };
      },
    },
    "ally://app/index.html",
  );
  const handler = registered.get("ally:core_connection");
  assert.throws(
    () => handler({ sender: {}, senderFrame: { url: "https://example.com" } }),
    /来源/,
  );
  assert.throws(
    () =>
      handler({
        sender: contents,
        senderFrame: { url: "ally://app/index.html" },
      }),
    /来源/,
  );
  assert.equal(
    (await handler({ sender: contents, senderFrame: contents.mainFrame }))
      .token,
    "secret",
  );
  assert.equal(calls, 1);
  const stage = registered.get("ally:stage_attachment");
  assert.throws(
    () =>
      stage(
        { sender: {}, senderFrame: contents.mainFrame },
        {
          name: "image.png",
          bytes: new Uint8Array([1]),
        },
      ),
    /来源/,
  );
  assert.throws(() =>
    stage(
      { sender: contents, senderFrame: contents.mainFrame },
      {
        name: "../image.png",
        bytes: new Uint8Array([1]),
      },
    ),
  );
  assert.throws(() =>
    stage(
      { sender: contents, senderFrame: contents.mainFrame },
      {
        name: "image.png",
        bytes: "not bytes",
      },
    ),
  );
  assert.throws(() =>
    registered.get("ally:terminal_resize")(
      { sender: contents, senderFrame: contents.mainFrame },
      { sessionId: "a", size: { rows: -1, cols: 80 } },
    ),
  );
  dispose();
  assert.equal(registered.size, 0);
  assert.equal(contents.listenerCount("render-process-gone"), 0);
  assert.equal(contents.listenerCount("did-start-navigation"), 0);
});

test("草稿对话可创建隔离网页浏览器，但不能借此打开本地文件", async () => {
  const registered = new Map();
  const views = [];
  class FakeView {
    constructor() {
      this.webContents = new EventEmitter();
      this.webContents.loadURL = async (url) => {
        this.loadedURL = url;
      };
      this.webContents.isDestroyed = () => false;
      this.webContents.close = () => undefined;
      this.webContents.setWindowOpenHandler = () => undefined;
      this.webContents.navigationHistory = {
        canGoBack: () => false,
        canGoForward: () => false,
      };
      views.push(this);
    }
    setVisible() {}
    setBounds() {}
    setBorderRadius() {}
  }
  const electron = {
    ipcMain: {
      handle: (name, handler) => registered.set(name, handler),
      removeHandler: (name) => registered.delete(name),
    },
    Notification: { isSupported: () => false },
    WebContentsView: FakeView,
    session: {
      fromPartition: () =>
        Object.assign(new EventEmitter(), {
          protocol: { handle() {}, unhandle() {} },
          setPermissionRequestHandler() {},
          setPermissionCheckHandler() {},
        }),
    },
  };
  const { installDesktopServices } = loadModule("main/desktop-services.ts", {
    electron,
    "node-pty": {},
  });
  const contents = new EventEmitter();
  contents.id = 1;
  contents.mainFrame = { url: "ally://app/index.html" };
  contents.isDestroyed = () => false;
  contents.send = () => undefined;
  const window = {
    webContents: contents,
    contentView: { addChildView() {}, removeChildView() {} },
    isDestroyed: () => false,
  };
  const dispose = installDesktopServices(
    window,
    {
      connection: () => ({ baseUrl: "http://127.0.0.1:8432", token: "secret" }),
    },
    "ally://app/index.html",
  );
  const create = (url) =>
    registered.get("ally:browser_create")(
      { sender: contents, senderFrame: contents.mainFrame },
      {
        url,
        localRoot: "/does-not-exist",
        x: 0,
        y: 0,
        width: 100,
        height: 100,
      },
    );
  try {
    assert.equal(typeof (await create("https://example.com/")), "string");
    assert.equal(views[0].loadedURL, "https://example.com/");
    await assert.rejects(create("file:///tmp/index.html"), /Chat 身份/);
    assert.equal(views.length, 1);
  } finally {
    dispose();
  }
});

test("事件IPC只接受白名单资源，Renderer重载释放自身订阅并保留主连接", async () => {
  const registered = new Map();
  const { installDesktopServices } = loadModule("main/desktop-services.ts", {
    electron: {
      ipcMain: {
        handle: (name, handler) => registered.set(name, handler),
        removeHandler: (name) => registered.delete(name),
      },
      Notification: { isSupported: () => false },
      dialog: {},
    },
    "node-pty": {},
  });
  const contents = new EventEmitter();
  contents.mainFrame = { url: "ally://app/index.html" };
  contents.isDestroyed = () => false;
  const sent = [];
  contents.send = (name, payload) => sent.push({ name, payload });
  let subscriptions = 0;
  let released = 0;
  let socketClosed = false;
  const recoveries = [];
  const dispose = installDesktopServices(
    { webContents: contents, isDestroyed: () => false },
    {},
    contents.mainFrame.url,
    {
      subscribe: (_topic, _resource, observer) => {
        subscriptions++;
        observer.onState("open");
        return () => released++;
      },
      requestResync: (...args) => recoveries.push(args),
      close: () => {
        socketClosed = true;
      },
    },
  );
  try {
    const sender = { sender: contents, senderFrame: contents.mainFrame };
    const subscribe = registered.get("ally:subscribe_core_events");
    const input = {
      subscriptionId: "00000000-0000-4000-8000-000000000001",
      topic: "chat",
      resource: { kind: "chat", id: "chat-1" },
      after: 4,
    };
    assert.throws(() =>
      subscribe(sender, { ...input, url: "https://example.com" }),
    );
    assert.throws(() =>
      subscribe(sender, { ...input, resource: { kind: "global" } }),
    );
    assert.throws(() =>
      subscribe(
        { ...sender, senderFrame: { url: "https://example.com" } },
        input,
      ),
    );
    assert.equal(await subscribe(sender, input), true);
    assert.equal(await subscribe(sender, input), true);
    assert.equal(subscriptions, 1);
    assert.deepEqual(sent[0], {
      name: "ally:core-event",
      payload: {
        subscriptionId: input.subscriptionId,
        kind: "state",
        state: "open",
        connectionChanged: undefined,
      },
    });
    await registered.get("ally:resync_core_events")(sender, {
      subscriptionId: input.subscriptionId,
      after: 42,
    });
    assert.deepEqual(recoveries, [
      ["chat", { kind: "chat", id: "chat-1" }, 42],
    ]);
    contents.emit("render-process-gone");
    assert.equal(released, 1);
    assert.equal(socketClosed, false);
    await registered.get("ally:unsubscribe_core_events")(sender, {
      subscriptionId: input.subscriptionId,
    });
    assert.equal(released, 1);
  } finally {
    dispose();
  }
});
