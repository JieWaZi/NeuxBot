import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

const desktopMocks = vi.hoisted(() => ({
  openExternalURL: vi.fn().mockResolvedValue(undefined),
  openWorkspaceFile: vi.fn().mockResolvedValue(true),
}));

vi.mock("@/shared/platform", () => ({
  closeBrowserView: vi.fn(),
  controlBrowserView: vi.fn(),
  createBrowserView: vi.fn(),
  isDesktopRuntime: () => false,
 hasPlatformService: () => false,
  navigateBrowserView: vi.fn(),
  normalizeBrowserURL: (value: string) =>
    value
      ? new URL(value.includes("://") ? value : `https://${value}`).href
      : null,
  openExternalURL: desktopMocks.openExternalURL,
  openWorkspaceFile: desktopMocks.openWorkspaceFile,
  setBrowserViewBounds: vi.fn(),
  setBrowserViewVisible: vi.fn(),
  subscribeBrowserNavigation: () => () => undefined,
}));

import { ChatBrowserView } from "./chat-browser-view";

describe("ChatBrowserView", () => {
  it("在地址栏左侧提供后退、前进和刷新入口", () => {
    render(<ChatBrowserView active onURLChange={vi.fn()} url="about:blank" />);

    expect(screen.getByRole("button", { name: "后退" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "前进" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "刷新" })).toBeDisabled();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "工具" })).toBeVisible();
  });

  it("新标签页直接提供文件、终端和变更入口", async () => {
    const onOpenTool = vi.fn();
    render(
      <ChatBrowserView
        active
        chatId="chat-1"
        onOpenTool={onOpenTool}
        onURLChange={vi.fn()}
        url=""
      />,
    );
    await userEvent.setup().click(screen.getByRole("button", { name: "文件" }));
    expect(onOpenTool).toHaveBeenCalledWith("files");
    await userEvent.setup().click(screen.getByRole("button", { name: "终端" }));
    expect(onOpenTool).toHaveBeenCalledWith("terminal");
    expect(screen.queryByRole("button", { name: "看板" })).toBeNull();
  });

  it("仅团队工作台的新标签页显示看板工具", async () => {
    const onOpenTool = vi.fn();
    render(
      <ChatBrowserView
        active
        onOpenTool={onOpenTool}
        onURLChange={vi.fn()}
        showBoardTool
        url=""
      />,
    );

    await userEvent.setup().click(screen.getByRole("button", { name: "看板" }));
    expect(onOpenTool).toHaveBeenCalledWith("board");
  });

  it("尚未开始对话时也能输入地址打开网页", async () => {
    const onURLChange = vi.fn();
    render(<ChatBrowserView active onURLChange={onURLChange} url="" />);
    const address = screen.getByRole("textbox", { name: "浏览器地址" });
    await userEvent.setup().type(address, "example.com{Enter}");
    expect(onURLChange).toHaveBeenCalledWith("https://example.com/");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("浏览器保留网页地址；本地 HTML 使用文件查看器的预览模式", () => {
    render(
      <ChatBrowserView
        active
        onURLChange={vi.fn()}
        url="https://example.com/report"
      />,
    );
    expect(screen.getByRole("textbox", { name: "浏览器地址" })).toHaveValue(
      "https://example.com/report",
    );
    expect(screen.getByRole("textbox", { name: "浏览器地址" })).toHaveClass(
      "h-control-sm",
      "rounded-full",
    );
  });
});
