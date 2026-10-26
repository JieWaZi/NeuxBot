import { execFileSync } from "node:child_process";
import { chmod, copyFile, cp, mkdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { allyCoreLdflags } from "./go-ldflags.mjs";
import { copyACPLicenses } from "./acp-licenses.mjs";
import "./builtin-skills.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const targets = {
  "darwin-arm64": ["darwin", "arm64"],
  "darwin-x64": ["darwin", "amd64"],
  "win32-arm64": ["windows", "arm64"],
  "win32-x64": ["windows", "amd64"],
};
const targetName = process.argv[2] || `${process.platform}-${process.arch}`;
const target = targets[targetName];
if (!target) throw new Error(`不支持的 Web 目标：${targetName}`);
const [goos, goarch] = target;
const output = path.join(root, "web/release", targetName);
const run = (command, args, options = {}) =>
  execFileSync(command, args, {
    cwd: root,
    stdio: "inherit",
    shell: process.platform === "win32" && command === "pnpm",
    ...options,
  });
run("pnpm", ["--filter", "@neuxbot/pi-bridge", "build"]);
run("pnpm", ["--dir", "web", "build"]);
run("pnpm", ["terminal:build"]);
await rm(output, { recursive: true, force: true });
await mkdir(path.join(output, "bin"), { recursive: true });
await cp(path.join(root, "web/dist"), path.join(output, "web"), {
  recursive: true,
});
await cp(
  path.join(root, "resources/pi-bridge"),
  path.join(output, "pi-bridge"),
  { recursive: true },
);
await cp(
  path.join(root, "resources/terminal-runtime"),
  path.join(output, "terminal-runtime"),
  { recursive: true },
);
for (const name of ["ally-core", "allycli"]) {
  const destination = path.join(
    output,
    "bin",
    name + (goos === "windows" ? ".exe" : ""),
  );
  run(
    "go",
    [
      "build",
      "-trimpath",
      `-ldflags=${allyCoreLdflags()}`,
      "-o",
      destination,
      `./cmd/${name}`,
    ],
    {
      cwd: path.join(root, "server"),
      env: { ...process.env, CGO_ENABLED: "0", GOOS: goos, GOARCH: goarch },
    },
  );
  if (goos !== "windows") await chmod(destination, 0o755);
}
for (const name of ["LICENSE", "THIRD_PARTY_NOTICES.md"]) {
  await copyFile(path.join(root, name), path.join(output, name));
}
const knowledge = JSON.parse(
  execFileSync(
    "go",
    ["list", "-m", "-json", "github.com/JieWaZi/neux-knowledge"],
    { cwd: path.join(root, "server"), encoding: "utf8" },
  ),
);
await mkdir(path.join(output, "licenses/neux-knowledge"), { recursive: true });
for (const name of ["LICENSE", "THIRD_PARTY_NOTICES.md"]) {
  await copyFile(
    path.join(knowledge.Dir, name),
    path.join(output, "licenses/neux-knowledge", name),
  );
}
await copyACPLicenses(root, path.join(output, "licenses"));
await copyFile(
  path.join(root, "scripts/start-web.mjs"),
  path.join(output, "start-web.mjs"),
);
await writeFile(
  path.join(output, "README.txt"),
  "NeuxBot Web\n需要 Node.js 22+；在此目录运行 node start-web.mjs。\n按终端地址打开网页即可进入工作台，无需配对码；Ctrl+C 退出。\n默认与 Native 共用 Core 数据目录，启动前请退出另一端。ALLY_DATA_DIR 可指定数据根；同一数据根只能由一个 Core 持有。独立测试还须设置 ALLY_MANAGED_FILES_DIR 为独立受管文件根。ALLY_WEB_CORE_PORT 可修改回环端口。\n智能体 CLI 按设置页面指引在本机安装。文件和 Git 操作限于当前聊天已授权目录；交互终端由 Core 持有，浏览器刷新可恢复。\n",
);
console.log(`已生成独立 Web 发布目录：${output}`);
