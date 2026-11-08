import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type * as Queries from "./plugin-queries";
import { usePluginConfiguration } from "./use-plugin-configuration";
const state = vi.hoisted(() => ({ read: vi.fn(), options: vi.fn() }));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("./plugin-queries", async (original) => ({
  ...(await original<typeof Queries>()),
  getPluginConfigurationOptions: (options: {
    path: { pluginId: string };
    query: { versionId: string; wakerId?: string };
  }) => {
    state.options(options);
    return {
      queryKey: [{ _id: "getPluginConfiguration", ...options }],
      queryFn: () => state.read(options),
    };
  },
}));
let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
beforeEach(() => {
  vi.clearAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  state.read.mockResolvedValue({
    versionId: "fixed",
    fields: [],
    values: {},
    configuredSecrets: [],
    configurationRevision: 3,
    accountGeneration: 4,
    ready: true,
  });
});
afterEach(() => {
  cleanup();
  client.clear();
});
it("does not read configuration before its explicit UI step is active", async () => {
  const { result, rerender } = renderHook(
    ({ active }) => usePluginConfiguration("p", "fixed", "w1", active),
    { initialProps: { active: false }, wrapper },
  );
  expect(state.read).not.toHaveBeenCalled();
  rerender({ active: true });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(state.read).toHaveBeenCalledOnce();
  expect(state.options).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "p" },
      query: { versionId: "fixed", wakerId: "w1" },
    }),
  );
});
it("uses separate fixed-version cache scopes without carrying values to another version", async () => {
  state.read.mockImplementation(
    async (options: { query: { versionId: string } }) => ({
      versionId: options.query.versionId,
      fields: [],
      values: { endpoint: options.query.versionId },
      configuredSecrets: [],
      configurationRevision: 3,
      accountGeneration: 4,
    }),
  );
  const { result, rerender } = renderHook(
    ({ version }) => usePluginConfiguration("p", version),
    { initialProps: { version: "old" }, wrapper },
  );
  await waitFor(() =>
    expect(result.current.data?.values?.endpoint).toBe("old"),
  );
  rerender({ version: "new" });
  expect(result.current.data).toBeUndefined();
  await waitFor(() =>
    expect(result.current.data?.values?.endpoint).toBe("new"),
  );
});
