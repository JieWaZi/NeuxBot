import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { readStartupAssets } from "./check-desktop-entry-budget.mjs";

async function fixture(t, manifest) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "ally-entry-budget-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  await mkdir(path.join(directory, ".vite"));
  await mkdir(path.join(directory, "assets"));
  await writeFile(
    path.join(directory, ".vite/manifest.json"),
    JSON.stringify(manifest),
  );
  for (const chunk of Object.values(manifest)) {
    await writeFile(path.join(directory, chunk.file), "x".repeat(chunk.size));
  }
  return directory;
}

const manifest = {
  "index.html": {
    file: "assets/entry.js",
    isEntry: true,
    imports: ["shared"],
    dynamicImports: [
      "../packages/frontend/src/app/workspace.tsx",
      "src/platform/index.ts",
    ],
    size: 10,
  },
  "src/platform/index.ts": {
    file: "assets/platform.js",
    imports: ["shared"],
    size: 9,
  },
  shared: { file: "assets/shared.js", size: 20 },
  "../packages/frontend/src/app/workspace.tsx": {
    file: "assets/workspace.js",
    imports: ["shared", "page"],
    dynamicImports: ["diff"],
    size: 30,
  },
  page: { file: "assets/page.js", imports: ["shared"], size: 40 },
  diff: { file: "assets/diff.js", size: 100 },
  "../vendor/companion/renderer.js": { file: "assets/companion.js", size: 6 },
  "../vendor/elastic-mesh/renderer.js": { file: "assets/elastic.js", size: 7 },
  "../packages/frontend/src/app/welcome/elastic-logo.tsx": {
    file: "assets/logo.js",
    imports: ["shared"],
    size: 5,
  },
};

test("启动预算包含工作台与品牌预热依赖，共享文件只计一次，按需弹窗不提前计入", async (t) => {
  const directory = await fixture(t, manifest);
  const assets = await readStartupAssets(directory);
  assert.equal(assets.entryBytes, 30);
  assert.equal(assets.startupBytes, 127);
  assert.deepEqual(assets.startupFiles.map((asset) => asset.file).sort(), [
    "assets/companion.js",
    "assets/elastic.js",
    "assets/entry.js",
    "assets/logo.js",
    "assets/page.js",
    "assets/platform.js",
    "assets/shared.js",
    "assets/workspace.js",
  ]);
});

test("依赖图存在循环时仍然有限遍历", async (t) => {
  const directory = await fixture(t, {
    ...manifest,
    shared: { ...manifest.shared, imports: ["page"] },
  });
  const assets = await readStartupAssets(directory);
  assert.equal(assets.startupBytes, 127);
});

test("工作台入口消失时拒绝只检查轻量启动画面", async (t) => {
  const {
    "../packages/frontend/src/app/workspace.tsx": workspace,
    ...withoutWorkspace
  } = manifest;
  assert.ok(workspace);
  const directory = await fixture(t, withoutWorkspace);
  await assert.rejects(readStartupAssets(directory), /工作台/);
});
