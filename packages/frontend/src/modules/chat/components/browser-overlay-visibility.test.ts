import { act, render, renderHook, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import "@/shared/i18n";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";
import { Toaster } from "@/shared/ui/sonner";
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

  it("keeps native browser content behind visible notifications", async () => {
    const { result, unmount } = renderHook(useBrowserOverlayVisible);
    const toaster = render(createElement(Toaster));

    act(() => {
      toast.error("保存失败", { duration: Infinity });
    });

    await waitFor(() => expect(result.current).toBe(true));

    act(() => toast.dismiss());
    await waitFor(() => expect(result.current).toBe(false));
    toaster.unmount();
    unmount();
  });
});
