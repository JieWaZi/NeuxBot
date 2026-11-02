import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type {
  GlobalEventFrame,
  GlobalEventObserver,
} from "@/shared/api/global-events";
import {
  getWorkflowQueryKey,
  getWorkflowSourceQueryKey,
  listWorkflowRevisionsQueryKey,
  listWorkflowRunsQueryKey,
  listWorkflowsQueryKey,
  useWorkflowDefinitionEvents,
} from "./workflow-queries";

const listeners = vi.hoisted(() => new Set<GlobalEventObserver>());
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
afterEach(() => listeners.clear());

function event(type: string): GlobalEventFrame {
  return {
    protocolVersion: 1,
    epoch: "test",
    connectionId: "test",
    deliverySequence: 1,
    topic: "workflow",
    type: "workflow.event",
    eventId: type,
    resource: { kind: "workflow", id: "flow" },
    payload: { type },
  };
}
it("创作与批注仅刷新定义，版本保存只刷新依赖版本的查询，恢复统一对账", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const reads = {
    detail: vi.fn(async () => ({})),
    source: vi.fn(async () => ({})),
    revisions: vi.fn(async () => ({})),
    runs: vi.fn(async () => ({})),
    directory: vi.fn(async () => ({})),
  };
  function Consumer() {
    useQuery({
      queryKey: getWorkflowQueryKey({ path: { id: "flow" } }),
      queryFn: reads.detail,
    });
    useQuery({
      queryKey: getWorkflowSourceQueryKey({ path: { id: "flow" } }),
      queryFn: reads.source,
    });
    useQuery({
      queryKey: listWorkflowRevisionsQueryKey({ path: { id: "flow" } }),
      queryFn: reads.revisions,
    });
    useQuery({
      queryKey: listWorkflowRunsQueryKey({ path: { id: "flow" } }),
      queryFn: reads.runs,
    });
    useQuery({ queryKey: listWorkflowsQueryKey(), queryFn: reads.directory });
    useWorkflowDefinitionEvents("flow", true);
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Consumer />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    Object.values(reads).forEach((read) =>
      expect(read).toHaveBeenCalledTimes(1),
    ),
  );
  act(() =>
    listeners.forEach((observer) => {
      observer.onEvent(event("workflow.creation.changed"));
      observer.onEvent(event("workflow.annotation.changed"));
    }),
  );
  await waitFor(() => expect(reads.detail).toHaveBeenCalledTimes(2));
  expect(reads.revisions).toHaveBeenCalledTimes(1);
  expect(reads.runs).toHaveBeenCalledTimes(1);
  expect(reads.source).toHaveBeenCalledTimes(1);
  expect(reads.directory).toHaveBeenCalledTimes(1);
  act(() =>
    listeners.forEach((observer) =>
      observer.onEvent(event("workflow.revision.saved")),
    ),
  );
  await waitFor(() => expect(reads.revisions).toHaveBeenCalledTimes(2));
  expect(reads.detail).toHaveBeenCalledTimes(3);
  expect(reads.source).toHaveBeenCalledTimes(2);
  expect(reads.directory).toHaveBeenCalledTimes(2);
  expect(reads.runs).toHaveBeenCalledTimes(1);
  act(() => listeners.forEach((observer) => observer.onResync?.("gap")));
  await waitFor(() => expect(reads.runs).toHaveBeenCalledTimes(2));
  expect(reads.detail).toHaveBeenCalledTimes(4);
  view.unmount();
  client.clear();
});
