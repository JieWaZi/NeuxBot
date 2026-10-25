import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";

import { listRuntimesOptions } from "./runtime-queries";

const { readSnapshots } = vi.hoisted(() => ({ readSnapshots: vi.fn() }));
vi.mock("./generated/@tanstack/react-query.gen", () => ({
  listRuntimesOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: readSnapshots,
  }),
}));

afterEach(() => {
  vi.useRealTimers();
  readSnapshots.mockReset();
});

it("首次探测只读取一次，由统一事件触发后续对账，空闲不轮询", async () => {
  vi.useFakeTimers();
  readSnapshots
    .mockResolvedValueOnce({
      resources: [
        { id: "codex", detectedAt: 0, state: "" },
        { id: "claude", detectedAt: 1, state: "ready" },
      ],
    })
    .mockResolvedValue({
      resources: [
        { id: "codex", detectedAt: 2, state: "unavailable" },
        { id: "claude", detectedAt: 1, state: "ready" },
      ],
    });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const observer = new QueryObserver(client, listRuntimesOptions());
  const unsubscribe = observer.subscribe(() => {});
  try {
    await vi.advanceTimersByTimeAsync(0);
    expect(readSnapshots).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1000);
    expect(readSnapshots).toHaveBeenCalledTimes(1);
    await client.invalidateQueries({ queryKey: ["runtimes"] });
    expect(observer.getCurrentResult().data?.resources?.[0]?.state).toBe(
      "unavailable",
    );
    await vi.advanceTimersByTimeAsync(5000);
    expect(readSnapshots).toHaveBeenCalledTimes(2);
  } finally {
    unsubscribe();
    client.clear();
  }
});
