import assert from "node:assert/strict";
import test from "node:test";
import path from "node:path";
import {
  webRuntimeEnvironment,
  defaultCoreDataDirectory,
} from "./start-web.mjs";

test("独立启动不继承桌面所有权或浏览器令牌", () => {
  const environment = webRuntimeEnvironment("/release", {
    NEUXBOT_DESKTOP_PARENT_PID: "42",
    NEUXBOT_LOCAL_TOKEN: "secret",
    NEUXBOT_WEB_DEV_ORIGIN: "http://evil",
    NEUXBOT_DATA_DIR: "/data",
    NEUXBOT_WEB_CORE_PORT: "9000",
  });
  assert.equal(environment.NEUXBOT_DESKTOP_PARENT_PID, undefined);
  assert.equal(environment.NEUXBOT_LOCAL_TOKEN, undefined);
  assert.equal(environment.NEUXBOT_WEB_DEV_ORIGIN, undefined);
  assert.equal(environment.SERVER_PORT, "9000");
  assert.equal(environment.SERVER_HOST, "127.0.0.1");
  assert.equal(environment.NEUXBOT_DATA_DIR, "/data");
  assert.equal(environment.NEUXBOT_WEB_DIR, path.join("/release", "web"));
  assert.equal(
    environment.PI_MCP_MODULE_PATH,
    path.join("/release", "pi-bridge", "bridge.mjs"),
  );
  assert.ok(
    !Object.values(environment).some((value) => value.includes("desktop")),
  );
});

test("Web 与 Native 默认使用同一个 Core 数据目录", () => {
  assert.equal(
    defaultCoreDataDirectory({}, "darwin", "/user"),
    "/user/Library/Application Support/com.neuxbot.desktop",
  );
  assert.equal(
    defaultCoreDataDirectory(
      { APPDATA: "C:\\Users\\User\\AppData\\Roaming" },
      "win32",
      "C:\\Users\\User",
    ),
    "C:\\Users\\User\\AppData\\Roaming\\com.neuxbot.desktop",
  );
  assert.equal(
    defaultCoreDataDirectory({ ALLY_DATA_DIR: "/custom" }, "darwin", "/user"),
    "/custom",
  );
});
