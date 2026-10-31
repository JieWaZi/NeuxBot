import { configurePlatform, type Platform } from "@neuxbot/frontend/platform";
import {
  configureEmbeddedBrowserSurface,
  configureBrowserSurface,
} from "@neuxbot/frontend/browser-surface";
import { createCoreWorkspaceServices } from "@neuxbot/frontend/core-workspace";
import { createCoreTerminalService } from "@neuxbot/frontend/core-terminal";
import type { DesktopBridge } from "../../electron/shared/contracts";
import { useEmbeddedBrowserSurface } from "./use-embedded-browser-surface";
import { useBrowserSurface } from "./use-browser-surface";

declare global {
  interface Window {
    allyDesktop?: DesktopBridge;
  }
}

/** 将 preload 的白名单按能力装配，公共应用只持有各自的窄服务。 */
export function createDesktopPlatform(bridge: DesktopBridge): Platform {
  const adapter: Platform = {
    kind: "desktop",
    chrome: { navigationInset: 72 },
    async getConnection() {
      const connection = await bridge.core_connection();
      const url = new URL(connection.baseUrl);
      if (
        url.protocol !== "http:" ||
        url.hostname !== "127.0.0.1" ||
        !connection.token
      ) {
        throw new Error("本地服务连接无效");
      }
      return {
        baseUrl: connection.baseUrl.replace(/\/$/, ""),
        token: connection.token,
      };
    },
    applicationUpdate: {
      application_update_state: bridge.application_update_state,
      check_application_update: bridge.check_application_update,
      download_application_update: bridge.download_application_update,
      open_application_installer: bridge.open_application_installer,
      onApplicationUpdate: bridge.onApplicationUpdate,
    },
    host: {
      save_file: bridge.save_file,
      select_directory: bridge.select_directory,
      select_files: bridge.select_files,
      stage_attachment: bridge.stage_attachment,
      open_external_url: bridge.open_external_url,
      show_notification: bridge.show_notification,
      onFileDrop: bridge.onFileDrop,
      onNotificationAction: bridge.onNotificationAction,
    },
    browser: {
      browser_create: bridge.browser_create,
      browser_prepare_webview: bridge.browser_prepare_webview,
      browser_navigate: bridge.browser_navigate,
      browser_control: bridge.browser_control,
      browser_set_bounds: bridge.browser_set_bounds,
      browser_set_visible: bridge.browser_set_visible,
      browser_close: bridge.browser_close,
      onBrowserNavigation: bridge.onBrowserNavigation,
    },
    events: {
      subscribe_core_events: bridge.subscribe_core_events,
      resync_core_events: bridge.resync_core_events,
      unsubscribe_core_events: bridge.unsubscribe_core_events,
      onCoreEvent: bridge.onCoreEvent,
    },
  };
  Object.assign(
    adapter,
    createCoreWorkspaceServices(adapter.host, {
      openFile: (root, relativePath) =>
        bridge.open_workspace_file({ root, relativePath }),
    }),
  );
  adapter.terminal = createCoreTerminalService(adapter.getConnection);
  return adapter;
}

export function configureDesktopPlatform(): void {
  if (!window.allyDesktop) throw new Error("桌面桥接不可用");
  configurePlatform(createDesktopPlatform(window.allyDesktop));
  configureEmbeddedBrowserSurface(useEmbeddedBrowserSurface);
  configureBrowserSurface(useBrowserSurface);
}
