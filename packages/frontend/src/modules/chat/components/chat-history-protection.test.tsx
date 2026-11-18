import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, beforeAll, expect, it } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { ChatHistoryNavigationRow } from "./chat-history-navigation";

const changes: unknown[] = [];
const server = setupServer(
  http.put(
    "http://127.0.0.1:8080/api/v1/storage/chats/kept/protection",
    async ({ request }) => {
      changes.push(await request.json());
      return HttpResponse.json({});
    },
  ),
);
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://127.0.0.1:8080" });
});
afterAll(() => server.close());

it("在对话菜单中取消原有保留例外", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ChatHistoryNavigationRow
        active={false}
        title="保留的会话"
        status="idle"
        onOpen={() => {}}
        onAction={() => {}}
        protection={{ chatId: "kept", wakerId: "w1", protected: true }}
      />
    </QueryClientProvider>,
  );
  await user.click(
    screen.getByRole("button", { name: "会话“保留的会话”操作" }),
  );
  await user.click(
    await screen.findByRole("menuitem", { name: "取消保留此会话" }),
  );
  await waitFor(() => expect(changes).toEqual([{ protected: false }]));
});
