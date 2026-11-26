import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useDiffPatchViewport } from "./diff-patch-viewport";

class ViewportObserver {
  static readonly instances = new Set<ViewportObserver>();
  readonly targets = new Set<Element>();

  constructor(private readonly onEntry: IntersectionObserverCallback) {
    ViewportObserver.instances.add(this);
  }

  observe(target: Element) {
    this.targets.add(target);
  }

  unobserve(target: Element) {
    this.targets.delete(target);
  }

  disconnect() {
    this.targets.clear();
    ViewportObserver.instances.delete(this);
  }

  static report(target: Element, isIntersecting: boolean) {
    act(() => {
      for (const observer of ViewportObserver.instances) {
        if (observer.targets.has(target)) {
          observer.onEntry(
            [{ target, isIntersecting } as IntersectionObserverEntry],
            observer as unknown as IntersectionObserver,
          );
        }
      }
    });
  }
}

describe("Git 补丁视区读取资格", () => {
  beforeEach(() => vi.stubGlobal("IntersectionObserver", ViewportObserver));
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("补丁滚出视区后停止自动读取，返回视区后恢复", () => {
    const content = { current: document.createElement("div") };
    const { result } = renderHook(() => useDiffPatchViewport(content, true));

    expect(result.current).toBe(false);
    ViewportObserver.report(content.current, true);
    expect(result.current).toBe(true);
    ViewportObserver.report(content.current, false);
    expect(result.current).toBe(false);
    ViewportObserver.report(content.current, true);
    expect(result.current).toBe(true);
  });

  it("重新激活时等待新的视区结果", () => {
    const content = { current: document.createElement("div") };
    const { result, rerender } = renderHook(
      (active: boolean) => useDiffPatchViewport(content, active),
      { initialProps: true },
    );

    ViewportObserver.report(content.current, true);
    expect(result.current).toBe(true);
    rerender(false);
    expect(result.current).toBe(false);
    rerender(true);
    expect(result.current).toBe(false);
    ViewportObserver.report(content.current, true);
    expect(result.current).toBe(true);
  });
});
