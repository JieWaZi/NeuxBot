import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import {
  applyTheme,
  persistThemeMode,
  readThemeMode,
  resolveTheme,
} from "./theme";

const storedValues = new Map<string, string>();
const memoryStorage: Storage = {
  get length() {
    return storedValues.size;
  },
  clear() {
    storedValues.clear();
  },
  getItem(key) {
    return storedValues.get(key) ?? null;
  },
  key(index) {
    return Array.from(storedValues.keys())[index] ?? null;
  },
  removeItem(key) {
    storedValues.delete(key);
  },
  setItem(key, value) {
    storedValues.set(key, value);
  },
};

function installColorScheme(matches: boolean) {
  const mediaQuery: MediaQueryList = {
    matches,
    media: "(prefers-color-scheme: dark)",
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  };
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn(() => mediaQuery),
  });
}

describe("theme", () => {
  beforeAll(() => {
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: memoryStorage,
    });
  });

  beforeEach(() => {
    window.localStorage.clear();
    delete document.documentElement.dataset.theme;
    installColorScheme(false);
  });

  it("uses system mode when storage has no valid preference", () => {
    window.localStorage.setItem("ally.ui.theme", "unexpected");
    expect(readThemeMode()).toBe("system");
  });

  it("persists and applies an explicit theme", () => {
    persistThemeMode("dark");
    expect(readThemeMode()).toBe("dark");
    expect(applyTheme("dark")).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("resolves system mode from the operating system preference", () => {
    installColorScheme(true);
    expect(resolveTheme("system")).toBe("dark");
  });
});
