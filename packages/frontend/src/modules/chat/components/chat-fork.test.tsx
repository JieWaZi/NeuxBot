import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
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
import "@/shared/i18n";
import { DropdownMenu, DropdownMenuContent } from "@/shared/ui/dropdown-menu";
import {
  ChatForkMenuItem,
  ChatForkContext,
  MessageForkAction,
  useChatFork,
} from "./chat-fork";

const messageState = vi.hoisted(() => ({
  message: { id: "turn-1", status: { type: "complete" } },
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
  cleanup();
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

function Harness({ enabled = true }: { enabled?: boolean }) {
  const fork = useChatFork({ chatId: "parent", wakerId: "waker", enabled });
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
    http.get(`${api}/fork-targets`, () =>
      HttpResponse.json([
        {
          executionId: "turn-1",
          available,
          latest: true,
          reasonCode: available ? "" : "latest_only",
        },
      ]),
    ),
  );
}

describe("原生会话分叉入口", () => {
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
      http.get(`${api}/fork-targets`, () =>
        HttpResponse.json([
          { executionId: "old-turn", available: false, latest: false },
          { executionId: "latest-turn", available: true, latest: true },
        ]),
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
