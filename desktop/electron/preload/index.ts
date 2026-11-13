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
type AttentionOpen = DesktopEvents["workspace-attention-open"];
const attentionHandlers = new Set<(target: AttentionOpen) => void>();
let pendingAttention: AttentionOpen | undefined;
subscribe("workspace-attention-open", (target) => {
  if (!attentionHandlers.size) pendingAttention = target;
  else for (const handler of attentionHandlers) handler(target);
});
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
  get_notification_preferences: (args) =>
    ipcRenderer.invoke("ally:get_notification_preferences", args),
  set_notification_preferences: (args) =>
    ipcRenderer.invoke("ally:set_notification_preferences", args),
  send_test_notification: () =>
    ipcRenderer.invoke("ally:send_test_notification"),
  open_notification_settings: () =>
    ipcRenderer.invoke("ally:open_notification_settings"),
  onBrowserNavigation: (handler) => subscribe("browser-navigation", handler),
  onWorkspaceAttentionOpen: (handler) => {
    attentionHandlers.add(handler);
    if (pendingAttention !== undefined) {
      const target = pendingAttention;
      pendingAttention = undefined;
      handler(target);
    }
    return () => {
      attentionHandlers.delete(handler);
    };
  },
  onFileDrop: (handler) => {
    drops.add(handler);
    return () => {
      drops.delete(handler);
    };
  },
};
contextBridge.exposeInMainWorld("allyDesktop", bridge);
