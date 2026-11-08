import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { i18n } from "@/shared/i18n";
import type { BrowsePluginsResponse } from "@/shared/api/generated/types.gen";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import type * as ResourceSdk from "@/shared/api/generated/sdk.gen";
import { browsePlugins } from "@/shared/api/generated/sdk.gen";
import { usePluginBrowse, usePluginBrowsePages } from "./use-plugin-browse";
const state = vi.hoisted(() => ({
  pages: [] as BrowsePluginsResponse[],
  failNext: false,
  holdSecond: false,
  release: undefined as (() => void) | undefined,
  signals: [] as AbortSignal[],
  listeners: new Set<GlobalEventObserver>(),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    state.listeners.add(observer);
    return () => state.listeners.delete(observer);
  },
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof ResourceSdk>()),
  browsePlugins: vi.fn(
    async (options: { query: { pageNum: number }; signal: AbortSignal }) => {
      state.signals.push(options.signal);
      if (state.failNext && options.query.pageNum === 2)
        throw new Error("second page failed");
      if (state.holdSecond && options.query.pageNum === 2)
        await new Promise<void>((resolve) => {
          state.release = resolve;
        });
      return { data: state.pages[options.query.pageNum - 1] };
    },
  ),
}));
const clients: QueryClient[] = [];
function wrapper({ children }: { children: ReactNode }) {
  const client = clients[clients.length - 1];
  if (!client) throw new Error("missing test client");
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
function twoPages() {
  state.pages = [
    {
      entries: [{ sourceKey: "s1", name: "第一项" }],
      totalSize: 2,
      hasMore: true,
      pageNum: 1,
    },
    {
      entries: [
        { sourceKey: "s1", name: "第一项更新" },
        { sourceKey: "s2", name: "第二项" },
      ],
      totalSize: 2,
      hasMore: false,
      pageNum: 2,
    },
  ];
}
beforeEach(async () => {
  vi.clearAllMocks();
  state.failNext = false;
  state.holdSecond = false;
  state.release = undefined;
  state.signals = [];
  state.pages = [
    {
      entries: [
        { sourceKey: "s1", pluginId: "p1", name: "办公", installedCount: 1 },
      ],
      totalSize: 1,
      installedTotalSize: 1,
      hasMore: false,
      pageNum: 1,
      pageSize: 100,
    },
  ];
  clients.push(
    new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  );
  await i18n.changeLanguage("zh-CN");
});
afterEach(() => {
  cleanup();
  state.release?.();
  for (const client of clients) client.clear();
  clients.length = 0;
  state.listeners.clear();
});
it("shares identical complete-directory queries and requests narrow batches of one hundred", async () => {
  const { result } = renderHook(
    () => [
      usePluginBrowse({ search: "office", installedOnly: true }),
      usePluginBrowse({ search: "office", installedOnly: true }),
    ],
    { wrapper },
  );
  await waitFor(() => expect(result.current[0]?.entries).toHaveLength(1));
  expect(browsePlugins).toHaveBeenCalledOnce();
  expect(browsePlugins).toHaveBeenCalledWith(
    expect.objectContaining({
      query: {
        query: "office",
        installedOnly: true,
        locale: "zh-CN",
        pageNum: 1,
        pageSize: 100,
      },
    }),
  );
});
it("reads through hasMore and derives unique sources before publishing the complete result", async () => {
  twoPages();
  const { result } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.entries.map((entry) => entry.name)).toEqual([
    "第一项更新",
    "第二项",
  ]);
  expect(
    vi.mocked(browsePlugins).mock.calls.map((call) => call[0]?.query?.pageNum),
  ).toEqual([1, 2]);
  expect(result.current.total).toBe(2);
});
it("does not expose a partial category or count while a later batch is pending", async () => {
  twoPages();
  state.holdSecond = true;
  const { result } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(browsePlugins).toHaveBeenCalledTimes(2));
  expect(result.current.isPending).toBe(true);
  expect(result.current.entries).toEqual([]);
  expect(result.current.total).toBeNull();
  act(() => state.release?.());
  await waitFor(() => expect(result.current.entries).toHaveLength(2));
});
it("fails the complete read without publishing a partial list and retries the full directory", async () => {
  twoPages();
  state.failNext = true;
  const { result } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.entries).toEqual([]);
  state.failNext = false;
  await act(async () => {
    await result.current.refetch();
  });
  await waitFor(() => expect(result.current.entries).toHaveLength(2));
  expect(
    vi.mocked(browsePlugins).mock.calls.map((call) => call[0]?.query?.pageNum),
  ).toEqual([1, 2, 1, 2]);
});
it("passes one cancellation signal through every batch and aborts when the consumer leaves", async () => {
  twoPages();
  state.holdSecond = true;
  const { unmount } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(browsePlugins).toHaveBeenCalledTimes(2));
  expect(state.signals[0]).toBe(state.signals[1]);
  unmount();
  expect(state.signals.every((signal) => signal.aborted)).toBe(true);
  state.release?.();
});
it("keeps an unknown total distinct from an accurate empty total", async () => {
  state.pages = [{ entries: [], installedTotalSize: 0, hasMore: false }];
  const { result } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.total).toBeNull();
  state.pages = [{ entries: [], totalSize: 0, hasMore: false }];
  await act(async () => {
    await result.current.refetch();
  });
  await waitFor(() => expect(result.current.total).toBe(0));
});
it("retains the previous complete snapshot if background refresh fails", async () => {
  const { result } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(result.current.entries).toHaveLength(1));
  twoPages();
  state.failNext = true;
  await act(async () => {
    await result.current.refetch();
  });
  await waitFor(() => expect(result.current.isRefetchError).toBe(true));
  expect(result.current.entries[0]?.name).toBe("办公");
});
it("refreshes the active complete browse query from plugin events", async () => {
  const { result } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(result.current.entries).toHaveLength(1));
  state.pages = [
    {
      entries: [{ sourceKey: "s1", name: "更新后" }],
      totalSize: 1,
      hasMore: false,
    },
  ];
  act(() => {
    for (const listener of state.listeners)
      listener.onEvent({
        protocolVersion: 1,
        epoch: "e",
        connectionId: "c",
        deliverySequence: 1,
        topic: "plugins",
        type: "plugins.changed",
        resource: { kind: "global" },
        eventId: "changed",
        transient: false,
        payload: {},
      });
  });
  await waitFor(() => expect(result.current.entries[0]?.name).toBe("更新后"));
  expect(browsePlugins).toHaveBeenCalledTimes(2);
});
it("uses a distinct query after a locale change", async () => {
  const { result } = renderHook(() => usePluginBrowse(), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  await act(async () => {
    await i18n.changeLanguage("en-US");
  });
  await waitFor(() => expect(browsePlugins).toHaveBeenCalledTimes(2));
  expect(browsePlugins).toHaveBeenLastCalledWith(
    expect.objectContaining({
      query: expect.objectContaining({ locale: "en" }),
    }),
  );
});

it("正文只读取首批24项并在滚动请求时追加、按来源去重", async () => {
  twoPages();
  const { result } = renderHook(() => usePluginBrowsePages(), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(browsePlugins).toHaveBeenCalledOnce();
  expect(browsePlugins).toHaveBeenCalledWith(
    expect.objectContaining({
      query: expect.objectContaining({ pageNum: 1, pageSize: 24 }),
    }),
  );
  expect(result.current.entries.map((entry) => entry.name)).toEqual(["第一项"]);
  expect(result.current.hasNextPage).toBe(true);
  await act(async () => {
    await result.current.fetchNextPage();
  });
  await waitFor(() =>
    expect(result.current.entries.map((entry) => entry.name)).toEqual([
      "第一项更新",
      "第二项",
    ]),
  );
  expect(result.current.hasNextPage).toBe(false);
});

it("正文后续页失败保留已经展示的条目并可重试该页", async () => {
  twoPages();
  const { result } = renderHook(() => usePluginBrowsePages(), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  state.failNext = true;
  await act(async () => {
    await result.current.fetchNextPage();
  });
  await waitFor(() => expect(result.current.isFetchNextPageError).toBe(true));
  expect(result.current.entries.map((entry) => entry.name)).toEqual(["第一项"]);
  state.failNext = false;
  await act(async () => {
    await result.current.fetchNextPage();
  });
  await waitFor(() => expect(result.current.entries).toHaveLength(2));
  expect(
    vi.mocked(browsePlugins).mock.calls.map((call) => call[0]?.query?.pageNum),
  ).toEqual([1, 2, 2]);
});
