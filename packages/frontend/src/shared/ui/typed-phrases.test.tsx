import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TypedPhrases } from "./typed-phrases";

describe("TypedPhrases", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn(() => ({
        matches: false,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("减少动效时直接展示首句", () => {
    vi.mocked(window.matchMedia).mockReturnValue({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    } as unknown as MediaQueryList);

    render(
      <TypedPhrases as="h1" phrases={["今天想一起完成什么？", "下一句"]} />,
    );

    expect(
      screen
        .getByRole("heading", { name: "今天想一起完成什么？" })
        .querySelector("[data-typed-phrase]"),
    ).toHaveTextContent("今天想一起完成什么？");
  });

  it("按欢迎页节奏逐字输入并切换下一句", () => {
    render(<TypedPhrases phrases={["你好", "世界"]} />);

    const typed = () =>
      screen
        .getByRole("paragraph", { name: "你好" })
        .querySelector("[data-typed-phrase]");
    expect(typed()).toHaveTextContent("");

    act(() => vi.advanceTimersByTime(450));
    expect(typed()).toHaveTextContent("你");

    act(() => vi.advanceTimersByTime(100));
    expect(typed()).toHaveTextContent("你好");

    act(() => vi.advanceTimersByTime(2200 + 55 + 55 + 450 + 100 + 100));
    expect(typed()).toHaveTextContent("世界");
  });

  it("关闭循环时逐行留下已输入的句子", () => {
    render(<TypedPhrases loop={false} wrap phrases={["你好", "世界"]} />);

    const typed = () =>
      screen
        .getByRole("paragraph", { name: "你好" })
        .querySelector("[data-typed-phrase]");

    act(() => vi.advanceTimersByTime(450 + 100));
    expect(typed()).toHaveTextContent("你好");
    act(() => vi.advanceTimersByTime(450 + 100 + 100));
    expect(typed()?.textContent?.replaceAll("\n", "")).toBe("你好世界");
    act(() => vi.advanceTimersByTime(20_000));
    expect(typed()?.textContent).toContain("\n");
    expect(typed()?.textContent?.replaceAll("\n", "")).toBe("你好世界");
  });
});
