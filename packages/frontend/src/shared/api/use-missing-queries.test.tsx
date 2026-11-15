import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { ApiError } from "./errors";
import { useMissingQueries } from "./use-missing-queries";

it("404 新增、重试、恢复和移除保持准确，普通缓存更新不扫描或重渲染", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const cache = client.getQueryCache();
  const key = ["record"];
  const query = cache.build(client, { queryKey: key });
  const render = vi.fn();
  const { result, unmount } = renderHook(
    () => {
      render();
      return useMissingQueries();
    },
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
  const scan = vi.spyOn(cache, "getAll");
  const initial = result.current;
  render.mockClear();
  act(() => {
    for (let index = 0; index < 200; index++)
      client.setQueryData(["stream"], index);
  });
  expect(result.current).toBe(initial);
  expect(scan).not.toHaveBeenCalled();
  expect(render).not.toHaveBeenCalled();

  await act(async () => {
    await expect(
      client.fetchQuery({
        queryKey: key,
        queryFn: async () => {
          throw new ApiError("Missing", "MISSING", 404);
        },
      }),
    ).rejects.toMatchObject({ status: 404 });
  });
  expect(result.current.has(query.queryHash)).toBe(true);

  act(() => client.setQueryData(key, {}));
  expect(result.current.size).toBe(0);
  act(() => query.setState({ error: new ApiError("Missing", "MISSING", 404) }));
  expect(result.current.has(query.queryHash)).toBe(true);
  act(() => client.removeQueries({ queryKey: key }));
  expect(result.current.size).toBe(0);
  scan.mockRestore();
  unmount();
  client.clear();
});
