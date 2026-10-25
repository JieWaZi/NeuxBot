import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { WorkspaceResourceChangeEvent } from "@neuxbot/platform-contracts/resources";
import {
  subscribeWorkspaceEvents,
  useWorkspaceQueryEvents,
} from "./workspace-events";
import "@/shared/i18n";
const { open, observe, registry } = vi.hoisted(() => ({
  open: vi.fn(async () => ({ id: "scope", root: "/workspace" })),
  observe: vi.fn(),
  registry: {
    callbacks: new Map<string, (event: WorkspaceResourceChangeEvent) => void>(),
    signals: new Map<string, AbortSignal>(),
    recovery: undefined as (() => void) | undefined,
  },
}));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: (listener: () => void) => {
    registry.recovery = listener;
    return () => {
      if (registry.recovery === listener) registry.recovery = undefined;
    };
  },
}));
vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => true,
  hasPlatformService: () => true,
  openWorkspaceResourceScope: open,
  observeWorkspaceResource: observe,
}));
beforeEach(() => {
  open.mockClear();
  observe.mockReset();
  registry.callbacks.clear();
  registry.signals.clear();
  registry.recovery = undefined;
  observe.mockImplementation(
    async (
      _scope: string,
      _path: string,
      callback: (event: WorkspaceResourceChangeEvent) => void,
      signal: AbortSignal,
      root: string,
    ) => {
      registry.callbacks.set(root, callback);
      registry.signals.set(root, signal);
      callback({
        subscriptionId: root,
        sequence: 0,
        resource: null,
        state: "ready",
      });
      return async () => {};
    },
  );
});
it("相同授权根共享 IPC 订阅，最后一个消费者退出才取消观察", async () => {
  const first = vi.fn();
  const second = vi.fn();
  const stopA = subscribeWorkspaceEvents("chat", "/workspace", first);
  const stopB = subscribeWorkspaceEvents("chat", "/workspace", second);
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(1));
  expect(first).toHaveBeenCalled();
  expect(second).toHaveBeenCalled();
  stopA();
  expect(registry.signals.get("/workspace")?.aborted).toBe(false);
  stopB();
  expect(registry.signals.get("/workspace")?.aborted).toBe(true);
});
it("变化只失效对应根，等待没有周期查询；失效可重试且隐藏释放观察", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const request = vi.fn(async () => "files");
  client.setQueryData(["workspace-directory", "chat", "/other", ""], "other");
  function Consumer() {
    const events = useWorkspaceQueryEvents(
      "chat",
      ["/workspace"],
      ["workspace-directory"],
    );
    useQuery({
      queryKey: ["workspace-directory", "chat", "/workspace", ""],
      queryFn: request,
    });
    return (
      <>
        <span>{events.error?.message}</span>
        <button onClick={events.retry}>重连</button>
      </>
    );
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Consumer />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(request).toHaveBeenCalled());
  await waitFor(() => expect(client.isFetching()).toBe(0));
  const initial = request.mock.calls.length;
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 80));
  });
  expect(request).toHaveBeenCalledTimes(initial);
  act(() =>
    registry.callbacks.get("/workspace")?.({
      subscriptionId: "/workspace",
      sequence: 1,
      resource: null,
      state: "changed",
    }),
  );
  await waitFor(() => expect(request).toHaveBeenCalledTimes(initial + 1));
  expect(
    client.getQueryState(["workspace-directory", "chat", "/other", ""])
      ?.isInvalidated,
  ).toBe(false);
  act(() =>
    registry.callbacks.get("/workspace")?.({
      subscriptionId: "/workspace",
      sequence: 2,
      resource: null,
      state: "failed",
    }),
  );
  expect(
    screen.getByText("实时更新暂不可用，请重试或重新打开当前页面。"),
  ).toBeVisible();
  act(() => screen.getByRole("button", { name: "重连" }).click());
  await waitFor(() => expect(observe.mock.calls.length).toBeGreaterThan(1));
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  await waitFor(() =>
    expect(registry.signals.get("/workspace")?.aborted).toBe(true),
  );
  vi.restoreAllMocks();
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  await waitFor(() =>
    expect(registry.signals.get("/workspace")?.aborted).toBe(false),
  );
  view.unmount();
  expect(registry.signals.get("/workspace")?.aborted).toBe(true);
});

it("首次读取期间到达变化会在结束后再对账，多视图只刷新同一 Query 一次", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  let finish: ((value: string) => void) | undefined;
  const request = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue("current");
  function Consumer() {
    useWorkspaceQueryEvents("chat", ["/workspace"], ["workspace-directory"]);
    const query = useQuery({
      queryKey: ["workspace-directory", "chat", "/workspace", ""],
      queryFn: request,
    });
    return <span>{query.data}</span>;
  }
  render(
    <QueryClientProvider client={client}>
      <Consumer />
      <Consumer />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(finish).toBeDefined());
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(1));
  act(() =>
    registry.callbacks.get("/workspace")?.({
      subscriptionId: "/workspace",
      sequence: 1,
      resource: null,
      state: "changed",
    }),
  );
  await act(async () => {
    finish?.("old");
  });
  await waitFor(() => expect(screen.getAllByText("current")).toHaveLength(2));
  expect(request).toHaveBeenCalledTimes(2);
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 80));
  });
  expect(request).toHaveBeenCalledTimes(2);
});

it("Core 物理连接恢复后重新核验并注册原生观察，最后消费者退出时释放恢复监听", async () => {
  const stop = subscribeWorkspaceEvents("chat", "/workspace", vi.fn());
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(1));
  const previous = registry.signals.get("/workspace");
  registry.recovery?.();
  expect(previous?.aborted).toBe(true);
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(2));
  expect(open).toHaveBeenCalledTimes(2);
  expect(registry.signals.get("/workspace")?.aborted).toBe(false);
  stop();
  expect(registry.signals.get("/workspace")?.aborted).toBe(true);
  expect(registry.recovery).toBeUndefined();
});
