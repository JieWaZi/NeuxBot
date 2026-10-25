import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const pnpm = "pnpm";

if (!["darwin", "win32"].includes(process.platform)) {
  throw new Error("NeuxBot 桌面安装包当前只支持在 macOS 或 Windows 构建");
}
const expectedArch = process.env.EXPECTED_ARCH ?? "";
if (expectedArch !== "" && process.arch !== expectedArch) {
  throw new Error(`构建机架构是 ${process.arch}，发布要求 ${expectedArch}`);
}

function run(label, command, args) {
  console.log(`\n[desktop-package] ${label}`);
  execFileSync(command, args, {
    cwd: root,
    env: process.env,
    stdio: "inherit",
    // Windows 上的 pnpm.cmd 不能无 shell 直接 spawn，否则会 EINVAL。
    shell: process.platform === "win32" && command === pnpm,
  });
}

run("构建 Go Core sidecar", pnpm, ["build:sidecar"]);
run("构建共享终端执行资源", pnpm, ["terminal:build"]);
run("构建 Renderer", pnpm, ["--dir", "desktop", "build"]);
run("检查首屏静态资源预算", pnpm, ["--dir", "desktop", "bundle:check"]);
run("准备原生Renderer无损交付资源", process.execPath, [
  path.join(root, "scripts/package-native-renderer.mjs"),
]);
run("构建 Electron 主进程与 preload", pnpm, [
  "--dir",
  "desktop",
  "electron:build",
]);
// 全量契约测试由调用方显式选择；打包仍执行资源预算和隔离产物冒烟。
if (process.argv.includes("--test-contracts")) {
  run("验证 Electron 桌面契约", pnpm, ["--dir", "desktop", "electron:test"]);
}
if (process.platform === "darwin") {
  run("生成当前架构应用包", pnpm, [
    "--dir",
    "desktop",
    "exec",
    "electron-builder",
    "--mac",
    "dir",
    "--publish",
    "never",
  ]);
  run("签名并验证应用包", process.execPath, [
    path.join(root, "scripts/sign-desktop.mjs"),
  ]);
  run("生成并验证 DMG 与体积报告", process.execPath, [
    path.join(root, "scripts/package-desktop-artifact.mjs"),
  ]);
} else {
  run("生成当前架构 Windows 安装包", pnpm, [
    "--dir",
    "desktop",
    "exec",
    "electron-builder",
    "--win",
    "nsis",
    process.arch === "arm64" ? "--arm64" : "--x64",
    "--publish",
    "never",
  ]);
  run("验证 Windows 安装包与体积报告", process.execPath, [
    path.join(root, "scripts/package-windows-artifact.mjs"),
  ]);
}
run("执行打包产物冒烟测试", process.execPath, [
  path.join(root, "scripts/smoke-desktop.mjs"),
]);

console.log(
  `\n[desktop-package] 当前架构的 NeuxBot ${
    process.platform === "darwin" ? "应用、DMG" : "应用、NSIS 安装包"
  }和体积报告已通过验证`,
);
