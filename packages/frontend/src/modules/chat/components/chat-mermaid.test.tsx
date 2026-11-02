import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ContentVisibility } from "@/shared/lib/content-visibility";
import { useNativeOverlayVisible } from "@/shared/ui/native-overlay";
import "@/shared/i18n";
import { ChatMermaid } from "./chat-mermaid";

const { renderDiagram } = vi.hoisted(() => ({
  renderDiagram:
    vi.fn<(id: string, code: string) => Promise<{ svg: string }>>(),
}));
vi.mock("./chat-mermaid-plugin", () => ({
  mermaid: { getMermaid: () => ({ render: renderDiagram }) },
}));
vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

let viewportWidth = 360;
let viewportHeight = 600;
const resizeCallbacks = new Set<() => void>();
const layoutStyles = document.createElement("style");

function animationEnd(element: Element, name: string) {
  const event = new Event("animationend", { bubbles: true });
  Object.defineProperty(event, "animationName", { value: name });
  fireEvent(element, event);
}

function OverlayState() {
  const active = useNativeOverlayVisible();
  return (
    <output data-testid="native-overlay-state">
      {active ? "active" : "idle"}
    </output>
  );
}

beforeEach(() => {
  viewportWidth = 360;
  viewportHeight = 600;
  resizeCallbacks.clear();
  layoutStyles.textContent =
    ".p-4 { padding: 16px; } .pt-16 { padding-top: 64px; } .overflow-auto { overflow: auto; } .overflow-hidden { overflow: hidden; }";
  document.head.append(layoutStyles);
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(
    () => viewportWidth,
  );
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(
    () => viewportHeight,
  );
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private readonly callback: () => void) {}
      observe() {
        resizeCallbacks.add(this.callback);
      }
      disconnect() {
        resizeCallbacks.delete(this.callback);
      }
    },
  );
  renderDiagram.mockReset().mockResolvedValue({
    svg: '<svg viewBox="0 0 1200 600" width="100%"></svg>',
  });
});

afterEach(() => {
  layoutStyles.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Mermaid 阅读尺寸与隐藏生命周期", () => {
  it("隐藏 Scene 不绘制，恢复后首次绘制，再次切换复用已完成图形", async () => {
    const { rerender } = render(
      <ContentVisibility value={false}>
        <ChatMermaid code="flowchart TD\nA --> B" />
      </ContentVisibility>,
    );
    expect(renderDiagram).not.toHaveBeenCalled();
    rerender(
      <ContentVisibility value>
        <ChatMermaid code="flowchart TD\nA --> B" />
      </ContentVisibility>,
    );
    await screen.findByRole("img", { name: "Mermaid diagram" });
    expect(renderDiagram).toHaveBeenCalledTimes(1);
    expect(resizeCallbacks.size).toBe(1);
    rerender(
      <ContentVisibility value={false}>
        <ChatMermaid code="flowchart TD\nA --> B" />
      </ContentVisibility>,
    );
    expect(resizeCallbacks.size).toBe(0);
    rerender(
      <ContentVisibility value>
        <ChatMermaid code="flowchart TD\nA --> B" />
      </ContentVisibility>,
    );
    await act(async () => Promise.resolve());
    expect(renderDiagram).toHaveBeenCalledTimes(1);
  });

  it("消息内宽图按容器宽度等比缩小，随分栏重排，小图保留自然尺寸", async () => {
    const view = render(<ChatMermaid code="flowchart TD\nA --> B" />);
    const diagram = await screen.findByRole("img", { name: "Mermaid diagram" });
    expect(diagram).toHaveStyle({ width: "328px", height: "164px" });
    viewportWidth = 200;
    act(() => resizeCallbacks.forEach((callback) => callback()));
    expect(diagram).toHaveStyle({ width: "168px", height: "84px" });
    renderDiagram.mockResolvedValue({
      svg: '<svg viewBox="0 0 160 80" width="100%"></svg>',
    });
    view.rerender(<ChatMermaid code="flowchart TD\nA --> C" />);
    await waitFor(() => expect(diagram).toHaveStyle({ width: "160px" }));
    expect(diagram).toHaveStyle({ height: "80px" });
  });

  it("消息内高图随比例展开完整高度，不添加内层滚动或高度上限", async () => {
    renderDiagram.mockResolvedValue({
      svg: '<svg viewBox="0 0 1200 4800" width="100%"></svg>',
    });
    render(<ChatMermaid code="tall" />);
    const diagram = await screen.findByRole("img", { name: "Mermaid diagram" });
    expect(diagram).toHaveStyle({ width: "328px", height: "1312px" });
    const viewport = diagram.parentElement?.parentElement;
    expect(viewport).not.toBeNull();
    if (viewport) {
      expect(getComputedStyle(viewport).maxHeight).toBe("none");
      expect(getComputedStyle(viewport).overflow).not.toBe("auto");
    }
  });

  it("全屏初始适配可用宽高，主动放大才开启滚动，重置和resize恢复fit", async () => {
    const user = userEvent.setup();
    viewportWidth = 800;
    viewportHeight = 400;
    renderDiagram.mockResolvedValue({
      svg: '<svg viewBox="0 0 1200 2400" width="100%"></svg>',
    });
    render(<ChatMermaid code="tall" />);
    await user.click(await screen.findByRole("button", { name: "全屏查看" }));
    const dialog = screen.getByRole("dialog", { name: "图形" });
    const diagram = within(dialog).getByRole("img", {
      name: "Mermaid diagram",
    });
    const canvas = diagram.parentElement;
    const viewport = canvas?.parentElement?.parentElement;
    expect(canvas).toHaveStyle({ width: "160px", height: "320px" });
    expect(viewport).toHaveStyle({ overflow: "hidden" });
    await user.click(within(dialog).getByRole("button", { name: "放大" }));
    expect(canvas).toHaveStyle({ width: "200px", height: "400px" });
    expect(viewport).toHaveStyle({ overflow: "auto" });
    await user.click(within(dialog).getByRole("button", { name: "重置缩放" }));
    expect(canvas).toHaveStyle({ width: "160px", height: "320px" });
    expect(viewport).toHaveStyle({ overflow: "hidden" });
    viewportWidth = 120;
    act(() => resizeCallbacks.forEach((callback) => callback()));
    expect(canvas).toHaveStyle({ width: "88px", height: "176px" });
    expect(renderDiagram).toHaveBeenCalledTimes(1);
  });

  it("关闭动画保留图形与原生浮层占位，动画结束才一起卸载，再打开从fit开始", async () => {
    const user = userEvent.setup();
    const originalStyle = window.getComputedStyle;
    vi.stubGlobal("CSS", { escape: (value: string) => value });
    vi.spyOn(window, "getComputedStyle").mockImplementation(
      (element, pseudo) => {
        const style = originalStyle(element, pseudo);
        if (
          !(element instanceof HTMLElement) ||
          !["popup", "overlay"].includes(element.dataset.motion ?? "")
        )
          return style;
        // Browsers expose a live computed-style object. jsdom snapshots it, so the
        // proxy models only the animation name Radix uses to retain exiting nodes.
        return new Proxy(style, {
          get(target, key) {
            if (key === "animationName")
              return element.dataset.state === "open"
                ? "preview-in"
                : "preview-out";
            return Reflect.get(target, key, target);
          },
        });
      },
    );
    render(
      <>
        <ChatMermaid code="retained" />
        <OverlayState />
      </>,
    );
    await user.click(await screen.findByRole("button", { name: "全屏查看" }));
    const dialog = screen.getByRole("dialog", { name: "图形" });
    const overlay = document.querySelector('[data-motion="overlay"]');
    await user.click(within(dialog).getByRole("button", { name: "放大" }));
    await user.click(within(dialog).getByRole("button", { name: "关闭" }));
    expect(dialog).toHaveAttribute("data-state", "closed");
    expect(
      within(dialog).getByRole("img", { name: "Mermaid diagram" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("native-overlay-state")).toHaveTextContent(
      "active",
    );
    act(() => {
      if (overlay) animationEnd(overlay, "preview-out");
      animationEnd(dialog, "preview-out");
    });
    expect(dialog).not.toBeInTheDocument();
    expect(screen.getByTestId("native-overlay-state")).toHaveTextContent(
      "idle",
    );
    await user.click(screen.getByRole("button", { name: "全屏查看" }));
    const reopened = screen.getByRole("dialog", { name: "图形" });
    expect(
      within(reopened).getByRole("img", { name: "Mermaid diagram" })
        .parentElement,
    ).toHaveStyle({ width: "328px", height: "164px" });
    expect(renderDiagram).toHaveBeenCalledTimes(1);
  });

  it("首次绘制使用局部骨架，迟到旧结果不覆盖新内容", async () => {
    let finishOld: ((value: { svg: string }) => void) | undefined;
    renderDiagram.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishOld = resolve;
        }),
    );
    const view = render(<ChatMermaid code="old" />);
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
    view.rerender(<ChatMermaid code="new" />);
    const diagram = await screen.findByRole("img", { name: "Mermaid diagram" });
    await act(async () => {
      finishOld?.({ svg: '<svg data-stale="true"></svg>' });
    });
    expect(diagram.querySelector("svg")).toHaveAttribute(
      "viewBox",
      "0 0 1200 600",
    );
    expect(diagram.querySelector("[data-stale]")).toBeNull();
  });
});
