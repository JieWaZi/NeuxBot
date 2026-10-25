import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ComposerSurface } from "@/shared/ui/composer";
import { ChatEffortEffects } from "./chat-effort-effects";
import { isMaximumReasoning } from "@/shared/lib/runtime-reasoning";

const preference = vi.hoisted(() => ({ reduced: false }));
vi.mock("motion/react", () => ({ useReducedMotion: () => preference.reduced }));
const frames = new Map<number, FrameRequestCallback>();
const disconnectResize = vi.fn(),
  disconnectIntersection = vi.fn();
let sequence = 0,
  time = 0;
let intersect: IntersectionObserverCallback;
const context = {
  clearRect: vi.fn(),
  setTransform: vi.fn(),
  beginPath: vi.fn(),
  arc: vi.fn(),
  fill: vi.fn(),
  fillStyle: "",
  shadowColor: "",
  shadowBlur: 0,
  globalAlpha: 1,
};
beforeEach(() => {
  vi.clearAllMocks();
  preference.reduced = false;
  frames.clear();
  time = performance.now() + 20;
  vi.stubGlobal(
    "requestAnimationFrame",
    vi.fn((callback: FrameRequestCallback) => {
      const id = ++sequence;
      frames.set(id, callback);
      return id;
    }),
  );
  vi.stubGlobal(
    "cancelAnimationFrame",
    vi.fn((id: number) => frames.delete(id)),
  );
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect = disconnectResize;
    },
  );
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: IntersectionObserverCallback) {
        intersect = callback;
      }
      observe() {}
      disconnect = disconnectIntersection;
    },
  );
  // 仅模拟 Canvas 绘制边界，仍运行真实动画调度、输入监听与清理。
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(
    context as unknown as CanvasRenderingContext2D,
  );
  vi.spyOn(
    HTMLCanvasElement.prototype,
    "getBoundingClientRect",
  ).mockReturnValue(new DOMRect(0, 0, 320, 100));
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function advanceFrame() {
  const entry = frames.entries().next().value;
  if (!entry) throw new Error("没有待绘制的动画帧");
  frames.delete(entry[0]);
  time += 16;
  entry[1](time);
}
function content(active: boolean) {
  return (
    <ComposerSurface visualEffect={<ChatEffortEffects active={active} />}>
      <input
        aria-label="消息"
        data-chat-composer-input
        defaultValue="保留草稿"
      />
      <input aria-label="其他控件" />
    </ComposerSurface>
  );
}
it("只用真实档位中的最高档触发，单档位及有效默认值保持一致", () => {
  const options = [{ id: "medium" }, { id: "xhigh" }];
  expect(isMaximumReasoning(options, "xhigh")).toBe(true);
  expect(isMaximumReasoning(options, "", "xhigh")).toBe(true);
  expect(isMaximumReasoning(options, "medium")).toBe(false);
  expect(isMaximumReasoning(options, "", "unknown")).toBe(false);
  expect(isMaximumReasoning([{ id: "high" }], "high")).toBe(true);
  expect(isMaximumReasoning([], "max")).toBe(false);
});
it("最高档增强当前编辑器输入的粒子，退出最高档保持草稿与输入框实例并清理动画", () => {
  const view = render(content(true));
  const input = screen.getByRole("textbox", { name: "消息" });
  expect(frames.size).toBe(1);
  advanceFrame();
  expect(context.fill).toHaveBeenCalled();
  const initialBlur = context.shadowBlur;
  fireEvent.input(screen.getByRole("textbox", { name: "其他控件" }));
  advanceFrame();
  expect(context.shadowBlur).toBe(initialBlur);
  fireEvent.input(input);
  advanceFrame();
  expect(context.shadowBlur).toBeGreaterThan(initialBlur);
  view.rerender(content(false));
  expect(screen.getByRole("textbox", { name: "消息" })).toBe(input);
  expect(input).toHaveValue("保留草稿");
  expect(view.container.querySelector("canvas")).not.toBeInTheDocument();
  expect(frames.size).toBe(0);
  expect(disconnectResize).toHaveBeenCalledTimes(1);
  expect(disconnectIntersection).toHaveBeenCalledTimes(1);
});
it("离开可见区域停止绘制，返回后恢复；卸载后没有残留帧", () => {
  const view = render(content(true));
  const notify = (visible: boolean) =>
    intersect(
      [{ isIntersecting: visible } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
  notify(false);
  expect(frames.size).toBe(0);
  notify(true);
  expect(frames.size).toBe(1);
  view.unmount();
  expect(frames.size).toBe(0);
});
it("减少动态时保留最高档的静态辉光，不创建 Canvas 或动画帧", () => {
  preference.reduced = true;
  const view = render(content(true));
  expect(
    view.container.querySelector(".composer-effort-effect"),
  ).toHaveAttribute("data-active");
  expect(view.container.querySelector("canvas")).not.toBeInTheDocument();
  expect(requestAnimationFrame).not.toHaveBeenCalled();
});
