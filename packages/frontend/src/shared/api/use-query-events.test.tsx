import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { GlobalEventObserver, GlobalEventFrame } from "./global-events";
import { useQueryEvents } from "./use-query-events";
const { listeners } = vi.hoisted(() => ({
  listeners: new Set<GlobalEventObserver>(),
}));
vi.mock("./global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    listeners.add(observer);
    return () => listeners.delete(observer);
  },
}));
afterEach(() => {
  listeners.clear();
});
function frame(
  type = "execution.completed",
  transient = false,
): GlobalEventFrame {
  return {
    protocolVersion: 1,
    epoch: "epoch",
    connectionId: "connection",
    deliverySequence: 1,
    topic: "chat",
    type: "chat.event",
    resource: { kind: "chat", id: "chat-1" },
    eventId: "event",
    transient,
    payload: { type },
  };
}
it("空闲不请求，真实语义事件合并刷新且不失效其他资源，隐藏时仅标记陈旧", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const read = vi.fn(async () => "最新"),
    other = vi.fn(async () => "其他");
  function Content({ active }: { active: boolean }) {
    const query = useQuery({
      queryKey: ["chat", "chat-1"],
      queryFn: read,
      enabled: active,
      staleTime: Infinity,
    });
    useQuery({
      queryKey: ["chat", "chat-2"],
      queryFn: other,
      staleTime: Infinity,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "chat-1" },
      queryKeys: [["chat", "chat-1"]],
      active,
      eventTypes: ["execution."],
    });
    return <p>{query.data}</p>;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content active />
    </QueryClientProvider>,
  );
  await screen.findByText("最新");
  act(() => {
    for (const listener of listeners) {
      listener.onEvent(frame("message.updated", true));
      listener.onEvent(frame("message.created"));
    }
  });
  await new Promise((resolve) => setTimeout(resolve, 300));
  expect(read).toHaveBeenCalledTimes(1);
  act(() => {
    for (const listener of listeners) {
      listener.onEvent(frame());
      listener.onEvent(frame());
    }
  });
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  expect(other).toHaveBeenCalledTimes(1);
  view.rerender(
    <QueryClientProvider client={client}>
      <Content active={false} />
    </QueryClientProvider>,
  );
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  await new Promise((resolve) => setTimeout(resolve, 300));
  expect(read).toHaveBeenCalledTimes(2);
  expect(client.getQueryState(["chat", "chat-1"])?.isInvalidated).toBe(true);
  view.rerender(
    <QueryClientProvider client={client}>
      <Content active />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(3));
  view.unmount();
  expect(listeners.size).toBe(0);
  client.clear();
});

it("ready无视业务类型过滤，取消挂起旧GET并读取订阅后的新事实", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  let releaseOld: (value: string) => void = () => {};
  const read = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          releaseOld = resolve;
        }),
    )
    .mockResolvedValue("新事实");
  function Content() {
    const query = useQuery({ queryKey: ["chat", "ready"], queryFn: read });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "ready" },
      queryKeys: [["chat", "ready"]],
      eventTypes: ["execution."],
    });
    return <p>{query.data}</p>;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  act(() => {
    for (const listener of listeners)
      listener.onEvent({
        ...frame(),
        type: "chat.ready",
        payload: { kind: "chat", id: "ready" },
      });
  });
  await screen.findByText("新事实");
  expect(read).toHaveBeenCalledTimes(2);
  await act(async () => {
    releaseOld("旧事实");
    await Promise.resolve();
  });
  expect(screen.getByText("新事实")).toBeInTheDocument();
  expect(client.getQueryData(["chat", "ready"])).toBe("新事实");
  view.unmount();
  client.clear();
});

it.each([false, true])(
  "changed 不被挂起的初次读取吞掉（ready 后读取：%s）",
  async (afterReady) => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const releases: ((value: string) => void)[] = [];
    const read = vi.fn(
      () => new Promise<string>((resolve) => releases.push(resolve)),
    );
    function Content() {
      const query = useQuery({ queryKey: ["chat", "pending"], queryFn: read });
      useQueryEvents({
        topic: "chat",
        resource: { kind: "chat", id: "pending" },
        queryKeys: [["chat", "pending"]],
      });
      return <p>{query.data}</p>;
    }
    const view = render(
      <QueryClientProvider client={client}>
        <Content />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
    if (afterReady) {
      act(() => {
        for (const listener of listeners)
          listener.onEvent({ ...frame(), type: "chat.ready", payload: {} });
      });
      await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
    }
    const oldReadCount = read.mock.calls.length;
    act(() => {
      for (const listener of listeners) listener.onEvent(frame());
    });
    await waitFor(() => expect(read).toHaveBeenCalledTimes(oldReadCount + 1));
    await act(async () => {
      for (const release of releases.slice(0, oldReadCount)) release("旧事实");
      releases.at(-1)?.("新事实");
    });
    expect(await screen.findByText("新事实")).toBeInTheDocument();
    expect(client.getQueryData(["chat", "pending"])).toBe("新事实");
    view.unmount();
    client.clear();
  },
);

it("隐藏期间旧初始 GET 返回清除陈旧标记，重新可见仍按 dirty 对账", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  let releaseOld: (value: string) => void = () => {};
  const read = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          releaseOld = resolve;
        }),
    )
    .mockResolvedValue("新事实");
  function Content({ active }: { active: boolean }) {
    const query = useQuery({
      queryKey: ["chat", "hidden-pending"],
      queryFn: read,
      enabled: active,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "hidden-pending" },
      queryKeys: [["chat", "hidden-pending"]],
      active,
    });
    return <p>{query.data}</p>;
  }
  const content = (active: boolean) => (
    <QueryClientProvider client={client}>
      <Content active={active} />
    </QueryClientProvider>
  );
  const view = render(content(true));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  view.rerender(content(false));
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  await waitFor(() =>
    expect(
      client.getQueryState(["chat", "hidden-pending"])?.isInvalidated,
    ).toBe(true),
  );
  expect(read).toHaveBeenCalledTimes(1);
  await act(async () => {
    releaseOld("旧事实");
  });
  expect(client.getQueryState(["chat", "hidden-pending"])?.isInvalidated).toBe(
    false,
  );
  view.rerender(content(true));
  await screen.findByText("新事实");
  expect(read).toHaveBeenCalledTimes(2);
  view.unmount();
  client.clear();
});

it("取消等待期间卸载不会再读取或留下事件监听", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const read = vi.fn(async () => "已有事实");
  let releaseCancellation: () => void = () => {};
  function Content() {
    useQuery({ queryKey: ["chat", "unmount-pending"], queryFn: read });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "unmount-pending" },
      queryKeys: [["chat", "unmount-pending"]],
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  const cancel = vi.spyOn(client, "cancelQueries").mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        releaseCancellation = resolve;
      }),
  );
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  await waitFor(() => expect(cancel).toHaveBeenCalledTimes(1));
  view.unmount();
  await act(async () => {
    releaseCancellation();
  });
  expect(read).toHaveBeenCalledTimes(1);
  expect(listeners.size).toBe(0);
  cancel.mockRestore();
  client.clear();
});

it("取消等待期间新事件属于下一批，不被前一批消耗", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const read = vi.fn(async () => "事实");
  let releaseCancellation: () => void = () => {};
  function Content() {
    useQuery({ queryKey: ["chat", "queued-change"], queryFn: read });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "queued-change" },
      queryKeys: [["chat", "queued-change"]],
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  const cancel = vi.spyOn(client, "cancelQueries").mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        releaseCancellation = resolve;
      }),
  );
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  await waitFor(() => expect(cancel).toHaveBeenCalledTimes(1));
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  await act(async () => {
    releaseCancellation();
  });
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(3));
  view.unmount();
  cancel.mockRestore();
  client.clear();
});
