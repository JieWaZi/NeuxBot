import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RetainedPresence } from "./retained-presence";

describe("RetainedPresence", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn(() => ({ matches: false })),
    });
  });

  it("退出期间保留内容，结束后再卸载", () => {
    const view = render(
      <RetainedPresence present exitDurationMs={140}>
        内容
      </RetainedPresence>,
    );
    view.rerender(
      <RetainedPresence present={false} exitDurationMs={140}>
        内容
      </RetainedPresence>,
    );

    expect(screen.getByText("内容")).toHaveAttribute("data-state", "closed");
    act(() => vi.advanceTimersByTime(140));
    expect(screen.queryByText("内容")).not.toBeInTheDocument();
  });
});
