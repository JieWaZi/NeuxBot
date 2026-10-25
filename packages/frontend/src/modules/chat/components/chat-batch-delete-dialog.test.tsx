import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatBatchDeleteDialog } from "./chat-batch-delete-dialog";

const deleteChat = vi.hoisted(() => vi.fn());
const evictDeletedChatsFromCache = vi.hoisted(() => vi.fn());

vi.mock("../api/chat-queries", () => ({
  deleteChatMutation: () => ({
    mutationFn: ({ path }: { path: { chatId: string } }) =>
      deleteChat(path.chatId),
  }),
  evictDeletedChatsFromCache,
}));

describe("ChatBatchDeleteDialog", () => {
  beforeEach(() => {
    deleteChat.mockReset();
    evictDeletedChatsFromCache.mockReset();
  });

  it("逐项删除所选对话并在全部成功后关闭", async () => {
    const user = userEvent.setup();
    const onChanged = vi.fn();
    const onDeleted = vi.fn();
    const onOpenChange = vi.fn();
    deleteChat.mockResolvedValue(undefined);

    renderDialog({ onChanged, onDeleted, onOpenChange });
    await user.click(screen.getByRole("button", { name: "删除所选" }));

    await waitFor(() => {
      expect(deleteChat.mock.calls).toEqual([["chat-1"], ["chat-2"]]);
      expect(onDeleted).toHaveBeenCalledWith(["chat-1", "chat-2"]);
      expect(evictDeletedChatsFromCache).toHaveBeenCalledWith(
        expect.any(QueryClient),
        ["chat-1", "chat-2"],
      );
      expect(onChanged).toHaveBeenCalledTimes(1);
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });
  });

  it("部分失败时保留失败项并允许重试", async () => {
    const user = userEvent.setup();
    const onDeleted = vi.fn();
    const onOpenChange = vi.fn();
    deleteChat.mockImplementation((chatId: string) =>
      chatId === "chat-2"
        ? Promise.reject(new Error("delete failed"))
        : Promise.resolve(),
    );

    renderDialog({ onDeleted, onOpenChange });
    await user.click(screen.getByRole("button", { name: "删除所选" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "1 个对话删除失败，请重试。",
    );
    expect(onDeleted).toHaveBeenCalledWith(["chat-1"]);
    expect(onOpenChange).not.toHaveBeenCalled();

    deleteChat.mockResolvedValue(undefined);
    await user.click(screen.getByRole("button", { name: "删除所选" }));

    await waitFor(() => {
      expect(deleteChat).toHaveBeenLastCalledWith("chat-2");
      expect(onDeleted).toHaveBeenLastCalledWith(["chat-2"]);
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });
  });
});

function renderDialog({
  onChanged = vi.fn(),
  onDeleted,
  onOpenChange,
}: {
  onChanged?: () => void;
  onDeleted: (chatIds: string[]) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <ChatBatchDeleteDialog
        chatIds={["chat-1", "chat-2"]}
        open
        onChanged={onChanged}
        onDeleted={onDeleted}
        onOpenChange={onOpenChange}
      />
    </QueryClientProvider>,
  );
}
