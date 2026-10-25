import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { ChatQueueProvider } from "../runtime/chat-queue-store";
import { WorkbenchProvider } from "../workbench/workbench-store";
import { ChatPane } from "./chat-pane";

vi.mock("@/shared/api/events", () => ({ subscribeChat: () => () => {} }));
vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: () => {} }));
const api = "http://chat-pane-retry.test/api/v1";
const server = setupServer(
  http.get(`${api}/chats/chat`, () =>
    HttpResponse.json({ chat: { id: "chat" } }),
  ),
  http.get(`${api}/chats/chat/messages`, () =>
    HttpResponse.json({
      resources: [
        {
          id: "failed",
          role: "assistant",
          content: "",
          status: "failed",
          errorMessage: "修订执行中断",
          retryable: true,
          executionId: "execution",
          revision: 1,
        },
      ],
      latestSequence: 1,
    }),
  ),
  http.get(`${api}/ai-accounts`, () => HttpResponse.json({ resources: [] })),
  http.get(`${api}/runtimes`, () =>
    HttpResponse.json({
      resources: [
        {
          id: "codex",
          name: "Codex",
          enabled: true,
          state: "ready",
          isDefault: true,
          defaultModel: "native",
          defaultPermissionLevel: "default",
          models: [
            { id: "native", name: "native", permissionLevels: ["default"] },
          ],
        },
      ],
    }),
  ),
);
beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  HTMLElement.prototype.scrollTo = vi.fn();
  client.setConfig({ baseUrl: "http://chat-pane-retry.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});
it.each([true, false])(
  "入口控制原生执行重试展示，allowExecutionRetry=%s",
  async (allowExecutionRetry) => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkbenchProvider>
          <ChatQueueProvider>
            <TooltipProvider>
              <MemoryRouter>
                <ChatPane
                  scopeKey="maintenance"
                  chatId="chat"
                  title="Wiki"
                  allowExecutionRetry={allowExecutionRetry}
                  onSend={async () => {
                    throw new Error("unexpected send");
                  }}
                />
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </WorkbenchProvider>
      </QueryClientProvider>,
    );
    await screen.findByText("修订执行中断");
    if (allowExecutionRetry)
      expect(screen.getByRole("button", { name: "重试" })).toBeInTheDocument();
    else
      expect(
        screen.queryByRole("button", { name: "重试" }),
      ).not.toBeInTheDocument();
  },
);
