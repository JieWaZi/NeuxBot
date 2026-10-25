import { configurePlatform, platform } from "@/shared/platform/provider";
import type { ApplicationUpdateService } from "@neuxbot/platform-contracts/application-update";
function configureUpdates(value: Partial<ApplicationUpdateService>) {
  configurePlatform({
    ...platform(),
    kind: "desktop",
    applicationUpdate: {
      application_update_state: async () => {
        throw new Error("not configured");
      },
      check_application_update: async () => {
        throw new Error("not configured");
      },
      download_application_update: async () => {
        throw new Error("not configured");
      },
      open_application_installer: async () => {
        throw new Error("not configured");
      },
      onApplicationUpdate: () => () => undefined,
      ...value,
    },
  });
}
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import type { ApplicationUpdateState } from "@/shared/platform/application-update";
import { AboutSoftwareSection } from "./about-software-section";
import "@/shared/i18n";

afterEach(() => vi.unstubAllGlobals());

it("下载完成后仍能检查后续版本并提供新版下载入口", async () => {
  const downloaded: ApplicationUpdateState = {
    revision: 1,
    currentVersion: "0.1.0",
    platform: "darwin",
    arch: "arm64",
    status: "downloaded",
    downloadedBytes: 100,
    release: {
      version: "0.2.0",
      notes: "更新",
      publishedAt: "2026-10-16T00:00:00Z",
      downloadSize: 100,
      url: "https://github.com/JieWaZi/NeuxBot/releases/tag/v0.2.0",
    },
  };
  const check = vi.fn(async (): Promise<ApplicationUpdateState> => ({
    ...downloaded,
    revision: 2,
    status: "available",
    downloadedBytes: 0,
    release: {
      version: "0.3.0",
      notes: "更新",
      publishedAt: "2026-10-16T00:00:00Z",
      downloadSize: 100,
      url: "https://github.com/JieWaZi/NeuxBot/releases/tag/v0.3.0",
    },
  }));
  configureUpdates({
    application_update_state: async () => downloaded,
    onApplicationUpdate: () => () => undefined,
    check_application_update: check,
  });
  const client = new QueryClient();
  const view = render(
    <QueryClientProvider client={client}>
      <AboutSoftwareSection />
    </QueryClientProvider>,
  );
  try {
    expect(
      await screen.findByRole("button", { name: "打开安装包" }),
    ).toBeInTheDocument();
    const button = screen.getByRole("button", { name: "检查更新" });
    expect(button).toBeEnabled();
    await userEvent.setup().click(button);
    expect(
      await screen.findByRole("heading", { name: "新版本 v0.3.0" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下载新版本" })).toBeEnabled();
    expect(
      screen.queryByRole("button", { name: "打开安装包" }),
    ).not.toBeInTheDocument();
  } finally {
    view.unmount();
    client.clear();
  }
});

it("关于软件展示真实版本，检查后下载、显示进度与打开安装包，不把下载视为已安装", async () => {
  const initial: ApplicationUpdateState = {
    revision: 0,
    currentVersion: "0.1.0",
    platform: "darwin",
    arch: "arm64",
    status: "idle",
    downloadedBytes: 0,
  };
  const available: ApplicationUpdateState = {
    ...initial,
    revision: 2,
    status: "available",
    release: {
      version: "0.2.0",
      notes: "修复界面并改进伙伴。",
      publishedAt: "2026-10-16T00:00:00Z",
      downloadSize: 100,
      url: "https://github.com/JieWaZi/NeuxBot/releases/tag/v0.2.0",
    },
  };
  let listener: ((state: ApplicationUpdateState) => void) | undefined;
  const check = vi.fn(async () => available);
  const download = vi.fn(async (): Promise<ApplicationUpdateState> => ({
    ...available,
    revision: 3,
    status: "downloading",
  }));
  const openInstaller = vi.fn(async (): Promise<ApplicationUpdateState> => ({
    ...available,
    revision: 5,
    status: "downloaded",
  }));
  configureUpdates({
    application_update_state: async () => initial,
    onApplicationUpdate: (
      callback: (state: ApplicationUpdateState) => void,
    ) => {
      listener = callback;
      return () => undefined;
    },
    check_application_update: check,
    download_application_update: download,
    open_application_installer: openInstaller,
  });
  const client = new QueryClient();
  const { unmount } = render(
    <QueryClientProvider client={client}>
      <AboutSoftwareSection />
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  expect(await screen.findByText("v0.1.0")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "检查更新" }));
  expect(
    await screen.findByRole("heading", { name: "新版本 v0.2.0" }),
  ).toBeInTheDocument();
  expect(screen.getByText("修复界面并改进伙伴。")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "下载新版本" }));
  expect(download).toHaveBeenCalledOnce();
  act(() =>
    listener?.({
      ...available,
      revision: 4,
      status: "downloading",
      downloadedBytes: 60,
    }),
  );
  await waitFor(() =>
    expect(screen.getByRole("progressbar")).toHaveAttribute("value", "60"),
  );
  act(() =>
    listener?.({
      ...available,
      revision: 5,
      status: "downloaded",
      downloadedBytes: 100,
    }),
  );
  expect(
    await screen.findByText("安装包已下载并通过校验，请手动安装后重新启动。"),
  ).toBeInTheDocument();
  expect(screen.getByText("v0.1.0")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "打开安装包" }));
  expect(openInstaller).toHaveBeenCalledOnce();
  unmount();
  client.clear();
});

it("Web 展示部署更新说明并禁用桌面安装操作", async () => {
  const client = new QueryClient();
  const view = render(
    <QueryClientProvider client={client}>
      <AboutSoftwareSection />
    </QueryClientProvider>,
  );
  try {
    expect(
      await screen.findByText(/网页版不提供桌面安装更新/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "检查更新" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "下载新版本" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "打开安装包" }),
    ).not.toBeInTheDocument();
  } finally {
    view.unmount();
    client.clear();
  }
});
