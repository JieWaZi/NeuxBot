import { useLocalRuntime, type ChatModelAdapter } from "@assistant-ui/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "@/app/workspace-i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type * as ChatReading from "@/shared/api/chat-reading";
import { ChatThread, type ChatTimelineEvent } from "./chat-thread";
import type { BotChatMessage } from "../runtime/chat-projection";

vi.mock("@/shared/api/chat-reading", async (original) => ({
  ...(await original<typeof ChatReading>()),
  useChatReading: () => ({ present() {}, state: { complete: false } }),
  usePresentedChatBoundary: () => undefined,
}));

const chatId = "a".repeat(32);
const model: ChatModelAdapter = { run: async () => ({ content: [] }) };
const message: BotChatMessage = {
  id: "message",
  role: "assistant",
  status: "completed",
  displayParts: [{ type: "text", text: "已经显示的消息" }],
};
const client = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});
afterEach(() => {
  client.clear();
  vi.unstubAllGlobals();
});

function Thread({
  readingChatId,
  timestamp,
  timelineEvents,
}: {
  readingChatId?: string;
  timestamp?: number;
  timelineEvents?: readonly ChatTimelineEvent[];
}) {
  const runtime = useLocalRuntime(model);
  return (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <MemoryRouter>
          <ChatThread
            runtime={runtime}
            messages={[
              { ...message, createdAt: timestamp, createdSequence: 20 },
            ]}
            timelineEvents={timelineEvents}
            emptyState={null}
            sendFailure={{ kind: "none" }}
            wakerName="NeuxBot"
            fileScopeChatId={chatId}
            hasEarlierMessages={false}
            isLoadingEarlier={false}
            onLoadEarlier={async () => {}}
            readingChatId={readingChatId}
          />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>
  );
}

it("详情补齐阅读身份时保留已经显示的消息 DOM", () => {
  const view = render(<Thread />);
  const before = screen.getByText("已经显示的消息");
  view.rerender(<Thread readingChatId={chatId} />);
  expect(screen.getByText("已经显示的消息")).toBe(before);
});

it("没有真实消息时刻时省略时间标签，补齐后显示权威时间", () => {
  const view = render(<Thread />);
  expect(view.container.querySelector("time")).toBeNull();
  view.rerender(<Thread timestamp={Date.UTC(2026, 10, 25, 5)} />);
  expect(view.container.querySelector("time")).toHaveAttribute(
    "datetime",
    "2026-11-25T05:00:00.000Z",
  );
});

it("有持久序号的时间线事件按序号排在消息前，即使时钟回拨", () => {
  render(
    <Thread
      timestamp={100}
      timelineEvents={[
        {
          id: "earlier",
          sequence: 19,
          createdAt: 200,
          content: <div>先提交的技能事件</div>,
        },
      ]}
    />,
  );
  const event = screen.getByText("先提交的技能事件");
  const row = screen.getByText("已经显示的消息");
  expect(
    event.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});
