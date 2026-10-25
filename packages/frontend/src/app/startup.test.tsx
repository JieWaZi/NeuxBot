import { readFileSync } from "node:fs";
import { act, fireEvent, screen } from "@testing-library/react";
import { createRoot } from "react-dom/client";
import type * as ReactDOMClient from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";

const connection = vi.hoisted(() => ({
  resolve: vi.fn<(value?: unknown) => void>(),
  reject: vi.fn<(error: unknown) => void>(),
  connect: vi.fn<() => Promise<unknown>>(),
  workspaceLoaded: vi.fn(),
  prepare: vi.fn<() => Promise<void>>(),
  prepared: vi.fn<() => void>(),
  assets: vi.fn<() => Promise<void>>(),
  assetsPrepared: vi.fn<() => void>(),
}));
vi.mock("@/shared/api/runtime", () => ({ initializeApi: connection.connect }));
vi.mock("@/app/welcome/startup-gate", () => ({
  StartupGate: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/app/welcome/welcome-brand", () => ({
  prepareWelcomeAssets: connection.assets,
  WelcomeBrand: ({
    children,
    onIntroComplete,
  }: {
    children?: React.ReactNode;
    onIntroComplete?: () => void;
  }) => (
    <main>
      <button onClick={onIntroComplete}>完成欢迎动画</button>
      {children}
    </main>
  ),
}));
vi.mock("@/app/welcome/elastic-logo", () => ({
  default: () => <span>NeuxBot</span>,
}));
vi.mock("@/app/router", () => {
  connection.workspaceLoaded();
  return { router: {} };
});
vi.mock("react-router-dom", () => ({
  RouterProvider: () => <div>工作台已就绪</div>,
}));
vi.mock("@/app/providers", () => ({
  AppProviders: ({ children }: { children: React.ReactNode }) => children,
  appQueryClient: { prefetchQuery: connection.prepare },
}));
vi.mock("@/app/error-boundary", () => ({
  ErrorBoundary: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("react-dom/client", async (importOriginal) => {
  const original = await importOriginal<typeof ReactDOMClient>();
  return { ...original, createRoot: vi.fn(original.createRoot) };
});

afterEach(() => {
  for (const result of vi.mocked(createRoot).mock.results) {
    if (result.type === "return") act(() => result.value.unmount());
  }
  vi.mocked(createRoot).mockClear();
  document.body.innerHTML = "";
  localStorage.clear();
});

it("入口脚本执行前已有加载状态，首帧主题读取当前偏好", () => {
  const html = readFileSync("../../desktop/index.html", "utf8");
  const parsed = new DOMParser().parseFromString(html, "text/html");
  expect(parsed.querySelector('[aria-label="NeuxBot"]')).not.toBeNull();
  expect(parsed.querySelector("p")).toBeNull();
  expect(parsed.querySelector('link[rel="stylesheet"]')).not.toBeNull();
  localStorage.setItem("ally.ui.theme", "dark");
  const themeScript = parsed.querySelector("script:not([src])")?.textContent;
  if (!themeScript) throw new Error("缺少首帧主题脚本");
  window.eval(themeScript);
  expect(document.documentElement.dataset.theme).toBe("dark");
  expect(parsed.querySelector(".welcome-eyes")).toBeNull();
  expect(
    parsed.querySelector(".welcome-character")?.getAttribute("data-ready"),
  ).toBe("false");
  expect(
    parsed
      .querySelector('link[as="image"][imagesrcset]')
      ?.getAttribute("imagesrcset"),
  ).toContain("plush-512.png 512w");
});

it("等待服务时显示进度，失败显示原因，重试成功后自动进入工作台", async () => {
  connection.assets.mockResolvedValue(undefined);
  connection.assets.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        connection.assetsPrepared.mockImplementation(resolve);
      }),
  );
  connection.prepare.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        connection.prepared.mockImplementation(resolve);
      }),
  );
  connection.connect.mockImplementation(
    () =>
      new Promise((resolve, reject) => {
        connection.resolve.mockImplementation(resolve);
        connection.reject.mockImplementation(reject);
      }),
  );
  localStorage.setItem("ally.ui.theme", "light");
  document.body.innerHTML = '<div id="root"></div>';
  await act(async () => {
    (await import("../start")).startApplication();
  });
  expect(connection.connect).toHaveBeenCalledTimes(1);
  expect(connection.assets).toHaveBeenCalledOnce();
  expect(connection.workspaceLoaded).not.toHaveBeenCalled();
  await act(async () => connection.assetsPrepared());
  await vi.waitFor(() =>
    expect(connection.workspaceLoaded).toHaveBeenCalledTimes(1),
  );
  expect(screen.queryByText("工作台已就绪")).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toBeVisible();
  expect(screen.getByText("正在连接本地服务")).toHaveClass("sr-only");
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();

  await act(async () => connection.reject("等待本地服务超时"));
  expect(screen.getByRole("alert")).toHaveTextContent("等待本地服务超时");
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  fireEvent.click(await screen.findByRole("button", { name: "重试启动" }));
  expect(connection.connect).toHaveBeenCalledTimes(2);
  expect(screen.getByRole("status")).toBeVisible();
  await act(async () => connection.resolve());
  await vi.waitFor(() => expect(connection.prepare).toHaveBeenCalledOnce());
  expect(screen.queryByText("工作台已就绪")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "完成欢迎动画" }));
  expect(screen.queryByText("工作台已就绪")).not.toBeInTheDocument();
  await act(async () => connection.prepared());
  expect(await screen.findByText("工作台已就绪")).toBeVisible();
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
});
