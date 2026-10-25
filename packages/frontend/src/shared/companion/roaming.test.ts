import { afterEach, expect, it, vi } from "vitest";
import { companionRoaming } from "./roaming";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

it("行走和跳跃不越过右侧活动区，悬停与键盘聚焦停留，缩窄和卸载取消动画", () => {
  let resize: () => void = () => {};
  const disconnect = vi.fn();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: () => void) {
        resize = callback;
      }
      observe() {}
      disconnect = disconnect;
    },
  );
  const track = document.createElement("div");
  const actor = document.createElement("div");
  const touch = document.createElement("button");
  const close = document.createElement("button");
  actor.append(touch, close);
  track.append(actor);
  document.body.append(track);
  let width = 320;
  Object.defineProperty(track, "clientWidth", { get: () => width });
  Object.defineProperty(actor, "offsetWidth", { value: 160 });
  let renderedPosition = 0;
  vi.spyOn(track, "getBoundingClientRect").mockImplementation(() =>
    DOMRect.fromRect({ x: 320, width }),
  );
  vi.spyOn(actor, "getBoundingClientRect").mockImplementation(() =>
    DOMRect.fromRect({ x: 320 + renderedPosition, width: 160 }),
  );
  const cancel = vi.fn();
  const animate = vi.fn(() => ({ cancel }));
  Object.defineProperty(actor, "animate", { value: animate });
  const roaming = companionRoaming(track, actor);
  for (let index = 0; index < 30; index++) {
    expect(roaming.move(index % 2 ? "walk" : "jump")).toBe(true);
    const target = Number(
      actor.style.transform.match(/translateX\((.+)px\)/)?.[1],
    );
    expect(target).toBeGreaterThanOrEqual(0);
    expect(target).toBeLessThanOrEqual(width - 160);
    renderedPosition = target;
  }
  renderedPosition = 100;
  actor.dispatchEvent(new Event("pointerenter"));
  expect(actor.style.transform).toBe("translateX(100px)");
  expect(roaming.move("walk")).toBe(false);
  actor.dispatchEvent(new Event("pointerleave"));
  roaming.move("walk");
  renderedPosition = 80;
  touch.focus();
  expect(actor.style.transform).toBe("translateX(80px)");
  expect(roaming.move("jump")).toBe(false);
  close.focus();
  expect(roaming.move("walk")).toBe(false);
  close.blur();
  expect(roaming.move("walk")).toBe(true);
  width = 180;
  renderedPosition = 250;
  resize();
  expect(actor.style.transform).toBe("translateX(20px)");
  width = 120;
  resize();
  expect(actor.style.transform).toBe("translateX(0px)");
  const calls = animate.mock.calls.length;
  expect(roaming.move("jump")).toBe(true);
  expect(animate).toHaveBeenCalledTimes(calls);
  roaming.destroy();
  expect(cancel).toHaveBeenCalled();
  expect(disconnect).toHaveBeenCalledOnce();
});
