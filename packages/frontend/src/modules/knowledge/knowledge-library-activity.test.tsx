import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { client as apiClient } from "@/shared/api/generated/client.gen";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { KnowledgeLibraryActivity } from "./knowledge-library-activity";
import { emptyLibrary } from "./knowledge-model";
import { knowledgeKeys } from "./knowledge-queries";

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
const base = "http://activity.test/api/v1/knowledge/libraries/kb";
const library = emptyLibrary("kb", "知识库", "general");
const job = {
  id: "job",
  requestId: "request",
  libraryId: "kb",
  state: "processing",
  maintenance: false,
  sourceVersionIds: ["source-a", "source-b"],
  attempt: 1,
  version: 2,
  createdAt: 1793001600000,
  attemptStartedAt: 1793001600000,
  updatedAt: 1793001600000,
  hostState: "active",
  hostErrorCode: "",
  hostDiagnostic: "",
  diagnostic: "",
  cancelRequested: false,
};

beforeAll(() => {
  apiClient.setConfig({ baseUrl: "http://activity.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function harness() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  function Content() {
    useQueryEvents({
      topic: "knowledge",
      resource: { kind: "knowledge_library", id: "kb", parentId: "local" },
      queryKeys: [knowledgeKeys.resource(library.ref)],
    });
    return <KnowledgeLibraryActivity library={library} />;
  }
  const result = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <Content />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...result, client };
}

function change() {
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
        eventId: "job-state-changed",
        transient: false,
        payload: { jobId: "job" },
      });
  });
}

it("当前任务显示真实文档范围，事件到达后以持久终态承接并收起当前区", async () => {
  let completed = false;
  let jobReads = 0;
  server.use(
    http.get(`${base}/jobs`, () => {
      jobReads++;
      return HttpResponse.json({
        items: [{ ...job, state: completed ? "completed" : "processing" }],
        activeJob: completed ? null : job,
        total: 1,
      });
    }),
    http.get(`${base}/activity`, () =>
      HttpResponse.json({
        data: completed
          ? [
              {
                id: 2,
                targetType: "knowledge_task",
                action: "knowledge.reparse_started",
                outcome: "success",
                details: { total: 2 },
              },
            ]
          : [],
        nextCursor: 0,
      }),
    ),
  );
  const state = harness();
  const current = await screen.findByRole("region", { name: "当前任务" });
  expect(within(current).getByText("整理文档")).toBeVisible();
  expect(within(current).getByText("涉及 2 份文档")).toBeVisible();
  expect(within(current).getByText("处理中")).toBeVisible();
  expect(within(current).getByText(/本次接纳于/)).toBeVisible();
  expect(screen.queryByText("成功")).not.toBeInTheDocument();
  completed = true;
  change();
  await screen.findByText("成功");
  await waitFor(() =>
    expect(screen.queryByRole("region", { name: "当前任务" })).toBeNull(),
  );
  expect(jobReads).toBe(2);
  state.unmount();
  expect(listeners.size).toBe(0);
  state.client.clear();
});

it.each([
  { patch: { state: "queued" }, label: "排队中" },
  {
    patch: { state: "cancel_requested", cancelRequested: true },
    label: "取消中",
  },
  {
    patch: {
      state: "processing",
      hostState: "blocked",
      hostDiagnostic: "请检查模型配置",
    },
    label: "等待处理",
  },
])(
  "维护任务保留真实状态 $label，不把空来源显示成零份文档",
  async ({ patch, label }) => {
    server.use(
      http.get(`${base}/jobs`, () =>
        HttpResponse.json({
          items: [],
          activeJob: {
            ...job,
            ...patch,
            maintenance: true,
            sourceVersionIds: [],
          },
          total: 1,
        }),
      ),
      http.get(`${base}/activity`, () =>
        HttpResponse.json({ data: [], nextCursor: 0 }),
      ),
    );
    harness();
    const current = await screen.findByRole("region", { name: "当前任务" });
    expect(within(current).getByText("整理 Wiki")).toBeVisible();
    expect(within(current).getByText("范围：已有 Wiki")).toBeVisible();
    expect(within(current).getByText(label)).toBeVisible();
    expect(within(current).queryByText(/0 份/)).toBeNull();
    if (patch.hostDiagnostic)
      expect(within(current).getByText(patch.hostDiagnostic)).toBeVisible();
  },
);

it("刷新失败保留已知任务并提示状态未确认，重试同时刷新历史和任务", async () => {
  let failed = false;
  server.use(
    http.get(`${base}/jobs`, () =>
      failed
        ? HttpResponse.json({ message: "offline" }, { status: 503 })
        : HttpResponse.json({ items: [], activeJob: job, total: 1 }),
    ),
    http.get(`${base}/activity`, () =>
      HttpResponse.json({ data: [], nextCursor: 0 }),
    ),
  );
  const user = userEvent.setup();
  harness();
  await screen.findByRole("region", { name: "当前任务" });
  failed = true;
  await user.click(screen.getByRole("button", { name: "刷新活动记录" }));
  expect(
    await screen.findByText(
      "当前任务状态读取失败，展示的是上次状态。请刷新重试。",
    ),
  ).toBeVisible();
  expect(screen.getByText("涉及 2 份文档")).toBeVisible();
  failed = false;
  await user.click(screen.getByRole("button", { name: "刷新活动记录" }));
  await waitFor(() =>
    expect(screen.queryByText(/当前任务状态读取失败/)).toBeNull(),
  );
});

it("历史写入显示有界标题与真实页数，旧记录缺少名称时保留范围摘要", async () => {
  server.use(
    http.get(`${base}/jobs`, () =>
      HttpResponse.json({ items: [], activeJob: null, total: 0 }),
    ),
    http.get(`${base}/activity`, () =>
      HttpResponse.json({
        data: [
          {
            id: 3,
            targetType: "wiki",
            action: "wiki.content_changed",
            outcome: "success",
            details: {
              title: "DNS 缓存",
              titles: ["DNS 缓存", "转发区", "递归解析", "不应展开的第四项"],
              count: 8,
            },
          },
          {
            id: 2,
            targetType: "wiki",
            action: "wiki.content_changed",
            outcome: "success",
            details: { count: 1 },
          },
          {
            id: 1,
            targetType: "knowledge_task",
            action: "knowledge.reparse_started",
            outcome: "failed",
            details: { total: 12 },
          },
        ],
        nextCursor: 0,
      }),
    ),
  );
  harness();
  expect(
    await screen.findByText("DNS 缓存、转发区、递归解析等 8 页"),
  ).toBeVisible();
  expect(screen.getByText("实际变更 8 页")).toBeVisible();
  expect(screen.getByText("实际变更 1 页")).toBeVisible();
  expect(screen.getByText("涉及 12 份文档")).toBeVisible();
  expect(screen.queryByText(/不应展开的第四项/)).toBeNull();
});

it("任务失败原因按需读取，重试后的其他尝试不能覆盖历史结果", async () => {
  let attempt = 1;
  let detailFailed = false;
  let detailReads = 0;
  server.use(
    http.get(`${base}/jobs`, () =>
      HttpResponse.json({ items: [], activeJob: null, total: 1 }),
    ),
    http.get(`${base}/activity`, () =>
      HttpResponse.json({
        data: [
          {
            id: 1,
            targetType: "knowledge_task",
            targetId: "job",
            action: "knowledge.reparse_started",
            outcome: "failed",
            details: { total: 2, attempt: 1 },
          },
        ],
        nextCursor: 0,
      }),
    ),
    http.get(`${base}/jobs/job`, () => {
      detailReads++;
      if (detailFailed)
        return HttpResponse.json({ message: "offline" }, { status: 503 });
      return HttpResponse.json({
        job: {
          ...job,
          state: "failed",
          attempt,
          diagnostic: "模型连接失败，请检查配置后重试。",
        },
      });
    }),
  );
  const user = userEvent.setup();
  const { client } = harness();
  await screen.findByText("失败");
  expect(detailReads).toBe(0);
  await user.click(screen.getByRole("button", { name: "查看结果" }));
  expect(
    await screen.findByText("模型连接失败，请检查配置后重试。"),
  ).toBeVisible();
  expect(detailReads).toBe(1);
  detailFailed = true;
  await act(async () => {
    await client.invalidateQueries({
      queryKey: knowledgeKeys.job(library.ref, "job"),
    });
  });
  await screen.findByRole("alert");
  expect(screen.getByText("模型连接失败，请检查配置后重试。")).toBeVisible();
  expect(
    screen.getByText("任务结果刷新失败，展示的是上次结果。请重试。"),
  ).toBeVisible();
  detailFailed = false;
  attempt = 2;
  await act(async () => {
    await client.invalidateQueries({
      queryKey: knowledgeKeys.job(library.ref, "job"),
    });
  });
  expect(
    await screen.findByText("该任务已重试，此条历史记录的详细结果不可用。"),
  ).toBeVisible();
  expect(screen.queryByText("模型连接失败，请检查配置后重试。")).toBeNull();
});
