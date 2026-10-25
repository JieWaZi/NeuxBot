import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import { KnowledgeRemoteAddDialog } from "./knowledge-remote-add-dialog";
const origin = "http://remote-add.test";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
function show(onAdded = vi.fn()) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <KnowledgeRemoteAddDialog onClose={() => {}} onAdded={onAdded} />
    </QueryClientProvider>,
  );
  return onAdded;
}
it("三字段验证后添加指定知识库，重试保留原请求身份且不要求提供者", async () => {
  const requests: unknown[] = [];
  server.use(
    http.post(
      `${origin}/api/v1/knowledge/connections/mounts`,
      async ({ request }) => {
        requests.push(await request.json());
        return requests.length === 1
          ? HttpResponse.json({ message: "unavailable" }, { status: 503 })
          : HttpResponse.json({
              mount: {
                id: "mount-a",
                connectionId: "connection-a",
                knowledgeBaseId: "kb-a",
              },
              connection: {
                id: "connection-a",
                backend: "integration",
                detection: { library: { id: "kb-a", name: "远端资料库" } },
              },
            });
      },
    ),
  );
  const user = userEvent.setup();
  const added = show();
  expect(screen.getAllByRole("textbox")).toHaveLength(2);
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  await user.type(
    screen.getByLabelText(/远程 URL/),
    "https://example.test/proxy/api/v1",
  );
  await user.type(screen.getByLabelText(/API Key/), "sk-private");
  await user.type(screen.getByLabelText(/知识库 ID/), "kb-a");
  await user.click(screen.getByRole("button", { name: "连接远端" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "连接远端" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(added).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "连接远端" }));
  await waitFor(() => expect(added).toHaveBeenCalledOnce());
  expect(requests[0]).toEqual(requests[1]);
  expect(requests[1]).toMatchObject({
    url: "https://example.test/proxy/api/v1",
    apiKey: "sk-private",
    knowledgeBaseId: "kb-a",
  });
});
