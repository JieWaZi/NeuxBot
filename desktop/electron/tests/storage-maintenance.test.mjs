import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { loadModule } from "./load-module.mjs";

const { createDesktopLogs } = loadModule("main/storage-maintenance.ts");

test("轮转与手动清理同时维护活动日志", async (t) => {
  const directory = await mkdtemp(path.join(tmpdir(), "neuxbot-log-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const logs = createDesktopLogs(directory, { rotationBytes: 6 });
  await writeFile(path.join(directory, "other.log"), "保留");
  await Promise.all([
    logs.append("123456"),
    logs.append("abc"),
    logs.append("def"),
  ]);
  assert.equal(await logs.preview(), 12);
  assert.equal(await logs.clear(), 12);
  assert.equal(await logs.preview(), 0);
  assert.equal(await readFile(path.join(directory, "desktop.log"), "utf8"), "");
  await logs.append("next");
  assert.equal(
    await readFile(path.join(directory, "desktop.log"), "utf8"),
    "next",
  );
  assert.equal(
    await readFile(path.join(directory, "other.log"), "utf8"),
    "保留",
  );
});

test("只有代码缓存时仍能预览并清理实际占用", async (t) => {
  const directory = await mkdtemp(path.join(tmpdir(), "neuxbot-cache-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const { desktopCacheSize, clearDesktopCache } = loadModule(
    "main/storage-maintenance.ts",
  );
  const code = path.join(directory, "Code Cache");
  const { mkdir } = await import("node:fs/promises");
  await mkdir(code);
  await writeFile(path.join(code, "cached-script"), "code");
  const session = {
    storagePath: directory,
    getCacheSize: async () => 0,
    clearCache: async () => {},
    clearCodeCaches: async () => rm(code, { recursive: true }),
  };
  assert.equal(await desktopCacheSize(session), 4);
  assert.equal(await clearDesktopCache(session), 4);
});
