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

it("跨任务到达的密集文件事件合并为一次刷新，空闲后不继续请求", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const request = vi.fn(async () => "status");
  function Consumer() {
    useWorkspaceQueryEvents("chat", ["/workspace"], ["workspace-directory"]);
    useQuery({
      queryKey: ["workspace-directory", "chat", "/workspace", "/workspace"],
      queryFn: request,
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Consumer />
      <Consumer />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(client.isFetching()).toBe(0));
  // 等待首次订阅的 ready 对账结束后，再模拟编辑器分批写入。
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 300));
  });
  const initial = request.mock.calls.length;
  vi.useFakeTimers();
  try {
    for (let sequence = 1; sequence <= 20; sequence++) {
      await act(async () => {
        registry.callbacks.get("/workspace")?.({
          subscriptionId: "/workspace",
          sequence,
          resource: null,
          state: "changed",
        });
        await vi.advanceTimersByTimeAsync(5);
      });
    }
    expect(request).toHaveBeenCalledTimes(initial);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    expect(request).toHaveBeenCalledTimes(initial + 1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(request).toHaveBeenCalledTimes(initial + 1);
  } finally {
    view.unmount();
    client.clear();
    vi.useRealTimers();
  }
});

it("已有缓存的刷新未完成时不重发请求，期间多次变化结束后只补读一次", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const key = ["workspace-directory", "chat", "/workspace", "/workspace"];
  client.setQueryData(key, "cached");
  let finish: ((value: string) => void) | undefined;
  const request = vi.fn(
    () =>
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
  );
  function Consumer() {
    useWorkspaceQueryEvents("chat", ["/workspace"], ["workspace-directory"]);
    const query = useQuery({ queryKey: key, queryFn: request });
    return <span>{query.data}</span>;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Consumer />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
  vi.useFakeTimers();
  try {
    for (let sequence = 1; sequence <= 4; sequence++) {
      await act(async () => {
        registry.callbacks.get("/workspace")?.({
          subscriptionId: "/workspace",
          sequence,
          resource: null,
          state: "changed",
        });
        await vi.advanceTimersByTimeAsync(300);
      });
    }
    expect(request).toHaveBeenCalledTimes(1);
    await act(async () => {
      finish?.("old");
    });
    expect(request).toHaveBeenCalledTimes(2);
    await act(async () => {
      finish?.("current");
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(screen.getByText("current")).toBeVisible();
    expect(request).toHaveBeenCalledTimes(2);
  } finally {
    view.unmount();
    client.clear();
    vi.useRealTimers();
  }
});

it("Git 连续变化时任意两次自动刷新间隔至少一秒", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const starts: number[] = [];
  const request = vi.fn(async () => {
    starts.push(Date.now());
    return "git";
  });
  function Consumer() {
    useWorkspaceQueryEvents("chat", ["/workspace"], ["scoped-git-detail"]);
    useQuery({
      queryKey: ["scoped-git-detail", "chat", "/workspace"],
      queryFn: request,
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Consumer />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(1));
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 1300));
  });
  starts.length = 0;
  vi.useFakeTimers();
  try {
    for (let sequence = 1; sequence <= 60; sequence++) {
      await act(async () => {
        registry.callbacks.get("/workspace")?.({
          subscriptionId: "/workspace",
          sequence,
          resource: null,
          state: "changed",
        });
        await vi.advanceTimersByTimeAsync(50);
      });
    }
    expect(starts.length).toBeGreaterThan(0);
    expect(starts.length).toBeLessThanOrEqual(3);
    for (let index = 1; index < starts.length; index++) {
      expect(
        (starts[index] ?? 0) - (starts[index - 1] ?? 0),
      ).toBeGreaterThanOrEqual(1000);
    }
  } finally {
    view.unmount();
    client.clear();
    vi.useRealTimers();
  }
});

it("Git 慢请求期间不并发，结束后的补读同样等待至少一秒", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  let finish: ((value: string) => void) | undefined;
  const request = vi.fn(
    () =>
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
  );
  function Consumer() {
    useWorkspaceQueryEvents("chat", ["/workspace"], ["scoped-git-detail"]);
    useQuery({
      queryKey: ["scoped-git-detail", "chat", "/workspace"],
      queryFn: request,
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Consumer />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(observe).toHaveBeenCalledTimes(1));
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 300));
  });
  vi.useFakeTimers();
  try {
    for (let sequence = 1; sequence <= 4; sequence++) {
      await act(async () => {
        registry.callbacks.get("/workspace")?.({
          subscriptionId: "/workspace",
          sequence,
          resource: null,
          state: "changed",
        });
        await vi.advanceTimersByTimeAsync(300);
      });
    }
    expect(request).toHaveBeenCalledTimes(1);
    await act(async () => {
      finish?.("old");
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(999);
    });
    expect(request).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(request).toHaveBeenCalledTimes(2);
    await act(async () => {
      finish?.("current");
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(request).toHaveBeenCalledTimes(2);
  } finally {
    view.unmount();
    client.clear();
    vi.useRealTimers();
  }
});
