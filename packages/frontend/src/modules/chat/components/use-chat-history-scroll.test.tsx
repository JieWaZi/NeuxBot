import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useChatHistoryScroll } from "./use-chat-history-scroll";

function Harness({
  scope = "chat-1",
  load,
  hasMore = true,
  loading = false,
}: {
  scope?: string;
  load: () => Promise<void>;
  hasMore?: boolean;
  loading?: boolean;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const history = useChatHistoryScroll({
    scope,
    viewport,
    hasMore,
    loading,
    load,
    onError: () => {},
  });
  return (
    <>
      <div data-testid="viewport" ref={viewport} onScroll={history.onScroll}>
        <div data-message-id="message-1" />
      </div>
      <button onClick={() => void history.loadEarlier()}>加载</button>
      <span>{history.error ? "失败" : "正常"}</span>
    </>
  );
}

function scroll(top: number) {
  const viewport = screen.getByTestId("viewport");
  Object.defineProperties(viewport, {
    scrollHeight: { configurable: true, value: 1000 },
    clientHeight: { configurable: true, value: 300 },
  });
  fireEvent.scroll(viewport, { target: { scrollTop: top } });
  return viewport;
}

afterEach(() => vi.unstubAllGlobals());

describe("chat history scroll", () => {
  it("前插已提交而补偿帧尚未执行时，用户继续滚动不会覆盖消息锚定基线", async () => {
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    });
    let finish!: () => void;
    render(
      <Harness
        load={() =>
          new Promise<void>((resolve) => {
            finish = resolve;
          })
        }
      />,
    );
    const viewport = scroll(200);
    const anchor = viewport.querySelector<HTMLElement>("[data-message-id]");
    if (!anchor) throw new Error("missing message anchor");
    let contentTop = 200;
    vi.spyOn(viewport, "getBoundingClientRect").mockImplementation(
      () => new DOMRect(0, 0, 500, 300),
    );
    vi.spyOn(anchor, "getBoundingClientRect").mockImplementation(
      () => new DOMRect(0, contentTop - viewport.scrollTop, 500, 100),
    );
    fireEvent.click(screen.getByRole("button", { name: "加载" }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    contentTop += 600;
    Object.defineProperty(viewport, "scrollHeight", {
      configurable: true,
      value: 1600,
    });
    fireEvent.scroll(viewport, { target: { scrollTop: 250 } });
    await act(async () => {
      finish();
    });
    act(() => {
      frames.shift()?.(0);
    });
    expect(viewport.scrollTop).toBe(850);
  });
  it("浏览器已经锚定前插消息时不二次补偿滚动高度", async () => {
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    });
    let finish!: () => void;
    render(
      <Harness
        load={() =>
          new Promise<void>((resolve) => {
            finish = resolve;
          })
        }
      />,
    );
    const viewport = scroll(200);
    const anchor = viewport.querySelector<HTMLElement>("[data-message-id]");
    if (!anchor) throw new Error("missing message anchor");
    let contentTop = 200;
    vi.spyOn(viewport, "getBoundingClientRect").mockImplementation(
      () => new DOMRect(0, 0, 500, 300),
    );
    vi.spyOn(anchor, "getBoundingClientRect").mockImplementation(
      () => new DOMRect(0, contentTop - viewport.scrollTop, 500, 100),
    );
    fireEvent.click(screen.getByRole("button", { name: "加载" }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    contentTop += 600;
    Object.defineProperty(viewport, "scrollHeight", {
      configurable: true,
      value: 1600,
    });
    viewport.scrollTop = 800;
    await act(async () => {
      finish();
    });
    act(() => {
      frames.shift()?.(0);
    });
    expect(viewport.scrollTop).toBe(800);
  });
  it("只在用户上滚接近顶部时加载一页，并合并在途的按钮与滚动请求", async () => {
    let finish!: () => void;
    const load = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    render(<Harness load={load} />);
    expect(load).not.toHaveBeenCalled();
    scroll(500);
    scroll(40);
    scroll(0);
    fireEvent.click(screen.getByRole("button", { name: "加载" }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    expect(load).toHaveBeenCalledTimes(1);
    await act(async () => {
      finish();
    });
  });

  it("前插历史后保持用户当前位置，切换会话后忽略旧加载的滚动恢复", async () => {
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    let finish!: () => void;
    const load = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const { rerender } = render(<Harness load={load} />);
    const viewport = scroll(200);
    fireEvent.click(screen.getByRole("button", { name: "加载" }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    fireEvent.scroll(viewport, { target: { scrollTop: 250 } });
    Object.defineProperty(viewport, "scrollHeight", {
      configurable: true,
      value: 1600,
    });
    await act(async () => {
      finish();
    });
    act(() => {
      frames.shift()?.(0);
    });
    expect(viewport.scrollTop).toBe(850);
    fireEvent.click(screen.getByRole("button", { name: "加载" }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    await waitFor(() => expect(load).toHaveBeenCalledTimes(2));
    rerender(<Harness scope="chat-2" load={load} />);
    viewport.scrollTop = 123;
    await act(async () => {
      finish();
    });
    act(() => {
      for (const frame of frames.splice(0)) frame(0);
    });
    expect(viewport.scrollTop).toBe(123);
  });

  it("失败后停止自动重试，保留手动重试入口；无更多历史或正在刷新时不加载", async () => {
    const load = vi
      .fn()
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValue(undefined);
    const { rerender } = render(<Harness load={load} />);
    scroll(500);
    await act(async () => {
      scroll(0);
    });
    await waitFor(() => expect(screen.getByText("失败")).toBeInTheDocument());
    scroll(500);
    scroll(0);
    expect(load).toHaveBeenCalledTimes(1);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "加载" }));
    });
    expect(load).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(screen.getByText("正常")).toBeInTheDocument());
    rerender(<Harness load={load} hasMore={false} />);
    fireEvent.click(screen.getByRole("button", { name: "加载" }));
    rerender(<Harness load={load} loading />);
    scroll(500);
    scroll(0);
    expect(load).toHaveBeenCalledTimes(2);
  });
});
