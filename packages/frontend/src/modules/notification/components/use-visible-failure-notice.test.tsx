import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { useRef } from "react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { FailureNoticeContent } from "./failure-notice-content";
import { useVisibleFailureNotice } from "./use-visible-failure-notice";
const origin = "http://failure-notice.test";
const noticeId = "automatic-run-failed:run-1";
const notice = {
  id: noticeId,
  kind: "automatic_failure",
  bucket: "unread",
  source: "automatic",
  title: "运行失败",
  description: "接纳失败",
  createdAt: 100,
  version: 1,
  executor: { id: "waker-1", kind: "waker", name: "伙伴" },
  target: {
    kind: "automatic_failure",
    taskId: "task-1",
    automaticRunId: "run-1",
  },
  dismissed: false,
};
const callbacks: IntersectionObserverCallback[] = [];
const confirmations: unknown[] = [];
const server = setupServer(
  http.get(`${origin}/api/v1/notifications/${noticeId}`, () =>
    HttpResponse.json(notice),
  ),
  http.post(
    `${origin}/api/v1/notifications/${noticeId}/view`,
    async ({ request }) => {
      confirmations.push(await request.json());
      return HttpResponse.json({ success: true });
    },
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 100, 100),
  );
  callbacks.length = 0;
  confirmations.length = 0;
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: IntersectionObserverCallback) {
        callbacks.push(callback);
      }
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );
});
afterEach(() => {
  server.resetHandlers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
afterAll(() => server.close());
function Failure({
  presented,
  runId = "run-1",
}: {
  presented: boolean;
  runId?: string;
}) {
  const root = useRef<HTMLDivElement>(null);
  useVisibleFailureNotice({ noticeId, runId, root, presented });
  return (
    <div ref={root} data-original-run>
      原运行失败记录
    </div>
  );
}
function wrap(presented: boolean, runId?: string) {
  const cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return {
    cache,
    content: (
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <Failure presented={presented} runId={runId} />
        </MemoryRouter>
      </QueryClientProvider>
    ),
  };
}
function show() {
  const target = document.querySelector(
    "[data-original-run], [data-notification-id]",
  );
  if (!target) throw new Error("缺少真实运行记录");
  const rect = new DOMRect(0, 0, 100, 100);
  act(() =>
    callbacks.at(-1)?.(
      [
        {
          target,
          isIntersecting: true,
          intersectionRect: rect,
        } as IntersectionObserverEntry,
      ],
      {} as IntersectionObserver,
    ),
  );
}
it("仅加载或预取不确认，匹配原运行在可见区域呈现后才提交该版本", async () => {
  const { cache, content } = wrap(false);
  const view = render(content);
  await waitFor(() => expect(cache.isFetching()).toBe(0));
  expect(confirmations).toEqual([]);
  expect(callbacks).toHaveLength(0);
  view.rerender(
    <QueryClientProvider client={cache}>
      <MemoryRouter>
        <Failure presented />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(callbacks).toHaveLength(1));
  show();
  show();
  await waitFor(() => expect(confirmations).toHaveLength(1));
  expect(confirmations[0]).toMatchObject({
    version: 1,
    target: { automaticRunId: "run-1" },
  });
});
it("另一原运行记录不能确认该失败通知", async () => {
  const { cache, content } = wrap(true, "run-2");
  render(content);
  await waitFor(() => expect(cache.isFetching()).toBe(0));
  expect(callbacks).toHaveLength(0);
  expect(confirmations).toEqual([]);
});
it("回执失败保留确认状态，组件重渲染不形成自动请求循环", async () => {
  let attempts = 0;
  server.use(
    http.post(`${origin}/api/v1/notifications/${noticeId}/view`, () => {
      attempts++;
      return HttpResponse.json({ error: "DatabaseError" }, { status: 500 });
    }),
  );
  const { cache, content } = wrap(true);
  const view = render(content);
  await waitFor(() => expect(callbacks).toHaveLength(1));
  show();
  await waitFor(() => expect(attempts).toBe(1));
  view.rerender(
    <QueryClientProvider client={cache}>
      <MemoryRouter>
        <Failure presented />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(attempts).toBe(1);
  expect(callbacks).toHaveLength(1);
});

it("从普通路由进入的真实失败正文也能确认，不依赖 notice 和 runId 查询参数", async () => {
  const cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={cache}>
      <MemoryRouter initialEntries={["/automatic-tasks/task-1"]}>
        <FailureNoticeContent runId="run-1">
          原运行失败记录
        </FailureNoticeContent>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(callbacks).toHaveLength(1));
  expect(confirmations).toEqual([]);
  show();
  await waitFor(() => expect(confirmations).toHaveLength(1));
  expect(confirmations[0]).toMatchObject({
    version: 1,
    target: { automaticRunId: "run-1" },
  });
  view.unmount();
  cache.clear();
});

it.each(["blur", "inert", "clipped"])(
  "失败正文%s时不确认，真正回到视区后统一确认",
  async (state) => {
    const { cache, content } = wrap(true);
    const view = render(content);
    await waitFor(() => expect(callbacks).toHaveLength(1));
    const target = document.querySelector<HTMLElement>("[data-original-run]");
    if (!target?.parentElement) throw new Error("缺少失败正文");
    const focus = vi.spyOn(document, "hasFocus");
    let outside = state === "clipped";
    Object.defineProperty(target, "getBoundingClientRect", {
      configurable: true,
      value: () => new DOMRect(0, outside ? 200 : 0, 100, 100),
    });
    target.parentElement.style.overflowY = "auto";
    if (state === "blur") focus.mockReturnValue(false);
    if (state === "inert") target.parentElement.setAttribute("inert", "");
    show();
    await act(async () => {
      await Promise.resolve();
    });
    expect(confirmations).toEqual([]);
    outside = false;
    target.parentElement.removeAttribute("inert");
    focus.mockReturnValue(true);
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(confirmations).toHaveLength(1));
    view.unmount();
    cache.clear();
  },
);

it("阅读提交失败不循环或伪装已读，失焦再回焦后可以重试同一版本", async () => {
  let attempts = 0;
  server.use(
    http.post(`${origin}/api/v1/notifications/${noticeId}/view`, () => {
      attempts++;
      return attempts === 1
        ? HttpResponse.json({ error: "DatabaseError" }, { status: 503 })
        : HttpResponse.json({ success: true });
    }),
  );
  const { cache, content } = wrap(true);
  const view = render(content);
  await waitFor(() => expect(callbacks).toHaveLength(1));
  show();
  await waitFor(() =>
    expect(
      cache
        .getMutationCache()
        .getAll()
        .some((mutation) => mutation.state.status === "error"),
    ).toBe(true),
  );
  expect(attempts).toBe(1);
  const focus = vi.spyOn(document, "hasFocus").mockReturnValue(false);
  act(() => window.dispatchEvent(new Event("blur")));
  focus.mockReturnValue(true);
  act(() => window.dispatchEvent(new Event("focus")));
  await waitFor(() => expect(attempts).toBe(2));
  await waitFor(() =>
    expect(
      cache
        .getMutationCache()
        .getAll()
        .some((mutation) => mutation.state.status === "success"),
    ).toBe(true),
  );
  view.unmount();
  cache.clear();
});
