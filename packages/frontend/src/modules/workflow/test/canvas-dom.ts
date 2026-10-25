import { vi } from "vitest";

/** 为画布尺寸与浮层交互测试补齐 jsdom 测量能力。 */
export function installCanvasDOM(
  getWidth: (element?: Element) => number = () => 800,
) {
  const width = vi
    .spyOn(HTMLElement.prototype, "offsetWidth", "get")
    .mockImplementation(function (this: HTMLElement) {
      return this.style.width.endsWith("px")
        ? Number.parseFloat(this.style.width)
        : getWidth(this);
    });
  const height = vi
    .spyOn(HTMLElement.prototype, "offsetHeight", "get")
    .mockImplementation(function (this: HTMLElement) {
      if (this.style.height.endsWith("px"))
        return Number.parseFloat(this.style.height);
      return 600;
    });
  const observers = new Set<() => void>();
  const resize = () => {
    for (const update of observers) update();
  };
  window.addEventListener("resize", resize);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      private targets = new Set<Element>();
      constructor(private callback: ResizeObserverCallback) {}
      private update = () => {
        if (!this.targets.size) return;
        this.callback(
          [...this.targets].map(
            (target) =>
              ({
                target,
                contentRect: new DOMRect(0, 0, getWidth(target), 600),
              }) as ResizeObserverEntry,
          ),
          this as unknown as ResizeObserver,
        );
      };
      observe(target: Element) {
        observers.add(this.update);
        this.targets.add(target);
        setTimeout(() => {
          if (!this.targets.has(target)) return;
          this.callback(
            [
              {
                target,
                contentRect: new DOMRect(0, 0, getWidth(target), 600),
              } as ResizeObserverEntry,
            ],
            this as unknown as ResizeObserver,
          );
        }, 0);
      }
      unobserve(target: Element) {
        this.targets.delete(target);
        if (!this.targets.size) observers.delete(this.update);
      }
      disconnect() {
        observers.delete(this.update);
        this.targets.clear();
      }
    },
  );
  return () => {
    window.removeEventListener("resize", resize);
    observers.clear();
    width.mockRestore();
    height.mockRestore();
  };
}
