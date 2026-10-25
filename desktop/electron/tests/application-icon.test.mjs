import { test } from "node:test";
import assert from "node:assert/strict";
import { loadModule } from "./load-module.mjs";

test("开发态使用 NeuxBot 图标，正式包保留应用包声明的图标", () => {
  const { configureApplicationIcon } = loadModule("main/application-icon.ts");
  const icons = [];
  const developmentApp = {
    isPackaged: false,
    getAppPath: () => "/workspace/desktop",
    dock: { setIcon: (icon) => icons.push(icon) },
  };

  configureApplicationIcon(developmentApp);
  configureApplicationIcon({ ...developmentApp, isPackaged: true });

  assert.deepEqual(icons, ["/workspace/desktop/resources/app-icon-source.png"]);
});

test("不支持 Dock 的平台跳过应用图标设置", () => {
  const { configureApplicationIcon } = loadModule("main/application-icon.ts");

  assert.doesNotThrow(() =>
    configureApplicationIcon({
      isPackaged: false,
      getAppPath: () => "/workspace/desktop",
    }),
  );
});
