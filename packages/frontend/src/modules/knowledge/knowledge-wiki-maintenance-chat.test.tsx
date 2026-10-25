import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { PERSONAL_ASSISTANT_WAKER_ID } from "@/modules/waker";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { WorkbenchProvider } from "@/modules/chat/workbench/workbench-store";
import { client } from "@/shared/api/generated/client.gen";
import { toApiError } from "@/shared/api/errors";
import { ThemeProvider } from "@/shared/theme";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { KnowledgePage } from "./knowledge-page";
import { KnowledgeWikiMaintenanceChat } from "./knowledge-wiki-maintenance-chat";

const scene = vi.hoisted(() => ({ active: true }));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => scene.active,
}));
vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: vi.fn() }));
vi.mock("@/shared/api/events", () => ({ subscribeChat: () => () => {} }));
const api = "http://wiki-maintenance.test/api/v1";
const resource = {
  backend: "local",
  connectionId: "local",
  knowledgeBaseId: "kb",
};
const page = {
  id: "one",
  slug: "concept/one",
  title: "原页面",
  version: 3,
  content: "保留可见的 Wiki 正文",
  pageType: "concept",
};
const server = setupServer(
  http.get(`${api}/runtimes`, () =>
    HttpResponse.json({
      resources: [
        {
          id: "codex",
          name: "Codex",
          enabled: true,
          state: "ready",
          isDefault: true,
          defaultModel: "native",
          defaultPermissionLevel: "default",
          models: [
            {
              id: "native",
              name: "Native Model",
              permissionLevels: ["default"],
            },
          ],
        },
      ],
    }),
  ),
  http.get(`${api}/ai-accounts`, () => HttpResponse.json({ resources: [] })),
  http.get(`${api}/wakers/${PERSONAL_ASSISTANT_WAKER_ID}`, () =>
    HttpResponse.json({
      id: PERSONAL_ASSISTANT_WAKER_ID,
      name: "内置私人助理",
    }),
  ),
  http.get(`${api}/knowledge/libraries/kb`, () =>
    HttpResponse.json({
      id: "kb",
      name: "Wiki资料库",
      ref: resource,
      version: 1,
      structureVersion: 1,
    }),
  ),
  http.get(`${api}/knowledge/libraries/kb/source-purges`, () =>
    HttpResponse.json({ items: [], total: 0 }),
  ),
  http.get(`${api}/knowledge/libraries/kb/sources`, () =>
    HttpResponse.json({ items: [], total: 0 }),
  ),
  http.get(`${api}/knowledge/libraries/kb/page-directory`, () =>
    HttpResponse.json({ items: [page], total: 1 }),
  ),
  http.get(`${api}/knowledge/libraries/kb/pages/one`, () =>
    HttpResponse.json(page),
  ),
  http.get(`${api}/knowledge/libraries/kb/themes`, () =>
    HttpResponse.json({ items: [] }),
  ),
);
let errorInterceptor: ReturnType<typeof client.interceptors.error.use>;
beforeAll(() => {
  errorInterceptor = client.interceptors.error.use((error, response) =>
    toApiError(error, response?.status),
  );
  window.localStorage.setItem("ally.ui.theme", "light");
  client.setConfig({ baseUrl: "http://wiki-maintenance.test" });
  server.listen({ onUnhandledRequest: "error" });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  HTMLElement.prototype.scrollTo = vi.fn();
});
afterEach(() => {
  server.resetHandlers();
  scene.active = true;
});
afterAll(() => {
  client.interceptors.error.eject(errorInterceptor);
  server.close();
  vi.unstubAllGlobals();
});
function show(inPage = false) {
  let shownPage = page;
  let openRequest = 1;
  const ui = () => (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <TooltipProvider>
          <WorkbenchProvider>
            <ChatQueueProvider>
              <MemoryRouter
                initialEntries={["/knowledge/kb?view=wiki&page=one"]}
              >
                {inPage ? (
                  <Routes>
                    <Route
                      path="/knowledge/:libraryId"
                      element={<KnowledgePage />}
                    />
                  </Routes>
                ) : (
                  <KnowledgeWikiMaintenanceChat
                    resource={resource}
                    page={shownPage}
                    openRequest={openRequest}
                  />
                )}
              </MemoryRouter>
            </ChatQueueProvider>
          </WorkbenchProvider>
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(ui());
  return {
    ...view,
    rerenderScene: () => view.rerender(ui()),
    reopenPage: (nextPage: typeof page) => {
      shownPage = nextPage;
      openRequest++;
      view.rerender(ui());
    },
  };
}
it("打开 AI 修订时保留 Wiki 与目录导航，浮窗宿主不占用 Scene 高度", async () => {
  const user = userEvent.setup();
  const view = show(true);
  const content = await screen.findByText("保留可见的 Wiki 正文");
  const dock = screen.getByRole("radio", { name: /^知识/ });
  await user.click(screen.getByRole("button", { name: "AI 修订" }));
  expect(await screen.findByRole("region", { name: "悬浮对话" })).toBeVisible();
  expect(screen.getByText("保留可见的 Wiki 正文")).toBe(content);
  expect(screen.getByRole("radio", { name: /^知识/ })).toBe(dock);
  // jsdom 不计算布局；隐藏属性验证宿主退出普通流，避免 h-full 将后面的 Wiki 挤出 Scene。
  const host = view.container.querySelector("[data-floating-chat-normal-host]");
  expect(host?.closest("[hidden]")).not.toBeNull();
  expect(host).not.toBeVisible();
  await user.click(screen.getByRole("button", { name: "收起对话窗口" }));
  expect(content).toBeVisible();
  expect(host).not.toBeVisible();
});
it("首条修订直接读取内置伙伴，并沿用普通聊天的默认模型发送", async () => {
  const requests: Record<string, unknown>[] = [];
  let partnerReads = 0;
  server.use(
    http.get(`${api}/wakers/${PERSONAL_ASSISTANT_WAKER_ID}`, () => {
      partnerReads++;
      return HttpResponse.json({
        id: PERSONAL_ASSISTANT_WAKER_ID,
        name: "内置私人助理",
      });
    }),
    http.post(
      `${api}/knowledge/libraries/kb/maintenance-chats`,
      async ({ request }) => {
        requests.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({
          chat: { id: "chat", wakerId: PERSONAL_ASSISTANT_WAKER_ID },
          execution: { id: "execution" },
        });
      },
    ),
    http.get(`${api}/chats/chat`, () =>
      HttpResponse.json({
        chat: { id: "chat", wakerId: PERSONAL_ASSISTANT_WAKER_ID },
      }),
    ),
    http.get(`${api}/chats/chat/messages`, () =>
      HttpResponse.json({ resources: [], latestSequence: 0 }),
    ),
  );
  const user = userEvent.setup();
  show();
  await waitFor(() => expect(partnerReads).toBeGreaterThan(0));
  const send = await screen.findByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]).toMatchObject({
    pageId: "one",
    expectedVersion: 3,
    runtimeId: "codex",
    accountId: "system",
    model: "native",
    permissionLevel: "default",
  });
  expect(
    screen.queryByRole("combobox", { name: /伙伴/ }),
  ).not.toBeInTheDocument();
});
it("隐藏知识 Scene 时释放悬浮层，重新显示仍绑定原页面和版本", async () => {
  const view = show();
  await screen.findByRole("region", { name: "悬浮对话" });
  expect(screen.getByText("当前页：原页面 · 版本 3")).toBeInTheDocument();
  scene.active = false;
  view.rerenderScene();
  expect(
    screen.queryByRole("region", { name: "悬浮对话" }),
  ).not.toBeInTheDocument();
  scene.active = true;
  view.rerenderScene();
  await screen.findByRole("region", { name: "悬浮对话" });
  expect(screen.getByText("当前页：原页面 · 版本 3")).toBeInTheDocument();
});

it("首次接纳前版本冲突后，再次打开当前页修订可使用新版本继续", async () => {
  const requests: Record<string, unknown>[] = [];
  server.use(
    http.post(
      `${api}/knowledge/libraries/kb/maintenance-chats`,
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        requests.push(body);
        return HttpResponse.json(
          { code: "KnowledgeConflict", message: "页面版本已变化" },
          { status: 409 },
        );
      },
    ),
  );
  const user = userEvent.setup();
  const view = show();
  const send = await screen.findByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await screen.findAllByText("页面版本已变化");
  await waitFor(() => expect(send).toBeEnabled());
  view.reopenPage({ ...page, version: 4 });
  expect(screen.getByText("当前页：原页面 · 版本 4")).toBeInTheDocument();
  await user.click(send);
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[0]).toMatchObject({ expectedVersion: 3 });
  expect(requests[1]).toMatchObject({ expectedVersion: 4 });
  expect(requests[1]?.requestId).not.toBe(requests[0]?.requestId);
});

it("修订接纳明确版本冲突时回读 Wiki，重开后无需依赖事件即可采用新版本", async () => {
  let currentPage = page;
  const requests: Record<string, unknown>[] = [];
  server.use(
    http.get(`${api}/knowledge/libraries/kb/pages/one`, () =>
      HttpResponse.json(currentPage),
    ),
    http.post(
      `${api}/knowledge/libraries/kb/maintenance-chats`,
      async ({ request }) => {
        requests.push((await request.json()) as Record<string, unknown>);
        currentPage = {
          ...page,
          version: 4,
          content: "其他会话已修改当前 Wiki",
        };
        return HttpResponse.json(
          { code: "KnowledgeConflict", message: "页面版本已变化" },
          { status: 409 },
        );
      },
    ),
  );
  const user = userEvent.setup();
  show(true);
  await user.click(await screen.findByRole("button", { name: "AI 修订" }));
  const send = await screen.findByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await screen.findByText("其他会话已修改当前 Wiki");
  expect(screen.getByText("当前页：原页面 · 版本 3")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "AI 修订" }));
  expect(screen.getByText("当前页：原页面 · 版本 4")).toBeInTheDocument();
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]).toMatchObject({ expectedVersion: 4 });
});
