import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { StrictMode } from "react";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import "@/shared/i18n";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { WorkbenchProvider } from "@/modules/chat/workbench/workbench-store";
import { client } from "@/shared/api/generated/client.gen";
import type {
  CreateChannelThreadResponse,
  ListChannelTimelineResponse,
} from "@/shared/api/generated/types.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import { TeamThreadPage } from "./team-thread-page";

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => false,
  hasPlatformService: () => false,
  subscribeFileDrop: () => () => undefined,
}));
vi.mock("@/shared/api/events", () => ({
  subscribeChannel: () => () => undefined,
  subscribeChat: () => () => undefined,
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

const api = "http://ally.test/api/v1";
const server = setupServer(
  http.get(`${api}/ai-accounts`, () => HttpResponse.json({ resources: [] })),
);
const originalScrollTo = HTMLElement.prototype.scrollTo;
beforeEach(() => {
  const preferences = new Map<string, string>();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => preferences.get(key) ?? null,
      setItem: (key: string, value: string) => preferences.set(key, value),
      removeItem: (key: string) => preferences.delete(key),
    },
  });
});
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
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
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
  HTMLElement.prototype.scrollTo = originalScrollTo;
});

describe("团队首发完整路由交接", () => {
  it.each<{
    withExecution: boolean;
    status: "running" | "completed";
    historyFails?: boolean;
  }>([
    { withExecution: true, status: "running" },
    { withExecution: false, status: "running" },
    { withExecution: false, status: "completed" },
    { withExecution: false, status: "running", historyFails: true },
  ])(
    "创建返回 execution=$withExecution，历史状态=$status，首次查询失败=$historyFails，消息在交接中持续可见",
    async ({ withExecution, status, historyFails }) => {
      const creation = deferred<CreateChannelThreadResponse>();
      const history = deferred<ListChannelTimelineResponse>();
      const received = vi.fn();
      let historyRequests = 0;
      const text = "团队首发连续可见";
      const thread = {
        id: "thread-1",
        leadChatId: "lead-chat",
        leadWakerId: "lead",
        title: text,
        workspaceDirectory: "/tmp/ally",
      };
      const members: CreateChannelThreadResponse["members"] = [
        {
          wakerId: "lead",
          chatId: "lead-chat",
          isLead: true,
          runtimeId: "codex",
          model: "gpt-5",
          permissionLevel: "full-access",
        },
        {
          wakerId: "member",
          chatId: "member-chat",
          isLead: false,
          runtimeId: "codex",
        },
      ];
      const execution: NonNullable<
        CreateChannelThreadResponse["leadExecution"]
      > = {
        id: "execution-1",
        userMessageId: "user-1",
        status: "running",
      };
      server.use(
        http.get(`${api}/channels/team-1`, () =>
          HttpResponse.json({
            channel: { id: "team-1", name: "研发团队", leadWakerId: "lead" },
            members: [
              {
                waker: { id: "lead", name: "团队负责人" },
                runtimeId: "codex",
                model: "gpt-5",
                permissionLevel: "full-access",
              },
              { waker: { id: "member", name: "开发成员" }, runtimeId: "codex" },
            ],
          }),
        ),
        http.get(`${api}/wakers`, () =>
          HttpResponse.json({
            resources: [
              { id: "lead", name: "团队负责人" },
              { id: "member", name: "开发成员" },
            ],
          }),
        ),
        http.get(`${api}/runtimes`, () =>
          HttpResponse.json({
            resources: [
              {
                id: "codex",
                name: "Codex",
                enabled: true,
                state: "ready",
                defaultModel: "gpt-5",
                defaultPermissionLevel: "full-access",
                models: [
                  {
                    id: "gpt-5",
                    name: "GPT-5",
                    permissionLevels: ["default", "auto", "full-access"],
                  },
                ],
              },
            ],
          }),
        ),
        http.get(`${api}/wakers/lead/projects`, () =>
          HttpResponse.json({
            resources: [
              {
                id: "project",
                name: "项目",
                available: true,
                sources: [
                  { path: "/tmp/ally", available: true, isDefault: true },
                ],
              },
            ],
          }),
        ),
        http.get(`${api}/chats`, () =>
          HttpResponse.json({
            resources: [],
            recentProjectId: "project",
            hasMore: false,
          }),
        ),
        http.get(`${api}/wakers/lead/skills`, () => HttpResponse.json([])),
        http.get(`${api}/wakers/lead/connectors`, () => HttpResponse.json([])),
        http.post(`${api}/channels/team-1/threads`, async () => {
          received();
          return HttpResponse.json(await creation.promise);
        }),
        http.get(`${api}/channels/team-1/threads/thread-1`, () =>
          HttpResponse.json({
            thread,
            members,
            tasks: [],
            leadProcessing: status === "running",
            leadActiveExecutionId:
              status === "running" ? execution.id : undefined,
          }),
        ),
        http.get(
          `${api}/channels/team-1/threads/thread-1/timeline`,
          async () => {
            const requestNumber = ++historyRequests;
            const snapshot = await history.promise;
            return historyFails && requestNumber === 1
              ? HttpResponse.json(
                  { message: "timeline unavailable" },
                  { status: 500 },
                )
              : HttpResponse.json(snapshot);
          },
        ),
        http.get(`${api}/chats/lead-chat`, () =>
          HttpResponse.json({
            chat: {
              id: "lead-chat",
              wakerId: "lead",
              workspaceDirectory: "/tmp/ally",
              activeExecutionId: withExecution ? execution.id : undefined,
            },
          }),
        ),
        http.get(`${api}/chats/lead-chat/messages`, () =>
          HttpResponse.json({ resources: [], hasMore: false }),
        ),
      );
      const queryClient = new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      });
      const router = createMemoryRouter(
        [
          {
            element: (
              <Outlet context={{ setChatRightPanel: () => undefined }} />
            ),
            children: [
              {
                path: "/teams/:channelId",
                element: <TeamThreadPage />,
                children: [
                  { index: true, element: <></> },
                  { path: "threads/:threadId", element: <></> },
                ],
              },
              { path: "/other", element: <div>其他页面</div> },
            ],
          },
        ],
        { initialEntries: ["/teams/team-1?draftId=first-send"] },
      );
      const view = render(
        <StrictMode>
          <QueryClientProvider client={queryClient}>
            <TooltipProvider>
              <WorkspaceProvider>
                <WorkbenchProvider>
                  <ChatQueueProvider>
                    <RouterProvider router={router} />
                  </ChatQueueProvider>
                </WorkbenchProvider>
              </WorkspaceProvider>
            </TooltipProvider>
          </QueryClientProvider>
        </StrictMode>,
      );
      await screen.findByText("GPT-5");
      await userEvent.setup().type(screen.getByRole("textbox"), text);
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled(),
      );
      fireEvent.click(screen.getByRole("button", { name: "发送消息" }));
      await waitFor(() => expect(received).toHaveBeenCalledOnce());
      expect(screen.getByText(text)).toBeVisible();
      expect(view.container.querySelector(".agent-grid-spinner")).toBeVisible();

      const frames: Array<{
        loading: boolean;
        message: boolean;
        running: boolean;
        composer: boolean;
      }> = [];
      const observe = () =>
        frames.push({
          loading: Boolean(
            view.container.querySelector('[role="status"][aria-busy="true"]'),
          ),
          message: Boolean(
            view.container
              .querySelector("[data-chat-scroll-viewport]")
              ?.textContent?.includes(text),
          ),
          running: Boolean(view.container.querySelector(".agent-grid-spinner")),
          composer: Boolean(view.container.querySelector('[role="textbox"]')),
        });
      const observer = new MutationObserver(observe);
      observer.observe(view.container, {
        subtree: true,
        childList: true,
        attributes: true,
        characterData: true,
      });
      try {
        await act(async () =>
          creation.resolve({
            thread,
            members,
            tasks: [],
            replayed: !withExecution,
            leadExecution: withExecution ? execution : undefined,
          }),
        );
        await waitFor(() =>
          expect(router.state.location.pathname).toBe(
            "/teams/team-1/threads/thread-1",
          ),
        );
        expect(screen.getByText(text)).toBeVisible();
        expect(
          view.container.querySelector(".agent-grid-spinner"),
        ).toBeVisible();
        expect(
          frames.filter(
            (frame) =>
              frame.loading ||
              !frame.message ||
              !frame.running ||
              !frame.composer,
          ),
        ).toEqual([]);
        await act(async () =>
          history.resolve({
            resources: [
              {
                chatId: "lead-chat",
                wakerId: "lead",
                coordinator: true,
                message: {
                  id: "user-1",
                  role: "user",
                  actor: "user",
                  content: text,
                  revision: 1,
                },
              },
              {
                chatId: "lead-chat",
                wakerId: "lead",
                coordinator: true,
                message: {
                  id: execution.id,
                  executionId: execution.id,
                  role: "assistant",
                  status,
                  revision: 1,
                  displayParts: [
                    { id: "reply", type: "text", text: "负责人的真实回复" },
                  ],
                },
              },
            ],
            hasMore: false,
          }),
        );
        if (historyFails) {
          const retry = await screen.findByRole("button", { name: "重试" });
          expect(retry).toBeVisible();
          expect(screen.getByText(text)).toBeVisible();
          fireEvent.click(retry);
        }
        expect(
          await screen.findByText("负责人的真实回复", {}, { timeout: 5_000 }),
        ).toBeVisible();
        expect(
          screen.queryByText("暂时无法载入团队对话。"),
        ).not.toBeInTheDocument();
        expect(received).toHaveBeenCalledOnce();
        expect(screen.getAllByText(text)).toHaveLength(1);
        observe();
        expect(frames.length).toBeGreaterThan(0);
        expect(
          frames.filter(
            (frame) => frame.loading || !frame.message || !frame.composer,
          ),
        ).toEqual([]);
        observer.disconnect();
        await act(async () => {
          await router.navigate("/other");
        });
        await act(async () => {
          await router.navigate("/teams/team-1/threads/thread-1");
        });
        expect(
          await screen.findByText("负责人的真实回复", {}, { timeout: 5_000 }),
        ).toBeVisible();
        expect(screen.getAllByText(text)).toHaveLength(1);
        if (status === "completed")
          expect(
            view.container.querySelector(".agent-grid-spinner"),
          ).toBeNull();
      } finally {
        observer.disconnect();
        view.unmount();
        router.dispose();
        queryClient.clear();
      }
    },
  );
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
