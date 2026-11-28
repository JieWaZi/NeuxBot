import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";
import type * as ChatEvents from "@/shared/api/events";
import type { subscribeChat } from "@/shared/api/events";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import { ApiError } from "@/shared/api/errors";
import type { ChatFileOpenRequest } from "./chat-attachment";
import { MainConversationSource } from "./main-conversation-source";

const source = vi.hoisted(() => vi.fn());
const subscribe = vi.hoisted(() => vi.fn<typeof subscribeChat>());
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  getMainConversationSource: source,
  getWaker: async () => ({ data: { id: "partner", name: "伙伴" } }),
}));
vi.mock("@/shared/api/events", async (original) => ({
  ...(await original<typeof ChatEvents>()),
  subscribeChat: subscribe,
}));
vi.mock("./chat-thread", () => ({
  ChatThread: ({
    messages,
    historyError,
    onOpenFile,
  }: {
    messages: { id: string; content?: string }[];
    historyError?: { message: string };
    onOpenFile?: (file: ChatFileOpenRequest) => void;
  }) => (
    <>
      {messages.map((m) => (
        <p key={m.id}>{m.content}</p>
      ))}
      {historyError && <p role="alert">{historyError.message}</p>}
      {onOpenFile && (
        <button
          type="button"
          onClick={() =>
            onOpenFile({
              name: "原件.png",
              path: "attachments/submission/原件.png",
              fileSource: "chat",
            })
          }
        >
          原件.png
        </button>
      )}
    </>
  ),
}));
vi.mock("./main-conversation-composer", () => ({
  MainConversationComposer: ({
    workChatId,
    ready,
  }: {
    workChatId: string;
    ready: boolean;
  }) => <button disabled={!ready}>{workChatId}</button>,
}));

function renderSource(onOpenFile = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MainConversationSource
        subject={{ subjectType: "waker", subjectId: "partner" }}
        workChatId="original"
        active
        onOpenFile={onOpenFile}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  source.mockReset();
  subscribe.mockReset().mockReturnValue(() => {});
});

it("来源对话把消息附件交给普通文件入口并保留来源身份", async () => {
  const openFile = vi.fn();
  source.mockResolvedValue({
    data: {
      chat: {
        id: "original",
        wakerId: "partner",
        workspaceDirectory: "/project",
      },
      resources: [],
    },
  });
  renderSource(openFile);
  fireEvent.click(await screen.findByRole("button", { name: "原件.png" }));
  expect(openFile).toHaveBeenCalledExactlyOnceWith({
    name: "原件.png",
    path: "attachments/submission/原件.png",
    fileSource: "chat",
    sourceChatId: "original",
  });
});

it("来源授权后显示未持久化的正文增量，结束事件从同一来源取回确认结果", async () => {
  const message = {
    id: "assistant",
    role: "assistant",
    executionId: "run",
    revision: 1,
    content: "第一步",
    status: "streaming",
    displayParts: [{ id: "part", type: "text", text: "第一步" }],
  };
  source.mockResolvedValue({
    data: {
      chat: { id: "original", wakerId: "partner" },
      resources: [message],
      latestSequence: 1,
    },
  });
  renderSource();
  expect(await screen.findByText("第一步")).toBeVisible();
  await waitFor(() => expect(subscribe).toHaveBeenCalled());
  const stream = subscribe.mock.calls.at(-1);
  expect(stream?.[0]).toBe("original");
  await act(async () =>
    stream?.[1].onEvent({
      transient: true,
      live: {
        generation: "one",
        sequence: 1,
        messageId: "assistant",
        executionId: "run",
        baseRevision: 1,
        revision: 2,
        updates: [
          {
            baseRevision: 1,
            revision: 2,
            updatedAt: 1,
            parts: [
              {
                kind: "text_append",
                partId: "part",
                partType: "text",
                text: "，正在查代码",
              },
            ],
          },
        ],
      },
    }),
  );
  expect(await screen.findByText("第一步，正在查代码")).toBeVisible();
  expect(source).toHaveBeenCalledTimes(1);
  source.mockResolvedValue({
    data: {
      chat: { id: "original", wakerId: "partner" },
      resources: [
        { ...message, status: "completed", revision: 3, content: "已完成核对" },
      ],
      latestSequence: 3,
    },
  });
  await act(async () =>
    stream?.[1].onEvent({ type: "execution.completed", sequence: 3 }),
  );
  expect(await screen.findByText("已完成核对")).toBeVisible();
  expect(screen.getByRole("button", { name: "original" })).toBeEnabled();
  expect(source.mock.lastCall?.[0].path).toEqual({
    subjectType: "waker",
    subjectId: "partner",
    workChatId: "original",
  });
});
it("来源读取被拒时不订阅原工作或放开输入", async () => {
  source.mockRejectedValue(new Error("来源不可读"));
  renderSource();
  await waitFor(() => expect(source).toHaveBeenCalledOnce());
  expect(subscribe).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "original" }),
  ).not.toBeInTheDocument();
});

it.each([
  { error: new ApiError("暂时读不到来源", "NETWORK"), retained: true },
  { error: new ApiError("来源已失效", "ChatNotFound", 404), retained: false },
])(
  "来源刷新失败时按授权结果保留已确认工作历史 $retained",
  async ({ error, retained }) => {
    source.mockResolvedValueOnce({
      data: {
        chat: { id: "original", wakerId: "partner" },
        resources: [
          { id: "saved", role: "assistant", content: "已确认的工作记录" },
        ],
        latestSequence: 1,
      },
    });
    renderSource();
    expect(await screen.findByText("已确认的工作记录")).toBeVisible();
    await waitFor(() => expect(subscribe).toHaveBeenCalled());
    source.mockRejectedValue(error);
    await act(async () =>
      subscribe.mock.lastCall?.[1].onEvent({
        type: "execution.completed",
        sequence: 2,
      }),
    );
    expect(await screen.findByRole("alert")).toBeVisible();
    if (retained) {
      expect(screen.getByText("已确认的工作记录")).toBeVisible();
      expect(screen.getByRole("button", { name: "original" })).toBeEnabled();
    } else {
      expect(screen.queryByText("已确认的工作记录")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "original" }),
      ).not.toBeInTheDocument();
    }
  },
);
