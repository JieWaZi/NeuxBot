import { createHash } from "node:crypto";
import {
  session,
  type BrowserWindow,
  type Session,
  type WebContents,
} from "electron";
import { browserURL } from "./browser-manager";

type GuestPartition = {
  session: Session;
  preventDownload: (event: Electron.Event) => void;
};

/** 浏览器标签的 DOM 访客视图仍由主进程限定来源、权限和会话范围。 */
export class BrowserWebviewPolicy {
  private readonly partitions = new Map<string, GuestPartition>();
  private readonly contents: WebContents;

  constructor(window: BrowserWindow) {
    this.contents = window.webContents;
    this.contents.on("will-attach-webview", this.willAttach);
    this.contents.on("did-attach-webview", this.didAttach);
  }

  prepare(url: string, root?: string): string {
    browserURL(url);
    const partition = root
      ? `bot-webview-${this.contents.id}-project-${createHash("sha256").update(root).digest("hex")}`
      : `bot-webview-${this.contents.id}-temporary`;
    if (this.partitions.has(partition)) return partition;
    const isolated = session.fromPartition(partition);
    isolated.setPermissionRequestHandler((_contents, _permission, callback) =>
      callback(false),
    );
    isolated.setPermissionCheckHandler(() => false);
    isolated.protocol.handle("file", () =>
      Promise.resolve(new Response("Forbidden", { status: 403 })),
    );
    const preventDownload = (event: Electron.Event) => event.preventDefault();
    isolated.on("will-download", preventDownload);
    this.partitions.set(partition, { session: isolated, preventDownload });
    return partition;
  }

  private readonly willAttach = (
    event: Electron.Event,
    preferences: Electron.WebPreferences,
    params: Record<string, string>,
  ) => {
    const partition = this.partitions.get(params.partition ?? "");
    try {
      browserURL(params.src ?? "");
      if (!partition) throw new Error("访客会话未授权");
    } catch {
      event.preventDefault();
      return;
    }
    delete preferences.preload;
    preferences.session = partition.session;
    preferences.nodeIntegration = false;
    preferences.nodeIntegrationInSubFrames = false;
    preferences.nodeIntegrationInWorker = false;
    preferences.contextIsolation = true;
    preferences.sandbox = true;
    preferences.webSecurity = true;
    preferences.webviewTag = false;
    preferences.plugins = false;
    preferences.allowRunningInsecureContent = false;
  };

  private readonly didAttach = (_event: Electron.Event, guest: WebContents) => {
    const allowed = (value: string) => {
      try {
        browserURL(value);
        return true;
      } catch {
        return false;
      }
    };
    guest.on("will-navigate", (event, target) => {
      if (!allowed(target)) event.preventDefault();
    });
    guest.on("will-frame-navigate", (event) => {
      if (!allowed(event.url)) event.preventDefault();
    });
    guest.on("will-redirect", (event, target) => {
      if (!allowed(target)) event.preventDefault();
    });
    guest.setWindowOpenHandler(({ url }) => {
      if (allowed(url)) {
        queueMicrotask(() => {
          if (!guest.isDestroyed())
            void guest.loadURL(url).catch(() => undefined);
        });
      }
      return { action: "deny" };
    });
  };

  reset() {
    for (const {
      session: isolated,
      preventDownload,
    } of this.partitions.values()) {
      isolated.off("will-download", preventDownload);
      isolated.protocol.unhandle("file");
      isolated.setPermissionRequestHandler(null);
      isolated.setPermissionCheckHandler(null);
    }
    this.partitions.clear();
  }

  dispose() {
    this.contents.off("will-attach-webview", this.willAttach);
    this.contents.off("did-attach-webview", this.didAttach);
    this.reset();
  }
}
