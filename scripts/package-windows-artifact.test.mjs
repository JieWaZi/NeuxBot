import assert from "node:assert/strict";
import { mkdtemp, mkdir, open, readFile, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { verifyWindowsArtifact } from "./package-windows-artifact.mjs";

const desktopRequire = createRequire(
  new URL("../desktop/package.json", import.meta.url),
);
const builderRequire = createRequire(
  desktopRequire.resolve("electron-builder"),
);
const { FileMatcher } = builderRequire("app-builder-lib/out/fileMatcher.js");
const manifest = desktopRequire("./package.json");

test("原生依赖裁剪使用打包器的真实过滤规则，保留运行文件并排除编译产物", () => {
  const app = path.resolve("fixture-app");
  for (const platform of ["win32", "darwin"]) {
    for (const arch of ["x64", "arm64"]) {
      const runtime = manifest.build.extraResources.find(
        (entry) => entry.to === "terminal-runtime/node_modules/node-pty",
      );
      assert.ok(runtime);
      const matcher = new FileMatcher(
        app,
        path.resolve("fixture-output"),
        (value) =>
          value.replaceAll("${platform}", platform).replaceAll("${arch}", arch),
        runtime.filter,
      );
      const filter = matcher.createFilter();
      const includes = (file) =>
        filter(path.join(app, file.replace(/^node_modules\/node-pty\//, "")), {
          isDirectory: () => false,
        });
      for (const file of [
        "node-pty/lib/index.js",
        "node-pty/lib/worker/conoutSocketWorker.js",
        "node-pty/package.json",
        "node-pty/LICENSE",
        "node-pty/build/Release/pty.node",
        "node-pty/build/Release/conpty.node",
        "node-pty/build/Release/conpty_console_list.node",
        "node-pty/build/Release/winpty-agent.exe",
        "node-pty/build/Release/winpty.dll",
        "node-pty/build/Release/conpty/conpty.dll",
        "node-pty/build/Release/conpty/OpenConsole.exe",
        "node-pty/build/Release/spawn-helper",
        `node-pty/prebuilds/${platform}-${arch}/pty.node`,
      ])
        assert.ok(
          includes(`node_modules/${file}`),
          `${platform}-${arch}: ${file}`,
        );
      for (const file of [
        "node-pty/build/config.gypi",
        "node-pty/build/Debug/pty.node",
        "node-pty/build/Release/conpty.lib",
        "node-pty/build/Release/pty.exp",
        "node-pty/build/Release/obj/conpty/conpty.iobj",
        "node-pty/build/Release/obj/conpty/conpty.node",
        "node-pty/build/Release/conpty/debug.pdb",
        "node-pty/prebuilds/linux-x64/pty.node",
        "node-addon-api/napi.h",
        "node-addon-api/tools/check-napi.js",
      ])
        assert.ok(
          !includes(`node_modules/${file}`),
          `${platform}-${arch}: ${file}`,
        );
      // 目录过滤必须允许遍历到运行文件，否则文件本身匹配也无法被复制。
      for (const directory of [
        "node-pty/build",
        "node-pty/build/Release",
        "node-pty/build/Release/conpty",
      ]) {
        assert.ok(
          filter(path.join(app, directory.replace(/^node-pty\//, "")), {
            isDirectory: () => true,
          }),
        );
      }
    }
  }
});

async function fixture(
  t,
  { application, resources, installer },
  architecture = "x64",
) {
  const release = await mkdtemp(
    path.join(os.tmpdir(), "neuxbot-windows-budget-"),
  );
  t.after(() => rm(release, { recursive: true, force: true }));
  const bundle = path.join(
    release,
    architecture === "arm64" ? "win-arm64-unpacked" : "win-unpacked",
  );
  await mkdir(path.join(bundle, "resources"), { recursive: true });
  // 稀疏文件验证真实目录计量与预算边界，不实际分配数百 MiB 的内容。
  for (const [file, size] of [
    [path.join(bundle, "NeuxBot.exe"), application - resources],
    [path.join(bundle, "resources/app.asar"), resources],
    [path.join(release, `NeuxBot-windows-${architecture}.exe`), installer],
  ]) {
    const handle = await open(file, "w");
    try {
      await handle.truncate(size);
    } finally {
      await handle.close();
    }
  }
  return release;
}

const mib = 1024 * 1024;
test("Windows x64 和 arm64 的三项体积在上限时通过", async (t) => {
  const bytes = {
    application: 450 * mib,
    resources: 215 * mib,
    installer: 170 * mib,
  };
  for (const architecture of ["x64", "arm64"]) {
    const release = await fixture(t, bytes, architecture);
    await verifyWindowsArtifact(release, architecture);
    const report = JSON.parse(
      await readFile(
        path.join(release, `size-report-windows-${architecture}.json`),
        "utf8",
      ),
    );
    assert.deepEqual(report.bytes, bytes);
    assert.equal(report.largestFiles[0].path, "NeuxBot.exe");
  }
});

test("超限仍拒绝发布并保存全部实测体积和大文件清单", async (t) => {
  const bytes = {
    application: 450 * mib + 1,
    resources: 215 * mib + 1,
    installer: 170 * mib + 1,
  };
  const release = await fixture(t, bytes);
  await assert.rejects(
    verifyWindowsArtifact(release, "x64"),
    /Installed application, Application resources, Installer exceeds release size budget/,
  );
  const report = JSON.parse(
    await readFile(path.join(release, "size-report-windows-x64.json"), "utf8"),
  );
  assert.deepEqual(report.bytes, bytes);
  assert.equal(report.largestFiles[1].path, "resources/app.asar");
  assert.equal(report.largestFiles[1].bytes, bytes.resources);
});
