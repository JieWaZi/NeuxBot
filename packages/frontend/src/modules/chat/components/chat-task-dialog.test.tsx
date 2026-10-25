import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatTaskDialog } from "./chat-task-dialog";

const deleteChat = vi.hoisted(() => vi.fn());
const evictDeletedChatsFromCache = vi.hoisted(() => vi.fn());

vi.mock("../api/chat-queries", () => ({
  deleteChatMutation: () => ({
    mutationFn: ({ path }: { path: { chatId: string } }) =>
      deleteChat(path.chatId),
  }),
  evictDeletedChatsFromCache,
  updateChatMutation: () => ({
    mutationFn: async () => undefined,
  }),
}));

describe("ChatTaskDialog", () => {
  beforeEach(() => {
    deleteChat.mockReset();
    evictDeletedChatsFromCache.mockReset();
    deleteChat.mockResolvedValue(undefined);
  });

  it("evicts deleted chat cache before notifying delete success", async () => {
    const user = userEvent.setup();
    const onDeleted = vi.fn();
    const onOpenChange = vi.fn();
    const callOrder: string[] = [];

    onDeleted.mockImplementation(() => {
      callOrder.push("deleted");
    });
    onOpenChange.mockImplementation(() => {
      callOrder.push("closed");
    });

    renderDialog({ onDeleted, onOpenChange });
    await user.click(screen.getByRole("button", { name: "删除对话" }));

    await waitFor(() => {
      expect(evictDeletedChatsFromCache).toHaveBeenCalledWith(
        expect.any(QueryClient),
        ["chat-1"],
      );
      expect(onDeleted).toHaveBeenCalledWith("chat-1");
      expect(onOpenChange).toHaveBeenCalledWith(false);
      expect(callOrder).toEqual(["deleted", "closed"]);
    });
  });
});

function renderDialog({
  onDeleted,
  onOpenChange,
}: {
  onDeleted: (deletedChatId: string) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <ChatTaskDialog
        chatId="chat-1"
        mode="delete"
        open
        title="测试对话"
        onDeleted={onDeleted}
        onOpenChange={onOpenChange}
      />
    </QueryClientProvider>,
  );
}
