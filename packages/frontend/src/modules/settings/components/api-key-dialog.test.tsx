import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import { APIKeyDialog } from "./api-key-dialog";
const origin = "http://integration-settings.test";
const server = setupServer(
  http.get(`${origin}/api/v1/integrations/resources`, () =>
    HttpResponse.json({
      resources: [{ id: "kb-a", name: "本机知识库" }],
      totalSize: 1,
    }),
  ),
);
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
beforeAll(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
  Element.prototype.scrollIntoView = () => {};
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
function show(key?: Parameters<typeof APIKeyDialog>[0]["entry"]) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <APIKeyDialog entry={key} onClose={() => {}} />
    </QueryClientProvider>,
  );
}
it("签发只使用明确选定资源并在本次响应展示秘密", async () => {
  let received: unknown;
  server.use(
    http.post(`${origin}/api/v1/integrations/keys`, async ({ request }) => {
      received = await request.json();
      return HttpResponse.json({
        keyId: "key-a",
        secret: "sk-once",
        issuanceState: "issued",
      });
    }),
  );
  const user = userEvent.setup();
  const view = show();
  await user.type(screen.getByLabelText(/集成名称/), "外部服务");
  const permissions = screen.getAllByRole("button", { name: "选择权限" })[0];
  if (!permissions) throw new Error("permission picker missing");
  await user.click(permissions);
  await user.click(screen.getByLabelText("读取与检索"));
  await user.keyboard("{Escape}");
  const picker = screen.getAllByRole("button", {
    name: "选择资源（已选 0）",
  })[0];
  if (!picker) throw new Error("resource picker missing");
  await user.click(picker);
  await user.click(await screen.findByLabelText("本机知识库"));
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: "创建 API Key" }));
  expect(await screen.findByDisplayValue("sk-once")).toBeVisible();
  expect(
    screen.getAllByText(
      "完整密钥只显示这一次。关闭后无法重新查看，请妥善保存。",
    ),
  ).toHaveLength(1);
  expect(screen.queryByText(/密钥主体 ID/)).not.toBeInTheDocument();
  const copy = screen.getByRole("button", { name: "复制" });
  expect(copy).toHaveTextContent("");
  await user.click(copy);
  expect(await navigator.clipboard.readText()).toBe("sk-once");
  expect(screen.getByRole("button", { name: "已复制" })).toBeVisible();
  expect(received).toMatchObject({
    name: "外部服务",
    grants: [
      {
        resourceKind: "knowledge",
        resourceIds: ["kb-a"],
        capabilities: ["retrieve"],
      },
    ],
  });
  view.unmount();
  show();
  expect(screen.queryByDisplayValue("sk-once")).not.toBeInTheDocument();
});
it("授权冲突保留草稿且不静默覆盖版本", async () => {
  let submitted: unknown;
  server.use(
    http.put(
      `${origin}/api/v1/integrations/keys/key-a`,
      async ({ request }) => {
        submitted = await request.json();
        return HttpResponse.json(
          { key: "IntegrationConflict", message: "conflict" },
          { status: 409 },
        );
      },
    ),
  );
  const user = userEvent.setup();
  show({
    id: "key-a",
    name: "原名称",
    revision: 4,
    expiresAt: 2_000_000_000_123,
    grants: [
      {
        resourceKind: "knowledge",
        resourceIds: ["kb-a"],
        capabilities: ["retrieve"],
      },
    ],
  });
  const name = screen.getByLabelText(/集成名称/);
  await user.clear(name);
  await user.type(name, "保留草稿");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(screen.getByRole("alert")).toBeVisible());
  expect(name).toHaveValue("保留草稿");
  expect(submitted).toMatchObject({
    expectedRevision: 4,
    expiresAt: 2_000_000_000_123,
  });
});

it("有效期使用共享日期时间选择器，并可清空为不过期", async () => {
  let submitted: unknown;
  server.use(
    http.put(
      `${origin}/api/v1/integrations/keys/key-a`,
      async ({ request }) => {
        submitted = await request.json();
        return HttpResponse.json({});
      },
    ),
  );
  const user = userEvent.setup();
  show({
    id: "key-a",
    name: "有期限的密钥",
    revision: 4,
    expiresAt: 2_000_000_000_123,
    grants: [
      {
        resourceKind: "knowledge",
        resourceIds: ["kb-a"],
        capabilities: ["retrieve"],
      },
    ],
  });
  await user.click(screen.getByRole("button", { name: "有效期日期" }));
  expect(await screen.findByRole("grid")).toBeVisible();
  await user.keyboard("{Escape}");
  expect(screen.getByRole("button", { name: "有效期时间" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "不过期" }));
  expect(screen.getByRole("button", { name: "有效期时间" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(submitted).toBeDefined());
  expect(submitted).not.toHaveProperty("expiresAt");
});
