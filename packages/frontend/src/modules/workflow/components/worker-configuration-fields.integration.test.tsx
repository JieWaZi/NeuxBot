import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import type { ExecutionDefaults } from "../model/types";
import { WorkerConfigurationFields } from "./worker-configuration-fields";

const origin = "http://worker-configuration.test";
const reasons = [
  { id: "low", name: "低" },
  { id: "high", name: "高" },
];
const server = setupServer(
  http.get(`${origin}/api/v1/wakers`, ({ request }) => {
    const pageNum = Number(new URL(request.url).searchParams.get("pageNum"));
    return HttpResponse.json({
      pageNum,
      pageSize: 100,
      totalSize: 102,
      resources:
        pageNum === 1
          ? Array.from({ length: 100 }, (_, index) => ({
              id: `partner-${index + 1}`,
              name: `伙伴 ${index + 1}`,
            }))
          : [
              { id: "partner-101", name: "伙伴 101" },
              { id: "partner-102", name: "伙伴 102" },
            ],
    });
  }),
  http.get(`${origin}/api/v1/wakers/:wakerId`, ({ params }) =>
    HttpResponse.json({ id: params.wakerId, name: "伙伴 101" }),
  ),
  http.get(`${origin}/api/v1/runtimes`, () =>
    HttpResponse.json({
      resources: [
        { id: "codex", name: "Codex", enabled: true, isDefault: true },
        {
          id: "claude",
          name: "Claude",
          enabled: true,
          defaultAccountId: "personal",
        },
      ],
    }),
  ),
  http.get(`${origin}/api/v1/ai-accounts`, () =>
    HttpResponse.json({
      resources: [
        { id: "work", name: "工作账号" },
        { id: "personal", name: "个人账号" },
      ].map((account) => ({
        ...account,
        runtimeId: "claude",
        hasCredential: true,
        authMode: "subscription",
        endpoint: {
          protocol: "anthropic-messages",
          baseUrl: "https://api.anthropic.com",
        },
      })),
    }),
  ),
  http.get(`${origin}/api/v1/runtimes/:id/models`, ({ request, params }) => {
    const accountId = new URL(request.url).searchParams.get("accountId");
    const [id, name] =
      params.id === "codex"
        ? ["codex-default", "Codex 默认模型"]
        : accountId === "personal"
          ? ["personal-default", "个人默认模型"]
          : ["work-default", "工作默认模型"];
    return HttpResponse.json({
      runtimeId: params.id,
      accountId,
      state: "fresh",
      defaultModel: id,
      models: [
        { id, name, defaultReasoning: "low", reasoningOptions: reasons },
        { id: "workflow-model", name: "流程模型", reasoningOptions: reasons },
        {
          id: "node-model",
          name: "节点模型",
          defaultReasoning: "low",
          reasoningOptions: reasons,
        },
      ],
    });
  }),
);

beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterAll(() => server.close());

const defaults: ExecutionDefaults = {
  runtimeId: "claude",
  accountId: "work",
  model: "workflow-model",
  reasoning: "high",
};

function renderFields(overrides: Record<string, unknown>) {
  const onChange = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkerConfigurationFields
        fieldId="worker"
        resolve={{ kind: "waker", wakerId: "partner-1", ...overrides }}
        defaults={defaults}
        disabled={false}
        onChange={onChange}
      />
    </QueryClientProvider>,
  );
  return onChange;
}

it("可展示并选择伙伴目录第一页以后的伙伴", async () => {
  const user = userEvent.setup();
  const onChange = renderFields({ wakerId: "partner-101" });
  const partner = screen.getByRole("combobox", { name: "指派伙伴" });
  await waitFor(() => expect(partner).toHaveTextContent("伙伴 101"));
  await user.click(partner);
  await user.click(await screen.findByRole("option", { name: "伙伴 102" }));
  expect(onChange).toHaveBeenCalledWith({
    kind: "waker",
    wakerId: "partner-102",
  });
});

it.each([
  ["切换 CLI", { runtimeId: "codex" }, "Codex", "Codex 默认模型"],
  ["切换账号", { accountId: "personal" }, "个人账号", "个人默认模型"],
  ["跟随 CLI 的默认账号", { accountId: "" }, "个人账号", "个人默认模型"],
  ["跟随账号的默认模型", { model: "" }, "工作账号", "默认模型"],
] as const)(
  "%s 后使用当前执行配置的默认值",
  async (_, overrides, account, model) => {
    renderFields(overrides);
    await waitFor(() =>
      expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
        account,
      ),
    );
    await waitFor(() =>
      expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
        model,
      ),
    );
  },
);

it("模型被覆盖后使用该模型的默认思考强度", async () => {
  renderFields({ model: "node-model" });
  await waitFor(() =>
    expect(screen.getByTestId("composer-model-control")).toHaveAttribute(
      "title",
      "节点模型 · 低",
    ),
  );
});
