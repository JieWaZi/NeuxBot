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
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  useApplicationUpdate,
  type ApplicationUpdateState,
} from "./application-update";

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
    url: "https://github.com/JieWaZi/NeuxBot/releases/tag/v0.2.0",
    notes: "新版",
    publishedAt: "2026-10-16T00:00:00Z",
    downloadSize: 100,
  },
};

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("桌面软件更新状态", () => {
  it("多个消费者共享监听，迟到查询与操作回执不能覆盖新进度，卸载后释放", async () => {
    let resolveSnapshot: ((state: ApplicationUpdateState) => void) | undefined;
    let listener: ((state: ApplicationUpdateState) => void) | undefined;
    const stop = vi.fn();
    const onApplicationUpdate = vi.fn((callback) => {
      listener = callback;
      return stop;
    });
    const application_update_state = vi.fn(
      () =>
        new Promise<ApplicationUpdateState>((resolve) => {
          resolveSnapshot = resolve;
        }),
    );
    configureUpdates({
      application_update_state,
      onApplicationUpdate,
      check_application_update: vi.fn(async () => available),
      download_application_update: vi.fn(async () => available),
      open_application_installer: vi.fn(async () => available),
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const hook = renderHook(
      () => [useApplicationUpdate(), useApplicationUpdate()],
      { wrapper: wrapper(client) },
    );
    await waitFor(() => expect(onApplicationUpdate).toHaveBeenCalledTimes(1));
    act(() =>
      listener?.({
        ...available,
        revision: 5,
        status: "downloading",
        downloadedBytes: 50,
      }),
    );
    await act(async () => {
      resolveSnapshot?.(initial);
    });
    await waitFor(() =>
      expect(hook.result.current[0]?.state?.downloadedBytes).toBe(50),
    );
    act(() => hook.result.current[0]?.download());
    await waitFor(() =>
      expect(
        platform().applicationUpdate?.download_application_update,
      ).toHaveBeenCalledOnce(),
    );
    await waitFor(() => expect(hook.result.current[0]?.busy).toBe(false));
    expect(hook.result.current[1]?.state?.revision).toBe(5);
    hook.unmount();
    expect(stop).toHaveBeenCalledTimes(1);
    client.clear();
  });

  it("重新接入补读主进程快照，恢复监听空窗中完成的下载", async () => {
    let state = initial;
    const application_update_state = vi.fn(async () => state);
    configureUpdates({
      application_update_state,
      onApplicationUpdate: () => () => undefined,
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const first = renderHook(useApplicationUpdate, {
      wrapper: wrapper(client),
    });
    await waitFor(() =>
      expect(first.result.current.state?.status).toBe("idle"),
    );
    first.unmount();
    state = {
      ...available,
      revision: 8,
      status: "downloaded",
      downloadedBytes: 100,
    };
    const second = renderHook(useApplicationUpdate, {
      wrapper: wrapper(client),
    });
    await waitFor(() =>
      expect(second.result.current.state?.status).toBe("downloaded"),
    );
    second.unmount();
    client.clear();
  });

  it("Web 使用明确的不可更新状态，不探测 Electron 全局对象", async () => {
    const desktopRead = vi.fn();
    vi.stubGlobal("allyDesktop", { application_update_state: desktopRead });
    const client = new QueryClient();
    const hook = renderHook(useApplicationUpdate, { wrapper: wrapper(client) });
    await waitFor(() =>
      expect(hook.result.current.state?.status).toBe("unavailable"),
    );
    expect(hook.result.current.state?.currentVersion).toBeNull();
    expect(hook.result.current.state?.unavailableReason).toBe("browser");
    expect(desktopRead).not.toHaveBeenCalled();
    hook.unmount();
    client.clear();
  });

  it("重试读取主进程状态时清除已经恢复的调用错误", async () => {
    configureUpdates({
      application_update_state: async () => initial,
      onApplicationUpdate: () => () => undefined,
      check_application_update: async () => {
        throw new Error("bridge unavailable");
      },
    });
    const client = new QueryClient();
    const hook = renderHook(useApplicationUpdate, { wrapper: wrapper(client) });
    await waitFor(() => expect(hook.result.current.state?.status).toBe("idle"));
    act(() => hook.result.current.check());
    await waitFor(() =>
      expect(hook.result.current.error).toBeInstanceOf(Error),
    );
    act(() => hook.result.current.retry());
    await waitFor(() => expect(hook.result.current.error).toBeNull());
    hook.unmount();
    client.clear();
  });
});
