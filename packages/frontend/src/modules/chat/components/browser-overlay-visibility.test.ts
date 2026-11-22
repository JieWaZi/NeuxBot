import { act, render, renderHook, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import {
  NativeOverlayPresence,
  observeNativeOverlayRegions,
} from "@/shared/ui/native-overlay";
import { Toaster, toast } from "@/shared/ui/sonner";
import { useBrowserOverlayVisible } from "./browser-overlay-visibility";

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

afterEach(() => toast.dismiss());

describe("native browser overlay visibility", () => {
  it("tracks registered overlays until every overlay is closed", async () => {
    const { result, unmount } = renderHook(useBrowserOverlayVisible);
    expect(result.current).toBe(false);
    const dialog = render(createElement(NativeOverlayPresence));
    const menu = render(createElement(NativeOverlayPresence));
    await waitFor(() => expect(result.current).toBe(true));
    dialog.unmount();
    expect(result.current).toBe(true);
    menu.rerender(createElement(NativeOverlayPresence, { active: false }));
    await waitFor(() => expect(result.current).toBe(false));
    menu.rerender(createElement(NativeOverlayPresence));
    await waitFor(() => expect(result.current).toBe(true));
    menu.unmount();
    await waitFor(() => expect(result.current).toBe(false));
    unmount();
  });

  it("registers only the visible notification region for native previews", async () => {
    let regions: HTMLElement[] = [];
    const stop = observeNativeOverlayRegions((elements) => {
      regions = [...elements];
    });
    const { result, unmount } = renderHook(useBrowserOverlayVisible);
    const toaster = render(createElement(Toaster));
    try {
      act(() => {
        toast.manual("有可用更新");
      });
      expect(regions).toEqual([
        toaster.getByRole("region", { name: "应用通知" }),
      ]);
      expect(result.current).toBe(false);

      act(() => toast.dismiss());
      expect(regions).toEqual([]);
    } finally {
      toaster.unmount();
      unmount();
      stop();
    }
  });
});
