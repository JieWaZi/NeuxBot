import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";

import {
  listRuntimesOptions,
  nativeModelCatalogOptions,
  nativeModelCatalogQueryKey,
} from "./runtime-queries";

const { readSnapshots, readCatalog } = vi.hoisted(() => ({
  readSnapshots: vi.fn(),
  readCatalog: vi.fn(),
}));
vi.mock("./generated/sdk.gen", () => ({
  getRuntimeModelCatalog: readCatalog,
}));
vi.mock("./generated/@tanstack/react-query.gen", () => ({
  listRuntimesOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: readSnapshots,
  }),
}));

afterEach(() => {
  vi.useRealTimers();
  readSnapshots.mockReset();
  readCatalog.mockReset();
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

it.each(["codex", "claude", "cursor"])(
  "%s 拒绝旧认证世代的模型目录",
  async (runtimeId) => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    readCatalog.mockResolvedValue({
      data: { runtimeId, accountId: "work", authGeneration: 1, state: "fresh" },
    });
    const query = nativeModelCatalogOptions({
      runtimeId,
      accountId: "work",
      authGeneration: 2,
    });
    await expect(client.fetchQuery(query)).rejects.toThrow();
    expect(client.getQueryData(query.queryKey)).toBeUndefined();
    expect(readCatalog).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId },
        query: { accountId: "work" },
        signal: expect.any(AbortSignal),
      }),
    );
  },
);

it("账号失效只影响该账号的各智能体目录", async () => {
  const client = new QueryClient();
  const workCodex = nativeModelCatalogQueryKey({
    runtimeId: "codex",
    accountId: "work",
  });
  const workClaude = nativeModelCatalogQueryKey({
    runtimeId: "claude",
    accountId: "work",
  });
  const systemClaude = nativeModelCatalogQueryKey({
    runtimeId: "claude",
    accountId: "system",
  });
  for (const key of [workCodex, workClaude, systemClaude])
    client.setQueryData(key, { state: "fresh" });
  await client.invalidateQueries({
    queryKey: nativeModelCatalogQueryKey({ accountId: "work" }),
    refetchType: "none",
  });
  expect(client.getQueryState(workCodex)?.isInvalidated).toBe(true);
  expect(client.getQueryState(workClaude)?.isInvalidated).toBe(true);
  expect(client.getQueryState(systemClaude)?.isInvalidated).toBe(false);
});
