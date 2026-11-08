import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ResourceGradient } from "./resource-gradient";

const shader = vi.hoisted(() => ({
  mount: vi.fn(),
  dispose: vi.fn(),
  color: vi.fn(() => [0, 0, 0, 1]),
}));

vi.mock("@paper-design/shaders", () => ({
  ShaderMount: class {
    dispose = shader.dispose;
    constructor(...args: unknown[]) {
      shader.mount(...args);
    }
  },
  getShaderColorFromString: shader.color,
  meshGradientFragmentShader: "mesh",
}));

beforeEach(() => {
  vi.clearAllMocks();
  document.documentElement.dataset.theme = "light";
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        private callback: (entries: { isIntersecting: boolean }[]) => void,
      ) {}
      observe() {
        this.callback([{ isIntersecting: true }]);
      }
      disconnect() {}
    },
  );
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete document.documentElement.dataset.theme;
});

it("rebuilds the visible shader with current theme colours and releases it on unmount", async () => {
  const computedStyle = document.createElement("div").style;
  vi.spyOn(computedStyle, "getPropertyValue").mockImplementation(() =>
    document.documentElement.dataset.theme === "dark" ? "#224466" : "#aaccee",
  );
  vi.spyOn(window, "getComputedStyle").mockReturnValue(computedStyle);
  const { unmount } = render(<ResourceGradient seed="Feishu" />);
  await waitFor(() => expect(shader.mount).toHaveBeenCalledTimes(1));
  expect(shader.color).toHaveBeenLastCalledWith("#aaccee");

  document.documentElement.dataset.theme = "dark";
  await waitFor(() => expect(shader.mount).toHaveBeenCalledTimes(2));
  expect(shader.dispose).toHaveBeenCalledTimes(1);
  expect(shader.color).toHaveBeenLastCalledWith("#224466");

  unmount();
  expect(shader.dispose).toHaveBeenCalledTimes(2);
});

it("keeps the CSS fallback when reduced motion is requested", async () => {
  vi.stubGlobal("matchMedia", () => ({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  const { container } = render(<ResourceGradient seed="Feishu" />);
  expect(container.firstElementChild).toHaveAttribute(
    "class",
    "catalog-try-gradient",
  );
  expect(container.firstElementChild).toHaveAttribute(
    "style",
    expect.stringContaining("var(--resource-artwork-"),
  );
  expect(shader.mount).not.toHaveBeenCalled();
});
