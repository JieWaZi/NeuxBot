import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { getChatQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { AppSceneTab } from "@/shared/workspace/app-scene-model";
import { useRouteSceneNotFound } from "./route-scene-not-found";

it("流式缓存更新不为每个已打开的 Scene 扫描整个查询缓存", () => {
  const client = new QueryClient();
  const tabs: AppSceneTab[] = Array.from({ length: 20 }, (_, index) => ({
    id: `chat:${index}`,
    kind: "route",
    lastUsed: index,
    label: `Chat ${index}`,
    href: `/chats/${index}`,
  }));
  for (let index = 0; index < 1_000; index++)
    client.setQueryData(getChatQueryKey({ path: { chatId: `${index}` } }), {});
  const cache = client.getQueryCache();
  const scan = vi.spyOn(cache, "findAll");
  const { result, unmount } = renderHook(() => useRouteSceneNotFound(tabs), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  scan.mockClear();
  const started = performance.now();
  act(() => {
    for (let index = 0; index < 200; index++)
      client.setQueryData(["stream"], index);
  });
  const elapsedMs = performance.now() - started;
  expect(result.current).toBe("0".repeat(tabs.length));
  expect({ scans: scan.mock.calls.length, elapsedMs }).toMatchObject({
    scans: 0,
  });
  unmount();
  client.clear();
});
