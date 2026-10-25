import {
  AssistantRuntimeProvider,
  ThreadPrimitive,
  useExternalStoreRuntime,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { expect, it } from "vitest";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { toThreadMessage } from "../runtime/chat-projection";
import { AssistantMessage } from "./chat-thread";

function MessageHarness({ message }: { message: ThreadMessageLike }) {
  const runtime = useExternalStoreRuntime({
    messages: [message],
    convertMessage: (message) => message,
    isRunning: true,
    onNew: async () => {},
  });
  return (
    <MemoryRouter>
      <TooltipProvider>
        <AssistantRuntimeProvider runtime={runtime}>
          <ThreadPrimitive.Root>
            <ThreadPrimitive.Messages
              components={{
                UserMessage: () => null,
                AssistantMessage: () => <AssistantMessage name="私人助理" />,
              }}
            />
          </ThreadPrimitive.Root>
        </AssistantRuntimeProvider>
      </TooltipProvider>
    </MemoryRouter>
  );
}

it("思考后的纯换行不占段落高度，也不把连续网络工具拆成多个活动组", () => {
  // 使用截图对应的实际片段顺序：思考、三个换行、工具、一个换行、工具。
  const message = toThreadMessage({
    id: "assistant-spacing",
    role: "assistant",
    status: "running",
    displayParts: [
      { type: "reasoning", text: "先收集今日 AI 新闻。" },
      { type: "process_commentary", text: "\n\n\n" },
      {
        type: "tool",
        toolCallId: "web-1",
        toolCategory: "network",
        toolAction: "search_web",
        toolStatus: "completed",
      },
      { type: "process_commentary", text: "\n" },
      { type: "reasoning", text: " \t\n" },
      { type: "text", text: "\r\n" },
      {
        type: "tool",
        toolCallId: "web-2",
        toolCategory: "network",
        toolAction: "search_web",
        toolStatus: "completed",
      },
    ],
  });
  const { container } = render(<MessageHarness message={message} />);

  expect(screen.getAllByRole("button", { name: "已访问网络" })).toHaveLength(1);
  const reasoning = screen.getByText("先收集今日 AI 新闻。");
  const parts = reasoning.closest(".space-y-4");
  expect(parts?.children).toHaveLength(2);
  expect(
    [...container.querySelectorAll("p")].filter(
      (paragraph) => paragraph.textContent && !paragraph.textContent.trim(),
    ),
  ).toHaveLength(0);
});
