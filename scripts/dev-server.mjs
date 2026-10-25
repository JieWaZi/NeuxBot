import { spawn, execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import "./builtin-skills.mjs";
import { prepareCatalog, prepareProvider } from "./plugin-resources.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const pluginOutput = path.join(root, "desktop/resources/plugins");
await prepareCatalog(pluginOutput);
await prepareProvider(pluginOutput, `${process.platform}-${process.arch}`);
const cliPath = path.join(
  root,
  "server/bin",
  process.platform === "win32" ? "allycli.exe" : "allycli",
);
execFileSync("go", ["build", "-o", cliPath, "./cmd/allycli"], {
  cwd: path.join(root, "server"),
  stdio: "inherit",
});
const core = spawn(
  "go",
  ["run", "./cmd/ally-core", "-f", "configs/local.yaml"],
  {
    cwd: path.join(root, "server"),
    env: {
      ...process.env,
      ALLYCLI_BINARY_PATH: cliPath,
      ALLY_PLUGIN_RESOURCES_DIR: pluginOutput,
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
