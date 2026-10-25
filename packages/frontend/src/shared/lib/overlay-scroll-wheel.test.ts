import { describe, expect, it, vi } from "vitest";

import { consumeOverlayWheelScroll } from "./overlay-scroll-wheel";

describe("consumeOverlayWheelScroll", () => {
  it("在可滚动容器内手动推进 scrollTop", () => {
    const element = document.createElement("div");
    Object.defineProperties(element, {
      scrollHeight: { value: 400, configurable: true },
      clientHeight: { value: 200, configurable: true },
      scrollTop: { value: 0, writable: true, configurable: true },
    });
    const preventDefault = vi.fn();
    const stopPropagation = vi.fn();
    consumeOverlayWheelScroll({
      currentTarget: element,
      deltaY: 40,
      preventDefault,
      stopPropagation,
    } as unknown as React.WheelEvent<HTMLElement>);
    expect(element.scrollTop).toBe(40);
    expect(preventDefault).toHaveBeenCalled();
    expect(stopPropagation).toHaveBeenCalled();
  });
});
