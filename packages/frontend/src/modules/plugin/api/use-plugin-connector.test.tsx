import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { usePluginConnector } from "./use-plugin-connector";

const probe = vi.hoisted(() => vi.fn());
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("./plugin-queries", () => ({
  getPluginConnectorOptions: (options: unknown) => ({
    queryKey: ["installed", options],
    queryFn: async () => ({}),
  }),
  getCatalogPluginConnectorOptions: (options: unknown) => ({
    queryKey: ["catalog", options],
    queryFn: async () => ({}),
  }),
  testPluginConnectorMutation: () => ({ mutationFn: probe }),
}));
afterEach(() => probe.mockReset());

it.each(["pending", "error"])(
  "keeps catalog probe %s attached to its exact snapshot and catalog identity",
  async (mode) => {
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const context = {
      catalogKey: "github",
      pluginId: "p",
      versionId: "v1",
      sourceCommit: "a".repeat(40),
      archiveSHA256: "b".repeat(64),
    };
    if (mode === "pending")
      probe.mockImplementationOnce(() => new Promise(() => {}));
    else probe.mockRejectedValueOnce(new Error("old snapshot failed"));
    const { result, rerender } = renderHook(
      ({ context }) => usePluginConnector(context, "github"),
      { wrapper, initialProps: { context } },
    );
    act(() => result.current.test());
    await waitFor(() =>
      expect(
        mode === "pending"
          ? result.current.testing
          : Boolean(result.current.testError),
      ).toBe(true),
    );
    rerender({ context: { ...context, sourceCommit: "c".repeat(40) } });
    expect(result.current.testing).toBe(false);
    expect(result.current.testError).toBeNull();
    rerender({ context: { ...context, catalogKey: "another" } });
    expect(result.current.testing).toBe(false);
    expect(result.current.testError).toBeNull();
  },
);
