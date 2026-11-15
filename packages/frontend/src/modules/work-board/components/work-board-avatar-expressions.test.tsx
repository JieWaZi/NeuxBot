import { act, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, expect, it, vi } from "vitest";
import {
  nextWorkBoardAvatarExpression,
  useWorkBoardAvatarExpression,
} from "./work-board-avatar-expressions";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function Face({ name }: { name: string }) {
  const host = useRef<HTMLDivElement>(null);
  const expression = useWorkBoardAvatarExpression(host, "neutre");
  return <div ref={host} data-testid={name} data-expression={expression} />;
}

function visibility() {
  const observers = new Map<
    Element,
    (entries: { isIntersecting: boolean }[]) => void
  >();
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        private readonly callback: (
          entries: { isIntersecting: boolean }[],
        ) => void,
      ) {}
      observe(element: Element) {
        observers.set(element, this.callback);
      }
      disconnect() {
        for (const [element, callback] of observers) {
          if (callback === this.callback) observers.delete(element);
        }
      }
    },
  );
  return {
    observers,
    set(visible: boolean) {
      act(() =>
        observers.forEach((callback) =>
          callback([{ isIntersecting: visible }]),
        ),
      );
    },
  };
}

it("轮换只取友好表情，并且每次避开当前表情", () => {
  for (const seed of [0, 0.15, 0.5, 0.99, 1, Number.NaN]) {
    let current = nextWorkBoardAvatarExpression("neutre", seed);
    for (let index = 0; index < 20; index++) {
      const next = nextWorkBoardAvatarExpression(current, seed);
      expect(next).not.toBe(current);
      expect(["colere", "triste", "effraye", "inquiet"]).not.toContain(next);
      current = next;
    }
  }
});

it("不同头像按各自的3到6秒时钟换脸，不同时轮换", () => {
  vi.useFakeTimers();
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  vi.spyOn(Math, "random")
    .mockReturnValueOnce(0)
    .mockReturnValueOnce(0.5)
    .mockReturnValue(0.9);
  const visible = visibility();
  const view = render(
    <>
      <Face name="first" />
      <Face name="second" />
    </>,
  );
  visible.set(true);
  const first = screen.getByTestId("first");
  const second = screen.getByTestId("second");
  act(() => vi.advanceTimersByTime(2999));
  expect(first).toHaveAttribute("data-expression", "neutre");
  expect(second).toHaveAttribute("data-expression", "neutre");
  act(() => vi.advanceTimersByTime(1));
  expect(first).not.toHaveAttribute("data-expression", "neutre");
  expect(second).toHaveAttribute("data-expression", "neutre");
  act(() => vi.advanceTimersByTime(1500));
  expect(second).not.toHaveAttribute("data-expression", "neutre");
  expect(vi.getTimerCount()).toBe(2);
  const lateObservers = [...visible.observers.values()];
  view.unmount();
  act(() =>
    lateObservers.forEach((callback) => callback([{ isIntersecting: true }])),
  );
  expect(vi.getTimerCount()).toBe(0);
  expect(visible.observers.size).toBe(0);
});

it.each(["隐藏窗口", "离开视区", "减少动态"])(
  "%s时暂停，恢复后重新计时且不补播",
  (reason) => {
    vi.useFakeTimers();
    const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(false);
    vi.spyOn(Math, "random").mockReturnValue(0);
    let reduced = reason === "减少动态";
    const media = new EventTarget();
    vi.stubGlobal("matchMedia", () => ({
      get matches() {
        return reduced;
      },
      addEventListener: media.addEventListener.bind(media),
      removeEventListener: media.removeEventListener.bind(media),
    }));
    const visible = visibility();
    const view = render(<Face name="avatar" />);
    const face = screen.getByTestId("avatar");
    expect(vi.getTimerCount()).toBe(0);
    visible.set(true);
    if (reduced) {
      expect(vi.getTimerCount()).toBe(0);
      act(() => {
        reduced = false;
        media.dispatchEvent(new Event("change"));
      });
    }
    act(() => vi.advanceTimersByTime(3000));
    const expression = face.getAttribute("data-expression");
    expect(expression).not.toBe("neutre");
    act(() => vi.advanceTimersByTime(1000));
    const pause = (paused: boolean) => {
      act(() => {
        if (reason === "隐藏窗口") {
          hidden.mockReturnValue(paused);
          document.dispatchEvent(new Event("visibilitychange"));
        } else if (reason === "离开视区") {
          visible.set(!paused);
        } else {
          reduced = paused;
          media.dispatchEvent(new Event("change"));
        }
      });
    };
    pause(true);
    expect(vi.getTimerCount()).toBe(0);
    act(() => vi.advanceTimersByTime(60_000));
    expect(face).toHaveAttribute("data-expression", expression);
    pause(false);
    expect(vi.getTimerCount()).toBe(1);
    act(() => vi.advanceTimersByTime(2999));
    expect(face).toHaveAttribute("data-expression", expression);
    act(() => vi.advanceTimersByTime(1));
    expect(face).not.toHaveAttribute("data-expression", expression);
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
    expect(visible.observers.size).toBe(0);
  },
);
