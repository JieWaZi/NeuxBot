import { test } from "node:test";
import assert from "node:assert/strict";
import { loadModule } from "./load-module.mjs";
import { EventEmitter } from "node:events";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

test("透明窗口使用系统材质并切换主题、语言和原生缩放", () => {
  const descriptor = Object.getOwnPropertyDescriptor(process, "platform");
  const calls = [];
  const nativeTheme = {};
  const menus = [];
  const { applyWindowAppearance, windowLanguage } = loadModule(
    "main/window-appearance.ts",
    {
      electron: {
        nativeTheme,
        Menu: {
          buildFromTemplate: (items) => items,
          setApplicationMenu: (items) => menus.push(items),
        },
      },
      "node:os": { release: () => "10.0.22631" },
    },
  );
  const window = {
    setVibrancy: (value) => calls.push(["vibrancy", value]),
    setBackgroundMaterial: (value) => calls.push(["material", value]),
    setBackgroundColor: (value) => calls.push(["background", value]),
    webContents: {
      setZoomFactor: (value) => calls.push(["zoom", value]),
      getZoomFactor: () => 1.25,
      send: (...args) => calls.push(["event", ...args]),
    },
  };
  try {
    Object.defineProperty(process, "platform", { value: "darwin" });
    assert.deepEqual(
      applyWindowAppearance(window, {
        theme: "dark",
        language: "en-US",
        transparent: true,
        scale: 1.25,
      }),
      { transparencySupported: true },
    );
    assert.deepEqual(calls.slice(0, 3), [
      ["vibrancy", "under-window"],
      ["background", "#00000000"],
      ["zoom", 1.25],
    ]);
    assert.equal(nativeTheme.themeSource, "dark");
    assert.equal(windowLanguage(window), "en-US");
    assert.equal(menus[0][1].label, "Edit");
    menus[0]
      .find((item) => item.label === "View")
      .submenu.find((item) => item.label === "Zoom In")
      .click();
    assert.deepEqual(calls.at(-1), ["event", "bot:interface-scale", 1.5]);
    applyWindowAppearance(window, {
      theme: "light",
      language: "zh-CN",
      transparent: false,
      scale: 1,
    });
    assert.equal(menus[1][1].label, "编辑");
    assert.equal(nativeTheme.themeSource, "system");
    assert.ok(
      calls.some(([kind, value]) => kind === "vibrancy" && value === null),
    );
    Object.defineProperty(process, "platform", { value: "win32" });
    applyWindowAppearance(window, {
      theme: "light",
      language: "en-US",
      transparent: true,
      scale: 0.9,
    });
    assert.ok(
      calls.some(([kind, value]) => kind === "material" && value === "acrylic"),
    );
    Object.defineProperty(process, "platform", { value: "linux" });
    assert.equal(
      applyWindowAppearance(window, {
        theme: "light",
        language: "en-US",
        transparent: true,
        scale: 1,
      }).transparencySupported,
      false,
    );
    assert.deepEqual(calls.at(-2), ["background", "#f5f5f6"]);
  } finally {
    Object.defineProperty(process, "platform", descriptor);
  }
});

test("持久透明偏好在创建窗口前恢复，构造参数直接包含材质", async () => {
  const descriptor = Object.getOwnPropertyDescriptor(process, "platform");
  const directory = await mkdtemp(
    path.join(tmpdir(), "neux-window-appearance-"),
  );
  const nativeTheme = new EventEmitter();
  nativeTheme.shouldUseDarkColors = false;
  const appearance = loadModule("main/window-appearance.ts", {
    electron: { nativeTheme },
  });
  const file = path.join(directory, "appearance.json");
  try {
    Object.defineProperty(process, "platform", { value: "darwin" });
    const preferences = await appearance.loadWindowAppearance(file);
    assert.equal(preferences.get().transparent, false);
    await Promise.all([
      preferences.set(true),
      preferences.set(false),
      preferences.set(true),
    ]);
    const restored = await appearance.loadWindowAppearance(file);
    assert.equal(restored.get().transparent, true);
    assert.deepEqual(
      appearance.windowAppearanceOptions(restored.get().transparent),
      { vibrancy: "under-window", visualEffectState: "active" },
    );
    assert.deepEqual(JSON.parse(await readFile(file, "utf8")), {
      transparent: true,
    });
    await restored.set(false);
    assert.deepEqual(appearance.windowAppearanceOptions(false), {
      visualEffectState: "active",
      backgroundColor: "#f5f5f6",
    });
    const broken = await appearance.loadWindowAppearance(
      path.join(directory, "missing", "appearance.json"),
    );
    await assert.rejects(broken.set(true));
    assert.equal(broken.get().transparent, false);
  } finally {
    Object.defineProperty(process, "platform", descriptor);
    await rm(directory, { recursive: true, force: true });
  }
});

test("已在构造时开启的材质不因语言或缩放重建，主题监听随窗口释放", () => {
  const descriptor = Object.getOwnPropertyDescriptor(process, "platform");
  const nativeTheme = new EventEmitter();
  nativeTheme.shouldUseDarkColors = false;
  const { initializeWindowAppearance, applyWindowAppearance } = loadModule(
    "main/window-appearance.ts",
    {
      electron: {
        nativeTheme,
        Menu: { buildFromTemplate: (items) => items, setApplicationMenu() {} },
      },
    },
  );
  const window = new EventEmitter();
  const materials = [];
  const backgrounds = [];
  window.isDestroyed = () => false;
  window.setVibrancy = (value) => materials.push(value);
  window.setBackgroundColor = (value) => backgrounds.push(value);
  window.webContents = { setZoomFactor() {} };
  try {
    Object.defineProperty(process, "platform", { value: "darwin" });
    initializeWindowAppearance(window, true);
    applyWindowAppearance(window, {
      theme: "system",
      language: "zh-CN",
      transparent: true,
      scale: 1,
    });
    applyWindowAppearance(window, {
      theme: "dark",
      language: "en-US",
      transparent: true,
      scale: 1.25,
    });
    assert.deepEqual(materials, []);
    assert.equal(nativeTheme.themeSource, "dark");
    applyWindowAppearance(window, {
      theme: "system",
      language: "en-US",
      transparent: false,
      scale: 1.25,
    });
    assert.deepEqual(materials, [null]);
    assert.equal(nativeTheme.themeSource, "system");
    nativeTheme.shouldUseDarkColors = true;
    nativeTheme.emit("updated");
    assert.equal(backgrounds.at(-1), "#1a1a1a");
    window.emit("closed");
    assert.equal(nativeTheme.listenerCount("updated"), 0);
  } finally {
    Object.defineProperty(process, "platform", descriptor);
  }
});

test("外观 IPC 只接受受限外观值", () => {
  const { commandSchemas } = loadModule("shared/validation.ts");
  const valid = {
    theme: "dark",
    language: "en-US",
    transparent: true,
    scale: 1.25,
  };
  assert.equal(
    commandSchemas.set_window_appearance.safeParse(valid).success,
    true,
  );
  for (const input of [
    { ...valid, scale: Infinity },
    { ...valid, scale: 2 },
    { ...valid, scale: 0.2 },
    { ...valid, language: "de-DE" },
    { ...valid, path: "/tmp" },
  ])
    assert.equal(
      commandSchemas.set_window_appearance.safeParse(input).success,
      false,
    );
});

test("原生网页视图坐标匹配主界面缩放", () => {
  const { browserBounds } = loadModule("main/browser-manager.ts", {
    electron: {},
  });
  assert.deepEqual(
    browserBounds({ x: 10, y: 20, width: 100, height: 80 }, 1.25),
    { x: 13, y: 25, width: 125, height: 100 },
  );
});
