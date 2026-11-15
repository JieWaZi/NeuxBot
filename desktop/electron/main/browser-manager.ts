import type { BrowserWindow } from "electron";
import { WebContentsView, session, net } from "electron";
import { createHash, randomUUID } from "node:crypto";
import { realpathSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { canonicalDirectory, within } from "./workspace-files";
import type {
  BrowserBounds,
  BrowserControlAction,
  BrowserNavigationEvent,
} from "../shared/contracts";

export function browserURL(value: string, localRoot?: string): string {
  const url = new URL(value);
  if (
    url.protocol === "http:" ||
    url.protocol === "https:" ||
    url.href === "about:blank"
  )
    return url.href;
  if (url.protocol === "file:" && localRoot) {
    const target = realpathSync(fileURLToPath(url));
    if (within(localRoot, target) && /\.(html?|xhtml)$/i.test(target))
      return url.href;
  }
  throw new Error("浏览器地址不在允许范围内");
}
export function browserBounds(bounds: BrowserBounds): BrowserBounds {
  if (
    !Object.values(bounds).every(Number.isFinite) ||
    bounds.width <= 0 ||
    bounds.height <= 0
  )
    throw new Error("浏览器视图尺寸无效");
  return {
    x: Math.round(bounds.x),
    y: Math.round(bounds.y),
    width: Math.max(1, Math.round(bounds.width)),
    height: Math.max(1, Math.round(bounds.height)),
  };
}
export class BrowserManager {
  private readonly views = new Map<
    string,
    {
      view: WebContentsView;
      localRoot?: string;
      partition: string;
      attached: boolean;
      localPreview: boolean;
    }
  >();
  private readonly partitions = new Map<
    string,
    { count: number; removeDownload: () => void }
  >();
  private disposed = false;
  constructor(
    private readonly window: BrowserWindow,
    private readonly emit: (event: BrowserNavigationEvent) => void,
  ) {}
  async create(
    input: BrowserBounds & { url: string; localRoot?: string },
  ): Promise<string> {
    const localRoot = input.localRoot
      ? await canonicalDirectory(input.localRoot)
      : undefined;
    const url = browserURL(input.url, localRoot);
    const localPreview = new URL(url).protocol === "file:";
    const bounds = browserBounds({
      x: input.x,
      y: input.y,
      width: input.width,
      height: input.height,
    });
    if (this.disposed || this.window.isDestroyed())
      throw new Error("浏览器管理器已关闭");
    const id = randomUUID();
    const partition = localRoot
      ? `bot-browser-${this.window.webContents.id}-project-${createHash("sha256").update(localRoot).digest("hex")}-${localPreview ? "local" : "web"}`
      : `bot-browser-${id}`;
    const isolated = session.fromPartition(partition);
    if (!this.partitions.has(partition)) {
      isolated.setPermissionRequestHandler((_contents, _permission, callback) =>
        callback(false),
      );
      isolated.setPermissionCheckHandler(() => false);
      // 同一项目的 guest 共用分区；文件读取仍逐次校验项目真实路径。
      isolated.protocol.handle("file", async (request) => {
        try {
          if (!localRoot || !localPreview)
            return new Response("Forbidden", { status: 403 });
          const target = realpathSync(fileURLToPath(request.url));
          if (!within(localRoot, target))
            return new Response("Forbidden", { status: 403 });
          return await net.fetch(pathToFileURL(target).href, {
            bypassCustomProtocolHandlers: true,
          });
        } catch {
          return new Response("Not found", { status: 404 });
        }
      });
      const preventDownload = (event: Electron.Event) => event.preventDefault();
      isolated.on("will-download", preventDownload);
      this.partitions.set(partition, {
        count: 0,
        removeDownload: () => isolated.off("will-download", preventDownload),
      });
    }
    const allocated = this.partitions.get(partition);
    if (!allocated) throw new Error("浏览器分区未初始化");
    allocated.count += 1;
    let view: WebContentsView;
    try {
      view = new WebContentsView({
        webPreferences: {
          session: isolated,
          nodeIntegration: false,
          contextIsolation: true,
          sandbox: true,
          webSecurity: true,
        },
      });
    } catch (cause) {
      this.releasePartition(partition);
      throw cause;
    }
    const allowed = (target: string) => {
      try {
        const candidate = new URL(target);
        if (
          candidate.href !== "about:blank" &&
          (candidate.protocol === "file:") !== localPreview
        )
          return false;
        browserURL(target, localRoot);
        return true;
      } catch {
        return false;
      }
    };
    view.webContents.on("will-navigate", (event, target) => {
      if (!allowed(target)) event.preventDefault();
    });
    view.webContents.on("will-frame-navigate", (event) => {
      if (!allowed(event.url)) event.preventDefault();
    });
    view.webContents.on("will-redirect", (event, target) => {
      if (!allowed(target)) event.preventDefault();
    });
    const publish = (target: string) => {
      if (!allowed(target)) return;
      const contents = view.webContents;
      const navigation = contents.navigationHistory;
      this.emit({
        viewId: id,
        url: target,
        title: contents.getTitle(),
        canGoBack: navigation.canGoBack(),
        canGoForward: navigation.canGoForward(),
      });
    };
    view.webContents.on("did-navigate", (_event, target) => publish(target));
    view.webContents.on("did-navigate-in-page", (_event, target, isMain) => {
      if (isMain) publish(target);
    });
    view.webContents.on("page-title-updated", () =>
      publish(view.webContents.getURL()),
    );
    view.webContents.setWindowOpenHandler(({ url: target }) => {
      if (allowed(target)) {
        queueMicrotask(() => {
          if (!view.webContents.isDestroyed())
            void view.webContents.loadURL(target).catch(() => undefined);
        });
      }
      return { action: "deny" };
    });
    const entry = { view, localRoot, partition, attached: false, localPreview };
    this.views.set(id, entry);
    try {
      view.setVisible(false);
      this.window.contentView.addChildView(view);
      entry.attached = true;
      view.setBounds(bounds);
      await view.webContents.loadURL(url);
      return id;
    } catch (error) {
      this.close(id);
      throw error;
    }
  }
  private require(id: string) {
    const entry = this.views.get(id);
    if (!entry || entry.view.webContents.isDestroyed())
      throw new Error("浏览器视图不存在");
    return entry;
  }
  async navigate(id: string, url: string) {
    const { view, localRoot, localPreview } = this.require(id);
    const target = browserURL(url, localRoot);
    if (
      target !== "about:blank" &&
      (new URL(target).protocol === "file:") !== localPreview
    )
      throw new Error("浏览器不能跨越本地预览与网页隔离边界");
    await view.webContents.loadURL(target);
  }
  control(id: string, action: BrowserControlAction) {
    const contents = this.require(id).view.webContents;
    const navigation = contents.navigationHistory;
    if (action === "reload") contents.reload();
    else if (action === "back" && navigation.canGoBack()) navigation.goBack();
    else if (action === "forward" && navigation.canGoForward())
      navigation.goForward();
  }
  bounds(id: string, bounds: BrowserBounds) {
    this.require(id).view.setBounds(browserBounds(bounds));
  }
  visible(id: string, visible: boolean) {
    const { view } = this.require(id);
    view.setVisible(visible);
    if (visible) {
      const contents = view.webContents;
      const target = contents.getURL();
      const navigation = contents.navigationHistory;
      this.emit({
        viewId: id,
        url: target,
        title: contents.getTitle(),
        canGoBack: navigation.canGoBack(),
        canGoForward: navigation.canGoForward(),
      });
    }
  }
  close(id: string) {
    const entry = this.views.get(id);
    if (!entry) return false;
    this.views.delete(id);
    try {
      if (entry.attached && !this.window.isDestroyed())
        this.window.contentView.removeChildView(entry.view);
    } finally {
      try {
        if (!entry.view.webContents.isDestroyed())
          entry.view.webContents.close();
      } finally {
        this.releasePartition(entry.partition);
      }
    }
    return true;
  }
  private releasePartition(partition: string) {
    const allocated = this.partitions.get(partition);
    if (!allocated) return;
    if (allocated.count > 1) {
      allocated.count -= 1;
      return;
    }
    this.partitions.delete(partition);
    allocated.removeDownload();
    session.fromPartition(partition).protocol.unhandle("file");
  }
  dispose() {
    this.disposed = true;
    for (const id of this.views.keys()) this.close(id);
  }
}
