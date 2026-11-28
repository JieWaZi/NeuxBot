import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { expect, it, onTestFinished, vi } from "vitest";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import type * as ChatEvents from "@/shared/api/events";
import { MainConversationSource } from "./main-conversation-source";

const getSource = vi.hoisted(() => vi.fn());
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  getMainConversationSource: getSource,
  getWaker: async () => ({ data: { id: "partner", name: "伙伴" } }),
}));
vi.mock("@/shared/api/events", async (original) => ({
  ...(await original<typeof ChatEvents>()),
  subscribeChat: () => () => {},
}));
vi.mock("./main-conversation-composer", () => ({
  MainConversationComposer: ({ reply }: { reply?: { preview?: string } }) =>
    reply ? <output aria-label="引用预览">{reply.preview}</output> : null,
}));

it.each([true, false])(
  "工作回合优先展示完整原生结尾，只在没有结尾时使用该回合的公开发言（来源内续聊 %s）",
  async (privateContinuation) => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    onTestFinished(() => {
      vi.unstubAllGlobals();
    });
    const user = userEvent.setup();
    getSource.mockResolvedValue({
      data: {
        chat: { id: "work", wakerId: "partner" },
        privateContinuation,
        inputTriggers: [
          {
            sourceMessageId: "user",
            publicMessageId: "main-post",
            preview: "请核对日志",
          },
          {
            sourceMessageId: "removed-user",
            preview: "已从主会话移除的消息",
          },
        ],
        resources: [
          {
            id: "user",
            role: "user",
            content: "请核对日志\n原消息的第二行",
          },
          {
            id: "source-user",
            role: "user",
            content: "在工作记录里继续",
          },
          {
            id: "removed-user",
            role: "user",
          },
          {
            id: "with-publication",
            executionId: "first-run",
            createdSequence: 2,
            role: "assistant",
            status: "completed",
            displayParts: [
              {
                id: "call",
                type: "tool",
                toolCategory: "mcp",
                toolStatus: "completed",
                toolLabel: "发布到对话",
              },
              { id: "native-ack", type: "text", text: "已回复到对话。" },
              { id: "duplicate", type: "text", text: "操作日志已核对。" },
              {
                id: "native-summary",
                type: "text",
                text: "**额外说明** 归档文件有完整记录。",
              },
            ],
          },
          {
            id: "without-publication",
            executionId: "second-run",
            role: "assistant",
            status: "completed",
            displayParts: [
              {
                id: "native-result",
                type: "text",
                text: "本轮在工作记录继续。",
              },
            ],
          },
          {
            id: "empty-result",
            executionId: "fourth-run",
            role: "assistant",
            status: "completed",
            displayParts: [{ id: "empty-tool", type: "tool" }],
          },
          {
            id: "tool-only",
            executionId: "third-run",
            createdSequence: 4,
            role: "assistant",
            status: "completed",
            displayParts: [
              { id: "setup", type: "text", text: "我先发送结果。" },
              { id: "another-post", type: "tool", toolStatus: "completed" },
            ],
          },
        ],
        publishedMessages: [
          {
            id: "published-1",
            executionId: "first-run",
            content: "操作日志已核对。",
          },
          {
            id: "published-2",
            executionId: "first-run",
            content: "共检查了 42 条。",
          },
          {
            id: "published-3",
            executionId: "first-run",
            content: "本轮公开两次的提醒。",
          },
          {
            id: "published-3",
            executionId: "first-run",
            content: "本轮公开两次的提醒。",
          },
          {
            id: "published-5",
            executionId: "first-run",
            content: "本轮公开两次的提醒。",
          },
          {
            id: "published-4",
            executionId: "third-run",
            content: "只通过工具发出的结果。",
          },
          {
            id: "published-4",
            executionId: "third-run",
            content: "只通过工具发出的结果。",
          },
          {
            id: "published-6",
            executionId: "third-run",
            content: "同轮追加的结果。",
          },
          {
            id: "earlier-page",
            executionId: "other-run",
            content: "其他工作结果。",
          },
        ],
      },
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <TooltipProvider>
            <MainConversationSource
              subject={{ subjectType: "waker", subjectId: "partner" }}
              workChatId="work"
              active
              onOpenFile={vi.fn()}
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );
    const first = await screen.findByText("额外说明");
    const row = first.closest<HTMLElement>("[data-message-id]");
    if (!row) throw new Error("missing work message");
    expect(row).toHaveAttribute("data-message-id", "with-publication");
    expect(within(row).getByText("操作日志已核对。")).toBeVisible();
    expect(within(row).queryByText("共检查了 42 条。")).not.toBeInTheDocument();
    expect(screen.getByText("本轮在工作记录继续。")).toBeVisible();
    expect(screen.queryByText("其他工作结果。")).not.toBeInTheDocument();
    const input = screen.getByText("原消息的第二行", { exact: false });
    const inputBubble = input.closest("[data-chat-message-bubble]");
    const inputRow = input.closest<HTMLElement>("[data-message-id]");
    if (!inputRow || !inputBubble) throw new Error("缺少用户消息气泡");
    const trigger = within(inputRow).getByRole("link", {
      name: "来自主会话",
    });
    expect(trigger).toHaveAttribute(
      "href",
      "/chat/main/waker/partner?message=main-post",
    );
    expect(inputBubble).not.toContainElement(trigger);
    expect(
      trigger.compareDocumentPosition(inputBubble) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    await user.click(within(inputRow).getByRole("button", { name: "复制" }));
    expect(await navigator.clipboard.readText()).toBe(
      "请核对日志\n原消息的第二行",
    );
    const local = screen.getByText("在工作记录里继续");
    const localRow = local.closest<HTMLElement>("[data-message-id]");
    if (!localRow) throw new Error("缺少工作记录内发送的消息");
    expect(local.closest("[data-chat-message-bubble]")).toBeInTheDocument();
    expect(within(localRow).queryByText("来自主会话")).not.toBeInTheDocument();
    const removed = screen.getByText("已从主会话移除的消息");
    const removedRow = removed.closest<HTMLElement>("[data-message-id]");
    if (!removedRow) throw new Error("缺少主会话已移除的输入");
    expect(removed.closest("[data-chat-message-bubble]")).toBeInTheDocument();
    expect(within(removedRow).getByText("来自主会话")).toBeVisible();
    expect(within(removedRow).queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("本轮没有回复")).toBeVisible();
    const toolOnly = screen
      .getByText("只通过工具发出的结果。")
      .closest<HTMLElement>("[data-message-id]");
    if (!toolOnly) throw new Error("missing tool-only work message");
    expect(
      within(toolOnly).queryByRole("button", { name: "回复" }),
    ).not.toBeInTheDocument();
    expect(
      within(toolOnly).queryByText("我先发送结果。"),
    ).not.toBeInTheDocument();
    expect(within(toolOnly).getByText("同轮追加的结果。")).toBeVisible();

    await user.click(within(toolOnly).getByRole("button", { name: "复制" }));
    expect(await navigator.clipboard.readText()).toBe(
      "只通过工具发出的结果。\n\n同轮追加的结果。",
    );
    fireEvent.click(within(row).getByRole("button", { name: "回复" }));
    expect(screen.getByRole("status", { name: "引用预览" })).toHaveTextContent(
      "额外说明",
    );

    fireEvent.click(within(row).getByRole("button", { name: "查看过程" }));
    expect(
      within(row).getByRole("button", { name: "已发布到对话" }),
    ).toBeVisible();
  },
);
