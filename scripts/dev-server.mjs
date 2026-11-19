import { spawn, execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defaultCoreDataDirectory } from "./start-web.mjs";
import "./builtin-skills.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const cliPath = path.join(
  root,
  "server/bin",
  process.platform === "win32" ? "botcli.exe" : "botcli",
);
execFileSync("go", ["build", "-o", cliPath, "./cmd/botcli"], {
  cwd: path.join(root, "server"),
  stdio: "inherit",
});
const core = spawn(
  "go",
  ["run", "./cmd/bot-core", "-f", "configs/local.yaml"],
  {
    cwd: path.join(root, "server"),
    env: {
      ...process.env,
      NEUXBOT_DATA_DIR: defaultCoreDataDirectory(),
      BOTCLI_BINARY_PATH: cliPath,
      PI_MCP_MODULE_PATH: path.join(root, "resources/pi-bridge/bridge.mjs"),
    },
    stdio: "inherit",
  },
);

core.on("error", (error) => {
  process.stderr.write(`NeuxBot Core 启动失败：${error}\n`);
  process.exitCode = 1;
});
core.on("exit", (code, signal) => {
  process.exitCode = code ?? (signal ? 128 : 1);
});
process.on("SIGINT", () => core.kill("SIGINT"));
process.on("SIGTERM", () => core.kill("SIGTERM"));
