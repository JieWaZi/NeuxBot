import { execFile, spawn } from "node:child_process";
import { existsSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";

/** 与 Native 的 appData/com.neuxbot.desktop 保持同一 Core 数据身份。 */
export function defaultCoreDataDirectory(
  inherited = process.env,
  platform = process.platform,
  home = os.homedir(),
) {
  if (inherited.NEUXBOT_DATA_DIR) return inherited.NEUXBOT_DATA_DIR;
  const paths = platform === "win32" ? path.win32 : path.posix;
  const appData =
    platform === "darwin"
      ? paths.join(home, "Library", "Application Support")
      : platform === "win32"
        ? inherited.APPDATA || paths.join(home, "AppData", "Roaming")
        : inherited.XDG_CONFIG_HOME || paths.join(home, ".config");
  return paths.join(appData, "com.neuxbot.desktop");
}

/** 发布目录只依赖同目录资源、本机 Node 和用户选择的 Runtime。 */
export function webRuntimeEnvironment(directory, inherited = process.env) {
  const environment = {
    ...inherited,
    SERVER_HOST: "127.0.0.1",
    GIN_MODE: inherited.GIN_MODE || "release",
    SERVER_PORT: inherited.NEUXBOT_WEB_CORE_PORT || "8432",
    SERVER_SHOW_PARAMS: "false",
    NEUXBOT_WEB_DIR: path.join(directory, "web"),
    NEUXBOT_DATA_DIR: defaultCoreDataDirectory(inherited),
    BOTCLI_BINARY_PATH: path.join(
      directory,
      "bin",
      process.platform === "win32" ? "botcli.exe" : "botcli",
    ),
    NEUXBOT_TERMINAL_RUNTIME_PATH: path.join(
      directory,
      "terminal-runtime",
      "worker.cjs",
    ),
    NEUXBOT_TERMINAL_NODE_PATH: process.execPath,
    PI_MCP_MODULE_PATH: path.join(directory, "pi-bridge", "bridge.mjs"),
  };
  delete environment.NEUXBOT_DESKTOP_PARENT_PID;
  delete environment.NEUXBOT_LOCAL_TOKEN;
  delete environment.NEUXBOT_WEB_DEV_ORIGIN;
  delete environment.NEUXBOT_TERMINAL_ELECTRON_NODE;
  delete environment.ELECTRON_RUN_AS_NODE;
  return environment;
}

/** 发布启动与显式终端安装共用同包 botcli 的安装入口。 */
export function installBundledBotCLI(environment) {
  if (environment.NEUXBOT_SKIP_BOTCLI_INSTALL === "1") return Promise.resolve();
  return promisify(execFile)(environment.BOTCLI_BINARY_PATH, ["install"], {
    env: environment,
    timeout: 10000,
    windowsHide: true,
  });
}

export function startWeb(directory, overrides = {}) {
  const binary = path.join(
    directory,
    "bin",
    process.platform === "win32" ? "bot-core.exe" : "bot-core",
  );
  if (!existsSync(binary))
    throw new Error("缺少 Core 二进制，请先运行 pnpm package:web。");
  const environment = { ...webRuntimeEnvironment(directory), ...overrides };
  void installBundledBotCLI(environment).catch((error) => {
    console.error(`botcli 安装失败：${error.stderr?.trim() || error.message}`);
  });
  const child = spawn(binary, [], {
    cwd: directory,
    env: environment,
    stdio: "inherit",
  });
  child.once("error", (error) => {
    console.error(`Web 启动失败：${error.message}`);
    process.exitCode = 1;
  });
  child.once("exit", (code, signal) => {
    process.exitCode = code ?? (signal ? 128 : 1);
  });
  // ginx 在 SIGTERM 分支执行有序关闭；两个终端信号均转发为退出请求。
  process.once("SIGINT", () => child.kill("SIGTERM"));
  process.once("SIGTERM", () => child.kill("SIGTERM"));
  return child;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href
) {
  const current = path.dirname(fileURLToPath(import.meta.url));
  const directory =
    path.basename(current) === "scripts"
      ? path.resolve(
          current,
          "../web/release",
          `${process.platform}-${process.arch}`,
        )
      : current;
  startWeb(directory);
}
