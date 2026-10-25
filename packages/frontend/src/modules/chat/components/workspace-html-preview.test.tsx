import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { WorkspaceHTMLPreview } from "./workspace-html-preview";

const useBrowserSurface = vi.hoisted(() =>
  vi.fn(() => ({
    hostRef: { current: null },
    browserReady: true,
    error: undefined,
    runBrowserAction: vi.fn(),
  })),
);
vi.mock("./use-browser-surface", () => ({ useBrowserSurface }));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));

describe("WorkspaceHTMLPreview", () => {
  it.each([
    {
      name: "工作流内的相对路径",
      rootPath: "/workflow/run-1",
      path: "reports/report.html",
      url: "file:///workflow/run-1/reports/report.html",
    },
    {
      name: "Chat 授权范围内的绝对软链接入口",
      rootPath: "/workflow/run-1",
      path: "/chats/worker/workspace/projects/default/report.html",
      url: "file:///chats/worker/workspace/projects/default/report.html",
    },
    {
      name: "Windows 绝对文件路径",
      rootPath: "C:\\workflow\\run-1",
      path: "C:\\workflow\\run-1\\reports\\daily report.html",
      url: "file:///C:/workflow/run-1/reports/daily%20report.html",
    },
  ])("正确打开 $name 并保留 Chat 范围校验", ({ rootPath, path, url }) => {
    render(
      <WorkspaceHTMLPreview
        rootPath={rootPath}
        path={path}
        content="<h1>报告</h1>"
        refreshToken={0}
        chatId="worker-chat"
      />,
    );
    expect(useBrowserSurface).toHaveBeenLastCalledWith(
      expect.objectContaining({
        url,
        workspaceRoot: rootPath,
        chatId: "worker-chat",
      }),
    );
  });

  it("默认启动互动浏览器，静态模式使用隔离 iframe", () => {
    const { rerender } = render(
      <WorkspaceHTMLPreview
        rootPath="/workspace"
        path="index.html"
        content="<h1>hello</h1><script>alert(1)</script>"
        refreshToken={0}
        chatId="chat-id"
      />,
    );
    expect(useBrowserSurface).toHaveBeenCalled();
    expect(screen.queryByTitle("静态预览")).not.toBeInTheDocument();

    rerender(
      <WorkspaceHTMLPreview
        interactive={false}
        rootPath="/workspace"
        path="index.html"
        content="<h1>hello</h1><script>alert(1)</script>"
        refreshToken={0}
        chatId="chat-id"
      />,
    );
    const frame = screen.getByTitle("静态预览");
    expect(frame).toHaveAttribute("sandbox", "");
    expect(frame.getAttribute("srcdoc")).toContain("hello");
    expect(frame.getAttribute("srcdoc")).not.toContain("<script");
  });

  it("切换工作台标签时隐藏互动预览的原生视图", () => {
    render(
      <WorkspaceHTMLPreview
        active={false}
        rootPath="/workspace"
        path="index.html"
        content="<h1>hello</h1>"
        refreshToken={0}
        chatId="chat-id"
      />,
    );
    expect(useBrowserSurface).toHaveBeenLastCalledWith(
      expect.objectContaining({ active: false }),
    );
  });
});
