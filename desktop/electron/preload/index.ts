import { contextBridge, ipcRenderer, webUtils } from "electron";
import type {
  DesktopBridge,
  DesktopEvents,
  FileDropEvent,
} from "../shared/contracts";

function subscribe<K extends keyof DesktopEvents>(
  name: K,
  handler: (event: DesktopEvents[K]) => void,
): () => void {
  const listener = (
    _event: Electron.IpcRendererEvent,
    payload: DesktopEvents[K],
  ) => handler(payload);
  ipcRenderer.on(`ally:${name}`, listener);
  return () => ipcRenderer.removeListener(`ally:${name}`, listener);
}
const drops = new Set<(event: FileDropEvent) => void>();
for (const [name, type] of [
  ["dragenter", "enter"],
  ["dragover", "over"],
  ["drop", "drop"],
  ["dragleave", "leave"],
] as const) {
  window.addEventListener(
    name,
    (event: DragEvent) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      event.preventDefault();
      if (type === "leave" && event.relatedTarget !== null) return;
      const payload: FileDropEvent =
        type === "leave"
          ? { type }
          : {
              type,
              paths:
                type === "drop"
                  ? Array.from(event.dataTransfer.files)
                      .map((file) => webUtils.getPathForFile(file))
                      .filter(Boolean)
                  : [],
              position: { x: event.clientX, y: event.clientY },
            };
      for (const handler of drops) handler(payload);
    },
    true,
  );
}
const bridge: DesktopBridge = {
  application_update_state: () =>
    ipcRenderer.invoke("ally:application_update_state"),
  check_application_update: () =>
    ipcRenderer.invoke("ally:check_application_update"),
  download_application_update: () =>
    ipcRenderer.invoke("ally:download_application_update"),
  open_application_installer: () =>
    ipcRenderer.invoke("ally:open_application_installer"),
  onApplicationUpdate: (handler) => subscribe("application-update", handler),
  core_connection: (args) => ipcRenderer.invoke("ally:core_connection", args),
  subscribe_core_events: (args) =>
    ipcRenderer.invoke("ally:subscribe_core_events", args),
  resync_core_events: (args) =>
    ipcRenderer.invoke("ally:resync_core_events", args),
  unsubscribe_core_events: (args) =>
    ipcRenderer.invoke("ally:unsubscribe_core_events", args),
  onCoreEvent: (handler) => subscribe("core-event", handler),
  save_file: (args) => ipcRenderer.invoke("ally:save_file", args),
  select_directory: (args) => ipcRenderer.invoke("ally:select_directory", args),
  select_files: (args) => ipcRenderer.invoke("ally:select_files", args),
  stage_attachment: (args) => ipcRenderer.invoke("ally:stage_attachment", args),
  open_workspace_file: (args) =>
    ipcRenderer.invoke("ally:open_workspace_file", args),
  browser_create: (args) => ipcRenderer.invoke("ally:browser_create", args),
  browser_prepare_webview: (args) =>
    ipcRenderer.invoke("ally:browser_prepare_webview", args),
  browser_navigate: (args) => ipcRenderer.invoke("ally:browser_navigate", args),
  browser_control: (args) => ipcRenderer.invoke("ally:browser_control", args),
  browser_set_bounds: (args) =>
    ipcRenderer.invoke("ally:browser_set_bounds", args),
  browser_set_visible: (args) =>
    ipcRenderer.invoke("ally:browser_set_visible", args),
  browser_close: (args) => ipcRenderer.invoke("ally:browser_close", args),
  open_external_url: (args) =>
    ipcRenderer.invoke("ally:open_external_url", args),
  show_notification: (args) =>
    ipcRenderer.invoke("ally:show_notification", args),
  onBrowserNavigation: (handler) => subscribe("browser-navigation", handler),
  onNotificationAction: (handler) => subscribe("notification-action", handler),
  onFileDrop: (handler) => {
    drops.add(handler);
    return () => {
      drops.delete(handler);
    };
  },
};
contextBridge.exposeInMainWorld("allyDesktop", bridge);
