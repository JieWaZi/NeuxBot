import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { client as apiClient } from "@/shared/api/generated/client.gen";
import { useInstalledPluginReferences } from "./use-installed-plugin-references";
import { invalidatePluginQueries } from "./plugin-queries";
import { usePluginResources } from "./use-plugin-resources";

const fixture = vi.hoisted(() => ({
  listeners: new Set<GlobalEventObserver>(),
  reads: 0,
  referenceReads: 0,
  referenceGate: undefined as Promise<void> | undefined,
  version: "1.0",
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    fixture.listeners.add(observer);
    return () => fixture.listeners.delete(observer);
  },
}));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  listWakerPluginReferences: async () => {
    fixture.referenceReads++;
    const version = fixture.version;
    const gate = fixture.referenceGate;
    fixture.referenceGate = undefined;
    await gate;
    return {
      data: [
        {
          id: "office",
          name: "办公",
          description: "",
          version,
          versionId: "version-1",
        },
      ],
    };
  },
  listWakerPlugins: async () => {
    fixture.reads++;
    return {
      data: {
        resources: [
          {
            plugin: { id: "office", name: "办公" },
            installations: [
              { wakerId: "waker-1", enabled: true, versionId: "version-1" },
            ],
            versions: [{ id: "version-1", version: fixture.version }],
          },
        ],
        totalSize: 1,
      },
    };
  },
}));
afterEach(() => {
  fixture.listeners.clear();
  fixture.reads = 0;
  fixture.referenceReads = 0;
  fixture.referenceGate = undefined;
  fixture.version = "1.0";
});
function publish(type: string, resource = { kind: "plugin", id: "office" }) {
  act(() => {
    for (const listener of fixture.listeners)
      listener.onEvent({
        protocolVersion: 1,
        epoch: "epoch",
        connectionId: "connection",
        deliverySequence: 1,
        topic: "plugins",
        type,
        resource,
        eventId: type,
        payload: {},
      });
  });
}
function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  return {
    client,
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  };
}
it("聊天插件候选只读取轻量引用，忽略配置使用及无关变化，真实能力变更刷新", async () => {
  const { client, wrapper } = setup();
  const view = renderHook(() => useInstalledPluginReferences("waker-1"), {
    wrapper,
  });
  await waitFor(() =>
    expect(view.result.current.plugins[0]?.version).toBe("1.0"),
  );
  publish("plugins.usage.changed");
  publish("plugins.changed");
  publish("plugins.references.changed", { kind: "plugin", id: "unrelated" });
  publish("plugins.references.changed", { kind: "waker", id: "other-waker" });
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(fixture.referenceReads).toBe(1);
  expect(fixture.reads).toBe(0);
  fixture.version = "2.0";
  publish("plugins.references.changed", { kind: "waker", id: "waker-1" });
  await waitFor(() =>
    expect(view.result.current.plugins[0]?.version).toBe("2.0"),
  );
  expect(fixture.referenceReads).toBe(2);
  expect(fixture.reads).toBe(0);
  view.unmount();
  client.clear();
});
it("插件管理读取保留使用变化的实时刷新", async () => {
  const { client, wrapper } = setup();
  const view = renderHook(() => usePluginResources("waker-1"), { wrapper });
  await waitFor(() => expect(view.result.current.isSuccess).toBe(true));
  publish("plugins.usage.changed");
  await waitFor(() => expect(fixture.reads).toBe(2));
  view.unmount();
  client.clear();
});

it("首份引用尚未确认时，展示变更淘汰旧响应并补读", async () => {
  const { client, wrapper } = setup();
  let release!: () => void;
  fixture.referenceGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const view = renderHook(() => useInstalledPluginReferences("waker-1"), {
    wrapper,
  });
  await waitFor(() => expect(fixture.referenceReads).toBe(1));
  fixture.version = "2.0";
  publish("plugins.references.changed");
  await waitFor(() => expect(fixture.referenceReads).toBe(2));
  release();
  await waitFor(() =>
    expect(view.result.current.plugins[0]?.version).toBe("2.0"),
  );
  expect(fixture.reads).toBe(0);
  view.unmount();
  client.clear();
});

it.each([undefined, "http://127.0.0.1:5140"])(
  "同一QueryClient管理与候选隔离，baseUrl=%s",
  async (baseUrl) => {
    const previous = apiClient.getConfig().baseUrl;
    apiClient.setConfig({ baseUrl });
    const { client, wrapper } = setup();
    const view = renderHook(
      () => ({
        management: usePluginResources("waker-1"),
        references: useInstalledPluginReferences("waker-1"),
      }),
      { wrapper },
    );
    try {
      await waitFor(() =>
        expect(view.result.current.references.plugins[0]?.id).toBe("office"),
      );
      expect(view.result.current.management.data?.[0]?.plugin?.id).toBe(
        "office",
      );
      expect(fixture.reads).toBe(1);
      expect(fixture.referenceReads).toBe(1);
      publish("plugins.usage.changed");
      publish("plugins.changed");
      publish("plugins.authorization.changed");
      await waitFor(() => expect(fixture.reads).toBe(2));
      await new Promise((resolve) => setTimeout(resolve, 350));
      expect(fixture.referenceReads).toBe(1);
      publish("plugins.references.changed", {
        kind: "plugin",
        id: "unrelated",
      });
      await waitFor(() => expect(fixture.reads).toBe(3));
      expect(fixture.referenceReads).toBe(1);
      fixture.version = "2.0";
      publish("plugins.references.changed", { kind: "waker", id: "waker-1" });
      await waitFor(() =>
        expect(view.result.current.references.plugins[0]?.version).toBe("2.0"),
      );
      expect(view.result.current.management.data?.[0]?.plugin?.id).toBe(
        "office",
      );
      expect(fixture.referenceReads).toBe(2);
      await act(async () => {
        await invalidatePluginQueries(client);
      });
      expect(fixture.referenceReads).toBe(2);
      expect(view.result.current.management.data?.[0]?.plugin?.id).toBe(
        "office",
      );
    } finally {
      view.unmount();
      client.clear();
      apiClient.setConfig({ baseUrl: previous });
    }
  },
);
