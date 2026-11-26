import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { expect, it, vi } from "vitest";
import type * as ChatNavigation from "@/shared/lib/chat-navigation";
import { ChatEntryPage } from "./chat-entry-page";

const recent = vi.hoisted(() => ({
  read: vi.fn<() => Promise<{ chat: { id: string; wakerId: string } }>>(),
}));
vi.mock("@/shared/lib/chat-navigation", async (original) => ({
  ...(await original<typeof ChatNavigation>()),
  readChatLastActive: () => ({ chatId: "last-visited", wakerId: "partner" }),
}));
vi.mock("./chat-page", () => ({
  ChatPageContent: ({ chatId }: { chatId?: string }) => (
    <div data-testid="visible-chat">{chatId}</div>
  ),
}));
vi.mock("@/modules/waker", () => ({
  listWakersOptions: () => ({
    queryKey: ["partners"],
    queryFn: async () => ({ resources: [{ id: "partner" }] }),
  }),
}));
vi.mock("../api/chat-queries", () => ({
  chatListKey: (id: string) => ["chat-list", id],
  chatListInfiniteOptions: (id: string) => ({
    queryKey: ["chat-list", id],
    queryFn: async () => ({
      resources: [{ id: "last-updated", wakerId: "partner" }],
    }),
    initialPageParam: null,
    getNextPageParam: () => undefined,
  }),
  getChatOptions: ({ path }: { path: { chatId: string } }) => ({
    queryKey: ["chat-detail", path.chatId],
    queryFn: recent.read,
  }),
}));

function Entry() {
  const { pathname } = useLocation();
  return (
    <>
      <output>{pathname}</output>
      <ChatEntryPage presentationScopeKey="chat-scene" />
    </>
  );
}

it("等待上次访问的会话验证，不让先返回的最近列表抢走路由", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  let settle:
    ((value: { chat: { id: string; wakerId: string } }) => void) | undefined;
  recent.read.mockImplementation(
    () =>
      new Promise((resolve) => {
        settle = resolve;
      }),
  );
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/chat?wakerId=partner"]}>
        <Routes>
          <Route element={<Entry />}>
            <Route path="/chat" element={null} />
            <Route path="/chats/:chatId" element={null} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  try {
    await waitFor(() =>
      expect(client.getQueryState(["chat-list", "partner"])?.status).toBe(
        "success",
      ),
    );
    expect(screen.getByText("/chat")).toBeInTheDocument();
    expect(screen.getByTestId("visible-chat")).toBeEmptyDOMElement();
    await act(async () =>
      settle?.({ chat: { id: "last-visited", wakerId: "partner" } }),
    );
    expect(await screen.findByText("/chats/last-visited")).toBeInTheDocument();
  } finally {
    view.unmount();
    client.clear();
  }
});
