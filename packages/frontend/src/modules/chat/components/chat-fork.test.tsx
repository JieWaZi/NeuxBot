import {
  QueryClient,
  QueryClientProvider,
  useInfiniteQuery,
} from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, useLocation } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import "@/shared/i18n";
import { DropdownMenu, DropdownMenuContent } from "@/shared/ui/dropdown-menu";
import {
  chatMessagesInfiniteOptions,
  mergeChatHistoryEvents,
} from "../api/chat-queries";
import {
  ChatForkMenuItem,
  ChatForkContext,
  MessageForkAction,
  useChatFork,
  messageForkTargets,
} from "./chat-fork";

const messageState = vi.hoisted(() => ({
  message: { id: "turn-1", status: { type: "complete" } },
}));
const { eventObservers } = vi.hoisted(() => ({
  eventObservers: new Set<GlobalEventObserver>(),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    eventObservers.add(observer);
    return () => eventObservers.delete(observer);
  },
}));
vi.mock("@assistant-ui/react", () => ({
  useAuiState: (selector: (state: typeof messageState) => unknown) =>
    selector(messageState),
}));
vi.mock("@/shared/ui/sonner", () => ({ toast: { error: vi.fn() } }));
const server = setupServer();
const api = "http://ally.test/api/v1/chats/parent";
const requests = new Map<string, string>();
beforeAll(() => {
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => requests.get(key) ?? null,
    setItem: (key: string, value: string) => requests.set(key, value),
    removeItem: (key: string) => requests.delete(key),
  });
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => {
  messageState.message.status.type = "complete";
  requests.clear();
  eventObservers.clear();
  cleanup();
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

function Harness({
  enabled = true,
  busy = false,
}: {
  enabled?: boolean;
  busy?: boolean;
}) {
  const source = useInfiniteQuery({
    ...chatMessagesInfiniteOptions("parent"),
    enabled,
  });
  const fork = useChatFork({
    messages: [...(source.data?.pages ?? [])]
      .reverse()
      .flatMap((page) => page.resources ?? []),
    latestExecutionId: source.data?.pages[0]?.forkLatestExecutionId,
    chatId: "parent",
    wakerId: "waker",
    enabled,
    busy,
  });
  const location = useLocation();
  return (
    <>
      <input aria-label="draft" defaultValue="保留草稿" />
      <output>{location.pathname + location.search}</output>
      <ChatForkContext value={enabled ? fork : undefined}>
        <MessageForkAction />
      </ChatForkContext>
    </>
  );
}
function mount(enabled = true, copies = 1) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/original"]}>
        {Array.from({ length: copies }, (_, index) => (
          <Harness key={index} enabled={enabled} />
        ))}
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
function targets(available = true) {
  server.use(
    http.get(`${api}/messages`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "turn-1",
            executionId: "turn-1",
            role: "assistant",
            status: "completed",
            fork: { available, reasonCode: available ? "" : "latest_only" },
          },
        ],
        forkLatestExecutionId: "turn-1",
        latestSequence: 1,
      }),
    ),
  );
}

describe("原生会话分叉入口", () => {
  it("能力随消息 Query 更新，发送至终态不新增分叉或消息读取", async () => {
    const reads = vi.fn();
    server.use(
      http.get(`${api}/messages`, () => {
        reads();
        return HttpResponse.json({
          resources: [
            {
              id: "turn-1",
              executionId: "turn-1",
              role: "assistant",
              status: "completed",
              fork: { available: true, latestOnly: true },
            },
          ],
          forkLatestExecutionId: "turn-1",
          latestSequence: 1,
        });
      }),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const content = (busy: boolean) => (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <Harness busy={busy} />
        </MemoryRouter>
      </QueryClientProvider>
    );
    const view = render(content(false));
    expect(
      await screen.findByRole("button", { name: "分支到新聊天" }),
    ).toBeEnabled();
    view.rerender(content(true));
    expect(screen.queryByRole("button", { name: "分支到新聊天" })).toBeNull();
    act(() =>
      mergeChatHistoryEvents(queryClient, "parent", [
        {
          sequence: 2,
          type: "execution.created",
          message: {
            id: "turn-2",
            executionId: "turn-2",
            role: "assistant",
            status: "running",
            fork: { available: false, reasonCode: "unfinished" },
          },
        },
        { sequence: 3, type: "invocation.finished" },
        {
          sequence: 4,
          type: "execution.completed",
          message: {
            id: "turn-2",
            executionId: "turn-2",
            role: "assistant",
            status: "completed",
            fork: { available: true, latestOnly: true },
          },
        },
      ]),
    );
    view.rerender(content(false));
    expect(
      await screen.findByRole("button", { name: "分支到新聊天" }),
    ).toBeDisabled();
    expect(reads).toHaveBeenCalledTimes(1);
    expect(eventObservers.size).toBe(0);
  });
  it("运行中的回复即使仍有可用目标缓存也不显示分叉，结束后才显示", () => {
    const controls = {
      targets: [{ executionId: "turn-1", available: true, latest: true }],
      pending: false,
      busy: false,
      fork: vi.fn(),
    };
    messageState.message.status.type = "running";
    const view = render(
      <ChatForkContext value={controls}>
        <MessageForkAction />
      </ChatForkContext>,
    );
    expect(
      screen.queryByRole("button", { name: "分支到新聊天" }),
    ).not.toBeInTheDocument();
    messageState.message.status.type = "complete";
    view.rerender(
      <ChatForkContext value={controls}>
        <MessageForkAction />
      </ChatForkContext>,
    );
    expect(screen.getByRole("button", { name: "分支到新聊天" })).toBeEnabled();
  });
  it("会话菜单仅提交后端标记的最新回合", async () => {
    server.use(
      http.get(`${api}/messages`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "old-turn",
              executionId: "old-turn",
              role: "assistant",
              status: "completed",
              fork: { available: true, latestOnly: true },
            },
            {
              id: "latest-turn",
              executionId: "latest-turn",
              role: "assistant",
              status: "completed",
              fork: { available: true, latestOnly: true },
            },
          ],
          forkLatestExecutionId: "latest-turn",
          latestSequence: 1,
        }),
      ),
    );
    const submitted = vi.fn();
    server.use(
      http.post(`${api}/fork`, async ({ request }) => {
        submitted(await request.json());
        return HttpResponse.json({ chatId: "child" });
      }),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <DropdownMenu open>
            <DropdownMenuContent>
              <ChatForkMenuItem chatId="parent" wakerId="waker" enabled />
            </DropdownMenuContent>
          </DropdownMenu>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    const item = await screen.findByRole("menuitem", { name: "分支到新聊天" });
    await waitFor(() => expect(item).not.toHaveAttribute("data-disabled"));
    fireEvent.click(item);
    await waitFor(() =>
      expect(submitted).toHaveBeenCalledWith({
        executionId: "latest-turn",
        clientRequestId: expect.any(String),
      }),
    );
  });
  it("使用后端禁用原因，团队不显示入口", async () => {
    targets(false);
    mount();
    expect(
      await screen.findByRole("button", { name: "分支到新聊天" }),
    ).toBeDisabled();
    expect(screen.getByRole("button").parentElement).toHaveAttribute(
      "title",
      "此智能体仅支持从对话末尾创建分支",
    );
    cleanup();
    mount(false);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
  it("当前会话执行中时隐藏旧回复的分叉入口", () => {
    const controls = {
      targets: [{ executionId: "turn-1", available: true, latest: true }],
      pending: false,
      busy: true,
      fork: vi.fn(),
    };
    render(
      <ChatForkContext value={controls}>
        <MessageForkAction />
      </ChatForkContext>,
    );
    expect(screen.queryByRole("button", { name: "分支到新聊天" })).toBeNull();
  });
  it("执行中禁用会话菜单并提示等待", async () => {
    targets();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <DropdownMenu open>
            <DropdownMenuContent>
              <ChatForkMenuItem chatId="parent" wakerId="waker" enabled busy />
            </DropdownMenuContent>
          </DropdownMenu>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    const item = await screen.findByRole("menuitem", { name: "分支到新聊天" });
    expect(item).toHaveAttribute("data-disabled");
    expect(item.parentElement).toHaveAttribute(
      "title",
      "请等待当前执行和交互结束",
    );
  });
  it("多个入口共用即时互斥，成功导航新会话", async () => {
    targets();
    let calls = 0;
    let finish!: () => void;
    const gate = new Promise<void>((resolve) => {
      finish = resolve;
    });
    server.use(
      http.post(`${api}/fork`, async ({ request }) => {
        calls++;
        expect(await request.json()).toEqual({
          executionId: "turn-1",
          clientRequestId: expect.any(String),
        });
        await gate;
        return HttpResponse.json({ chatId: "child" });
      }),
    );
    mount(true, 2);
    await waitFor(() => expect(screen.getAllByRole("button")).toHaveLength(2));
    for (const button of screen.getAllByRole("button")) fireEvent.click(button);
    await waitFor(() => expect(calls).toBe(1));
    expect(
      screen
        .getAllByRole("button")
        .every((button) => button.hasAttribute("disabled")),
    ).toBe(true);
    finish();
    await waitFor(() =>
      expect(screen.getAllByRole("status")[0]?.textContent).toContain("child"),
    );
  });
  it("失败保留页面与草稿，重试复用 clientRequestId", async () => {
    targets();
    const bodies: unknown[] = [];
    server.use(
      http.post(`${api}/fork`, async ({ request }) => {
        bodies.push(await request.json());
        return bodies.length === 1
          ? HttpResponse.json(
              { message: "原生结果待对账", code: "CHAT_FORK_INDETERMINATE" },
              { status: 409 },
            )
          : HttpResponse.json({ chatId: "child" });
      }),
    );
    mount();
    fireEvent.click(await screen.findByRole("button"));
    const { toast } = await import("@/shared/ui/sonner");
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("原生结果待对账"),
    );
    expect(screen.getByRole("status")).toHaveTextContent("/original");
    expect(screen.getByLabelText("draft")).toHaveValue("保留草稿");
    cleanup();
    mount();
    fireEvent.click(await screen.findByRole("button"));
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain("child"),
    );
    expect(bodies[0]).toEqual(bodies[1]);
  });
});

it("末轮身份取自权威head，旧页和继承消息不猜测执行身份", () => {
  expect(
    messageForkTargets(
      [
        {
          id: "old",
          executionId: "old",
          role: "assistant",
          status: "completed",
          fork: { available: true, latestOnly: true },
        },
      ],
      "new-outside-page",
    ),
  ).toEqual([
    {
      executionId: "old",
      available: false,
      latest: false,
      reasonCode: "latest_only",
    },
  ]);
  expect(
    messageForkTargets(
      [
        {
          id: "inherited",
          executionId: "source",
          inherited: true,
          role: "assistant",
          status: "completed",
          fork: { available: true, latestOnly: true },
        },
      ],
      "inherited",
    ),
  ).toEqual([
    {
      executionId: "inherited",
      available: true,
      latest: true,
      reasonCode: undefined,
    },
  ]);
});

it("未打开Chat的运行中菜单不读取，保持打开并空闲后一次权威读取恢复可用", async () => {
  let reads = 0;
  server.use(
    http.get(`${api}/messages`, () => {
      reads++;
      return HttpResponse.json({
        resources: [
          {
            id: "turn-1",
            executionId: "turn-1",
            role: "assistant",
            status: "completed",
            fork: { available: true },
          },
        ],
        forkLatestExecutionId: "turn-1",
        latestSequence: 5,
      });
    }),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const content = (busy: boolean) => (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <DropdownMenu open>
          <DropdownMenuContent>
            <ChatForkMenuItem chatId="parent" enabled busy={busy} />
          </DropdownMenuContent>
        </DropdownMenu>
      </MemoryRouter>
    </QueryClientProvider>
  );
  const view = render(content(true));
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(reads).toBe(0);
  view.rerender(content(false));
  await waitFor(() =>
    expect(
      screen.getByRole("menuitem", { name: "分支到新聊天" }),
    ).not.toHaveAttribute("data-disabled"),
  );
  expect(reads).toBe(1);
  view.unmount();
  queryClient.clear();
});
it("菜单打开与忙转闲都核对已有新鲜running缓存，不依赖缓存TTL", async () => {
  let reads = 0;
  let turn = "turn-1";
  server.use(
    http.get(`${api}/messages`, () => {
      reads++;
      return HttpResponse.json({
        resources: [
          {
            id: turn,
            executionId: turn,
            role: "assistant",
            status: "completed",
            fork: { available: true },
          },
        ],
        forkLatestExecutionId: turn,
        latestSequence: reads + 10,
      });
    }),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClient.setQueryData(["chats", "messages", "parent"], {
    pages: [
      {
        resources: [
          {
            id: "turn-1",
            executionId: "turn-1",
            role: "assistant",
            status: "running",
            fork: { available: false, reasonCode: "unfinished" },
          },
        ],
        forkLatestExecutionId: "turn-1",
        latestSequence: 1,
      },
    ],
    pageParams: [undefined],
  });
  const content = (enabled: boolean, busy: boolean) => (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <DropdownMenu open>
          <DropdownMenuContent>
            <ChatForkMenuItem chatId="parent" enabled={enabled} busy={busy} />
          </DropdownMenuContent>
        </DropdownMenu>
      </MemoryRouter>
    </QueryClientProvider>
  );
  const view = render(content(true, false));
  await waitFor(() =>
    expect(
      screen.getByRole("menuitem", { name: "分支到新聊天" }),
    ).not.toHaveAttribute("data-disabled"),
  );
  expect(reads).toBe(1);
  view.rerender(content(true, true));
  turn = "turn-2";
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(reads).toBe(1);
  view.rerender(content(true, false));
  await waitFor(() => expect(reads).toBe(2));
  await waitFor(() =>
    expect(
      screen.getByRole("menuitem", { name: "分支到新聊天" }),
    ).not.toHaveAttribute("data-disabled"),
  );
  view.rerender(content(false, false));
  view.rerender(content(true, false));
  await waitFor(() => expect(reads).toBe(3));
  view.unmount();
  queryClient.clear();
});
