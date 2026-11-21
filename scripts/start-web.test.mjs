import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import test from "node:test";
import path from "node:path";
import os from "node:os";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import {
  webRuntimeEnvironment,
  defaultCoreDataDirectory,
  installBundledBotCLI,
} from "./start-web.mjs";

test("Linux Web 打包在目标架构与宿主不一致时提前失败", () => {
  const target =
    process.platform === "linux" && process.arch === "x64"
      ? "linux-arm64"
      : "linux-x64";
  const script = new URL("./package-web.mjs", import.meta.url).href;
  const { status, stderr } = spawnSync(
    process.execPath,
    [
      "--input-type=module",
      "--eval",
      `import childProcess from 'node:child_process';
       import { syncBuiltinESMExports } from 'node:module';
       childProcess.execFileSync = () => { throw new Error('Build started'); };
       syncBuiltinESMExports();
       process.argv[2] = ${JSON.stringify(target)};
       await import(${JSON.stringify(script)});`,
    ],
    { encoding: "utf8" },
  );
  assert.equal(status, 1);
  assert.match(stderr, /Linux Web 包须在对应的 Linux 平台与架构上构建/);
});

test(
  "Web 发布启动用同包 botcli 安装终端命令，支持隔离冒烟运行",
  {
    skip: process.platform === "win32",
  },
  async (t) => {
    const root = await mkdtemp(path.join(os.tmpdir(), "neuxbot-cli-install-"));
    t.after(() => rm(root, { recursive: true, force: true }));
    const binary = path.join(root, "bin", "botcli");
    await mkdir(path.dirname(binary));
    await writeFile(
      binary,
      '#!/bin/sh\nprintf "%s" "$1" > "$INSTALL_TEST_OUT"\n',
      {
        mode: 0o700,
      },
    );
    const output = path.join(root, "result");
    const environment = {
      ...webRuntimeEnvironment(root, {}),
      INSTALL_TEST_OUT: output,
    };
    await installBundledBotCLI({
      ...environment,
      NEUXBOT_SKIP_BOTCLI_INSTALL: "1",
    });
    await assert.rejects(readFile(output), { code: "ENOENT" });
    await installBundledBotCLI(environment);
    assert.equal(await readFile(output, "utf8"), "install");
  },
);

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
    defaultCoreDataDirectory(
      { NEUXBOT_DATA_DIR: "/custom" },
      "darwin",
      "/user",
    ),
    "/custom",
  );
});
