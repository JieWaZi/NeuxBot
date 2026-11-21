import {
  app,
  BrowserWindow,
  protocol,
  net,
  Menu,
  dialog,
  powerMonitor,
  powerSaveBlocker,
} from "electron";
import { execFile } from "node:child_process";
import path from "node:path";
import { mkdir } from "node:fs/promises";
import { CoreSupervisor } from "./core-supervisor";
import { installDesktopServices } from "./desktop-services";
import {
  windowLanguage,
  initializeWindowAppearance,
  loadWindowAppearance,
  windowAppearanceOptions,
} from "./window-appearance";
import { configureApplicationIcon } from "./application-icon";
import { createApplicationLifecycle } from "./application-lifecycle";
import { rendererResponse } from "./renderer-protocol";
import { installIMPowerLifecycle } from "./im-power-lifecycle";
import { createCoreEvents } from "./core-events";
import type { CoreEventClient } from "../shared/core-event-client";
import { installAutomaticTaskPowerLifecycle } from "./automatic-task-power-lifecycle";
import { loadNotificationPreferences } from "./notification-preferences";
import { createDesktopLogs } from "./storage-maintenance";
import { createDesktopNotification } from "./desktop-notifications";
import {
  installWorkspaceAttention,
  resolveWorkspaceAttentionTarget,
} from "./workspace-attention";

protocol.registerSchemesAsPrivileged([
  {
    scheme: "bot",
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
  process.env.NEUXBOT_DATA_DIR ||
  path.join(app.getPath("appData"), "com.neuxbot.desktop");
app.setPath("userData", path.join(dataDir, "electron"));
const desktopLogs = createDesktopLogs(path.join(dataDir, "logs"));
const isDevelopment =
  !app.isPackaged && process.env.NEUXBOT_RENDERER_URL !== undefined;
let core: CoreSupervisor | undefined;
let coreEvents: CoreEventClient | undefined;
let disposePowerLifecycle: (() => void) | undefined;
let disposeIMPowerLifecycle: (() => void) | undefined;
let disposeWorkspaceAttention: (() => void) | undefined;
let notificationPreferences:
  Awaited<ReturnType<typeof loadNotificationPreferences>> | undefined;
let windowAppearance:
  Awaited<ReturnType<typeof loadWindowAppearance>> | undefined;
async function log(message: string) {
  if (isDevelopment) {
    process.stderr.write(message.endsWith("\n") ? message : `${message}\n`);
  }
  const line = message.endsWith("\n") ? message : `${message}\n`;
  try {
    await desktopLogs.append(`${new Date().toISOString()} ${line}`);
  } catch {
    // 日志失败不能阻止桌面生命周期继续收尾。
  }
}
async function createWindow(): Promise<BrowserWindow> {
  const development =
    !app.isPackaged && process.env.NEUXBOT_RENDERER_URL !== undefined;
  const rendererURL = new URL(
    development
      ? (process.env.NEUXBOT_RENDERER_URL ?? "")
      : "bot://app/index.html",
  ).href;
  if (development && new URL(rendererURL).origin !== "http://127.0.0.1:1420")
    throw new Error("开发页面地址无效");
  if (!windowAppearance)
    throw new Error("Window appearance is not initialized");
  const transparent = windowAppearance.get().transparent;
  const current = new BrowserWindow({
    ...windowAppearanceOptions(transparent),
    title: "NeuxBot",
    width: 1280,
    height: 820,
    minWidth: 900,
    minHeight: 600,
    show: false,
    titleBarStyle: "hidden",
    // 原生窗口几何：38px 区域顶行对应 traffic lights x12/y13。
    trafficLightPosition: { x: 12, y: 13 },
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      webviewTag: true,
    },
  });
  initializeWindowAppearance(current, transparent);
  if (!core) throw new Error("Core 尚未装配");
  const dispose = installDesktopServices(
    current,
    core,
    rendererURL,
    coreEvents,
    notificationPreferences,
    windowAppearance,
    desktopLogs,
  );
  current.webContents.on("will-prevent-unload", (event) => {
    const en = windowLanguage(current) === "en-US";
    const answer = dialog.showMessageBoxSync(current, {
      type: "warning",
      buttons: en
        ? ["Keep editing", "Discard draft and close"]
        : ["继续编辑", "放弃草稿并关闭"],
      defaultId: 0,
      cancelId: 0,
      message: en
        ? "The workbench has unsaved file changes"
        : "右侧工作台有未保存的文件草稿",
      detail: en
        ? "Closing or reloading will discard these changes."
        : "关闭窗口或刷新页面会丢失这些修改。",
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
      disposePowerLifecycle?.();
      disposeIMPowerLifecycle?.();
      disposeWorkspaceAttention?.();
      await coreEvents?.close();
      await core?.stop();
    },
    log,
  });
  void app
    .whenReady()
    .then(async () => {
      configureApplicationIcon(app);
      if (app.isPackaged && process.env.NEUXBOT_SKIP_BOTCLI_INSTALL !== "1") {
        const executable = path.join(
          process.resourcesPath,
          "bin",
          process.platform === "win32" ? "botcli.exe" : "botcli",
        );
        execFile(
          executable,
          ["install"],
          { timeout: 10000, windowsHide: true },
          (error, _stdout, stderr) => {
            if (error)
              void log(`botcli 安装失败：${stderr.trim() || error.message}`);
          },
        );
      }
      const dist = path.join(__dirname, "../dist");
      protocol.handle("bot", async (request) => {
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
              process.platform === "win32" ? "bot-core.exe" : "bot-core",
            )
          : path.join(
              __dirname,
              "../binaries",
              process.platform === "win32" ? "bot-core.exe" : "bot-core",
            ),
        dataDir,
        env: {
          ...process.env,
          NEUXBOT_TERMINAL_RUNTIME_PATH: app.isPackaged
            ? path.join(process.resourcesPath, "terminal-runtime", "worker.cjs")
            : path.join(
                __dirname,
                "../../resources/terminal-runtime/worker.cjs",
              ),
          NEUXBOT_TERMINAL_NODE_PATH: process.execPath,
          NEUXBOT_TERMINAL_ELECTRON_NODE: "1",
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
      const coreConnection = () => {
        if (!core) return Promise.reject(new Error("Core 尚未装配"));
        return core.connection();
      };
      coreEvents = createCoreEvents(coreConnection, log);
      const preferencesPath = app.getPath("userData");
      await mkdir(preferencesPath, { recursive: true });
      windowAppearance = await loadWindowAppearance(
        path.join(preferencesPath, "window-appearance.json"),
      );
      const preferences = await loadNotificationPreferences(
        path.join(preferencesPath, "notification-preferences.json"),
      );
      notificationPreferences = preferences;
      disposeWorkspaceAttention = installWorkspaceAttention({
        events: coreEvents,
        preferences: preferences.get,
        language: () => {
          const window = BrowserWindow.getAllWindows()[0];
          return (window && windowLanguage(window)) ?? app.getLocale();
        },
        isFocused: () =>
          BrowserWindow.getAllWindows().some((window) => window.isFocused()),
        resolveTarget: resolveWorkspaceAttentionTarget(coreConnection),
        showNotification: createDesktopNotification,
        async open(takeTarget) {
          const window = await lifecycle.showWindow();
          if (!window || window.isDestroyed()) {
            takeTarget();
            return;
          }
          const contents = window.webContents;
          const send = () => {
            const target = takeTarget();
            if (target !== undefined && !contents.isDestroyed())
              contents.send("bot:workspace-attention-open", target);
          };
          if (contents.isLoadingMainFrame())
            contents.once("did-finish-load", send);
          else send();
        },
      });
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
        disposeIMPowerLifecycle?.();
        disposeWorkspaceAttention?.();
        await coreEvents?.close();
        await core?.stop();
      } catch (stopError) {
        await log(`Core 收尾失败：${String(stopError)}`);
      }
      app.exit(1);
    });
}
