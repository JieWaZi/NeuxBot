import { act, renderHook } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import { initializeApi } from "@/shared/api/runtime";
import { client } from "@/shared/api/generated/client.gen";
import { getChatQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { chatMessagesInfiniteOptions, getChatOptions } from "./chat-queries";

vi.mock("@/shared/platform", () => ({
  getCoreConnection: async () => ({ baseUrl: "http://127.0.0.1:19876" }),
}));
beforeAll(async () => {
  await initializeApi();
});
afterEach(() => {
  const { result, unmount } = renderHook(useErrorNotices);
  act(() => {
    for (const notice of result.current) dismissErrorNotice(notice.id);
  });
  unmount();
});

it("详情查询的错误归属不改变权威缓存键", () => {
  const input = { path: { chatId: "chat-1" } };
  expect(getChatOptions(input).queryKey).toEqual(getChatQueryKey(input));
});

it("聊天历史由所属查询处理错误，自动重试期间不发布聊天通知", async () => {
  const { result } = renderHook(useErrorNotices);
  let finishRetry: ((response: Response) => void) | undefined;
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      new Response('{"key":"ReadFailed","message":"读取失败"}', {
        status: 503,
        headers: { "Content-Type": "application/json" },
      }),
    )
    .mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finishRetry = resolve;
        }),
    );
  client.setConfig({ fetch });
  const queries = new QueryClient({
    defaultOptions: { queries: { retry: 1, retryDelay: 0 } },
  });
  const reading = queries.fetchInfiniteQuery(
    chatMessagesInfiniteOptions("retry-probe"),
  );
  try {
    await act(async () => {
      await vi.waitFor(() => expect(finishRetry).toBeDefined());
    });
    expect(result.current).toHaveLength(0);
  } finally {
    finishRetry?.(
      new Response('{"resources":[]}', {
        headers: { "Content-Type": "application/json" },
      }),
    );
    await reading;
    queries.clear();
  }
});
