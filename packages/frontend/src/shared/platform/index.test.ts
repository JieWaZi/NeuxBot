import { configurePlatform, type Platform } from "./provider";

function installServices(methods: Record<string, unknown>) {
  configurePlatform({
    kind: "desktop",
    getConnection: async () => ({
      baseUrl: "http://127.0.0.1:8080",
      token: "test",
    }),
    host: methods,
    workspace: methods,
    git: methods,
    browser: methods,
    terminal: methods,
  } as unknown as Platform);
}

import { afterEach, describe, expect, it, vi } from "vitest";
import {
  controlBrowserView,
  createBrowserView,
  getCoreConnection,
  importDroppedWorkspaceFiles,
  normalizeBrowserURL,
  observeWorkspaceResource,
  readWorkspaceResource,
  sanitizeDownloadFileName,
  showDesktopNotification,
  stageAttachment,
} from "./index";
import type {
  WorkspaceResourceChangeEvent,
  WorkspaceResourceRead,
} from "@neuxbot/platform-contracts/resources";
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("桌面桥接", () => {
  it("剪贴板附件只传输文件名和原始字节", async () => {
    const stage_attachment = vi.fn(
      async () => "/tmp/ally-attachments/image.png",
    );
    installServices({ stage_attachment });
    await expect(
      stageAttachment(
        new File([new Uint8Array([0, 255])], "image.png", {
          type: "image/png",
        }),
      ),
    ).resolves.toBe("/tmp/ally-attachments/image.png");
    expect(stage_attachment).toHaveBeenCalledWith({
      name: "image.png",
      bytes: new Uint8Array([0, 255]),
    });
  });
  it("不把内置浏览器命令转成任意脚本", async () => {
    const browser_control = vi.fn();
    installServices({ browser_control });
    await controlBrowserView("view", "back");
    expect(browser_control).toHaveBeenCalledWith({
      viewId: "view",
      action: "back",
    });
  });
  it("本地 HTML 预览传递授权根目录", async () => {
    const browser_create = vi.fn(async () => "view");
    installServices({ browser_create });
    await createBrowserView(
      "file:///work/a.html",
      { x: 0, y: 0, width: 800, height: 600 },
      "/work",
      "chat-id",
    );
    expect(browser_create).toHaveBeenCalledWith({
      url: "file:///work/a.html",
      x: 0,
      y: 0,
      width: 800,
      height: 600,
      localRoot: "/work",
      chatId: "chat-id",
    });
  });
  it("连接由注入的平台提供", async () => {
    installServices({});
    await expect(getCoreConnection()).resolves.toEqual({
      baseUrl: "http://127.0.0.1:8080",
      token: "test",
    });
  });
  it("通知点击返回对应回调并释放监听", async () => {
    let action:
      ((event: { id: string; action: "click" | "close" }) => void) | undefined;
    const stop = vi.fn();
    const onClick = vi.fn();
    const show_notification = vi.fn(async () => true);
    installServices({
      show_notification,
      onNotificationAction: (handler: typeof action) => {
        action = handler;
        return stop;
      },
    });
    await showDesktopNotification({
      id: "approval",
      title: "审批",
      body: "执行命令",
      onClick,
    });
    expect(show_notification).toHaveBeenCalledWith({
      id: "approval",
      title: "审批",
      body: "执行命令",
    });
    action?.({ id: "other", action: "click" });
    expect(onClick).not.toHaveBeenCalled();
    action?.({ id: "approval", action: "click" });
    expect(onClick).toHaveBeenCalledOnce();
    expect(stop).toHaveBeenCalledOnce();
  });
  it("通知发送失败时释放监听", async () => {
    const stop = vi.fn();
    installServices({
      show_notification: async () => false,
      onNotificationAction: () => stop,
    });
    await expect(
      showDesktopNotification({ id: "a", title: "审批", body: "命令" }),
    ).resolves.toBe(false);
    expect(stop).toHaveBeenCalledOnce();
  });
});

describe("sanitizeDownloadFileName", () => {
  it("preserves case while replacing path and system separators", () => {
    expect(sanitizeDownloadFileName("Waker-Alice:/Profile?.zip")).toBe(
      "Waker-Alice__Profile_.zip",
    );
  });
});

describe("normalizeBrowserURL", () => {
  it("只接受 HTTP 和 HTTPS，并为地址栏域名补全 HTTPS", () => {
    expect(normalizeBrowserURL("example.com/path")).toBe(
      "https://example.com/path",
    );
    expect(normalizeBrowserURL("http://127.0.0.1:3000")).toBe(
      "http://127.0.0.1:3000/",
    );
    expect(normalizeBrowserURL("javascript:alert(1)")).toBeNull();
    expect(normalizeBrowserURL("file:///tmp/example.html")).toBeNull();
  });
});

describe("工作区资源 bridge", () => {
  it("拖入文件只发送 bytes 与 Chat scope，并逐项保留导入失败", async () => {
    const imported = vi.fn(async ({ name }: { name: string }) => {
      if (name === "已有.txt") throw new Error("文件已存在");
      return "资料/" + name;
    });
    installServices({
      open_workspace_scope: async () => ({ id: "scope", root: "/workspace" }),
      import_workspace_resource_bytes: imported,
    });
    const result = await importDroppedWorkspaceFiles(
      "chat-id",
      "资料",
      [new File(["hello"], "新文件.txt"), new File(["old"], "已有.txt")],
      "/workspace",
    );
    expect(result).toEqual({
      imported: ["资料/新文件.txt"],
      failed: [{ name: "已有.txt", message: "文件已存在" }],
    });
    expect(imported).toHaveBeenCalledWith({
      scopeId: "scope",
      directoryPath: "资料",
      root: "/workspace",
      name: "新文件.txt",
      bytes: new Uint8Array([104, 101, 108, 108, 111]),
    });
  });

  it("先监听后请求 ready，按序号保留 ready 期间到达的变化", async () => {
    let listener: ((event: WorkspaceResourceChangeEvent) => void) | undefined;
    let resolveReady!: (event: WorkspaceResourceChangeEvent) => void;
    const stopEvents = vi.fn();
    const unwatch_workspace_resource = vi.fn(async () => true);
    installServices({
      onWorkspaceResourceChange: (
        handler: (event: WorkspaceResourceChangeEvent) => void,
      ) => {
        listener = handler;
        return stopEvents;
      },
      watch_workspace_resource: () =>
        new Promise<WorkspaceResourceChangeEvent>((resolve) => {
          resolveReady = resolve;
        }),
      unwatch_workspace_resource,
    });
    const seen: number[] = [];
    const pending = observeWorkspaceResource("scope", "a.txt", (event) =>
      seen.push(event.sequence),
    );
    listener?.({ subscriptionId: "sub", sequence: 1, resource: null });
    resolveReady({ subscriptionId: "sub", sequence: 0, resource: null });
    const stop = await pending;
    expect(seen).toEqual([0, 1]);
    listener?.({ subscriptionId: "sub", sequence: 1, resource: null });
    expect(seen).toEqual([0, 1]);
    await stop();
    expect(stopEvents).toHaveBeenCalledOnce();
    expect(unwatch_workspace_resource).toHaveBeenCalledWith({
      scopeId: "scope",
      subscriptionId: "sub",
    });
  });

  it("read 发出后取消会通知 Main，迟到的 bytes 不回写", async () => {
    let finish!: (value: WorkspaceResourceRead) => void;
    let requestId = "";
    const read_workspace_resource = vi.fn(
      (request: {
        scopeId: string;
        relativePath: string;
        requestId: string;
      }) => {
        requestId = request.requestId;
        return new Promise<WorkspaceResourceRead>((resolve) => {
          finish = resolve;
        });
      },
    );
    const cancel_workspace_resource_read = vi.fn(async () => true);
    installServices({
      read_workspace_resource,
      cancel_workspace_resource_read,
    });
    const controller = new AbortController();
    const pending = readWorkspaceResource("scope", "a.txt", controller.signal);
    expect(requestId).toMatch(/^[0-9a-f-]{36}$/);
    controller.abort();
    finish({
      relativePath: "a.txt",
      size: 1,
      version: "1",
      modifiedAt: 0,
      mediaType: "text/plain",
      bytes: new Uint8Array([65]),
    });
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(cancel_workspace_resource_read).toHaveBeenCalledWith({
      scopeId: "scope",
      requestId,
    });
  });
});
