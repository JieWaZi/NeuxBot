import type { MouseEvent } from "react";
import type { NavigateFunction } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { navigateRouteImmediately } from "./immediate-route-navigation";

function clickEvent(overrides: Partial<MouseEvent<HTMLAnchorElement>> = {}) {
  return {
    altKey: false,
    button: 0,
    ctrlKey: false,
    defaultPrevented: false,
    metaKey: false,
    preventDefault: vi.fn(),
    shiftKey: false,
    ...overrides,
  } as unknown as MouseEvent<HTMLAnchorElement>;
}

describe("navigateRouteImmediately", () => {
  it("立即提交普通主键点击", () => {
    const event = clickEvent();
    const navigate = vi.fn() as unknown as NavigateFunction;

    navigateRouteImmediately(event, navigate, "/chat/1");

    expect(event.preventDefault).toHaveBeenCalledOnce();
    expect(navigate).toHaveBeenCalledWith("/chat/1", {
      flushSync: true,
    });
  });

  it("保留修饰键点击的 Link 原生行为", () => {
    const event = clickEvent({ metaKey: true });
    const navigate = vi.fn() as unknown as NavigateFunction;

    navigateRouteImmediately(event, navigate, "/chat/1");

    expect(event.preventDefault).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });
});
