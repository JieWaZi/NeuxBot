import { useRef } from "react";
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi, type Mock } from "vitest";
import { usePresentationReading } from "./presentation-reading";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("旧阅读失败晚于新版本提交时，不能清掉新版本的去重状态；卸载后回调失效", async () => {
  const focus = vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 100, 100),
  );
  const observers: {
    callback: IntersectionObserverCallback;
    target?: Element;
    disconnect: Mock<() => void>;
  }[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      record: (typeof observers)[number];
      constructor(callback: IntersectionObserverCallback) {
        this.record = { callback, disconnect: vi.fn() };
        observers.push(this.record);
      }
      observe(target: Element) {
        this.record.target = target;
      }
      unobserve() {}
      disconnect() {
        this.record.disconnect();
      }
    },
  );
  let rejectOld: (reason: Error) => void = () => {};
  const first = vi.fn(
    () =>
      new Promise<void>((_resolve, reject) => {
        rejectOld = reject;
      }),
  );
  const second = vi.fn(async () => {});
  function Reader({
    identity,
    read,
  }: {
    identity: string;
    read: () => Promise<void>;
  }) {
    const root = useRef<HTMLDivElement>(null);
    usePresentationReading({ root, identity, read });
    return <div ref={root}>对应版本的真实正文</div>;
  }
  const show = (index: number) =>
    act(() => {
      const observer = observers[index];
      if (observer?.target)
        observer.callback(
          [
            {
              target: observer.target,
              isIntersecting: true,
              intersectionRect: new DOMRect(0, 0, 100, 100),
            } as IntersectionObserverEntry,
          ],
          {} as IntersectionObserver,
        );
    });
  const view = render(<Reader identity="result:chat:chat-1:1" read={first} />);
  show(0);
  expect(first).toHaveBeenCalledTimes(1);
  view.rerender(<Reader identity="result:chat:chat-1:2" read={second} />);
  show(0);
  expect(second).not.toHaveBeenCalled();
  show(1);
  expect(second).toHaveBeenCalledTimes(1);
  await act(async () => {
    rejectOld(new Error("旧提交失败"));
    await Promise.resolve();
  });
  focus.mockReturnValue(false);
  act(() => window.dispatchEvent(new Event("blur")));
  focus.mockReturnValue(true);
  act(() => window.dispatchEvent(new Event("focus")));
  expect(second).toHaveBeenCalledTimes(1);
  view.unmount();
  show(1);
  await waitFor(() =>
    expect(
      observers.every(
        (observer) => observer.disconnect.mock.calls.length === 1,
      ),
    ).toBe(true),
  );
  expect(second).toHaveBeenCalledTimes(1);
});
