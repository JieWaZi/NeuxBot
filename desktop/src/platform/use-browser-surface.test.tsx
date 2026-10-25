import { act, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useBrowserSurface } from "./use-browser-surface";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";
import {
  createBrowserView,
  closeBrowserView,
  setBrowserViewVisible,
} from "@/shared/platform";

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => true,
  createBrowserView: vi.fn().mockResolvedValue("view-1"),
  closeBrowserView: vi.fn().mockResolvedValue(undefined),
  setBrowserViewVisible: vi.fn().mockResolvedValue(undefined),
  setBrowserViewBounds: vi.fn().mockResolvedValue(undefined),
  navigateBrowserView: vi.fn().mockResolvedValue(undefined),
  controlBrowserView: vi.fn().mockResolvedValue(undefined),
  subscribeBrowserNavigation: () => () => {},
}));

function Surface() {
  const { hostRef } = useBrowserSurface({
    active: true,
    url: "https://example.com",
    onURLChange: () => {},
  });
  return <div ref={hostRef} />;
}

function ScopedSurface({
  chatId,
  url = "https://example.com",
  workspaceRoot = "/project",
}: {
  chatId?: string;
  url?: string;
  workspaceRoot?: string;
}) {
  const { hostRef } = useBrowserSurface({
    active: true,
    chatId,
    url,
    workspaceRoot,
    onURLChange: () => {},
  });
  return <div ref={hostRef} />;
}

function SurfaceWithOverlay({ overlay }: { overlay: boolean }) {
  return (
    <>
      <NativeOverlayPresence active={overlay} />
      <Surface />
    </>
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("browser surface overlays", () => {
  it("初始化可见性失败时回收已创建的原生视图", async () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, 0, 800, 600),
    );
    vi.mocked(setBrowserViewVisible).mockRejectedValueOnce(
      new Error("visibility failed"),
    );
    const { unmount } = render(<Surface />);
    await waitFor(() =>
      expect(closeBrowserView).toHaveBeenCalledWith("view-1"),
    );
    unmount();
    expect(closeBrowserView).toHaveBeenCalledTimes(1);
  });

  it("创建结果晚于卸载时仍关闭迟到的原生视图", async () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, 0, 800, 600),
    );
    let resolveView: ((id: string) => void) | undefined;
    vi.mocked(createBrowserView).mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          resolveView = resolve;
        }),
    );
    const { unmount } = render(<Surface />);
    await waitFor(() => expect(createBrowserView).toHaveBeenCalledTimes(1));
    unmount();
    await act(async () => resolveView?.("late-view"));
    expect(closeBrowserView).toHaveBeenCalledWith("late-view");
    expect(setBrowserViewVisible).not.toHaveBeenCalled();
  });

  it("Renderer 浮层出现时隐藏原生视图并复用同一会话", async () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, 0, 800, 600),
    );
    const { rerender, unmount } = render(<SurfaceWithOverlay overlay />);
    try {
      await waitFor(() =>
        expect(setBrowserViewVisible).toHaveBeenCalledWith("view-1", false),
      );
      expect(setBrowserViewVisible).not.toHaveBeenCalledWith("view-1", true);
      rerender(<SurfaceWithOverlay overlay={false} />);
      await waitFor(() =>
        expect(setBrowserViewVisible).toHaveBeenLastCalledWith("view-1", true),
      );
      rerender(<SurfaceWithOverlay overlay />);
      await waitFor(() =>
        expect(setBrowserViewVisible).toHaveBeenLastCalledWith("view-1", false),
      );
      expect(createBrowserView).toHaveBeenCalledTimes(1);
      expect(closeBrowserView).not.toHaveBeenCalled();
    } finally {
      unmount();
    }
    expect(closeBrowserView).toHaveBeenCalledWith("view-1");
  });

  it("网页浏览从草稿转为正式对话时不重建原生视图", async () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, 0, 800, 600),
    );
    const { rerender, unmount } = render(<ScopedSurface />);
    await waitFor(() => expect(createBrowserView).toHaveBeenCalledTimes(1));
    expect(createBrowserView).toHaveBeenCalledWith(
      "https://example.com",
      expect.any(Object),
      "/project",
      undefined,
    );
    rerender(<ScopedSurface chatId="chat-1" workspaceRoot="/new-project" />);
    expect(createBrowserView).toHaveBeenCalledTimes(1);
    expect(closeBrowserView).not.toHaveBeenCalled();
    unmount();
  });
});
