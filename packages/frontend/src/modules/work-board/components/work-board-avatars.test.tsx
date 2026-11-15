import { act, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CompanionAvatar } from "@/shared/companion/companion-avatar";
import { WorkBoardAvatars } from "./work-board-avatars";

vi.mock("@/shared/companion/companion-avatar", () => ({
  CompanionAvatar: vi.fn(
    ({ avatar, expression }: { avatar: string; expression?: string }) => (
      <span data-avatar-source={avatar} data-expression={expression} />
    ),
  ),
}));

afterEach(() => {
  vi.clearAllMocks();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it("只切换表情参数，不重建头像或改写物理位置，场景隐藏后释放计时器", () => {
  vi.useFakeTimers();
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(420);
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  vi.spyOn(Math, "random").mockReturnValue(0.5);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        private readonly callback: (
          entries: { isIntersecting: boolean }[],
        ) => void,
      ) {}
      observe(element: Element) {
        this.callback([
          { isIntersecting: element.hasAttribute("data-pile-avatar") },
        ]);
      }
      disconnect() {}
    },
  );
  const partners = [
    {
      id: "partner",
      name: "伙伴",
      avatar: "bloub:plush:cercle:heureux:8b5cf6",
    },
  ];
  const view = render(
    <WorkBoardAvatars active partners={partners} side="left" />,
  );
  const pieces = [
    ...view.container.querySelectorAll<HTMLElement>("[data-pile-avatar]"),
  ];
  const sources = pieces.map((piece) =>
    piece
      .querySelector("[data-avatar-source]")
      ?.getAttribute("data-avatar-source"),
  );
  const expressions = pieces.map((piece) =>
    piece.querySelector("[data-expression]")?.getAttribute("data-expression"),
  );
  const first = pieces[0];
  if (!first) throw new Error("缺少装饰头像");
  first.style.setProperty("--pile-live-x", "37%");
  first.dataset.falling = "";
  act(() => vi.advanceTimersByTime(4500));
  expect([...view.container.querySelectorAll("[data-pile-avatar]")]).toEqual(
    pieces,
  );
  expect(first.style.getPropertyValue("--pile-live-x")).toBe("37%");
  expect(first).toHaveAttribute("data-falling");
  for (let index = 0; index < pieces.length; index++) {
    const avatar = pieces[index]?.querySelector("[data-avatar-source]");
    expect(avatar).toHaveAttribute("data-avatar-source", sources[index]);
    expect(avatar).not.toHaveAttribute("data-expression", expressions[index]);
  }
  view.rerender(
    <WorkBoardAvatars active={false} partners={partners} side="left" />,
  );
  expect(view.container.querySelectorAll("[data-pile-avatar]")).toHaveLength(0);
  expect(vi.getTimerCount()).toBe(0);
});

it("窄栏不挂载头像，宽度恢复时显示并在收窄后释放", () => {
  let width = 0;
  const observers = new Set<() => void>();
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(
    () => width,
  );
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private readonly callback: () => void) {}
      observe() {
        observers.add(this.callback);
      }
      disconnect() {
        observers.delete(this.callback);
      }
    },
  );
  vi.stubGlobal("matchMedia", () => ({
    matches: true,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  const { container } = render(
    <WorkBoardAvatars
      active
      partners={[{ id: "partner", name: "伙伴" }]}
      side="left"
    />,
  );
  const avatars = () => container.querySelectorAll("[data-pile-avatar]");
  expect(avatars()).toHaveLength(0);

  width = 160;
  act(() => observers.forEach((update) => update()));
  expect(avatars()).toHaveLength(7);
  expect(CompanionAvatar).toHaveBeenCalledWith(
    expect.objectContaining({ animated: true, interactive: false }),
    undefined,
  );
  expect(container.querySelector("button")).toBeNull();

  width = 0;
  act(() => observers.forEach((update) => update()));
  expect(avatars()).toHaveLength(0);
});
