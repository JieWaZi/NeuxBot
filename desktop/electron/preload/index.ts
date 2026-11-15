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
  ipcRenderer.on(`bot:${name}`, listener);
  return () => ipcRenderer.removeListener(`bot:${name}`, listener);
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
  core_connection: (args) => ipcRenderer.invoke("bot:core_connection", args),
  get_window_appearance: (args) =>
    ipcRenderer.invoke("bot:get_window_appearance", args),
  set_window_appearance: (args) =>
    ipcRenderer.invoke("bot:set_window_appearance", args),
  onInterfaceScale: (handler) => subscribe("interface-scale", handler),
  subscribe_core_events: (args) =>
    ipcRenderer.invoke("bot:subscribe_core_events", args),
  resync_core_events: (args) =>
    ipcRenderer.invoke("bot:resync_core_events", args),
  unsubscribe_core_events: (args) =>
    ipcRenderer.invoke("bot:unsubscribe_core_events", args),
  onCoreEvent: (handler) => subscribe("core-event", handler),
  save_file: (args) => ipcRenderer.invoke("bot:save_file", args),
  select_directory: (args) => ipcRenderer.invoke("bot:select_directory", args),
  select_files: (args) => ipcRenderer.invoke("bot:select_files", args),
  stage_attachment: (args) => ipcRenderer.invoke("bot:stage_attachment", args),
  open_workspace_file: (args) =>
    ipcRenderer.invoke("bot:open_workspace_file", args),
  browser_create: (args) => ipcRenderer.invoke("bot:browser_create", args),
  browser_prepare_webview: (args) =>
    ipcRenderer.invoke("bot:browser_prepare_webview", args),
  browser_navigate: (args) => ipcRenderer.invoke("bot:browser_navigate", args),
  browser_control: (args) => ipcRenderer.invoke("bot:browser_control", args),
  browser_set_bounds: (args) =>
    ipcRenderer.invoke("bot:browser_set_bounds", args),
  browser_set_visible: (args) =>
    ipcRenderer.invoke("bot:browser_set_visible", args),
  browser_close: (args) => ipcRenderer.invoke("bot:browser_close", args),
  open_external_url: (args) =>
    ipcRenderer.invoke("bot:open_external_url", args),
  get_notification_preferences: (args) =>
    ipcRenderer.invoke("bot:get_notification_preferences", args),
  set_notification_preferences: (args) =>
    ipcRenderer.invoke("bot:set_notification_preferences", args),
  send_test_notification: () =>
    ipcRenderer.invoke("bot:send_test_notification"),
  open_notification_settings: () =>
    ipcRenderer.invoke("bot:open_notification_settings"),
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
contextBridge.exposeInMainWorld("botDesktop", bridge);
