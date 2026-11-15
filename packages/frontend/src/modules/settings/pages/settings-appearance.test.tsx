import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { SettingsScene } from "./settings-scene";
import { ThemeProvider } from "@/shared/theme";
import { configurePlatform, platform } from "@/shared/platform/provider";
import { setLanguageMode } from "@/shared/i18n";
import type { InterfaceScale } from "@neuxbot/platform-contracts/services";

beforeEach(() => {
  localStorage.clear();
  setLanguageMode("zh-CN");
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
  setLanguageMode("system");
  document.documentElement.style.removeProperty("zoom");
  delete document.documentElement.dataset.windowTransparent;
});

function settings() {
  return render(
    <MemoryRouter>
      <ThemeProvider>
        <SettingsScene section="general" onClose={() => undefined} />
      </ThemeProvider>
    </MemoryRouter>,
  );
}

it("通用设置即时应用中英文、透明窗口、主题和大小，并在重新挂载后恢复", async () => {
  let savedTransparent = false;
  const configure = vi.fn(async (value: { transparent: boolean }) => {
    savedTransparent = value.transparent;
    return { transparencySupported: true };
  });
  const scaleListeners = new Set<(scale: InterfaceScale) => void>();
  const current = platform();
  configurePlatform({
    ...current,
    kind: "desktop",
    host: {
      ...current.host,
      get_window_appearance: async () => ({
        transparent: savedTransparent,
        transparencySupported: true,
      }),
      set_window_appearance: configure,
      onInterfaceScale(listener) {
        scaleListeners.add(listener);
        return () => {
          scaleListeners.delete(listener);
        };
      },
    },
  });
  const user = userEvent.setup();
  const view = settings();
  const transparent = screen.getByRole("switch", { name: "透明窗口" });
  await waitFor(() => expect(transparent).toBeEnabled());
  await user.click(transparent);
  await user.click(screen.getByRole("combobox", { name: "界面主题" }));
  await user.click(screen.getByRole("option", { name: "暗色" }));
  await user.click(screen.getByRole("combobox", { name: "语言" }));
  await user.click(screen.getByRole("option", { name: "English" }));
  expect(
    screen.getByRole("switch", { name: "Transparent window" }),
  ).toBeChecked();
  await user.click(screen.getByRole("combobox", { name: "Interface size" }));
  await user.click(screen.getByRole("option", { name: "125%" }));
  await waitFor(() =>
    expect(configure).toHaveBeenLastCalledWith({
      theme: "dark",
      language: "en-US",
      transparent: true,
      scale: 1.25,
    }),
  );
  expect(document.documentElement.dataset.windowTransparent).toBe("true");
  view.unmount();
  settings();
  await waitFor(() =>
    expect(
      screen.getByRole("switch", { name: "Transparent window" }),
    ).toBeChecked(),
  );
  expect(
    screen.getByRole("combobox", { name: "Interface size" }),
  ).toHaveTextContent("125%");
  expect(
    screen.getByRole("combobox", { name: "Interface theme" }),
  ).toHaveTextContent("Dark");
  await act(() => {
    for (const listener of scaleListeners) listener(1.5);
  });
  expect(
    screen.getByRole("combobox", { name: "Interface size" }),
  ).toHaveTextContent("150%");
  await waitFor(() =>
    expect(configure).toHaveBeenLastCalledWith({
      theme: "dark",
      language: "en-US",
      transparent: true,
      scale: 1.5,
    }),
  );
});

it("浏览器禁用透明窗口并支持保存界面大小", async () => {
  const user = userEvent.setup();
  settings();
  expect(screen.getByRole("switch", { name: "透明窗口" })).toBeDisabled();
  await user.click(screen.getByRole("combobox", { name: "界面大小" }));
  await user.click(screen.getByRole("option", { name: "110%" }));
  expect(document.documentElement.style.zoom).toBe("1.1");
});

it("等待主进程恢复透明偏好，首次同步不会把已开启的窗口改为关闭", async () => {
  let finishRead: (value: {
    transparent: boolean;
    transparencySupported: boolean;
  }) => void = () => undefined;
  const read = new Promise<{
    transparent: boolean;
    transparencySupported: boolean;
  }>((resolve) => {
    finishRead = resolve;
  });
  const configure = vi.fn(async () => ({ transparencySupported: true }));
  const current = platform();
  configurePlatform({
    ...current,
    kind: "desktop",
    host: {
      ...current.host,
      get_window_appearance: () => read,
      set_window_appearance: configure,
    },
  });
  settings();
  expect(configure).not.toHaveBeenCalled();
  expect(screen.getByRole("switch", { name: "透明窗口" })).toBeDisabled();
  await act(async () => {
    finishRead({ transparent: true, transparencySupported: true });
  });
  await waitFor(() =>
    expect(configure).toHaveBeenLastCalledWith({
      theme: "system",
      language: "zh-CN",
      transparent: true,
      scale: 1,
    }),
  );
  expect(screen.getByRole("switch", { name: "透明窗口" })).toBeChecked();
  expect(document.documentElement.dataset.windowTransparent).toBe("true");
  expect(localStorage.getItem("bot.ui.appearance")).toBeNull();
});

it("原生外观失败在设置就近反馈", async () => {
  const current = platform();
  configurePlatform({
    ...current,
    kind: "desktop",
    host: {
      ...current.host,
      set_window_appearance: vi
        .fn()
        .mockRejectedValue(new Error("native appearance failed")),
    },
  });
  vi.spyOn(console, "warn").mockImplementation(() => undefined);
  await act(async () => {
    settings();
  });
  expect(screen.getByRole("alert")).toHaveTextContent("未能应用窗口外观");
});
