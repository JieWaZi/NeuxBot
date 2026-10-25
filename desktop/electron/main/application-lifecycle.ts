import path from "node:path";
import {
  Menu,
  Tray,
  nativeImage,
  type App,
  type BrowserWindow,
} from "electron";

type ApplicationLifecycleOptions = {
  readonly app: App;
  readonly createWindow: () => Promise<BrowserWindow>;
  readonly stopCore: () => Promise<void>;
  readonly resourcesPath: string;
  readonly platform?: NodeJS.Platform;
  readonly log?: (message: string) => void | Promise<void>;
};

export function resolveTrayIconPath(
  application: Pick<App, "getAppPath" | "isPackaged">,
  resourcesPath: string,
  platform: NodeJS.Platform,
): string {
  if (application.isPackaged) {
    return path.join(
      resourcesPath,
      "tray",
      platform === "darwin" ? "allyTemplate.png" : "ally.ico",
    );
  }
  return path.join(
    application.getAppPath(),
    "resources",
    platform === "darwin" ? "tray/allyTemplate.png" : "tray/ally.ico",
  );
}

export function createApplicationLifecycle({
  app,
  createWindow,
  stopCore,
  resourcesPath,
  platform = process.platform,
  log = () => undefined,
}: ApplicationLifecycleOptions) {
  let window: BrowserWindow | undefined;
  let tray: Tray | undefined;
  let started = false;
  let creatingWindow: Promise<BrowserWindow> | undefined;
  let coreStop: Promise<void> | undefined;
  let finishingQuit = false;
  let allowQuit = false;
  let sessionEnding = false;

  const stopCoreOnce = () => {
    coreStop ??= stopCore().catch(async (error: unknown) => {
      await log(`Core 收尾失败：${String(error)}`);
    });
    return coreStop;
  };

  const attachWindow = (current: BrowserWindow) => {
    window = current;
    current.on("close", (event) => {
      if (allowQuit || sessionEnding) return;
      event.preventDefault();
      current.hide();
    });
    current.once("closed", () => {
      if (window === current) window = undefined;
    });
    if (platform === "win32") {
      current.on("query-session-end", () => {
        sessionEnding = true;
        void stopCoreOnce();
      });
      current.on("session-end", () => {
        sessionEnding = true;
        tray?.destroy();
        tray = undefined;
        void stopCoreOnce();
      });
    }
  };

  const showWindow = async () => {
    if (!started) return;
    if (window?.isDestroyed()) window = undefined;
    if (!window) {
      creatingWindow ??= createWindow()
        .then((current) => {
          attachWindow(current);
          return current;
        })
        .finally(() => {
          creatingWindow = undefined;
        });
      await creatingWindow;
    }
    if (!window) return;
    if (platform === "darwin") app.show();
    if (window.isMinimized()) window.restore();
    window.show();
    window.focus();
  };

  const restoreWindow = () => {
    void showWindow().catch(async (error: unknown) => {
      await log(`窗口恢复失败：${String(error)}`);
    });
  };

  const requestQuit = () => app.quit();

  app.on("second-instance", restoreWindow);
  app.on("activate", restoreWindow);
  // 注册监听即覆盖 Electron 在 Windows 最后一个窗口关闭时的默认退出行为。
  app.on("window-all-closed", () => undefined);
  app.on("before-quit", (event) => {
    if (allowQuit) return;
    event.preventDefault();
    if (finishingQuit) return;
    finishingQuit = true;
    void stopCoreOnce().finally(() => {
      allowQuit = true;
      tray?.destroy();
      tray = undefined;
      app.quit();
    });
  });

  const start = async () => {
    if (started) return showWindow();
    started = true;
    const icon = nativeImage.createFromPath(
      resolveTrayIconPath(app, resourcesPath, platform),
    );
    if (icon.isEmpty()) throw new Error("托盘图标加载失败");
    if (platform === "darwin") icon.setTemplateImage(true);
    tray = new Tray(icon);
    tray.setToolTip("NeuxBot");
    const menu = Menu.buildFromTemplate([
      { label: "打开 NeuxBot", click: restoreWindow },
      { type: "separator" },
      { label: "退出 NeuxBot", click: requestQuit },
    ]);
    tray.setContextMenu(menu);
    tray.on("click", restoreWindow);
    if (platform === "darwin" && app.dock) {
      app.dock.setMenu(
        Menu.buildFromTemplate([
          { label: "打开 NeuxBot", click: restoreWindow },
        ]),
      );
    }
    await showWindow();
  };

  return { requestQuit, showWindow, start };
}
