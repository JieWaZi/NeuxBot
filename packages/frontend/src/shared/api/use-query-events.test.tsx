import {
  InfiniteQueryObserver,
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { GlobalEventObserver, GlobalEventFrame } from "./global-events";
import { refreshQueryFromEvent, useQueryEvents } from "./use-query-events";
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

it("同一查询的多个活跃 Scene 将一次变化合并成一次读取", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const read = vi.fn(async () => "共享事实");
  function Content() {
    useQuery({ queryKey: ["shared", "multiple"], queryFn: read });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "multiple" },
      queryKeys: [["shared", "multiple"]],
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content />
      <Content />
      <Content />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  act(() => {
    const change = frame();
    for (const listener of listeners) listener.onEvent(change);
  });
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(read).toHaveBeenCalledTimes(2);
  view.unmount();
  expect(listeners.size).toBe(0);
  client.clear();
});

it("隐藏消费者先接收变化也不能重复取消活跃消费者的读取", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const read = vi.fn(async () => "共享事实");
  function Content({
    active,
    broad = false,
  }: {
    active: boolean;
    broad?: boolean;
  }) {
    useQuery({
      queryKey: ["shared", "visibility"],
      queryFn: read,
      enabled: active,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "visibility" },
      queryKeys: [broad ? ["shared"] : ["shared", "visibility"]],
      active,
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content active={false} broad />
      <Content active />
      <Content active />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  const cancel = vi.spyOn(client, "cancelQueries");
  act(() => {
    const change = frame();
    for (const listener of listeners) listener.onEvent(change);
  });
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(read).toHaveBeenCalledTimes(2);
  expect(cancel).toHaveBeenCalledTimes(1);
  view.unmount();
  cancel.mockRestore();
  client.clear();
});

it("多个消费者的重连对账只发起一个刷新，卸载最后消费者后释放待处理刷新", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const read = vi.fn(async () => "恢复事实");
  function Content() {
    useQuery({ queryKey: ["shared", "resync"], queryFn: read });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "resync" },
      queryKeys: [["shared", "resync"]],
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content />
      <Content />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  act(() => {
    for (const listener of listeners) listener.onResync?.("reconnected");
  });
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(read).toHaveBeenCalledTimes(2);
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  view.unmount();
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(read).toHaveBeenCalledTimes(2);
  expect(listeners.size).toBe(0);
  client.clear();
});

it("隐藏消费者恢复后复用 Query 已自动发起的新读取", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const read = vi.fn(async () => "事实");
  function Content({ active }: { active: boolean }) {
    useQuery({
      queryKey: ["shared", "automatic"],
      queryFn: read,
      enabled: active,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "automatic" },
      queryKeys: [["shared", "automatic"]],
      active,
    });
    return null;
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
    expect(client.getQueryState(["shared", "automatic"])?.isInvalidated).toBe(
      true,
    ),
  );
  view.rerender(content(true));
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(read).toHaveBeenCalledTimes(2);
  view.unmount();
  client.clear();
});

it("只有隐藏消费者关心的事件不取消不关心该事件的活跃消费者请求", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  let release: (value: string) => void = () => {};
  const read = vi.fn(
    () =>
      new Promise<string>((resolve) => {
        release = resolve;
      }),
  );
  function Content({ active }: { active: boolean }) {
    useQuery({
      queryKey: ["shared", "filtered"],
      queryFn: read,
      enabled: active,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "filtered" },
      queryKeys: [["shared", "filtered"]],
      active,
      eventTypes: active ? ["plugins.changed"] : ["plugins.usage.changed"],
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content active={false} />
      <Content active />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  const cancel = vi.spyOn(client, "cancelQueries");
  act(() => {
    for (const listener of listeners)
      listener.onEvent(frame("plugins.usage.changed"));
  });
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(cancel).not.toHaveBeenCalled();
  expect(read).toHaveBeenCalledTimes(1);
  await act(async () => {
    release("完整事实");
  });
  view.unmount();
  cancel.mockRestore();
  client.clear();
});

it("初始读取挂起时多个消费者只重连一次，移除一个消费者仍保留其他消费者的刷新", async () => {
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
    .mockResolvedValue("恢复后的事实");
  function Content() {
    const query = useQuery({
      queryKey: ["shared", "pending-resync"],
      queryFn: read,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "pending-resync" },
      queryKeys: [["shared", "pending-resync"]],
    });
    return <p>{query.data}</p>;
  }
  const content = (first: boolean) => (
    <QueryClientProvider client={client}>
      {first && <Content key="first" />}
      <Content key="second" />
    </QueryClientProvider>
  );
  const view = render(content(true));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  act(() => {
    for (const listener of listeners) listener.onResync?.("reconnected");
  });
  view.rerender(content(false));
  await screen.findByText("恢复后的事实");
  expect(read).toHaveBeenCalledTimes(2);
  await act(async () => {
    releaseOld("重连前的事实");
  });
  expect(client.getQueryData(["shared", "pending-resync"])).toBe(
    "恢复后的事实",
  );
  view.unmount();
  expect(listeners.size).toBe(0);
  client.clear();
});

it("事件刷新失败保留已有事实，后续重连仍可重试", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const read = vi
    .fn()
    .mockResolvedValueOnce("已有事实")
    .mockRejectedValueOnce(new Error("暂时不可用"))
    .mockResolvedValue("恢复事实");
  function Content() {
    const query = useQuery({
      queryKey: ["shared", "error-retry"],
      queryFn: read,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "error-retry" },
      queryKeys: [["shared", "error-retry"]],
    });
    return <p>{query.data}</p>;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content />
    </QueryClientProvider>,
  );
  await screen.findByText("已有事实");
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  await waitFor(() =>
    expect(client.getQueryState(["shared", "error-retry"])?.status).toBe(
      "error",
    ),
  );
  expect(screen.getByText("已有事实")).toBeInTheDocument();
  expect(read).toHaveBeenCalledTimes(2);
  act(() => {
    for (const listener of listeners) listener.onResync?.("reconnected");
  });
  await screen.findByText("恢复事实");
  expect(read).toHaveBeenCalledTimes(3);
  view.unmount();
  client.clear();
});

it("文档隐藏期间不读取，恢复可见后多个消费者只对账一次", async () => {
  const visibility = vi.spyOn(document, "visibilityState", "get");
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const read = vi.fn(async () => "事实");
  function Content() {
    useQuery({ queryKey: ["shared", "document-hidden"], queryFn: read });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "document-hidden" },
      queryKeys: [["shared", "document-hidden"]],
    });
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Content />
      <Content />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  visibility.mockReturnValue("hidden");
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
    for (const listener of listeners) listener.onEvent(frame());
  });
  await waitFor(() =>
    expect(
      client.getQueryState(["shared", "document-hidden"])?.isInvalidated,
    ).toBe(true),
  );
  expect(read).toHaveBeenCalledTimes(1);
  visibility.mockReturnValue("visible");
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(read).toHaveBeenCalledTimes(2);
  view.unmount();
  visibility.mockRestore();
  client.clear();
});

it("恢复时自动 GET 被采纳后外部取消，无需额外事件仍自动对账", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  let releaseCanceled: (value: string) => void = () => {};
  const read = vi
    .fn()
    .mockResolvedValueOnce("已有事实")
    .mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          releaseCanceled = resolve;
        }),
    )
    .mockResolvedValue("取消后恢复的事实");
  function Content({ active }: { active: boolean }) {
    const query = useQuery({
      queryKey: ["shared", "canceled-restore"],
      queryFn: read,
      enabled: active,
    });
    useQueryEvents({
      topic: "chat",
      resource: { kind: "chat", id: "canceled-restore" },
      queryKeys: [["shared", "canceled-restore"]],
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
  await screen.findByText("已有事实");
  view.rerender(content(false));
  act(() => {
    for (const listener of listeners) listener.onEvent(frame());
  });
  await waitFor(() =>
    expect(
      client.getQueryState(["shared", "canceled-restore"])?.isInvalidated,
    ).toBe(true),
  );
  view.rerender(content(true));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  // 让恢复队列先采纳这次 GET，再单独取消；之后不派发领域或可见性事件。
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 50));
  });
  const cancel = vi.spyOn(client, "cancelQueries");
  await act(async () => {
    await client.cancelQueries({ queryKey: ["shared", "canceled-restore"] });
  });
  await screen.findByText("取消后恢复的事实");
  expect(read).toHaveBeenCalledTimes(3);
  expect(cancel).toHaveBeenCalledTimes(2);
  await act(async () => {
    releaseCanceled("已取消的旧事实");
    await new Promise((resolve) => setTimeout(resolve, 350));
  });
  expect(client.getQueryData(["shared", "canceled-restore"])).toBe(
    "取消后恢复的事实",
  );
  expect(client.getQueryState(["shared", "canceled-restore"])).toMatchObject({
    fetchStatus: "idle",
    isInvalidated: false,
  });
  expect(read).toHaveBeenCalledTimes(3);
  expect(cancel).toHaveBeenCalledTimes(2);
  view.unmount();
  expect(listeners.size).toBe(0);
  cancel.mockRestore();
  client.clear();
});

it("无observer显式命令合并一次原Query读取，完成后释放owner监听", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "unobserved"];
  const read = vi.fn(async () => "明确恢复事实");
  const execute = () =>
    client.fetchQuery({ queryKey: key, queryFn: read, staleTime: 0 });
  await Promise.all([
    refreshQueryFromEvent(client, key, execute),
    refreshQueryFromEvent(client, key, execute),
  ]);
  expect(read).toHaveBeenCalledTimes(1);
  expect(client.getQueryData(key)).toBe("明确恢复事实");
  expect(client.getQueryCache().hasListeners()).toBe(false);
  client.clear();
});

it("命令排队后开始的更早页正常提交，但不覆盖头部恢复或提前完成命令", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "pagination"];
  client.setQueryData(key, {
    pages: ["旧头部"],
    pageParams: [0],
  });
  let releasePage = () => {},
    releaseHead = () => {};
  const pageGate = new Promise<void>((resolve) => {
    releasePage = resolve;
  });
  const headGate = new Promise<void>((resolve) => {
    releaseHead = resolve;
  });
  let pageSignal: AbortSignal | undefined;
  const reads: number[] = [];
  const options = {
    queryKey: key,
    initialPageParam: 0,
    queryFn: async ({
      pageParam,
      signal,
    }: {
      pageParam: number;
      signal: AbortSignal;
    }) => {
      reads.push(pageParam);
      if (pageParam === 1) {
        pageSignal = signal;
        await pageGate;
        return "更早页";
      }
      await headGate;
      return "最新头部";
    },
    getNextPageParam: (_page: string, _pages: string[], pageParam: number) =>
      pageParam === 0 ? 1 : undefined,
  };
  const observer = new InfiniteQueryObserver(client, options);
  let completed = 0;
  const restore = () =>
    refreshQueryFromEvent(client, key, () =>
      client.fetchInfiniteQuery({ ...options, staleTime: 0 }),
    ).then(() => {
      completed++;
    });
  const command = restore();
  const pagination = observer.fetchNextPage();
  let laterCommand: Promise<void> | undefined;
  try {
    await waitFor(() => expect(reads).toEqual([1]));
    await new Promise((resolve) => setTimeout(resolve, 20));
    laterCommand = restore();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(pageSignal?.aborted).toBe(false);
    expect(completed).toBe(0);
    releasePage();
    expect((await pagination).data?.pages).toEqual(["旧头部", "更早页"]);
    await waitFor(() => expect(reads).toContain(0));
    expect(completed).toBe(0);
    releaseHead();
    await Promise.all([command, laterCommand]);
    expect(reads).toEqual([1, 0, 1]);
    expect(client.getQueryData(key)).toEqual({
      pages: ["最新头部", "更早页"],
      pageParams: [0, 1],
    });
    expect(client.getQueryCache().hasListeners()).toBe(false);
  } finally {
    releasePage();
    releaseHead();
    await Promise.allSettled([pagination, command, laterCommand]);
    client.clear();
  }
});

it("命令读取期间后到变化取消旧signal并补读，两个promise均等到最新事实", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "in-flight"];
  let started = () => {},
    release = () => {};
  const oldStarted = new Promise<void>((resolve) => {
    started = resolve;
  });
  const oldGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let previousSignal: AbortSignal | undefined;
  let reads = 0;
  const execute = () =>
    client.fetchQuery({
      queryKey: key,
      staleTime: 0,
      queryFn: async ({ signal }) => {
        if (++reads === 1) {
          previousSignal = signal;
          started();
          await oldGate;
          return "变化前";
        }
        return "变化后";
      },
    });
  const first = refreshQueryFromEvent(client, key, execute);
  await oldStarted;
  const second = refreshQueryFromEvent(client, key, execute);
  await Promise.all([first, second]);
  expect(previousSignal?.aborted).toBe(true);
  expect(reads).toBe(2);
  expect(client.getQueryData(key)).toBe("变化后");
  release();
  await Promise.resolve();
  expect(client.getQueryData(key)).toBe("变化后");
  expect(client.getQueryCache().hasListeners()).toBe(false);
  client.clear();
});

it("命令期间Query销毁会取消旧读取并释放owner，不重建已销毁查询", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "removed"];
  let started = () => {},
    release = () => {};
  const oldStarted = new Promise<void>((resolve) => {
    started = resolve;
  });
  const oldGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const read = vi.fn(() =>
    client.fetchQuery({
      queryKey: key,
      queryFn: async () => {
        started();
        await oldGate;
        return "过期事实";
      },
    }),
  );
  const command = refreshQueryFromEvent(client, key, read);
  await oldStarted;
  client.removeQueries({ queryKey: key, exact: true });
  await command;
  release();
  await Promise.resolve();
  expect(client.getQueryData(key)).toBeUndefined();
  expect(read).toHaveBeenCalledTimes(1);
  expect(client.getQueryCache().hasListeners()).toBe(false);
  client.clear();
});

it("隐藏文档的命令仅标记原Query陈旧并释放owner，前台显式恢复才读取", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "hidden"];
  client.setQueryData(key, "已有事实");
  const read = vi.fn(() =>
    client.fetchQuery({
      queryKey: key,
      staleTime: 0,
      queryFn: async () => "恢复事实",
    }),
  );
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("hidden");
  try {
    await refreshQueryFromEvent(client, key, read);
    expect(read).not.toHaveBeenCalled();
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryCache().hasListeners()).toBe(false);
    visibility.mockReturnValue("visible");
    await refreshQueryFromEvent(client, key, read);
    expect(client.getQueryData(key)).toBe("恢复事实");
    expect(read).toHaveBeenCalledTimes(1);
    expect(client.getQueryCache().hasListeners()).toBe(false);
  } finally {
    visibility.mockRestore();
    client.clear();
  }
});

it("取消最后一个恢复命令会终止真实读取且保留期间提交的新事实", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "abort-owned"];
  const controller = new AbortController();
  let signal: AbortSignal | undefined;
  let release = () => {};
  client.setQueryData(key, "原事实");
  const command = refreshQueryFromEvent(
    client,
    key,
    () =>
      client.fetchQuery({
        queryKey: key,
        staleTime: 0,
        queryFn: async (context) => {
          signal = context.signal;
          await new Promise<void>((resolve) => {
            release = resolve;
          });
          return "迟到旧事实";
        },
      }),
    0,
    controller.signal,
  );
  const rejected = expect(command).rejects.toMatchObject({
    name: "AbortError",
  });
  try {
    await waitFor(() => expect(signal).toBeDefined());
    client.setQueryData(key, "期间提交的新事实");
    controller.abort();
    await rejected;
    expect(signal?.aborted).toBe(true);
    expect(client.getQueryData(key)).toBe("期间提交的新事实");
    expect(client.getQueryState(key)?.fetchStatus).toBe("idle");
    expect(client.getQueryCache().hasListeners()).toBe(false);
    release();
    await Promise.resolve();
    expect(client.getQueryData(key)).toBe("期间提交的新事实");
    await refreshQueryFromEvent(client, key, () =>
      client.fetchQuery({
        queryKey: key,
        queryFn: async () => "恢复事实",
        staleTime: 0,
      }),
    );
    expect(client.getQueryData(key)).toBe("恢复事实");
  } finally {
    release();
    client.clear();
  }
});

it("一个恢复调用者退出不取消另一个调用者共用的实际请求", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "abort-shared"];
  const controller = new AbortController();
  let signal: AbortSignal | undefined;
  let release = () => {};
  let reads = 0;
  const execute = () =>
    client.fetchQuery({
      queryKey: key,
      staleTime: 0,
      queryFn: async (context) => {
        reads++;
        signal = context.signal;
        await new Promise<void>((resolve) => {
          release = resolve;
        });
        return "共享的新事实";
      },
    });
  const first = refreshQueryFromEvent(
    client,
    key,
    execute,
    0,
    controller.signal,
  );
  const rejected = expect(first).rejects.toMatchObject({ name: "AbortError" });
  const second = refreshQueryFromEvent(client, key, execute);
  try {
    await waitFor(() => expect(signal).toBeDefined());
    controller.abort();
    await rejected;
    expect(signal?.aborted).toBe(false);
    expect(reads).toBe(1);
    release();
    await second;
    expect(client.getQueryData(key)).toBe("共享的新事实");
    expect(client.getQueryCache().hasListeners()).toBe(false);
  } finally {
    release();
    await Promise.allSettled([first, second]);
    client.clear();
  }
});

it("等待分页的恢复命令退出时及时结束等待，原分页继续正常提交", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = ["command", "abort-pagination"];
  client.setQueryData(key, { pages: ["头部"], pageParams: [0] });
  let signal: AbortSignal | undefined;
  let release = () => {};
  let heads = 0;
  const options = {
    queryKey: key,
    initialPageParam: 0,
    queryFn: async (context: { signal: AbortSignal; pageParam: number }) => {
      signal = context.signal;
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      return "旧页";
    },
    getNextPageParam: (_page: string, _pages: string[], cursor: number) =>
      cursor === 0 ? 1 : undefined,
  };
  const observer = new InfiniteQueryObserver(client, options);
  const page = observer.fetchNextPage();
  const controller = new AbortController();
  const command = refreshQueryFromEvent(
    client,
    key,
    async () => {
      heads++;
    },
    0,
    controller.signal,
  );
  const rejected = expect(command).rejects.toMatchObject({
    name: "AbortError",
  });
  try {
    await waitFor(() => expect(signal).toBeDefined());
    controller.abort();
    await rejected;
    expect(signal?.aborted).toBe(false);
    expect(heads).toBe(0);
    release();
    expect((await page).data?.pages).toEqual(["头部", "旧页"]);
    expect(client.getQueryCache().hasListeners()).toBe(false);
  } finally {
    release();
    await Promise.allSettled([page, command]);
    client.clear();
  }
});
