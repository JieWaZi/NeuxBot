import { lstat, readdir, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const mib = 1024 * 1024;
const budgets = {
  application: 450 * mib,
  // 与 macOS 共用资源预算，包含固定飞书与 GitHub 程序；各平台仍以实测报告约束。
  resources: 215 * mib,
  installer: 170 * mib,
};

async function sizeOf(file) {
  const info = await lstat(file);
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
  return size > limit;
}

export async function verifyWindowsArtifact(release, architecture) {
  const bundle = path.join(
    release,
    architecture === "arm64" ? "win-arm64-unpacked" : "win-unpacked",
  );
  const installer = path.join(release, `NeuxBot-windows-${architecture}.exe`);
  const report = path.join(release, `size-report-windows-${architecture}.json`);
  await rm(report, { force: true });
  const application = await sizeOf(bundle);
  const resources = await sizeOf(path.join(bundle, "resources"));
  const installerSize = (await stat(installer)).size;
  // 保留最大的文件及相对路径，便于在 Windows CI 中定位原生编译产物等体积来源。
  const files = [];
  async function collectFiles(directory) {
    for (const name of await readdir(directory)) {
      const file = path.join(directory, name);
      const info = await lstat(file);
      if (info.isDirectory()) await collectFiles(file);
      else
        files.push({
          path: path.relative(bundle, file).replaceAll("\\", "/"),
          bytes: info.size,
        });
    }
  }
  await collectFiles(bundle);
  const largestFiles = files.sort((a, b) => b.bytes - a.bytes).slice(0, 30);
  // 超限仍写出报告并打印全部检查，避免第一项失败遮蔽资源与安装包的真实体积。
  await writeFile(
    report,
    JSON.stringify(
      {
        platform: "windows",
        architecture,
        bytes: { application, resources, installer: installerSize },
        budgets,
        largestFiles,
      },
      null,
      2,
    ) + "\n",
  );
  const failures = [
    ["Installed application", application, budgets.application],
    ["Application resources", resources, budgets.resources],
    ["Installer", installerSize, budgets.installer],
  ].filter(([label, size, limit]) => check(label, size, limit));
  if (failures.length > 0) {
    for (const file of largestFiles) {
      console.log(`${(file.bytes / mib).toFixed(1)} MiB  ${file.path}`);
    }
    throw new Error(
      `${failures.map(([label]) => label).join(", ")} exceeds release size budget`,
    );
  }
}

const isMain =
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) {
  if (process.platform !== "win32") {
    throw new Error("Windows 安装包只能在 Windows 构建机上验证");
  }
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  await verifyWindowsArtifact(path.join(root, "desktop/release"), process.arch);
}
