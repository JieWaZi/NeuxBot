import { act, fireEvent, render, screen } from "@testing-library/react";
import { useLayoutEffect, useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useChatViewportScroll } from "./use-chat-viewport-scroll";

const layout = { height: 300, top: 0, second: 150 };
const callbacks = new Set<ResizeObserverCallback>();
const frames = new Map<number, FrameRequestCallback>();
let nextFrame = 0;
function flush() {
  act(() => {
    const batch = [...frames.values()];
    frames.clear();
    batch.forEach((callback) => callback(0));
  });
}
function resize() {
  act(() => {
    callbacks.forEach((callback) => callback([], {} as ResizeObserver));
  });
  flush();
}
function Harness({
  revision = 0,
  scope = "chat-a",
  enabled = true,
  isHistoryAnchoring,
}: {
  revision?: number;
  scope?: string;
  enabled?: boolean;
  isHistoryAnchoring?: () => boolean;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const element = viewport.current;
    if (!element) return;
    Object.defineProperties(element, {
      clientHeight: { configurable: true, get: () => 100 },
      scrollHeight: { configurable: true, get: () => layout.height },
      scrollTop: {
        configurable: true,
        get: () => layout.top,
        set: (value: number) => {
          layout.top = Math.max(0, Math.min(value, layout.height - 100));
        },
      },
    });
    element.scrollTo = (options?: ScrollToOptions | number, y?: number) => {
      element.scrollTop = (typeof options === "number" ? y : options?.top) ?? 0;
    };
    element.getBoundingClientRect = () => new DOMRect(0, 0, 400, 100);
    const first = element.querySelector<HTMLElement>("[data-first]");
    const second = element.querySelector<HTMLElement>("[data-second]");
    if (first)
      first.getBoundingClientRect = () =>
        new DOMRect(0, -layout.top, 400, layout.second);
    if (second)
      second.getBoundingClientRect = () =>
        new DOMRect(0, layout.second - layout.top, 400, 100);
  }, []);
  useChatViewportScroll({
    viewport,
    revision,
    scope,
    enabled,
    isHistoryAnchoring,
  });
  return (
    <div ref={viewport} data-testid="viewport">
      <div data-chat-message-column>
        <p data-first data-chat-node-id="first">
          第一段
        </p>
        <p data-second data-chat-node-id="second">
          阅读中的段落
        </p>
      </div>
    </div>
  );
}
beforeEach(() => {
  Object.assign(layout, { height: 300, top: 0, second: 150 });
  callbacks.clear();
  frames.clear();
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    const id = ++nextFrame;
    frames.set(id, callback);
    return id;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private readonly callback: ResizeObserverCallback) {
        callbacks.add(callback);
      }
      observe() {}
      unobserve() {}
      disconnect() {
        callbacks.delete(this.callback);
      }
    },
  );
});
afterEach(() => vi.unstubAllGlobals());

describe("canonical 消息阅读滚动", () => {
  it("初始在底部，正文追加与异步增高继续跟随", () => {
    const view = render(<Harness />);
    expect(layout.top).toBe(200);
    layout.height = 380;
    view.rerender(<Harness revision={1} />);
    flush();
    expect(layout.top).toBe(280);
    layout.height = 420;
    resize();
    expect(layout.top).toBe(320);
  });
  it("向上阅读后正文不抢位置，图片和前插内容增高保持原段落偏移", () => {
    const view = render(<Harness />);
    layout.top = 150;
    fireEvent.scroll(screen.getByTestId("viewport"));
    layout.height = 400;
    view.rerender(<Harness revision={1} />);
    flush();
    expect(layout.top).toBe(150);
    layout.second = 190;
    layout.height = 440;
    resize();
    expect(Math.abs(layout.top - 190)).toBeLessThanOrEqual(2);
    layout.top = 340;
    fireEvent.scroll(screen.getByTestId("viewport"));
    layout.height = 480;
    resize();
    expect(layout.top).toBe(380);
  });
  it("翻页进行时只由历史锚点补偿，不重复移动相同布局差值", () => {
    let loadingHistory = true;
    const guard = () => loadingHistory;
    render(<Harness isHistoryAnchoring={guard} />);
    layout.top = 150;
    fireEvent.scroll(screen.getByTestId("viewport"));
    layout.second = 190;
    layout.height = 440;
    resize();
    expect(layout.top).toBe(150);
    // 原 history anchor 已完成其一次补偿，并在下一次浏览器 scroll 事件前释放。
    layout.top = 190;
    loadingHistory = false;
    fireEvent.scroll(screen.getByTestId("viewport"));
    resize();
    expect(layout.top).toBe(190);
  });
  it("100次正文追加和程序滚动都不扫描历史块，已有阅读锚点也直接复用", () => {
    const view = render(<Harness />);
    const element = screen.getByTestId("viewport");
    const first = element.querySelector<HTMLElement>("[data-first]");
    if (!first) throw new Error("missing history block");
    const historyBounds = vi.spyOn(first, "getBoundingClientRect");
    const historyScan = vi.spyOn(element, "querySelectorAll");
    for (let revision = 1; revision <= 100; revision += 1) {
      layout.height += 3;
      view.rerender(<Harness revision={revision} />);
      flush();
      fireEvent.scroll(element);
    }
    expect(historyBounds).not.toHaveBeenCalled();
    expect(historyScan).not.toHaveBeenCalled();
    layout.top = 150;
    fireEvent.scroll(element);
    historyBounds.mockClear();
    historyScan.mockClear();
    for (let revision = 101; revision <= 200; revision += 1) {
      layout.height += 3;
      view.rerender(<Harness revision={revision} />);
      flush();
    }
    expect(layout.top).toBe(150);
    expect(historyBounds).not.toHaveBeenCalled();
    expect(historyScan).not.toHaveBeenCalled();
  });
  it("同一聊天隐藏后恢复仍保留阅读锚点", () => {
    const view = render(<Harness />);
    layout.top = 150;
    fireEvent.scroll(screen.getByTestId("viewport"));
    view.rerender(<Harness enabled={false} />);
    expect(callbacks.size).toBe(0);
    layout.second = 190;
    layout.height = 440;
    view.rerender(<Harness enabled revision={1} />);
    expect(layout.top).toBe(190);
  });
  it("停用、来源切换与卸载释放observer和排队帧", () => {
    const view = render(<Harness />);
    expect(callbacks.size).toBe(1);
    resize();
    view.rerender(<Harness enabled={false} revision={1} />);
    expect(callbacks.size).toBe(0);
    expect(frames.size).toBe(0);
    layout.top = 20;
    view.rerender(<Harness scope="chat-b" revision={2} />);
    expect(layout.top).toBe(200);
    expect(callbacks.size).toBe(1);
    view.unmount();
    expect(callbacks.size).toBe(0);
    expect(frames.size).toBe(0);
  });
});
