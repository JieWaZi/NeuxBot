import path from "node:path";
import { fileURLToPath } from "node:url";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { loadModule } from "../desktop/electron/tests/load-module.mjs";
const { CoreSupervisor } = loadModule("main/core-supervisor.ts");

export async function smokeCore({
  binary,
  resources,
  executable,
  env: environment = process.env,
  label = "Go Core",
  diagnostic = console.error,
}) {
  const dataDir = await mkdtemp(path.join(tmpdir(), "ally-packaged-core-"));
  const env = { ...environment };
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
  let coreOutput = "";
  const core = new CoreSupervisor({
    log: (chunk) => {
      coreOutput = (coreOutput + chunk).slice(-65536);
    },
    binary,
    dataDir,
    env,
  });
  try {
    const connection = await core.connection();
    const response = await fetch(`${connection.baseUrl}/api/v1/system/health`, {
      headers: { Authorization: `Bearer ${connection.token}` },
    });
    if (!response.ok) throw new Error(`Core health failed: ${response.status}`);
    console.log(`${label} health: passed`);
    await core.stop();
    if (
      await fetch(connection.baseUrl)
        .then(() => true)
        .catch(() => false)
    )
      throw new Error("Core still accepts connections after stop");
    console.log(`${label} shutdown: passed`);
  } catch (error) {
    if (coreOutput)
      diagnostic(
        `${label} diagnostics:\n${coreOutput.replace(/ally_[a-f0-9]{32}/g, "[redacted]")}`,
      );
    throw error;
  } finally {
    await core.stop();
    await rm(dataDir, { recursive: true, force: true });
  }
}

// 未打包 sidecar 与安装包复用同一启动、健康和退出验证。
if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const require = createRequire(path.join(root, "desktop/package.json"));
  await smokeCore({
    binary: path.join(
      root,
      "desktop/binaries",
      process.platform === "win32" ? "ally-core.exe" : "ally-core",
    ),
    resources: path.join(root, "resources"),
    executable: require("electron"),
  });
}
