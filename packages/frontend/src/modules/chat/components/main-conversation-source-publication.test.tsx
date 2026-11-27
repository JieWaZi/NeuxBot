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

it("在对应工作回合直接显示已发布发言，展开过程可读原生结尾和工具", async () => {
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
      resources: [
        {
          id: "user",
          role: "user",
          content: "请核对日志",
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
            { id: "native-result", type: "text", text: "本轮在工作记录继续。" },
          ],
        },
        {
          id: "tool-only",
          executionId: "third-run",
          createdSequence: 4,
          role: "assistant",
          status: "completed",
          displayParts: [
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
  const first = await screen.findByText("操作日志已核对。");
  const row = first.closest<HTMLElement>("[data-message-id]");
  if (!row) throw new Error("missing work message");
  expect(row).toHaveAttribute("data-message-id", "with-publication");
  expect(within(row).getByText("共检查了 42 条。")).toBeVisible();
  expect(within(row).getAllByText("本轮公开两次的提醒。")).toHaveLength(2);
  expect(within(row).queryByText("已回复到对话。")).not.toBeInTheDocument();
  expect(screen.getByText("本轮在工作记录继续。")).toBeVisible();
  expect(screen.queryByText("其他工作结果。")).not.toBeInTheDocument();
  const toolOnly = screen
    .getByText("只通过工具发出的结果。")
    .closest<HTMLElement>("[data-message-id]");
  if (!toolOnly) throw new Error("missing tool-only work message");
  expect(
    within(toolOnly).queryByRole("button", { name: "回复" }),
  ).not.toBeInTheDocument();

  await user.click(within(row).getByRole("button", { name: "复制" }));
  expect(await navigator.clipboard.readText()).toBe(
    "操作日志已核对。\n\n共检查了 42 条。\n\n本轮公开两次的提醒。\n\n本轮公开两次的提醒。",
  );
  fireEvent.click(within(row).getByRole("button", { name: "回复" }));
  expect(screen.getByRole("status", { name: "引用预览" })).toHaveTextContent(
    "已回复到对话。",
  );

  fireEvent.click(within(row).getByRole("button", { name: "查看过程" }));
  expect(within(row).getByText("已回复到对话。")).toBeVisible();
  expect(within(row).getByText("额外说明").tagName).toBe("STRONG");
  expect(
    within(row).getByRole("button", { name: "已发布到对话" }),
  ).toBeVisible();
  expect(within(row).getAllByText("操作日志已核对。")).toHaveLength(1);
});
