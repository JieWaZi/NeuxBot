import { act, fireEvent, render, screen } from "@testing-library/react";
import { useLayoutEffect, useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ChatViewportPositionContext,
  useChatViewportScroll,
  type ChatViewportPositions,
} from "./use-chat-viewport-scroll";

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
  rowVersion = 0,
  withRows = true,
  isHistoryAnchoring,
  onFollowRevision,
  onUserScroll,
}: {
  revision?: number;
  scope?: string;
  enabled?: boolean;
  rowVersion?: number;
  withRows?: boolean;
  isHistoryAnchoring?: () => boolean;
  onFollowRevision?: (distance: number) => void;
  onUserScroll?: () => void;
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
  }, []);
  useChatViewportScroll({
    viewport,
    revision,
    scope,
    enabled,
    isHistoryAnchoring,
    onFollowRevision,
    onUserScroll,
  });
  return (
    <div ref={viewport} data-testid="viewport">
      <div data-chat-message-column>
        {withRows && (
          <>
            <p
              data-first
              data-chat-node-id="first"
              ref={(node) => {
                if (node)
                  node.getBoundingClientRect = () =>
                    new DOMRect(0, -layout.top, 400, layout.second);
              }}
            >
              第一段
            </p>
            <p
              key={rowVersion}
              data-second
              data-chat-node-id="second"
              ref={(node) => {
                if (node)
                  node.getBoundingClientRect = () =>
                    new DOMRect(0, layout.second - layout.top, 400, 100);
              }}
            >
              阅读中的段落
            </p>
          </>
        )}
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
  it("正文提交只报告此次跟随产生的实际滚动距离", () => {
    const onFollowRevision = vi.fn();
    const view = render(<Harness onFollowRevision={onFollowRevision} />);
    expect(layout.top).toBe(200);
    expect(onFollowRevision.mock.calls).toEqual([[0]]);

    layout.height = 380;
    view.rerender(<Harness revision={1} onFollowRevision={onFollowRevision} />);
    expect(layout.top).toBe(280);
    expect(onFollowRevision.mock.calls).toEqual([[0], [80]]);

    layout.height = 420;
    resize();
    expect(layout.top).toBe(320);
    layout.height = 440;
    fireEvent(document, new Event("visibilitychange"));
    expect(layout.top).toBe(340);
    expect(onFollowRevision.mock.calls).toEqual([[0], [80]]);

    layout.height = 440.25;
    view.rerender(<Harness revision={2} onFollowRevision={onFollowRevision} />);
    expect(layout.top).toBe(340);
    expect(onFollowRevision.mock.calls).toEqual([[0], [80], [0]]);
  });
  it("程序滚动不触发用户回调，用户滚动先清理位移再测量阅读锚点", () => {
    const onFollowRevision = vi.fn();
    const onUserScroll = vi.fn(() => {
      layout.second = 150;
    });
    const scene = (revision = 0, enabled = true) => (
      <Harness
        revision={revision}
        enabled={enabled}
        onFollowRevision={onFollowRevision}
        onUserScroll={onUserScroll}
      />
    );
    const view = render(scene());
    const element = screen.getByTestId("viewport");
    fireEvent.scroll(element);
    layout.height = 360;
    resize();
    fireEvent.scroll(element);
    expect(onUserScroll).not.toHaveBeenCalled();

    Object.assign(layout, { top: 150, second: 170 });
    fireEvent.scroll(element);
    expect(onUserScroll).toHaveBeenCalledTimes(1);

    Object.assign(layout, { height: 440, second: 190 });
    view.rerender(scene(1));
    expect(layout.top).toBe(190);
    fireEvent.scroll(element);
    expect(onUserScroll).toHaveBeenCalledTimes(1);

    view.rerender(scene(2, false));
    layout.second = 210;
    view.rerender(scene(3));
    expect(layout.top).toBe(210);
    expect(onFollowRevision.mock.calls).toEqual([[0], [0], [0], [0]]);
  });
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
  it("同一个段落换了 DOM 时在提交阶段恢复原视口偏移", () => {
    const view = render(<Harness />);
    layout.top = 150;
    fireEvent.scroll(screen.getByTestId("viewport"));
    const before = screen.getByText("阅读中的段落");
    layout.second = 190;
    layout.height = 440;
    view.rerender(<Harness rowVersion={1} revision={1} />);
    expect(screen.getByText("阅读中的段落")).not.toBe(before);
    expect(layout.top).toBe(190);
  });
  it("切到其他会话后返回，恢复原聊天的阅读位置", () => {
    const positions: ChatViewportPositions = new Map();
    const tab = (scope: string) => (
      <ChatViewportPositionContext value={positions}>
        <Harness key={scope} scope={scope} />
      </ChatViewportPositionContext>
    );
    const view = render(tab("chat-a"));
    layout.top = 150;
    fireEvent.scroll(screen.getByTestId("viewport"));
    view.rerender(tab("chat-b"));
    expect(layout.top).toBe(200);
    layout.second = 190;
    layout.height = 440;
    view.rerender(tab("chat-a"));
    expect(layout.top).toBe(190);
  });
  it("切回会话时历史暂空，保留锚点直到消息到达", () => {
    const view = render(<Harness />);
    layout.top = 150;
    fireEvent.scroll(screen.getByTestId("viewport"));
    view.rerender(<Harness scope="chat-b" revision={1} />);
    Object.assign(layout, { top: 0, height: 100 });
    view.rerender(<Harness scope="chat-a" withRows={false} revision={2} />);
    layout.second = 190;
    layout.height = 440;
    view.rerender(<Harness scope="chat-a" revision={3} />);
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
