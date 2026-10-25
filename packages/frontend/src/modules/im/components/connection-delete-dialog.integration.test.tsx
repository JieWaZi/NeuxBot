import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { MemoryRouter } from "react-router-dom";
import { ConnectionDeleteDialog } from "./connection-delete-dialog";

const origin = "http://im-dependencies.test";
const endpoint = `${origin}/api/v1/im/connections/connection`;
const server = setupServer();
const clients: QueryClient[] = [];
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((item) => item.clear());
  server.resetHandlers();
});
afterAll(() => server.close());

function show() {
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(query);
  render(
    <QueryClientProvider client={query}>
      <MemoryRouter>
        <ConnectionDeleteDialog
          connection={{ id: "connection", name: "微信", revision: 1 }}
          onClose={vi.fn()}
          onDeleted={vi.fn()}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

it("列出已保存工作流并把用户看到的依赖摘要传给删除接口", async () => {
  const user = userEvent.setup();
  const remove = vi.fn();
  server.use(
    http.get(`${endpoint}/dependencies`, () =>
      HttpResponse.json({
        digest: "reviewed",
        workflows: [
          {
            definitionId: "flow",
            revisionId: "revision",
            nodeId: "send",
            title: "今日缺陷",
            label: "发送微信",
          },
        ],
        pendingDeliveries: 0,
      }),
    ),
    http.delete(endpoint, ({ request }) => {
      remove(new URL(request.url).searchParams.get("dependencyDigest"));
      return HttpResponse.json({});
    }),
  );
  show();
  expect(
    await screen.findByRole("link", { name: "今日缺陷" }),
  ).toBeInTheDocument();
  expect(screen.getByText(/可先通过优化工作流更换目标/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "删除" }));
  await waitFor(() => expect(remove).toHaveBeenCalledWith("reviewed"));
});

it("活跃运行或依赖查询失败时不能删除，读取失败可以重试", async () => {
  const user = userEvent.setup();
  const remove = vi.fn();
  let mode = "error";
  server.use(
    http.get(`${endpoint}/dependencies`, () =>
      mode === "error"
        ? HttpResponse.json({ message: "unavailable" }, { status: 503 })
        : HttpResponse.json({
            digest: "active",
            workflows: [
              {
                definitionId: "flow",
                nodeId: "send",
                runId: "run",
                title: "今日缺陷",
                label: "发送微信",
              },
            ],
            pendingDeliveries: 1,
          }),
    ),
    http.delete(endpoint, () => {
      remove();
      return HttpResponse.json({});
    }),
  );
  show();
  expect(await screen.findByText(/无法确认删除影响/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "删除" })).toBeDisabled();
  mode = "active";
  await user.click(screen.getByRole("button", { name: "重试读取" }));
  expect(
    await screen.findByText(/此连接仍被运行中的工作流或待发送消息使用/),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "删除" })).toBeDisabled();
  expect(remove).not.toHaveBeenCalled();
});

it("删除时依赖发生变化，刷新影响清单并等待下一次确认", async () => {
  const user = userEvent.setup();
  const confirmations: Array<string | null> = [];
  let digest = "first";
  server.use(
    http.get(`${endpoint}/dependencies`, () =>
      HttpResponse.json({
        digest,
        workflows: [
          {
            definitionId: "flow",
            nodeId: "send",
            title: digest,
            label: "发送微信",
          },
        ],
        pendingDeliveries: 0,
      }),
    ),
    http.delete(endpoint, ({ request }) => {
      confirmations.push(
        new URL(request.url).searchParams.get("dependencyDigest"),
      );
      digest = "changed";
      return confirmations.length === 1
        ? HttpResponse.json({ message: "changed" }, { status: 409 })
        : HttpResponse.json({});
    }),
  );
  show();
  await screen.findByRole("link", { name: "first" });
  await user.click(screen.getByRole("button", { name: "删除" }));
  await screen.findByRole("link", { name: "changed" });
  expect(confirmations).toEqual(["first"]);
  await user.click(screen.getByRole("button", { name: "删除" }));
  await waitFor(() => expect(confirmations).toEqual(["first", "changed"]));
});
