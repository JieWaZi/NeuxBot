import { execFileSync } from "node:child_process";
import { chmod, copyFile, mkdir, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { allyCoreLdflags } from "./go-ldflags.mjs";
import { copyACPLicenses } from "./acp-licenses.mjs";
import "./builtin-skills.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const requested = process.argv[2];
const nativeTarget = `${process.platform}-${process.arch}`;
const targets = {
  "darwin-arm64": { goos: "darwin", goarch: "arm64", file: "ally-core" },
  "darwin-x64": { goos: "darwin", goarch: "amd64", file: "ally-core" },
  "win32-arm64": {
    goos: "windows",
    goarch: "arm64",
    file: "ally-core.exe",
  },
  "win32-x64": {
    goos: "windows",
    goarch: "amd64",
    file: "ally-core.exe",
  },
  "aarch64-apple-darwin": {
    goos: "darwin",
    goarch: "arm64",
    file: "ally-core",
  },
  "x86_64-apple-darwin": {
    goos: "darwin",
    goarch: "amd64",
    file: "ally-core",
  },
  "aarch64-pc-windows-msvc": {
    goos: "windows",
    goarch: "arm64",
    file: "ally-core.exe",
  },
  "x86_64-pc-windows-msvc": {
    goos: "windows",
    goarch: "amd64",
    file: "ally-core.exe",
  },
};
const targetName = requested ?? nativeTarget;
const target = targets[targetName];
if (!target) throw new Error(`不支持的桌面目标：${targetName}`);

const outputDirectory = path.join(root, "desktop/binaries");
const output = path.join(outputDirectory, target.file);
await mkdir(outputDirectory, { recursive: true });
await rm(
  path.join(
    outputDirectory,
    target.file === "ally-core" ? "ally-core.exe" : "ally-core",
  ),
  { force: true },
);
execFileSync(
  "go",
  [
    "build",
    "-trimpath",
    `-ldflags=${allyCoreLdflags()}`,
    "-o",
    output,
    "./cmd/ally-core",
  ],
  {
    cwd: path.join(root, "server"),
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: target.goos,
      GOARCH: target.goarch,
    },
    stdio: "inherit",
  },
);
if (target.goos !== "windows") await chmod(output, 0o755);
console.log(`已生成 NeuxBot Core sidecar: ${output}`);

const cliName = target.goos === "windows" ? "allycli.exe" : "allycli";
await rm(
  path.join(
    outputDirectory,
    target.goos === "windows" ? "allycli" : "allycli.exe",
  ),
  { force: true },
);
const cliOutput = path.join(outputDirectory, cliName);
execFileSync(
  "go",
  [
    "build",
    "-trimpath",
    `-ldflags=${allyCoreLdflags()}`,
    "-o",
    cliOutput,
    "./cmd/allycli",
  ],
  {
    cwd: path.join(root, "server"),
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: target.goos,
      GOARCH: target.goarch,
    },
    stdio: "inherit",
  },
);
if (target.goos !== "windows") await chmod(cliOutput, 0o755);
console.log(`已生成软件能力 CLI: ${cliOutput}`);

// 许可随实际引用的模块版本交付，宿主不重复维护依赖许可正文。
const knowledgeModule = JSON.parse(
  execFileSync(
    "go",
    ["list", "-m", "-json", "github.com/JieWaZi/neux-knowledge"],
    {
      cwd: path.join(root, "server"),
      encoding: "utf8",
    },
  ),
);
const knowledgeLicenses = path.join(
  root,
  "desktop/resources/licenses/neux-knowledge",
);
await mkdir(knowledgeLicenses, { recursive: true });
for (const name of ["LICENSE", "THIRD_PARTY_NOTICES.md"]) {
  const destination = path.join(knowledgeLicenses, name);
  // Go 模块缓存文件只读；先替换旧产物，保证重复构建可以覆盖。
  await rm(destination, { force: true });
  await copyFile(path.join(knowledgeModule.Dir, name), destination);
  await chmod(destination, 0o644);
}
await copyACPLicenses(root, path.join(root, "desktop/resources/licenses"));
