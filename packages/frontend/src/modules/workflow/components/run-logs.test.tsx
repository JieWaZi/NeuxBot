import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { workflowRunLogsOptions } from "../api/workflow-queries";
import type {
  GlobalEventFrame,
  GlobalEventObserver,
  GlobalEventResource,
  GlobalEventTopic,
} from "@/shared/api/global-events";
import { RunLogs } from "./run-logs";

const { listeners, registrations } = vi.hoisted(() => ({
  listeners: new Set<GlobalEventObserver>(),
  registrations: [] as {
    topic: GlobalEventTopic;
    resource: GlobalEventResource;
  }[],
}));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: (
    topic: GlobalEventTopic,
    resource: GlobalEventResource,
    observer: GlobalEventObserver,
  ) => {
    registrations.push({ topic, resource });
    listeners.add(observer);
    return () => listeners.delete(observer);
  },
}));

const origin = "http://run-logs.test";
const endpoint = `${origin}/api/v1/workflows/flow/events/history`;
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  vi.useRealTimers();
  server.resetHandlers();
  listeners.clear();
  registrations.length = 0;
});
afterAll(() => server.close());

function renderLogs(completedAt?: number) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const content = (completedAt?: number) => (
    <QueryClientProvider client={queryClient}>
      <RunLogs id="flow" runId="run" completedAt={completedAt} />
    </QueryClientProvider>
  );
  const view = render(content(completedAt));
  return {
    queryClient,
    setCompletedAt: (value: number) => view.rerender(content(value)),
  };
}

it("仅查询日志，没有日志时隐藏整个区域且不扫描事件历史", async () => {
  const cursors: number[] = [];
  server.use(
    http.get(endpoint, ({ request }) => {
      const params = new URL(request.url).searchParams;
      expect(params.get("logsOnly")).toBe("true");
      cursors.push(Number(params.get("after")));
      return HttpResponse.json({ resources: [] });
    }),
  );
  const { queryClient } = renderLogs(100);
  await waitFor(() => expect(cursors).toEqual([0]));
  await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  expect(screen.queryByText("运行日志")).not.toBeInTheDocument();
});

it("日志按需翻页，不在打开弹窗时预取全部历史", async () => {
  const user = userEvent.setup();
  const cursors: number[] = [];
  server.use(
    http.get(endpoint, ({ request }) => {
      const params = new URL(request.url).searchParams;
      expect(params.get("logsOnly")).toBe("true");
      const after = Number(params.get("after"));
      cursors.push(after);
      return HttpResponse.json({
        resources:
          after === 0
            ? Array.from({ length: 500 }, (_, index) => ({
                eventId: `log-${index + 1}`,
                sequence: index + 501,
                type: "workflow.log",
                payload: { value: `运行日志 ${index + 1}` },
              }))
            : [
                {
                  eventId: "log-tail",
                  sequence: 1500,
                  type: "workflow.log",
                  payload: { value: "最后一页日志" },
                },
              ],
      });
    }),
  );
  const { queryClient } = renderLogs(100);
  await user.click(await screen.findByText("运行日志"));
  await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  expect(cursors).toEqual([0]);
  await user.click(screen.getByRole("button", { name: "下一页" }));
  expect(await screen.findByText(/最后一页日志/)).toBeVisible();
  expect(cursors).toEqual([0, 1000]);
  await user.click(screen.getByRole("button", { name: "上一页" }));
  expect(await screen.findByText(/运行日志 1"/)).toBeVisible();
});

it("运行结束时补读最终日志，隐藏中的区域也能出现", async () => {
  const user = userEvent.setup();
  let finished = false;
  let reads = 0;
  server.use(
    http.get(endpoint, () => {
      reads += 1;
      return HttpResponse.json({
        resources: finished
          ? [
              {
                eventId: "final-log",
                sequence: 1,
                type: "workflow.log",
                payload: { stdout: "最终校验通过", stderr: "" },
              },
            ]
          : [],
      });
    }),
  );
  const { queryClient, setCompletedAt } = renderLogs();
  await waitFor(() => expect(reads).toBe(1));
  await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  expect(screen.queryByText("运行日志")).not.toBeInTheDocument();
  finished = true;
  setCompletedAt(200);
  await user.click(await screen.findByText("运行日志"));
  expect(await screen.findByText("最终校验通过")).toBeVisible();
});

it("读取失败时保留重试入口，不把失败当成没有日志", async () => {
  const user = userEvent.setup();
  server.use(http.get(endpoint, () => new HttpResponse(null, { status: 500 })));
  renderLogs(100);
  await user.click(await screen.findByText("运行日志"));
  const retry = await screen.findByRole("button", { name: "重试" });
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        resources: [
          {
            eventId: "log-1",
            sequence: 1,
            type: "workflow.log",
            payload: { value: "恢复后的日志" },
          },
        ],
      }),
    ),
  );
  await user.click(retry);
  expect(await screen.findByText(/恢复后的日志/)).toBeVisible();
});

it("日志仅按需分页，统一 WS 刷新当前 logsOnly 页且空闲不轮询", async () => {
  const cursors: number[] = [];
  let latest = "当前页原日志";
  server.use(
    http.get(endpoint, ({ request }) => {
      const params = new URL(request.url).searchParams;
      expect(params.get("logsOnly")).toBe("true");
      const after = Number(params.get("after"));
      cursors.push(after);
      return HttpResponse.json({
        resources:
          after === 0
            ? Array.from({ length: 500 }, (_, index) => ({
                eventId: `event-${index + 1}`,
                sequence: index + 1,
                type: "workflow.log",
                payload: {
                  value: index === 0 ? "首页日志" : `其他日志 ${index}`,
                },
              }))
            : [
                {
                  eventId: "log-501",
                  sequence: 501,
                  type: "workflow.log",
                  payload: { value: latest },
                },
              ],
      });
    }),
  );
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity, gcTime: Infinity },
    },
  });
  const otherKey = workflowRunLogsOptions({
    id: "flow",
    runId: "another-run",
    after: 0,
  }).queryKey;
  queryClient.setQueryData(otherKey, { resources: [] });
  const user = userEvent.setup();
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RunLogs id="flow" runId="run" />
    </QueryClientProvider>,
  );
  await user.click(await screen.findByText("运行日志"));
  expect(await screen.findByText(/首页日志/)).toBeVisible();
  expect(cursors).toEqual([0]);
  await user.click(screen.getByRole("button", { name: "下一页" }));
  expect(await screen.findByText(/当前页原日志/)).toBeVisible();
  await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  expect(cursors).toEqual([0, 500]);
  expect(registrations).toEqual([
    {
      topic: "workflow",
      resource: { kind: "workflow", id: "flow", parentId: "run" },
    },
  ]);

  vi.useFakeTimers();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(15000);
  });
  expect(cursors).toEqual([0, 500]);
  latest = "当前页新日志";
  const frame: GlobalEventFrame = {
    protocolVersion: 1,
    epoch: "epoch",
    connectionId: "connection",
    deliverySequence: 1,
    topic: "workflow",
    type: "workflow.event",
    resource: { kind: "workflow", id: "flow", parentId: "run" },
    eventId: "log-502",
    transient: false,
    payload: { type: "workflow.log" },
  };
  await act(async () => {
    for (const listener of listeners) listener.onEvent(frame);
    await vi.advanceTimersByTimeAsync(300);
  });
  vi.useRealTimers();
  expect(await screen.findByText(/当前页新日志/)).toBeVisible();
  expect(cursors).toEqual([0, 500, 500]);
  const firstKey = workflowRunLogsOptions({
    id: "flow",
    runId: "run",
    after: 0,
  }).queryKey;
  expect(queryClient.getQueryState(firstKey)?.isInvalidated).toBe(false);
  expect(queryClient.getQueryState(otherKey)?.isInvalidated).toBe(false);
  view.unmount();
  expect(listeners.size).toBe(0);
  queryClient.clear();
});
