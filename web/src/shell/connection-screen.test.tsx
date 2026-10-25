import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { ConnectionScreen } from "./connection-screen";
import { requestSession } from "../platform/session";
import type * as SessionModule from "../platform/session";

vi.mock("../platform/session", async (importOriginal) => ({
  ...(await importOriginal<typeof SessionModule>()),
  requestSession: vi.fn(),
}));
let root: Root;
let container: HTMLDivElement;
async function renderScreen(onConnected = vi.fn()) {
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () =>
    root.render(<ConnectionScreen onConnected={onConnected} />),
  );
}
async function retry() {
  await act(async () =>
    container
      .querySelector("form")
      ?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
  );
}
afterEach(() => {
  act(() => root?.unmount());
  container?.remove();
  vi.clearAllMocks();
});

it("连接失败后只提供重试，无需输入配对码", async () => {
  const session = {
    protocolVersion: 1,
    csrfToken: "csrf",
    expiresAt: Date.now() + 1000,
    capabilities: { workspace: true, git: true, terminal: true },
  };
  vi.mocked(requestSession).mockResolvedValue(session);
  const connected = vi.fn();
  await renderScreen(connected);
  expect(container.querySelector("input")).toBeNull();
  expect(container.textContent).not.toContain("断开本机连接");
  expect(container.querySelector("button")?.textContent).toBe("重试连接");
  await retry();
  expect(connected).toHaveBeenCalledWith(session);
  expect(requestSession).toHaveBeenCalledWith();
});

it("重试失败保留错误，并允许再次连接", async () => {
  vi.mocked(requestSession).mockRejectedValue(new TypeError("offline"));
  await renderScreen();
  await retry();
  expect(container.querySelector('[role="alert"]')?.textContent).toContain(
    "无法连接本机服务",
  );
  expect(container.querySelector("button")?.disabled).toBe(false);
});
