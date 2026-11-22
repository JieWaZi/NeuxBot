import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import "@/app/workspace-i18n";
import { Toaster, toast } from "./sonner";

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

beforeEach(() => vi.useFakeTimers());

async function flushSonner() {
  await act(() => vi.advanceTimersByTimeAsync(0));
}
afterEach(() => {
  act(() => toast.dismiss());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("应用通知", () => {
  it("错误在三秒后自动关闭，主动通知在悬停展开后仍等用户关闭", async () => {
    const onAutoClose = vi.fn();
    const onDismiss = vi.fn();
    render(<Toaster />);
    act(() => {
      toast.manual("Cursor 有可用更新", {
        description: "1.2.3 → 1.3.0",
        onDismiss,
      });
      toast.error("请求失败", { description: "保存失败", onAutoClose });
    });

    await flushSonner();
    const viewport = screen.getByRole("region", { name: "应用通知" });
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(viewport.querySelector("[data-toast-fuse]")).toBeInTheDocument();
    const error = screen.getByRole("alert");
    expect(
      screen.getByRole("button", { name: "关闭通知「请求失败」" }),
    ).toBeVisible();
    expect(error).toHaveTextContent("保存失败");
    fireEvent.mouseEnter(viewport);
    const update = screen
      .getByText("1.2.3 → 1.3.0")
      .closest("[data-sonner-toast]");
    expect(update).toHaveAttribute("data-expanded", "true");
    expect(update).toHaveClass("notification-sonner-card");
    expect(error.closest("[data-sonner-toast]")).toHaveClass(
      "notification-sonner-card",
    );
    expect(update?.querySelector("[data-toast-fuse]")).toBeNull();
    act(() => vi.advanceTimersByTime(2999));
    expect(screen.getByText("保存失败")).toBeVisible();
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(onAutoClose).toHaveBeenCalledOnce();
    await act(() => vi.advanceTimersByTimeAsync(1));
    const exiting = error.closest("[data-sonner-toast]");
    if (!exiting) throw new Error("通知卡片未挂载");
    expect(exiting).toHaveAttribute("data-notification-exiting");
    fireEvent(
      exiting,
      Object.assign(new Event("animationend", { bubbles: true }), {
        animationName: "notification-card-exit",
      }),
    );
    await flushSonner();
    expect(screen.queryByText("保存失败")).not.toBeInTheDocument();
    act(() => vi.advanceTimersByTime(10000));
    expect(viewport.querySelector("[data-toast-fuse]")).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "关闭通知「Cursor 有可用更新」" }),
    );
    expect(onDismiss).toHaveBeenCalledOnce();
    await act(() => vi.advanceTimersByTimeAsync(300));
    expect(
      screen.queryByRole("region", { name: "应用通知" }),
    ).not.toBeInTheDocument();
  });

  it("点击自动通知的关闭按钮会立即开始退场，并取消到期回调", async () => {
    const onDismiss = vi.fn();
    const onAutoClose = vi.fn();
    render(<Toaster />);
    act(() => toast.error("请求失败", { onDismiss, onAutoClose }));
    await flushSonner();
    const card = screen.getByRole("alert").closest("[data-sonner-toast]");
    fireEvent.click(
      screen.getByRole("button", { name: "关闭通知「请求失败」" }),
    );
    await flushSonner();
    expect(card).toHaveAttribute("data-notification-exiting");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(onDismiss).toHaveBeenCalledOnce();
    act(() => vi.advanceTimersByTime(3000));
    expect(onAutoClose).not.toHaveBeenCalled();
  });

  it("减少动态效果时到期直接关闭", async () => {
    vi.stubGlobal("matchMedia", () => ({ matches: true }));
    render(<Toaster />);
    act(() => toast.error("请求失败"));
    await flushSonner();
    expect(screen.getByRole("alert")).toBeVisible();
    await act(() => vi.advanceTimersByTimeAsync(3000));
    expect(screen.queryByRole("region", { name: "应用通知" })).toBeNull();
  });

  it("翻到第二页后到达的错误回到第一页展示，列表保持展开", async () => {
    render(<Toaster />);
    act(() => {
      for (let number = 1; number <= 8; number += 1)
        toast.manual(`通知 ${number}`);
    });
    await flushSonner();
    fireEvent.mouseEnter(screen.getByRole("region", { name: "应用通知" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页通知" }));
    await flushSonner();
    expect(screen.getByText("2 / 2")).toBeVisible();
    act(() => toast.error("刚到的请求错误"));
    await flushSonner();
    expect(screen.getByRole("alert")).toHaveTextContent("刚到的请求错误");
    expect(screen.getByText("1 / 3")).toBeVisible();
    expect(screen.getAllByRole("listitem")).toHaveLength(4);
    fireEvent.click(screen.getByRole("button", { name: "下一页通知" }));
    await flushSonner();
    expect(screen.getByText("2 / 3")).toBeVisible();
  });

  it("一百条通知收成一摞，鼠标移入后每页最多四条，最后一页仍可访问", async () => {
    render(<Toaster />);
    act(() => {
      for (let number = 1; number <= 100; number += 1)
        toast.manual(`通知 ${number}`);
    });
    await flushSonner();
    const viewport = screen.getByRole("region", { name: "应用通知" });
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("通知 100")).toBeVisible();
    const card = screen.getByText("通知 99").closest("[data-sonner-toast]");
    expect(card).toHaveAttribute("data-expanded", "false");
    fireEvent.mouseEnter(viewport);
    expect(screen.getByText("通知 99").closest("[data-sonner-toast]")).toBe(
      card,
    );
    expect(card).toHaveAttribute("data-expanded", "true");
    fireEvent.mouseLeave(viewport);
    expect(screen.getByText("通知 99").closest("[data-sonner-toast]")).toBe(
      card,
    );
    expect(card).toHaveAttribute("data-expanded", "false");
    fireEvent.mouseEnter(viewport);
    expect(screen.getAllByRole("listitem")).toHaveLength(4);
    const scroll = viewport.querySelector<HTMLElement>(
      "[data-notification-scroll]",
    );
    if (!scroll) throw new Error("通知滚动区未挂载");
    scroll.scrollTop = 100;
    fireEvent.click(screen.getByRole("button", { name: "下一页通知" }));
    fireEvent.mouseLeave(viewport);
    await flushSonner();
    expect(screen.getByText("2 / 25")).toBeVisible();
    expect(scroll.scrollTop).toBe(0);
    expect(screen.getAllByRole("listitem")).toHaveLength(4);
    for (let page = 2; page < 25; page += 1) {
      fireEvent.click(screen.getByRole("button", { name: "下一页通知" }));
      await flushSonner();
    }
    expect(screen.getByText("25 / 25")).toBeVisible();
    expect(screen.getByText("通知 1")).toBeVisible();
    const next = screen.getByRole("button", { name: "下一页通知" });
    expect(next).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(next);
    expect(screen.getByText("25 / 25")).toBeVisible();
    const list = viewport.querySelector("[data-sonner-toaster]");
    if (!list) throw new Error("Sonner 列表未挂载");
    let finish = () => {};
    const finished = new Promise<void>((resolve) => (finish = resolve));
    Object.defineProperty(list, "getAnimations", {
      value: () => [{ finished }],
    });
    fireEvent.pointerDown(document.body);
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("通知 4")).toBeVisible();
    await act(async () => finish());
    await flushSonner();
    expect(screen.getByText("通知 100")).toBeVisible();
  });
});
