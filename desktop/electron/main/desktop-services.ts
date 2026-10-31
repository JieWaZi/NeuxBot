import {
  dialog,
  ipcMain,
  Notification,
  shell,
  type BrowserWindow,
} from "electron";
import { writeFile as save } from "node:fs/promises";
import path from "node:path";
import type { DesktopCommands, DesktopEvents } from "../shared/contracts";
import { commandSchemas } from "../shared/validation";
import type { CoreSupervisor } from "./core-supervisor";
import { BrowserManager } from "./browser-manager";
import { BrowserWebviewPolicy } from "./browser-webview-policy";
import { authorizeBrowserRoot } from "./browser-workspace-root";
import * as files from "./workspace-files";
import { createAttachmentStaging } from "./attachment-staging";
import type { CoreEventClient } from "../shared/core-event-client";
import type { ApplicationUpdate } from "./application-update";

export function installDesktopServices(
  window: BrowserWindow,
  core: CoreSupervisor,
  trustedURL: string,
  events?: CoreEventClient,
  updates?: ApplicationUpdate,
) {
  // BrowserWindow 的 closed 事件触发时再读取 window.webContents 会抛出
  // “Object has been destroyed”，因此安装阶段固定持有本次窗口的内容对象。
  const contents = window.webContents;
  const emit = <K extends keyof DesktopEvents>(
    name: K,
    payload: DesktopEvents[K],
  ) => {
    if (!window.isDestroyed() && !contents.isDestroyed())
      contents.send(`ally:${name}`, payload);
  };
  let browser = new BrowserManager(window, (event) =>
    emit("browser-navigation", event),
  );
  const browserWebviews = new BrowserWebviewPolicy(window);
  const notifications = new Map<string, Notification>();
  const eventSubscriptions = new Map<
    string,
    { stop: () => void; resync: (after?: number | string) => void }
  >();
  let attachments = createAttachmentStaging();
  const handlers: {
    [K in keyof DesktopCommands]: (
      args: DesktopCommands[K]["args"],
    ) => DesktopCommands[K]["result"] | Promise<DesktopCommands[K]["result"]>;
  } = {
    application_update_state: () => {
      if (!updates) throw new Error("软件更新服务尚未装配");
      return updates.snapshot();
    },
    check_application_update: () => {
      if (!updates) throw new Error("软件更新服务尚未装配");
      return updates.check();
    },
    download_application_update: () => {
      if (!updates) throw new Error("软件更新服务尚未装配");
      return updates.download();
    },
    open_application_installer: () => {
      if (!updates) throw new Error("软件更新服务尚未装配");
      return updates.openInstaller();
    },
    core_connection: () => core.connection(),
    subscribe_core_events: ({ subscriptionId, topic, resource, after }) => {
      if (!events) throw new Error("全局事件服务尚未装配");
      if (eventSubscriptions.has(subscriptionId)) return true;
      if (eventSubscriptions.size >= 128) throw new Error("事件订阅超过上限");
      const stop = events.subscribe(topic, resource, {
        after,
        onEvent: (frame) => {
          emit("core-event", { subscriptionId, kind: "event", frame });
        },
        onState: (state, connectionChanged) =>
          emit("core-event", {
            subscriptionId,
            kind: "state",
            state,
            connectionChanged,
          }),
        onResync: (reason) =>
          emit("core-event", { subscriptionId, kind: "resync", reason }),
      });
      eventSubscriptions.set(subscriptionId, {
        stop,
        resync: (after) => events.requestResync(topic, resource, after),
      });
      return true;
    },
    resync_core_events: ({ subscriptionId, after }) => {
      const subscription = eventSubscriptions.get(subscriptionId);
      subscription?.resync(after);
      return Boolean(subscription);
    },
    unsubscribe_core_events: ({ subscriptionId }) => {
      eventSubscriptions.get(subscriptionId)?.stop();
      return eventSubscriptions.delete(subscriptionId);
    },
    save_file: async ({ bytes, fileName }) => {
      const result = await dialog.showSaveDialog(window, {
        defaultPath: path.basename(fileName),
      });
      if (result.canceled || !result.filePath) return false;
      await save(result.filePath, Uint8Array.from(bytes));
      return true;
    },
    select_directory: async () => {
      const result = await dialog.showOpenDialog(window, {
        properties: ["openDirectory"],
      });
      return result.canceled ? null : (result.filePaths[0] ?? null);
    },
    select_files: async () => {
      const result = await dialog.showOpenDialog(window, {
        properties: ["openFile", "multiSelections"],
      });
      return result.canceled ? [] : result.filePaths;
    },
    stage_attachment: ({ name, bytes }) => attachments.stage(name, bytes),
    open_workspace_file: async ({ root, relativePath }) => {
      const error = await shell.openPath(
        await files.workspacePath(root, relativePath),
      );
      if (error) throw new Error(error);
      return true;
    },
    browser_create: async (input) => {
      if (!input.localRoot) return browser.create(input);
      if (new URL(input.url).protocol !== "file:" && !input.chatId)
        return browser.create({ ...input, localRoot: undefined });
      if (!input.chatId) throw new Error("本地浏览器预览缺少 Chat 身份");
      const localRoot = await authorizeBrowserRoot(
        core,
        input.chatId,
        input.localRoot,
      );
      return browser.create({ ...input, localRoot });
    },
    browser_prepare_webview: async ({ url, localRoot, chatId }) => {
      if (!localRoot || !chatId) return browserWebviews.prepare(url);
      const root = await authorizeBrowserRoot(core, chatId, localRoot);
      return browserWebviews.prepare(url, root);
    },
    browser_navigate: ({ viewId, url }) => browser.navigate(viewId, url),
    browser_control: ({ viewId, action }) => browser.control(viewId, action),
    browser_set_bounds: ({ viewId, x, y, width, height }) =>
      browser.bounds(viewId, { x, y, width, height }),
    browser_set_visible: ({ viewId, visible }) =>
      browser.visible(viewId, visible),
    browser_close: ({ viewId }) => browser.close(viewId),
    open_external_url: async ({ url }) => {
      const parsed = new URL(url);
      if (!["http:", "https:"].includes(parsed.protocol))
        throw new Error("外链仅支持 HTTP 或 HTTPS");
      await shell.openExternal(parsed.href);
    },
    show_notification: ({ id, title, body }) => {
      if (!Notification.isSupported()) return false;
      notifications.get(id)?.close();
      const notification = new Notification({ title, body });
      notifications.set(id, notification);
      const finish = (action: "click" | "close") => {
        if (notifications.get(id) !== notification) return;
        notifications.delete(id);
        emit("notification-action", { id, action });
      };
      notification.on("click", () => {
        if (!window.isDestroyed()) {
          window.show();
          window.focus();
        }
        finish("click");
      });
      notification.on("close", () => finish("close"));
      notification.on("failed", () => finish("close"));
      notification.show();
      return true;
    },
  };
  const trusted = (url: string) =>
    url.split("#")[0] === trustedURL.split("#")[0];
  function register<K extends keyof DesktopCommands>(name: K) {
    ipcMain.handle(`ally:${name}`, (event, args: unknown) => {
      if (
        event.sender !== contents ||
        event.senderFrame !== contents.mainFrame ||
        !trusted(event.senderFrame.url)
      )
        throw new Error("桌面调用来源无效");
      // 共享 Schema 校验外部输入后才进入与同名契约绑定的处理函数。
      const input = commandSchemas[name].parse(args);
      return handlers[name](input);
    });
  }
  for (const name of Object.keys(commandSchemas) as (keyof DesktopCommands)[])
    register(name);
  const stopUpdates = updates?.subscribe((state) =>
    emit("application-update", state),
  );
  const releaseViews = () => {
    for (const subscription of eventSubscriptions.values()) subscription.stop();
    eventSubscriptions.clear();
    void attachments.dispose();
    browser.dispose();
    browserWebviews.reset();
    for (const notification of notifications.values()) notification.close();
    notifications.clear();
  };
  const handleRenderProcessGone = () => releaseViews();
  const handleNavigation = (
    _event: unknown,
    _url: string,
    isInPlace: boolean,
    isMainFrame: boolean,
  ) => {
    if (isMainFrame && !isInPlace) {
      releaseViews();
      attachments = createAttachmentStaging();
      browser = new BrowserManager(window, (event) =>
        emit("browser-navigation", event),
      );
    }
  };
  contents.on("render-process-gone", handleRenderProcessGone);
  contents.on("did-start-navigation", handleNavigation);
  let disposed = false;
  return () => {
    if (disposed) return;
    disposed = true;
    stopUpdates?.();
    if (!window.isDestroyed() && !contents.isDestroyed()) {
      contents.off("render-process-gone", handleRenderProcessGone);
      contents.off("did-start-navigation", handleNavigation);
    }
    releaseViews();
    browserWebviews.dispose();
    for (const name of Object.keys(commandSchemas))
      ipcMain.removeHandler(`ally:${name}`);
  };
}
