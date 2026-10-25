import { execFileSync, spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { createRequire } from "node:module";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { startWeb } from "./start-web.mjs";
import "./builtin-skills.mjs";
import { prepareCatalog, prepareProvider } from "./plugin-resources.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const directory = path.join(root, "web/.runtime");
await mkdir(path.join(directory, "bin"), { recursive: true });
execFileSync("pnpm", ["--filter", "@neuxbot/pi-bridge", "build"], {
  cwd: root,
  stdio: "inherit",
  shell: process.platform === "win32",
});
execFileSync("pnpm", ["terminal:build"], {
  cwd: root,
  stdio: "inherit",
  shell: process.platform === "win32",
});
await prepareCatalog(path.join(directory, "plugins"));
await prepareProvider(
  path.join(directory, "plugins"),
  `${process.platform}-${process.arch}`,
);
for (const name of ["ally-core", "allycli"]) {
  execFileSync(
    "go",
    [
      "build",
      "-o",
      path.join(
        directory,
        "bin",
        name + (process.platform === "win32" ? ".exe" : ""),
      ),
      `./cmd/${name}`,
    ],
    { cwd: path.join(root, "server"), stdio: "inherit" },
  );
}
const webPort = process.env.ALLY_WEB_PORT || "1422";
const core = startWeb(directory, {
  ALLY_WEB_DIR: "",
  ALLY_TERMINAL_RUNTIME_PATH: path.join(
    root,
    "resources/terminal-runtime/worker.cjs",
  ),
  ALLY_WEB_DEV_ORIGIN: `http://127.0.0.1:${webPort}`,
  PI_MCP_MODULE_PATH: path.join(root, "resources/pi-bridge/bridge.mjs"),
});
// Core 完成加载后再启动开发页面，首屏会话请求不与业务初始化竞争。
const coreOrigin = `http://127.0.0.1:${process.env.ALLY_WEB_CORE_PORT || "8432"}`;
let coreReady = false;
for (let attempt = 0; attempt < 300; attempt++) {
  if (core.exitCode !== null || core.signalCode !== null) break;
  try {
    const response = await fetch(`${coreOrigin}/api/web/session`, {
      signal: AbortSignal.timeout(500),
    });
    if (response.status === 401) {
      coreReady = true;
      break;
    }
  } catch {}
  await delay(100);
}
if (!coreReady) {
  core.kill("SIGTERM");
  throw new Error("本机 Core 未就绪，开发页面未启动。");
}

// 直接持有 Vite，避免 pnpm 中间进程退出后留下未受管开发服务器。
const requireWeb = createRequire(path.join(root, "web/package.json"));
const viteEntry = path.join(
  path.dirname(requireWeb.resolve("vite/package.json")),
  "bin/vite.js",
);
const vite = spawn(process.execPath, [viteEntry], {
  cwd: path.join(root, "web"),
  env: process.env,
  stdio: ["ignore", "inherit", "inherit"],
});
const stop = () => {
  core.kill("SIGTERM");
  vite.kill("SIGTERM");
};
core.once("exit", () => vite.kill("SIGTERM"));
vite.once("exit", () => core.kill("SIGTERM"));
vite.once("error", (error) => {
  console.error(error.message);
  process.exitCode = 1;
  stop();
});
process.once("SIGINT", stop);
process.once("SIGTERM", stop);
