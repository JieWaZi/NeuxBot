import {
  app,
  BrowserWindow,
  protocol,
  net,
  Menu,
  dialog,
  powerMonitor,
  powerSaveBlocker,
  shell,
} from "electron";
import path from "node:path";
import { appendFile, mkdir } from "node:fs/promises";
import { CoreSupervisor } from "./core-supervisor";
import { installDesktopServices } from "./desktop-services";
import { configureApplicationIcon } from "./application-icon";
import { createApplicationLifecycle } from "./application-lifecycle";
import { rendererResponse } from "./renderer-protocol";
import { installIMPowerLifecycle } from "./im-power-lifecycle";
import { createCoreEvents } from "./core-events";
import type { CoreEventClient } from "../shared/core-event-client";
import { installAutomaticTaskPowerLifecycle } from "./automatic-task-power-lifecycle";
import { ApplicationUpdate } from "./application-update";

protocol.registerSchemesAsPrivileged([
  {
    scheme: "ally",
    privileges: {
      standard: true,
      secure: true,
      supportFetchAPI: true,
      corsEnabled: true,
    },
  },
]);
app.setName("NeuxBot");
// 桌面会话存储与 Core 数据分别拥有目录，Core 延续应用标识对应的数据位置。
const dataDir =
  process.env.ALLY_DATA_DIR ||
  path.join(app.getPath("appData"), "com.neuxally.desktop");
app.setPath("userData", path.join(dataDir, "electron"));
const isDevelopment =
  !app.isPackaged && process.env.ALLY_RENDERER_URL !== undefined;
let core: CoreSupervisor | undefined;
let coreEvents: CoreEventClient | undefined;
let updates: ApplicationUpdate | undefined;
let disposePowerLifecycle: (() => void) | undefined;
let disposeIMPowerLifecycle: (() => void) | undefined;
async function log(message: string) {
  if (isDevelopment) {
    process.stderr.write(message.endsWith("\n") ? message : `${message}\n`);
  }
  const line = message.endsWith("\n") ? message : `${message}\n`;
  try {
    await mkdir(path.join(dataDir, "logs"), { recursive: true });
    await appendFile(
      path.join(dataDir, "logs", "desktop.log"),
      `${new Date().toISOString()} ${line}`,
    );
  } catch {
    // 日志失败不能阻止桌面生命周期继续收尾。
  }
}
async function createWindow(): Promise<BrowserWindow> {
  const development =
    !app.isPackaged && process.env.ALLY_RENDERER_URL !== undefined;
  const rendererURL = new URL(
    development
      ? (process.env.ALLY_RENDERER_URL ?? "")
      : "ally://app/index.html",
  ).href;
  if (development && new URL(rendererURL).origin !== "http://127.0.0.1:1420")
    throw new Error("开发页面地址无效");
  const current = new BrowserWindow({
    title: "NeuxBot",
    width: 1280,
    height: 820,
    minWidth: 900,
    minHeight: 600,
    show: false,
    titleBarStyle: "hidden",
    // Electron 使用控件左上角坐标；14px 交通灯在 44px 标题栏内居中时 y 为 15px。
    trafficLightPosition: { x: 14, y: 15 },
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      webviewTag: true,
    },
  });
  if (!core) throw new Error("Core 尚未装配");
  const dispose = installDesktopServices(
    current,
    core,
    rendererURL,
    coreEvents,
    updates,
  );
  current.webContents.on("will-prevent-unload", (event) => {
    const answer = dialog.showMessageBoxSync(current, {
      type: "warning",
      buttons: ["继续编辑", "放弃草稿并关闭"],
      defaultId: 0,
      cancelId: 0,
      message: "右侧工作台有未保存的文件草稿",
      detail: "关闭窗口或刷新页面会丢失这些修改。",
    });
    if (answer === 1) event.preventDefault();
  });
  current.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  current.webContents.on("will-navigate", (event, url) => {
    if (url.split("#")[0] !== rendererURL.split("#")[0]) event.preventDefault();
  });
  current.on("closed", () => {
    dispose();
  });
  await current.loadURL(rendererURL);
  return current;
}
if (!app.requestSingleInstanceLock()) app.quit();
else {
  const lifecycle = createApplicationLifecycle({
    app,
    createWindow,
    resourcesPath: process.resourcesPath,
    stopCore: async () => {
      await updates?.dispose();
      disposePowerLifecycle?.();
      disposeIMPowerLifecycle?.();
      await coreEvents?.close();
      await core?.stop();
    },
    log,
  });
  void app
    .whenReady()
    .then(async () => {
      configureApplicationIcon(app);
      updates = new ApplicationUpdate({
        version: app.getVersion(),
        packaged: app.isPackaged,
        platform: process.platform,
        arch: process.arch,
        cacheDirectory: path.join(app.getPath("userData"), "updates"),
        fetch: (url, options) => net.fetch(url, options),
        openInstaller: (file) => shell.openPath(file),
        log,
      });
      updates.start();
      const dist = path.join(__dirname, "../dist");
      protocol.handle("ally", async (request) => {
        try {
          return await rendererResponse(dist, request.url, (url) =>
            net.fetch(url),
          );
        } catch {
          return new Response("Not found", { status: 404 });
        }
      });
      Menu.setApplicationMenu(
        Menu.buildFromTemplate([
          { role: "appMenu" },
          { role: "editMenu" },
          { role: "viewMenu" },
          { role: "windowMenu" },
        ]),
      );
      core = new CoreSupervisor({
        binary: app.isPackaged
          ? path.join(
              process.resourcesPath,
              "bin",
              process.platform === "win32" ? "ally-core.exe" : "ally-core",
            )
          : path.join(
              __dirname,
              "../binaries",
              process.platform === "win32" ? "ally-core.exe" : "ally-core",
            ),
        dataDir,
        env: {
          ...process.env,
          ALLY_PLUGIN_RESOURCES_DIR: app.isPackaged
            ? path.join(process.resourcesPath, "plugins")
            : path.join(__dirname, "../resources/plugins"),
          ALLY_TERMINAL_RUNTIME_PATH: app.isPackaged
            ? path.join(process.resourcesPath, "terminal-runtime", "worker.cjs")
            : path.join(
                __dirname,
                "../../resources/terminal-runtime/worker.cjs",
              ),
          ALLY_TERMINAL_NODE_PATH: process.execPath,
          ALLY_TERMINAL_ELECTRON_NODE: "1",
          PI_MCP_MODULE_PATH: app.isPackaged
            ? path.join(process.resourcesPath, "pi-bridge", "bridge.mjs")
            : path.join(__dirname, "../../resources/pi-bridge/bridge.mjs"),
        },
        log,
      });
      // 服务启动与窗口加载并行，界面连接复用同一个启动任务。
      disposePowerLifecycle = installAutomaticTaskPowerLifecycle(
        powerMonitor,
        () => {
          if (!core) return Promise.reject(new Error("Core 尚未装配"));
          return core.connection();
        },
        log,
      );
      coreEvents = createCoreEvents(() => {
        if (!core) return Promise.reject(new Error("Core 尚未装配"));
        return core.connection();
      }, log);
      disposeIMPowerLifecycle = installIMPowerLifecycle(
        powerSaveBlocker,
        coreEvents,
      );
      void core.connection().catch((error: unknown) => log(String(error)));
      await lifecycle.start();
    })
    .catch(async (error) => {
      await log(String(error));
      try {
        await updates?.dispose();
        disposeIMPowerLifecycle?.();
        await coreEvents?.close();
        await core?.stop();
      } catch (stopError) {
        await log(`Core 收尾失败：${String(stopError)}`);
      }
      app.exit(1);
    });
}
