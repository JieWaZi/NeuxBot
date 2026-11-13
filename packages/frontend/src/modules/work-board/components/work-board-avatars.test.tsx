import { act, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { WorkBoardAvatars } from "./work-board-avatars";

vi.mock("@/shared/companion/companion-avatar", () => ({
  CompanionAvatar: () => null,
}));

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
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

  width = 0;
  act(() => observers.forEach((update) => update()));
  expect(avatars()).toHaveLength(0);
});
