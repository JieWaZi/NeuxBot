import { EventEmitter } from "node:events";
import { test } from "node:test";
import assert from "node:assert/strict";
import { loadModule } from "./load-module.mjs";

class FakeApplication extends EventEmitter {
  isPackaged = false;
  quitCalls = 0;
  showCalls = 0;
  dockMenus = [];
  dock = { setMenu: (menu) => this.dockMenus.push(menu) };
  getAppPath() {
    return "/workspace/desktop";
  }
  quit() {
    this.quitCalls++;
  }
  show() {
    this.showCalls++;
  }
}

class FakeWindow extends EventEmitter {
  destroyed = false;
  minimized = false;
  hideCalls = 0;
  showCalls = 0;
  focusCalls = 0;
  restoreCalls = 0;
  isDestroyed() {
    return this.destroyed;
  }
  isMinimized() {
    return this.minimized;
  }
  hide() {
    this.hideCalls++;
  }
  show() {
    this.showCalls++;
  }
  focus() {
    this.focusCalls++;
  }
  restore() {
    this.minimized = false;
    this.restoreCalls++;
  }
}

function fixture(platform = "darwin") {
  const application = new FakeApplication();
  const windows = [];
  const trays = [];
  const images = [];
  let stopCalls = 0;
  let finishStop;
  const stop = new Promise((resolve) => (finishStop = resolve));
  class FakeTray extends EventEmitter {
    constructor(icon) {
      super();
      this.icon = icon;
      trays.push(this);
    }
    setToolTip(value) {
      this.toolTip = value;
    }
    setContextMenu(value) {
      this.menu = value;
    }
    destroy() {
      this.destroyed = true;
    }
  }
  const electron = {
    Menu: { buildFromTemplate: (template) => ({ template }) },
    Tray: FakeTray,
    nativeImage: {
      createFromPath(iconPath) {
        const image = {
          iconPath,
          template: false,
          isEmpty: () => false,
          setTemplateImage(value) {
            this.template = value;
          },
        };
        images.push(image);
        return image;
      },
    },
  };
  const { createApplicationLifecycle, resolveTrayIconPath } = loadModule(
    "main/application-lifecycle.ts",
    { electron },
  );
  const lifecycle = createApplicationLifecycle({
    app: application,
    platform,
    resourcesPath: "/bundle/Resources",
    createWindow: async () => {
      const window = new FakeWindow();
      windows.push(window);
      return window;
    },
    stopCore: async () => {
      stopCalls++;
      await stop;
    },
  });
  return {
    application,
    finishStop,
    images,
    lifecycle,
    resolveTrayIconPath,
    stopCalls: () => stopCalls,
    trays,
    windows,
  };
}

const flush = () => new Promise((resolve) => setImmediate(resolve));

test("点击关闭只隐藏窗口，托盘点击和第二实例会恢复同一个窗口", async () => {
  const context = fixture();
  await context.lifecycle.start();
  assert.equal(context.windows.length, 1);
  const window = context.windows[0];

  let prevented = false;
  window.emit("close", { preventDefault: () => (prevented = true) });
  assert.equal(prevented, true);
  assert.equal(window.hideCalls, 1);
  assert.equal(context.stopCalls(), 0);

  context.trays[0].emit("click");
  await flush();
  context.application.emit("second-instance");
  await flush();
  assert.equal(context.windows.length, 1);
  assert.equal(window.showCalls, 3);
  assert.equal(window.focusCalls, 3);
});

test("只有显式退出才停止 Core，并在收尾完成后真正退出", async () => {
  const context = fixture();
  await context.lifecycle.start();
  const event = {
    prevented: false,
    preventDefault() {
      this.prevented = true;
    },
  };

  context.application.emit("before-quit", event);
  context.application.emit("before-quit", event);
  assert.equal(event.prevented, true);
  assert.equal(context.stopCalls(), 1);
  assert.equal(context.application.quitCalls, 0);

  context.finishStop();
  await flush();
  assert.equal(context.application.quitCalls, 1);
  assert.equal(context.trays[0].destroyed, true);

  let prevented = false;
  context.windows[0].emit("close", {
    preventDefault: () => (prevented = true),
  });
  assert.equal(prevented, false);
});

test("托盘菜单提供打开和退出，macOS 使用 Template 图标", async () => {
  const context = fixture();
  await context.lifecycle.start();
  assert.equal(context.images[0].template, true);
  assert.equal(
    context.images[0].iconPath,
    "/workspace/desktop/resources/tray/allyTemplate.png",
  );
  assert.deepEqual(
    context.trays[0].menu.template.map((item) => item.label ?? item.type),
    ["打开 NeuxBot", "separator", "退出 NeuxBot"],
  );

  context.trays[0].menu.template[2].click();
  assert.equal(context.application.quitCalls, 1);
});

test("Windows 使用 ICO，并在系统会话结束时直接收尾而不隐藏窗口", async () => {
  const context = fixture("win32");
  await context.lifecycle.start();
  assert.equal(
    context.images[0].iconPath,
    "/workspace/desktop/resources/tray/ally.ico",
  );
  assert.equal(context.images[0].template, false);

  context.windows[0].emit("query-session-end", {});
  assert.equal(context.stopCalls(), 1);
  let prevented = false;
  context.windows[0].emit("close", {
    preventDefault: () => (prevented = true),
  });
  assert.equal(prevented, false);
  const quitEvent = { preventDefault() {} };
  context.application.emit("before-quit", quitEvent);
  context.finishStop();
  await flush();
  assert.equal(context.application.quitCalls, 1);
});

test("正式包从 Resources/tray 读取平台托盘图标", () => {
  const context = fixture();
  const application = {
    isPackaged: true,
    getAppPath: () => "/bundle/app.asar",
  };
  assert.equal(
    context.resolveTrayIconPath(application, "/bundle/Resources", "darwin"),
    "/bundle/Resources/tray/allyTemplate.png",
  );
  assert.equal(
    context.resolveTrayIconPath(application, "/bundle/Resources", "win32"),
    "/bundle/Resources/tray/ally.ico",
  );
});
