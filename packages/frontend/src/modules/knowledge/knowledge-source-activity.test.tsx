import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterAll, beforeAll, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { client as apiClient } from "@/shared/api/generated/client.gen";
import { knowledgeKeys, sourcesOptions, toSource } from "./knowledge-queries";

const { listeners } = vi.hoisted(() => ({
  listeners: new Set<GlobalEventObserver>(),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    listeners.add(observer);
    return () => listeners.delete(observer);
  },
}));
const server = setupServer();
beforeAll(() => {
  apiClient.setConfig({ baseUrl: "http://knowledge-activity.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterAll(() => server.close());

it("阅读领取事件刷新真实状态，隐藏Scene停止消费且恢复时只对账一次", async () => {
  let state = "queued";
  let reads = 0;
  server.use(
    http.get(
      "http://knowledge-activity.test/api/v1/knowledge/libraries/kb/sources",
      () => {
        reads++;
        return HttpResponse.json({
          items: [{ id: "source", filename: "source.md", state }],
          total: 1,
        });
      },
    ),
  );
  const ref = {
    backend: "local",
    connectionId: "local",
    knowledgeBaseId: "kb",
  };
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  function Content({ active }: { active: boolean }) {
    const query = useQuery(sourcesOptions(ref, 0, active));
    useQueryEvents({
      topic: "knowledge",
      resource: { kind: "knowledge_library", id: "kb", parentId: "local" },
      queryKeys: [knowledgeKeys.resource(ref)],
      active,
      enabled: active,
    });
    const source = query.data?.items?.[0];
    return <p>{source ? toSource(source).state : "loading"}</p>;
  }
  const view = (active: boolean) => (
    <QueryClientProvider client={client}>
      <Content active={active} />
    </QueryClientProvider>
  );
  const rendered = render(view(true));
  await screen.findByText("queued");
  expect(reads).toBe(1);
  state = "processing";
  act(() => {
    for (const listener of listeners)
      listener.onEvent({
        protocolVersion: 1,
        epoch: "epoch",
        connectionId: "connection",
        deliverySequence: 1,
        topic: "knowledge",
        type: "knowledge.changed",
        resource: { kind: "knowledge_library", id: "kb", parentId: "local" },
        eventId: "reading-started",
        transient: false,
        payload: { jobId: "job", sourceVersionId: "source" },
      });
  });
  await screen.findByText("processing");
  expect(reads).toBe(2);
  rendered.rerender(view(false));
  expect(listeners.size).toBe(0);
  state = "cancelled";
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 350));
  });
  expect(reads).toBe(2);
  rendered.rerender(view(true));
  await screen.findByText("cancelled");
  await waitFor(() => expect(reads).toBe(3));
  rendered.unmount();
  expect(listeners.size).toBe(0);
  client.clear();
});
