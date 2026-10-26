import path from "node:path";
import { fileURLToPath } from "node:url";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { execFileSync } from "node:child_process";
import { loadModule } from "../desktop/electron/tests/load-module.mjs";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const release = path.join(root, "desktop/release");
const bundle =
  process.platform === "darwin"
    ? path.join(
        release,
        process.arch === "arm64" ? "mac-arm64" : "mac",
        "NeuxBot.app",
      )
    : path.join(
        release,
        process.arch === "arm64" ? "win-arm64-unpacked" : "win-unpacked",
      );
const resources =
  process.platform === "darwin"
    ? path.join(bundle, "Contents/Resources")
    : path.join(bundle, "resources");
const executable =
  process.platform === "darwin"
    ? path.join(bundle, "Contents/MacOS/NeuxBot")
    : path.join(bundle, "NeuxBot.exe");
execFileSync(
  executable,
  [
    path.join(root, "scripts/smoke-desktop.cjs"),
    resources,
    path.join(root, "desktop/dist"),
  ],
  {
    env: { ...process.env, ELECTRON_RUN_AS_NODE: "1" },
    stdio: "inherit",
    timeout: 15000,
  },
);
const { CoreSupervisor } = loadModule("main/core-supervisor.ts");
const dataDir = await mkdtemp(path.join(tmpdir(), "ally-packaged-core-"));
const env = { ...process.env };
// 冒烟测试的业务文件与数据库一起隔离，不写入用户的默认受管目录。
env.ALLY_MANAGED_FILES_DIR = path.join(dataDir, "managed-files");
env.PI_MCP_MODULE_PATH = path.join(resources, "pi-bridge", "bridge.mjs");
env.ALLY_TERMINAL_RUNTIME_PATH = path.join(
  resources,
  "terminal-runtime",
  "worker.cjs",
);
env.ALLY_TERMINAL_NODE_PATH = executable;
env.ALLY_TERMINAL_ELECTRON_NODE = "1";
delete env.ALLY_CORE_URL;
delete env.ALLY_LOCAL_TOKEN;
const core = new CoreSupervisor({
  binary: path.join(
    resources,
    "bin",
    process.platform === "win32" ? "ally-core.exe" : "ally-core",
  ),
  dataDir,
  env,
});
try {
  const connection = await core.connection();
  const response = await fetch(`${connection.baseUrl}/api/v1/system/health`, {
    headers: { Authorization: `Bearer ${connection.token}` },
  });
  if (!response.ok) throw new Error(`Core health failed: ${response.status}`);
  console.log("Packaged Go Core health: passed");
  await core.stop();
  if (
    await fetch(connection.baseUrl)
      .then(() => true)
      .catch(() => false)
  )
    throw new Error("Core still accepts connections after stop");
  console.log("Packaged Go Core shutdown: passed");
} finally {
  await core.stop();
  await rm(dataDir, { recursive: true, force: true });
}
