import { act, renderHook } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { initializeApi } from "./runtime";
import { client } from "./generated/client.gen";
import { listWorkBoardOptions } from "./generated/@tanstack/react-query.gen";
import { dismissErrorNotice, useErrorNotices } from "./error-notices";

const { connectionMock } = vi.hoisted(() => ({
  connectionMock: vi.fn(async () => ({
    baseUrl: "http://127.0.0.1:19876",
    token: "test-token",
  })),
}));
vi.mock("@/shared/platform", () => ({ getCoreConnection: connectionMock }));

function generatedRequest(input: RequestInfo | URL) {
  if (!(input instanceof Request)) throw new Error("SDK 未提供已构造的请求");
  return input;
}
beforeAll(async () => {
  await initializeApi();
});
afterEach(async () => {
  const { result, unmount } = renderHook(useErrorNotices);
  act(() => {
    for (const notice of result.current) dismissErrorNotice(notice.id);
  });
  unmount();
  vi.useRealTimers();
  connectionMock.mockReset().mockResolvedValue({
    baseUrl: "http://127.0.0.1:19876",
    token: "test-token",
  });
  await initializeApi();
});

it("Core 恢复后，旧生成 QueryKey 仍请求当前受信地址和令牌，空闲不重复请求", async () => {
  connectionMock.mockClear();
  connectionMock.mockResolvedValue({
    baseUrl: "http://127.0.0.1:8100",
    token: "old-token",
  });
  await initializeApi();
  const options = listWorkBoardOptions({
    query: { mode: "all", query: "查找" },
    headers: { Authorization: "Bearer old-token" },
  });
  const originalKey = options.queryKey;
  connectionMock.mockResolvedValue({
    baseUrl: "http://127.0.0.1:8200",
    token: "new-token",
  });
  await initializeApi();
  const received: Request[] = [];
  const fetch = vi.fn(async (input: RequestInfo | URL) => {
    const request = generatedRequest(input);
    received.push(request);
    return new Response(JSON.stringify({ resources: [], totalSize: 0 }), {
      headers: { "Content-Type": "application/json" },
    });
  });
  client.setConfig({ fetch });
  const queries = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  try {
    await queries.fetchQuery({ ...options, staleTime: Infinity });
    expect(received[0]?.url).toBe(
      "http://127.0.0.1:8200/api/v1/work-board?mode=all&query=%E6%9F%A5%E6%89%BE",
    );
    expect(received[0]?.headers.get("Authorization")).toBe("Bearer new-token");
    expect(options.queryKey).toBe(originalKey);
    expect(originalKey[0].baseUrl).toBe("http://127.0.0.1:8100");
    vi.useFakeTimers();
    await vi.advanceTimersByTimeAsync(15000);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(connectionMock).toHaveBeenCalledTimes(2);
  } finally {
    queries.clear();
  }
});

it.each([
  "https://files.example.test/api/v1/attachment?version=2",
  "http://127.0.0.1:8100/attachments/file?version=2",
  "https://127.0.0.1:8100/api/v1/work-board",
  "http://127.0.0.1:8100/api/v10/attachment",
])("Core 地址修正不改其他资源目标 %s", async (url) => {
  connectionMock.mockResolvedValue({
    baseUrl: "http://127.0.0.1:8200",
    token: "new-token",
  });
  await initializeApi();
  let received: Request | undefined;
  client.setConfig({
    fetch: async (input: RequestInfo | URL) => {
      const request = generatedRequest(input);
      received = request;
      return new Response("{}", {
        headers: { "Content-Type": "application/json" },
      });
    },
  });
  const target = new URL(url);
  await client.get({
    baseUrl: target.origin,
    url: target.pathname + target.search,
    headers: { Authorization: "external-token" },
    throwOnError: true,
  });
  expect(received?.url).toBe(url);
  expect(received?.headers.get("Authorization")).toBeNull();
});

it("Core 地址修正保留请求正文、路径参数和取消信号", async () => {
  connectionMock.mockResolvedValue({
    baseUrl: "http://127.0.0.1:8200",
    token: "new-token",
  });
  await initializeApi();
  const abort = new AbortController();
  let received: Request | undefined;
  let receivedBody: string | undefined;
  const fetch = vi.fn(async (input: RequestInfo | URL) => {
    const request = generatedRequest(input);
    received = request;
    receivedBody = await request.clone().text();
    return new Promise<Response>((_resolve, reject) => {
      request.signal.addEventListener(
        "abort",
        () => reject(new DOMException("aborted", "AbortError")),
        { once: true },
      );
    });
  });
  client.setConfig({ fetch });
  expect(client.getConfig().bodySerializer?.({ content: "消息" })).toBe(
    '{"content":"消息"}',
  );
  const response = client.post({
    baseUrl: "http://127.0.0.1:8100",
    url: "/api/v1/chats/chat-1/messages?source=im",
    body: { content: "消息" },
    headers: {
      "Content-Type": "application/json",
      Authorization: "Bearer old-token",
    },
    signal: abort.signal,
    throwOnError: true,
  });
  const outcome = response.then(
    () => undefined,
    (error: unknown) => error,
  );
  await vi.waitFor(() => expect(receivedBody).toBeDefined());
  expect(received?.url).toBe(
    "http://127.0.0.1:8200/api/v1/chats/chat-1/messages?source=im",
  );
  expect(received?.headers.get("Authorization")).toBe("Bearer new-token");
  expect(received?.method).toBe("POST");
  expect(receivedBody).toBe('{"content":"消息"}');
  abort.abort();
  expect(await outcome).toBeDefined();
  expect(received?.signal.aborted).toBe(true);
});

describe("API 错误展示入口", () => {
  it("同一远端知识库的并行请求失败只生成一条通知", async () => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              key: "KnowledgeRemoteUnreachable",
              message: "远端知识服务连接失败",
            }),
            { status: 502, headers: { "Content-Type": "application/json" } },
          ),
      ),
    });
    await act(async () => {
      await Promise.allSettled([
        client.get({
          url: "/api/v1/knowledge/connections/connection-1/libraries/library-1",
          throwOnError: true,
        }),
        client.get({
          url: "/api/v1/knowledge/connections/connection-1/libraries/library-1/sources",
          throwOnError: true,
        }),
        client.get({
          url: "/api/v1/knowledge/connections/connection-1/libraries/library-1/capabilities",
          throwOnError: true,
        }),
      ]);
    });
    expect(result.current).toEqual([
      expect.objectContaining({
        id: "api:knowledge:connection-1:library-1:KnowledgeRemoteUnreachable",
        operation: "api:knowledge:connection-1:library-1",
        message: "远端知识服务连接失败",
      }),
    ]);
  });

  it("相同路径的其他方法、分页或其他 Chat 成功不会清除错误", async () => {
    const { result } = renderHook(useErrorNotices);
    let fails = true;
    client.setConfig({
      fetch: vi.fn(
        async () =>
          new Response(
            fails ? '{"key":"ReadFailed","message":"读取失败"}' : "{}",
            {
              status: fails ? 503 : 200,
              headers: { "Content-Type": "application/json" },
            },
          ),
      ),
    });
    const url = "/api/v1/chats/one/messages";
    await act(async () => {
      await expect(
        client.get({ url, query: { before: 10 }, throwOnError: true }),
      ).rejects.toBeDefined();
    });
    fails = false;
    await act(async () => {
      await client.get({ url, throwOnError: true });
      await client.get({
        url: "/api/v1/chats/two/messages",
        query: { before: 10 },
        throwOnError: true,
      });
      await client.post({ url, query: { before: 10 }, throwOnError: true });
    });
    expect(result.current).toHaveLength(1);
    await act(async () => {
      await client.get({ url, query: { before: 10 }, throwOnError: true });
    });
    expect(result.current).toHaveLength(0);
  });

  it("旧请求晚到的成功不会清除新发生的同操作失败", async () => {
    const { result } = renderHook(useErrorNotices);
    let finish!: (response: Response) => void;
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response('{"key":"ReadFailed","message":"读取失败"}', {
          status: 503,
        }),
      )
      .mockImplementationOnce(
        () =>
          new Promise<Response>((resolve) => {
            finish = resolve;
          }),
      )
      .mockResolvedValueOnce(
        new Response('{"key":"ReadFailed","message":"读取失败"}', {
          status: 503,
        }),
      );
    client.setConfig({ fetch });
    const request = () =>
      client.get({ url: "/api/v1/chats/one/messages", throwOnError: true });
    await act(async () => {
      await expect(request()).rejects.toBeDefined();
    });
    const old = request();
    await vi.waitFor(() => expect(finish).toBeDefined());
    await act(async () => {
      await expect(request()).rejects.toBeDefined();
    });
    await act(async () => {
      finish(new Response("{}"));
      await old;
    });
    expect(result.current).toHaveLength(1);
  });

  it("HTTP 成功但 JSON 解析失败时保留原错误", async () => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi
        .fn()
        .mockResolvedValueOnce(
          new Response('{"key":"ReadFailed","message":"读取失败"}', {
            status: 503,
          }),
        )
        .mockResolvedValueOnce(
          new Response("{bad json", {
            headers: { "Content-Type": "application/json" },
          }),
        ),
    });
    const request = () =>
      client.get({ url: "/api/v1/chats/one/messages", throwOnError: true });
    await act(async () => {
      await expect(request()).rejects.toBeDefined();
      await expect(request()).rejects.toBeDefined();
    });
    expect(result.current).toHaveLength(1);
  });

  it("SDK 接纳的空 JSON 响应也会清除对应错误", async () => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi
        .fn()
        .mockResolvedValueOnce(
          new Response('{"key":"ReadFailed","message":"读取失败"}', {
            status: 503,
          }),
        )
        .mockResolvedValueOnce(
          new Response("", { headers: { "Content-Type": "application/json" } }),
        ),
    });
    const request = () =>
      client.get({ url: "/api/v1/chats/one/messages", throwOnError: true });
    await act(async () => {
      await expect(request()).rejects.toBeDefined();
    });
    await act(async () => {
      await request();
    });
    expect(result.current).toHaveLength(0);
  });

  it.each([
    "/api/v1/chats/chat-1",
    "/api/v1/channels/c1/threads/t1",
    "/api/v1/wakers/w1",
    "/api/v1/wakers/w1/projects/p1",
    "/api/v1/wakers/w1/skills/s1",
    "/api/v1/wakers/w1/connectors/m1",
    "/api/v1/automatic-tasks/task-1",
    "/api/v1/workflows/workflow-1",
    "/api/v1/workflows/workflow-1/runs",
    "/api/v1/workflows/workflow-1/revisions",
    "/api/v1/chats/chat-1/messages",
  ])("页面主记录 %s 返回 404 时不生成通知", async (url) => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({ key: "NotFound", message: "记录不存在" }),
            { status: 404, headers: { "Content-Type": "application/json" } },
          ),
      ),
    });
    await act(async () => {
      await expect(
        client.get({ url, throwOnError: true }),
      ).rejects.toMatchObject({ status: 404 });
    });
    expect(result.current).toHaveLength(0);
  });

  it.each([
    ["DELETE", "/api/v1/wakers/w1/projects/p1", 404],
    ["GET", "/api/v1/wakers/w1/projects/p1", 503],
    ["GET", "/api/v1/workflows/sdk", 404],
    ["GET", "/api/v1/runtimes", 404],
  ] as const)("%s %s 的 %s 错误继续通知", async (method, url, status) => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({ key: "ReadFailed", message: "操作失败" }),
            { status, headers: { "Content-Type": "application/json" } },
          ),
      ),
    });
    await act(async () => {
      await expect(
        client.request({ method, url, throwOnError: true }),
      ).rejects.toMatchObject({ status });
    });
    expect(result.current).toHaveLength(1);
  });

  it.each([
    ["/api/v1/chats/chat-1/messages", "chat:chat-1"],
    ["/api/v1/channels/c1/threads/t1/timeline", "team:t1"],
    ["/api/v1/runtimes", "global"],
  ])("%s 的失败进入对应范围，同一请求恢复成功后清除", async (url, scope) => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({ key: "ReadFailed", message: "读取失败" }),
            { status: 503, headers: { "Content-Type": "application/json" } },
          ),
      ),
    });
    await act(async () => {
      await expect(
        client.get({ url, throwOnError: true }),
      ).rejects.toMatchObject({ code: "ReadFailed" });
    });
    expect(result.current).toEqual([
      expect.objectContaining({ scope, message: "读取失败" }),
    ]);
    client.setConfig({
      fetch: vi.fn(
        async () =>
          new Response("{}", {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    });
    await act(async () => {
      await client.get({ url, throwOnError: true });
    });
    expect(result.current).toHaveLength(0);
  });

  it("发送和执行重试的错误交由消息入口处理，不生成重复提示", async () => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi.fn(
        async () => new Response('{"message":"拒绝"}', { status: 409 }),
      ),
    });
    await act(async () => {
      for (const url of [
        "/api/v1/chats",
        "/api/v1/chats/c1/messages",
        "/api/v1/chats/c1/executions/e1/retry",
      ]) {
        await expect(
          client.post({ url, throwOnError: true }),
        ).rejects.toBeDefined();
      }
    });
    expect(result.current).toHaveLength(0);
  });

  it("标记就地错误的请求不进入全局通知", async () => {
    const { result } = renderHook(useErrorNotices);
    client.setConfig({
      fetch: vi.fn(
        async () =>
          new Response(JSON.stringify({ message: "删除失败" }), {
            status: 503,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    });
    await act(async () => {
      await expect(
        client.delete({
          url: "/api/v1/wakers/w1/skills/s1/versions/v1",
          headers: { "X-NeuxAlly-Error-Surface": "local" },
          throwOnError: true,
        }),
      ).rejects.toMatchObject({ message: "删除失败" });
    });
    expect(result.current).toHaveLength(0);
  });

  it("主动取消和后端网络断连不生成持久提示", async () => {
    const { result } = renderHook(useErrorNotices);
    const abort = new AbortController();
    abort.abort();
    client.setConfig({
      fetch: vi.fn(async () => {
        throw new DOMException("aborted", "AbortError");
      }),
    });
    await act(async () => {
      await expect(
        client.get({
          url: "/api/v1/chats/c1",
          signal: abort.signal,
          throwOnError: true,
        }),
      ).rejects.toBeDefined();
    });
    expect(result.current).toHaveLength(0);
    client.setConfig({
      fetch: vi.fn(async () => {
        throw new TypeError("Failed to fetch");
      }),
    });
    await act(async () => {
      await expect(
        client.get({ url: "/api/v1/chats/c1", throwOnError: true }),
      ).rejects.toMatchObject({ code: "NETWORK" });
    });
    expect(result.current).toHaveLength(0);
  });
});
