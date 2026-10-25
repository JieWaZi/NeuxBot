import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useParams } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatEntryPage } from "./chat-entry-page";

const { chatState } = vi.hoisted(() => ({
  chatState: {
    current: "latest" as "latest" | "pending" | "empty" | "missing",
  },
}));
const storage = new Map<string, string>();

vi.mock("@/modules/waker", () => ({
  listWakersOptions: () => ({
    queryKey: ["wakers"],
    queryFn: async () => ({
      resources: [{ id: "waker-1", name: "测试伙伴" }],
    }),
  }),
}));

vi.mock("../api/chat-queries", () => ({
  chatListKey: (wakerId: string) => ["chats", "list", wakerId],
  chatListInfiniteOptions: (wakerId: string) => ({
    queryKey: ["chats", "list", wakerId],
    initialPageParam: undefined,
    queryFn: async () => {
      if (chatState.current === "pending") {
        await new Promise(() => undefined);
      }
      return {
        resources:
          chatState.current === "empty"
            ? []
            : [
                {
                  id: "chat-latest",
                  wakerId: "waker-1",
                },
              ],
        hasMore: false,
      };
    },
    getNextPageParam: () => undefined,
  }),
  getChatOptions: () => ({
    queryKey: ["chat", "chat-b"],
    queryFn: async () => {
      if (chatState.current === "missing") {
        throw new Error("chat deleted");
      }
      return {
        chat: { id: "chat-b", wakerId: "waker-1" },
      };
    },
  }),
}));

vi.mock("./chat-page", () => ({
  ChatPageContent: ({
    routeTransitioning,
  }: {
    routeTransitioning?: boolean;
  }) => (
    <div data-testid="draft-page" aria-busy={routeTransitioning}>
      <textarea aria-label="输入消息" />
    </div>
  ),
}));

function ChatTarget() {
  const { chatId } = useParams();
  return <div data-testid="chat-page">{chatId}</div>;
}

function renderEntry(initialEntry = "/chat") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/chat" element={<ChatEntryPage />} />
          <Route path="/chats/:chatId" element={<ChatTarget />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ChatEntryPage", () => {
  beforeEach(() => {
    chatState.current = "latest";
    storage.clear();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => storage.set(key, value),
      },
    });
  });

  it("restores the last active chat instead of the server latest", async () => {
    window.localStorage.setItem("ally.chat.last-waker-id", "waker-1");
    window.localStorage.setItem(
      "ally.chat.last-active",
      JSON.stringify({ wakerId: "waker-1", chatId: "chat-b" }),
    );
    renderEntry();

    expect(await screen.findByTestId("chat-page")).toHaveTextContent("chat-b");
  });

  it("查询最近对话时保留真实输入区并标记历史加载", async () => {
    chatState.current = "pending";
    renderEntry();

    expect(await screen.findByTestId("draft-page")).toHaveAttribute(
      "aria-busy",
      "true",
    );
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBeVisible();
  });

  it("ignores a deleted last active chat and falls back to the latest", async () => {
    chatState.current = "missing";
    window.localStorage.setItem("ally.chat.last-waker-id", "waker-1");
    window.localStorage.setItem(
      "ally.chat.last-active",
      JSON.stringify({ wakerId: "waker-1", chatId: "chat-b" }),
    );

    renderEntry();

    expect(await screen.findByTestId("chat-page")).toHaveTextContent(
      "chat-latest",
    );
    expect(screen.queryByTestId("draft-page")).not.toBeInTheDocument();
  });

  it("persists the requested waker id when switching partners", async () => {
    window.localStorage.setItem("ally.chat.last-waker-id", "waker-old");
    renderEntry("/chat?wakerId=waker-1");

    expect(await screen.findByTestId("chat-page")).toBeInTheDocument();
    expect(window.localStorage.getItem("ally.chat.last-waker-id")).toBe(
      "waker-1",
    );
  });
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
