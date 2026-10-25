import { afterEach, expect, it, vi } from "vitest";

import { trackHorizontalDrag } from "@/shared/lib/horizontal-drag";

const cleanups: Array<() => void> = [];
afterEach(() => {
  cleanups.splice(0).forEach((cleanup) => cleanup());
  vi.restoreAllMocks();
});

function setup() {
  let tick: FrameRequestCallback = () => {};
  const request = vi
    .spyOn(window, "requestAnimationFrame")
    .mockImplementation((callback) => {
      tick = callback;
      return 1;
    });
  const cancel = vi
    .spyOn(window, "cancelAnimationFrame")
    .mockImplementation(() => {});
  const onChange = vi.fn();
  const onCommit = vi.fn();
  const onFinish = vi.fn();
  const cleanup = trackHorizontalDrag({
    initialX: 100,
    initialValue: 280,
    valueAt: (x) => 380 - x,
    onChange,
    onCommit,
    onFinish,
  });
  cleanups.push(cleanup);
  return {
    request,
    cancel,
    tick: () => tick(0),
    onChange,
    onCommit,
    onFinish,
    cleanup,
  };
}

it("高频移动每帧只使用最新位置，松手才持久化最终位置", () => {
  const drag = setup();
  document.dispatchEvent(new MouseEvent("mousemove", { clientX: 90 }));
  document.dispatchEvent(new MouseEvent("mousemove", { clientX: 80 }));
  expect(drag.request).toHaveBeenCalledTimes(1);
  drag.tick();
  expect(drag.onChange).toHaveBeenLastCalledWith(300);
  expect(drag.onCommit).not.toHaveBeenCalled();
  document.dispatchEvent(new MouseEvent("mousemove", { clientX: 70 }));
  document.dispatchEvent(new MouseEvent("mouseup", { clientX: 60 }));
  expect(drag.onChange).toHaveBeenLastCalledWith(320);
  expect(drag.onCommit).toHaveBeenCalledExactlyOnceWith(320);
  expect(drag.cancel).toHaveBeenCalled();
  document.dispatchEvent(new MouseEvent("mousemove", { clientX: 10 }));
  expect(drag.request).toHaveBeenCalledTimes(2);
});

it("窗口失焦会提交最新位置并恢复页面状态", () => {
  document.body.style.cursor = "pointer";
  const drag = setup();
  document.dispatchEvent(new MouseEvent("mousemove", { clientX: 80 }));
  window.dispatchEvent(new Event("blur"));
  expect(drag.onCommit).toHaveBeenCalledExactlyOnceWith(300);
  expect(drag.onFinish).toHaveBeenCalledTimes(1);
  expect(document.body.style.cursor).toBe("pointer");
});

it("卸载保留未刷新的位置且不再更新组件", () => {
  const drag = setup();
  document.dispatchEvent(new MouseEvent("mousemove", { clientX: 80 }));
  drag.cleanup();
  drag.cleanup();
  expect(drag.onCommit).toHaveBeenCalledExactlyOnceWith(300);
  expect(drag.onChange).not.toHaveBeenCalled();
  expect(drag.onFinish).not.toHaveBeenCalled();
});

it("未移动不产生持久化写入", () => {
  const drag = setup();
  document.dispatchEvent(new MouseEvent("mouseup", { clientX: 100 }));
  expect(drag.onCommit).not.toHaveBeenCalled();
});
