import { act } from "@testing-library/react";
import { vi } from "vitest";

export function mockChatPresentation(focused = true) {
  const focus = vi.spyOn(document, "hasFocus").mockReturnValue(focused);
  const scroll = vi.spyOn(window, "scrollBy").mockImplementation(() => {});
  const bounds = vi
    .spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(0, 0, 100, 24));
  const observed = new Map<Element, IntersectionObserverCallback>();
  class Intersection {
    constructor(private readonly callback: IntersectionObserverCallback) {}
    observe(target: Element) {
      observed.set(target, this.callback);
    }
    unobserve(target: Element) {
      observed.delete(target);
    }
    disconnect() {
      for (const [target, callback] of observed)
        if (callback === this.callback) observed.delete(target);
    }
  }
  const intersection = Object.getOwnPropertyDescriptor(
    globalThis,
    "IntersectionObserver",
  );
  Object.defineProperty(globalThis, "IntersectionObserver", {
    configurable: true,
    value: Intersection,
  });
  const present = (id: string) =>
    act(() => {
      for (const [target, callback] of observed) {
        if (
          target
            .closest("[data-message-id]")
            ?.getAttribute("data-message-id") !== id
        )
          continue;
        const entry = {
          target,
          isIntersecting: true,
          intersectionRect: new DOMRect(0, 0, 100, 24),
        } as IntersectionObserverEntry;
        callback([entry], {} as IntersectionObserver);
      }
    });
  return {
    focus,
    present,
    restore() {
      scroll.mockRestore();
      focus.mockRestore();
      bounds.mockRestore();
      if (intersection)
        Object.defineProperty(globalThis, "IntersectionObserver", intersection);
      else Reflect.deleteProperty(globalThis, "IntersectionObserver");
    },
  };
}
