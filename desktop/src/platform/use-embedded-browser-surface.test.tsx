import { render, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { useEmbeddedBrowserSurface } from "./use-embedded-browser-surface";

const desktop = vi.hoisted(() => ({
  prepare: vi.fn().mockResolvedValue("temporary"),
}));
vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => true,
  prepareBrowserWebview: desktop.prepare,
}));
beforeEach(() => vi.clearAllMocks());

function Browser({
  chatId,
  root = "",
  active = true,
}: {
  chatId?: string;
  root?: string;
  active?: boolean;
}) {
  const { hostRef } = useEmbeddedBrowserSurface({
    active,
    enabled: true,
    url: "https://example.com",
    workspaceRoot: root,
    chatId,
    onURLChange: vi.fn(),
  });
  return <div ref={hostRef} />;
}

it("无本地范围的网页在首次接纳和隐藏恢复后沿用访客实例，关闭才释放", async () => {
  const view = render(<Browser />);
  await waitFor(() =>
    expect(view.container.querySelector("webview")).not.toBeNull(),
  );
  const guest = view.container.querySelector("webview");
  view.rerender(<Browser chatId="accepted" active={false} />);
  expect(view.container.querySelector("webview")).toBe(guest);
  expect(guest).toHaveStyle({ visibility: "hidden" });
  view.rerender(<Browser chatId="accepted" />);
  expect(view.container.querySelector("webview")).toBe(guest);
  expect(desktop.prepare).toHaveBeenCalledTimes(1);
  view.unmount();
  expect(guest?.isConnected).toBe(false);
});

it("有本地范围时按真实 Chat 重新校验访客分区", async () => {
  const view = render(<Browser chatId="a" root="/allowed" />);
  await waitFor(() =>
    expect(view.container.querySelector("webview")).not.toBeNull(),
  );
  const guest = view.container.querySelector("webview");
  view.rerender(<Browser chatId="b" root="/allowed" />);
  await waitFor(() =>
    expect(desktop.prepare).toHaveBeenCalledWith(
      "https://example.com",
      "/allowed",
      "b",
    ),
  );
  expect(view.container.querySelector("webview")).not.toBe(guest);
  expect(guest?.isConnected).toBe(false);
});
