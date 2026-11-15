import { test } from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { loadModule } from "./load-module.mjs";

test("browser guests share a project partition until their final close", async (t) => {
  const parent = await mkdtemp(path.join(tmpdir(), "bot-browser-"));
  t.after(() => rm(parent, { recursive: true, force: true }));
  const firstRoot = path.join(parent, "first");
  const secondRoot = path.join(parent, "second");
  await Promise.all([mkdir(firstRoot), mkdir(secondRoot)]);
  const partitions = new Map();
  const views = [];
  class FakeView {
    constructor(options) {
      this.session = options.webPreferences.session;
      this.webContents = new EventEmitter();
      this.webContents.loaded = [];
      this.webContents.loadURL = async (url) => {
        this.webContents.loaded.push(url);
        this.webContents.currentURL = url;
      };
      this.webContents.getURL = () =>
        this.webContents.currentURL ?? "about:blank";
      this.webContents.getTitle = () => this.webContents.currentTitle ?? "";
      this.webContents.isDestroyed = () => false;
      this.webContents.close = () => undefined;
      this.webContents.setWindowOpenHandler = (handler) => {
        this.popup = handler;
      };
      this.webContents.navigationHistory = {
        canGoBack: () => false,
        canGoForward: () => false,
      };
      views.push(this);
    }
    setVisible() {}
    setBounds() {}
  }
  const fromPartition = (name) => {
    let value = partitions.get(name);
    if (!value) {
      value = Object.assign(new EventEmitter(), {
        handled: 0,
        unhandled: 0,
        protocol: {
          handle: (_scheme, handler) => {
            value.handled += 1;
            value.fileHandler = handler;
          },
          unhandle: () => {
            value.unhandled += 1;
          },
        },
        setPermissionRequestHandler: () => undefined,
        setPermissionCheckHandler: () => undefined,
      });
      partitions.set(name, value);
    }
    return value;
  };
  const { BrowserManager } = loadModule("main/browser-manager.ts", {
    electron: {
      WebContentsView: FakeView,
      session: { fromPartition },
      net: { fetch: async () => new Response("ok") },
    },
  });
  const window = {
    webContents: { id: 7 },
    contentView: { addChildView() {}, removeChildView() {} },
    isDestroyed: () => false,
  };
  const events = [];
  const manager = new BrowserManager(window, (event) => events.push(event));
  const create = (localRoot) =>
    manager.create({
      url: "about:blank",
      localRoot,
      x: 0,
      y: 0,
      width: 100,
      height: 100,
    });
  const first = await create(firstRoot);
  const second = await create(firstRoot);
  const third = await create(secondRoot);
  const localPage = path.join(firstRoot, "index.html");
  await writeFile(localPage, "<title>Local</title>");
  const local = await manager.create({
    url: pathToFileURL(localPage).href,
    localRoot: firstRoot,
    x: 0,
    y: 0,
    width: 100,
    height: 100,
  });
  assert.equal(views[0].session, views[1].session);
  assert.notEqual(views[0].session, views[2].session);
  assert.notEqual(views[0].session, views[3].session);
  assert.equal(views[0].session.handled, 1);
  views[0].webContents.currentTitle = "Example";
  views[0].webContents.emit("page-title-updated");
  assert.deepEqual(events.at(-1), {
    viewId: first,
    url: "about:blank",
    title: "Example",
    canGoBack: false,
    canGoForward: false,
  });
  manager.visible(first, true);
  assert.equal(events.at(-1).title, "Example");
  assert.equal(
    (await views[0].session.fileHandler({ url: pathToFileURL(localPage).href }))
      .status,
    403,
  );
  assert.equal(
    (await views[3].session.fileHandler({ url: pathToFileURL(localPage).href }))
      .status,
    200,
  );
  manager.close(first);
  assert.equal(views[0].session.unhandled, 0);
  assert.deepEqual(views[1].popup({ url: "https://example.com/" }), {
    action: "deny",
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.ok(views[1].webContents.loaded.includes("https://example.com/"));
  assert.deepEqual(views[1].popup({ url: "app://private/" }), {
    action: "deny",
  });
  assert.deepEqual(views[3].popup({ url: "https://example.com/" }), {
    action: "deny",
  });
  await assert.rejects(
    manager.navigate(second, pathToFileURL(localPage).href),
    /隔离边界/,
  );
  await assert.rejects(
    manager.navigate(local, "https://example.com/"),
    /隔离边界/,
  );
  manager.close(second);
  assert.equal(views[0].session.unhandled, 1);
  manager.close(third);
  assert.equal(views[2].session.unhandled, 1);
  manager.close(local);
  assert.equal(views[3].session.unhandled, 1);
});
