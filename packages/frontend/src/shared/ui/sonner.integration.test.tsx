import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { Toaster, toast } from "./sonner";

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

beforeEach(() => vi.useFakeTimers());
afterEach(async () => {
  act(() => toast.dismiss());
  await act(() => vi.advanceTimersByTimeAsync(1000));
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("通知倒计时", () => {
  it("错误超时后自动关闭，只调用自动关闭回调", async () => {
    const onAutoClose = vi.fn();
    const onDismiss = vi.fn();
    render(<Toaster />);
    await act(async () => {
      toast.error("保存失败", { duration: 1000, onAutoClose, onDismiss });
      await vi.advanceTimersByTimeAsync(16);
    });

    const card = screen.getByText("保存失败").closest("[data-sonner-toast]");
    expect(card?.querySelector("[data-toast-fuse]")).toBeInTheDocument();
    expect(card?.querySelector("[data-close-button]")).toBeNull();
    expect(card?.querySelector("[data-icon] svg")).toHaveClass("lucide");
    await act(() => vi.advanceTimersByTimeAsync(1500));
    expect(screen.queryByText("保存失败")).not.toBeInTheDocument();
    expect(onAutoClose).toHaveBeenCalledOnce();
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("悬停暂停关闭计时与细线动画，离开后继续", async () => {
    let playState = "running";
    const pause = vi.fn(() => {
      playState = "paused";
    });
    const play = vi.fn(() => {
      playState = "running";
    });
    const cancel = vi.fn();
    const animate = vi.fn(() => ({
      get playState() {
        return playState;
      },
      pause,
      play,
      cancel,
    }));
    Object.defineProperty(Element.prototype, "animate", {
      configurable: true,
      value: animate,
    });
    const onAutoClose = vi.fn();
    const view = render(<Toaster />);
    await act(async () => {
      toast.error("文件保存失败", { duration: 1000, onAutoClose });
      await vi.advanceTimersByTimeAsync(16);
    });
    const toaster = screen
      .getByText("文件保存失败")
      .closest("[data-sonner-toaster]");
    if (!toaster) throw new Error("通知容器未挂载");
    fireEvent.mouseEnter(toaster);
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(screen.getByText("文件保存失败")).toBeInTheDocument();
    expect(onAutoClose).not.toHaveBeenCalled();
    expect(pause).toHaveBeenCalled();

    fireEvent.mouseLeave(toaster);
    await act(() => vi.advanceTimersByTimeAsync(1500));
    expect(play).toHaveBeenCalled();
    expect(onAutoClose).toHaveBeenCalledOnce();
    view.unmount();
    expect(cancel).toHaveBeenCalled();
    Reflect.deleteProperty(Element.prototype, "animate");
  });
});
