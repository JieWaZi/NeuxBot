import { execFileSync } from "node:child_process";
import {
  lstat,
  mkdtemp,
  readdir,
  readlink,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const release = path.join(root, "desktop/release");
const bundle = path.join(
  release,
  process.arch === "arm64" ? "mac-arm64" : "mac",
  "NeuxBot.app",
);
const mib = 1024 * 1024;
const budgets = {
  application: 450 * mib,
  // 资源预算覆盖 Core、CLI、前端和必要原生运行资源，各平台以构建报告为准。
  resources: 215 * mib,
  installer: 170 * mib,
};

async function sizeOf(file) {
  const info = await lstat(file);
  if (info.isSymbolicLink()) return 0;
  if (!info.isDirectory()) return info.size;
  const sizes = await Promise.all(
    (await readdir(file)).map((name) => sizeOf(path.join(file, name))),
  );
  return sizes.reduce((sum, size) => sum + size, 0);
}

function check(label, size, limit) {
  console.log(
    `${label}: ${(size / mib).toFixed(1)} MiB / ${(limit / mib).toFixed(0)} MiB`,
  );
  if (size > limit) throw new Error(`${label} exceeds release size budget`);
}

async function removeLegacyApplicationsLink(file) {
  try {
    const info = await lstat(file);
    if (!info.isSymbolicLink() || (await readlink(file)) !== "/Applications") {
      throw new Error(
        "Installer output contains an unexpected Applications entry",
      );
    }
    await rm(file);
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }
}

const report = path.join(release, `size-report-${process.arch}.json`);
await rm(report, { force: true });
const application = await sizeOf(bundle);
const resources = await sizeOf(path.join(bundle, "Contents/Resources"));
check("Installed application", application, budgets.application);
check("Application resources", resources, budgets.resources);
const source = path.dirname(bundle);
await removeLegacyApplicationsLink(path.join(source, "Applications"));
const entries = await readdir(source);
if (entries.some((name) => name !== "NeuxBot.app")) {
  throw new Error("Installer source contains unexpected build artifacts");
}
const installer = path.join(release, `NeuxBot-mac-${process.arch}.dmg`);
const staging = await mkdtemp(path.join(tmpdir(), `ally-dmg-${process.arch}-`));
try {
  const stagedBundle = path.join(staging, "NeuxBot.app");
  // DMG 专用内容位于仓库外，避免 Applications 链接被 Vite 或其他工具遍历。
  execFileSync("/usr/bin/ditto", [bundle, stagedBundle], { stdio: "inherit" });
  execFileSync(
    "/usr/bin/codesign",
    ["--verify", "--deep", "--strict", stagedBundle],
    { stdio: "inherit" },
  );
  await symlink("/Applications", path.join(staging, "Applications"));
  // 在签名后生成分架构安装镜像，避免将不同架构及其他构建产物重复装入。
  // Chromium 的 ULMO 方案使用 8192 sectors（4 MiB）；本项目为交付压缩
  // 采用 32768 个 512-byte sector（16 MiB），以最终镜像实测为准。
  // 这减少 x64 大二进制在块间的重复，实际镜像仍须通过校验与体积预算。
  // https://chromium.googlesource.com/chromium/src/+/HEAD/chrome/installer/mac/pkg-dmg
  execFileSync(
    "/usr/bin/hdiutil",
    [
      "create",
      "-volname",
      "NeuxBot",
      "-srcfolder",
      staging,
      "-fs",
      "HFS+",
      "-format",
      "ULMO",
      "-imagekey",
      "udif-chunk-size=32768",
      "-ov",
      installer,
    ],
    { stdio: "inherit" },
  );
} finally {
  await rm(staging, { recursive: true, force: true });
}
execFileSync("/usr/bin/hdiutil", ["verify", installer], { stdio: "inherit" });
const installerSize = (await stat(installer)).size;
check("Installer", installerSize, budgets.installer);
await writeFile(
  report,
  JSON.stringify(
    {
      architecture: process.arch,
      bytes: { application, resources, installer: installerSize },
      budgets,
    },
    null,
    2,
  ) + "\n",
);
