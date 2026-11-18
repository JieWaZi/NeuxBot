import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { ReactNode } from "react";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import type { WorkflowNode } from "../model/types";
import { NodeInspector } from "./node-inspector";

const origin = "http://workflow-node-inspector.test";
const server = setupServer();
const requests: unknown[] = [];
const worker: WorkflowNode = {
  id: "review",
  kind: "worker",
  label: "复核材料",
  inputs: [],
  execution: {
    promptTemplate: "`检查 ${args.material}`",
    resolve: {
      kind: "waker",
      wakerId: "one",
      runtimeId: "codex",
      model: "model-a",
    },
    toolPolicy: {
      connectors: { mode: "inherit" },
      nativeTools: { mode: "inherit" },
    },
  },
};
beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  requests.length = 0;
  server.use(
    http.get(`${origin}/api/v1/wakers`, () =>
      HttpResponse.json({
        resources: [
          { id: "one", name: "后端伙伴" },
          { id: "two", name: "复核伙伴" },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/wakers/:id`, ({ params }) =>
      HttpResponse.json({ id: params.id, name: "后端伙伴" }),
    ),
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            isDefault: true,
            defaultModel: "model-a",
          },
          {
            id: "claude",
            name: "Claude",
            enabled: true,
            defaultModel: "model-a",
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/ai-accounts`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "work",
            runtimeId: "claude",
            name: "工作账号",
            authMode: "subscription",
            hasCredential: true,
            endpoint: {
              protocol: "anthropic-messages",
              baseUrl: "https://api.anthropic.com",
            },
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/runtimes/:id/models`, ({ params, request }) =>
      HttpResponse.json({
        runtimeId: params.id,
        accountId: new URL(request.url).searchParams.get("accountId"),
        state: "fresh",
        defaultModel: "model-a",
        models: [
          {
            id: "model-a",
            name: "模型 A",
            permissionLevels: ["default", "auto", "full-access"],
          },
          {
            id: "model-b",
            name: "模型 B",
            permissionLevels: ["default", "auto", "full-access"],
            reasoningOptions: [
              { id: "low", name: "低" },
              { id: "high", name: "高" },
            ],
          },
        ],
      }),
    ),
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        requests.push(await request.json());
        return HttpResponse.json({ revisionId: "revision-2", ordinal: 2 });
      },
    ),
  );
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

function renderInspector(content: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={queryClient}>{content}</QueryClientProvider>,
  );
  return {
    ...result,
    rerender: (next: ReactNode) =>
      result.rerender(
        <QueryClientProvider client={queryClient}>{next}</QueryClientProvider>,
      ),
  };
}
it("用户决策详情展示业务描述和完整问题，隐藏内部实现", () => {
  renderInspector(
    <NodeInspector
      id="flow"
      revisionId="revision"
      readOnly
      node={{
        id: "question-internal",
        kind: "askUser",
        label: "选择方案",
        description: "根据候选方案作出选择",
        inputs: [],
        execution: {
          kind: "select",
          promptTemplate: '"请选择需要分析的方案，确认后伙伴开始分析。"',
        },
      }}
    />,
  );
  expect(screen.getByText("用户决策")).toBeInTheDocument();
  expect(screen.getByText("根据候选方案作出选择")).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Prompt" })).toHaveValue(
    "请选择需要分析的方案，确认后伙伴开始分析。",
  );
  expect(screen.queryByText("question-internal")).not.toBeInTheDocument();
});
it("计算详情保留业务修改入口，不展示代码、摘要或执行模式", async () => {
  renderInspector(
    <NodeInspector
      id="flow"
      revisionId="revision"
      node={{
        id: "calculate",
        kind: "functionTask",
        label: "整理数据",
        description: "合并用户选择的数据",
        inputs: [],
        execution: {
          language: "python",
          executionMode: "trusted_local",
          codeDigest: "private-digest",
        },
      }}
    />,
  );
  expect(screen.getByText("本地处理")).toBeInTheDocument();
  expect(
    screen.queryByText(/trusted_local|private-digest|python/),
  ).not.toBeInTheDocument();
  const user = userEvent.setup();
  const label = screen.getByRole("textbox", { name: "节点标题" });
  await user.clear(label);
  await user.type(label, "整理销售数据");
  await user.tab();
  expect(requests).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]).toMatchObject({
    expectedRevisionId: "revision",
    edit: {
      target: "node",
      id: "calculate",
      fields: { label: "整理销售数据" },
    },
  });
  expect(await screen.findByRole("status")).toHaveTextContent("已保存");
});

it("脚本详情使用可读风险标签，不泄漏翻译键或内部标识", () => {
  renderInspector(
    <NodeInspector
      id="flow"
      revisionId="revision"
      node={{
        id: "private-node",
        kind: "action",
        label: "生成报告",
        inputs: [],
        execution: {
          actionId: "private-action",
          actionType: "script",
          scriptId: "private-script",
        },
      }}
      manifest={{
        sdk: "bot.workflow",
        title: "报告",
        description: "",
        phases: [],
        nodes: [],
        controls: [],
        edges: [],
        scripts: [],
        actions: [
          {
            id: "private-action",
            label: "生成报告",
            description: "在本地生成报告",
            type: "script",
            risk: "low",
          },
        ],
      }}
    />,
  );
  expect(screen.getByText("操作风险 · 低")).toBeInTheDocument();
  expect(
    screen.queryByText(/nodeOption|private-node|private-action/),
  ).not.toBeInTheDocument();
});

it("伙伴、CLI、账号、模型、思考强度和权限可编辑并与标题和 Prompt 一次保存", async () => {
  const user = userEvent.setup();
  renderInspector(
    <NodeInspector
      id="flow"
      revisionId="revision"
      node={{
        ...worker,
        execution: {
          ...worker.execution,
          toolPolicy: { connectors: { mode: "allowlist", connectorIds: [] } },
        },
      }}
    />,
  );
  const partner = await screen.findByRole("combobox", { name: "指派伙伴" });
  await waitFor(() => expect(partner).toBeEnabled());
  await user.click(partner);
  await user.click(screen.getByRole("option", { name: "复核伙伴" }));
  const runtime = screen.getByTestId("composer-runtime-control");
  await waitFor(() => expect(runtime).toBeEnabled());
  await user.click(runtime);
  await user.click(screen.getByRole("menuitemradio", { name: /Claude/ }));
  await user.click(screen.getByRole("menuitemradio", { name: "工作账号" }));
  const model = screen.getByTestId("composer-model-control");
  await waitFor(() => expect(model).toBeEnabled());
  await user.click(model);
  await user.click(await screen.findByRole("radio", { name: "模型 B" }));
  const strength = screen.getByRole("slider");
  strength.focus();
  await user.keyboard("{End}");
  await user.keyboard("{Escape}");
  await user.click(screen.getByTestId("composer-permission-control"));
  await user.click(screen.getByRole("menuitemradio", { name: /完全访问/ }));
  const label = screen.getByRole("textbox", { name: "节点标题" });
  await user.clear(label);
  await user.type(label, "复核销售材料");
  const prompt = screen.getByRole("textbox", { name: "Prompt" });
  await user.clear(prompt);
  await user.type(prompt, "复核本周材料");
  await user.tab();
  expect(requests).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]).toMatchObject({
    expectedRevisionId: "revision",
    requestId: expect.any(String),
    edit: {
      target: "node",
      id: "review",
      prompt: "复核本周材料",
      fields: {
        label: "复核销售材料",
        "resolve.wakerId": "two",
        "resolve.runtimeId": "claude",
        "resolve.accountId": "work",
        "resolve.model": "model-b",
        "resolve.reasoning": "high",
        "resolve.permissionLevel": "full-access",
      },
    },
  });
  expect(requests[0]).not.toHaveProperty("edit.fields.toolPolicy");
  expect(await screen.findByText("已保存")).toBeVisible();
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
});

it("保存失败保留草稿并复用请求身份重试", async () => {
  let fail = true;
  server.use(
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        requests.push(await request.json());
        return fail
          ? HttpResponse.json({ message: "保存失败" }, { status: 409 })
          : HttpResponse.json({ revisionId: "revision-2" });
      },
    ),
  );
  const user = userEvent.setup();
  renderInspector(
    <NodeInspector
      id="flow"
      revisionId="revision"
      node={{
        id: "local",
        kind: "functionTask",
        label: "原标题",
        inputs: [],
        execution: {
          language: "javascript",
          executionMode: "pure",
          codeDigest: "digest",
        },
      }}
    />,
  );
  const label = screen.getByRole("textbox", { name: "节点标题" });
  await user.clear(label);
  await user.type(label, "保留草稿");
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(label).toHaveValue("保留草稿");
  expect(screen.queryByText("已保存")).not.toBeInTheDocument();
  fail = false;
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByText("已保存")).toBeVisible();
  expect(requests).toHaveLength(2);
  expect(requests[1]).toEqual(requests[0]);
});

it("详情刷新前再次保存使用已提交的版本回执", async () => {
  const user = userEvent.setup();
  renderInspector(
    <NodeInspector id="flow" revisionId="revision" node={worker} />,
  );
  const title = screen.getByRole("textbox", { name: "节点标题" });
  await user.type(title, "一");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("已保存");
  await user.type(title, "二");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]).toMatchObject({ expectedRevisionId: "revision-2" });
});

it("历史版本不提供伙伴与执行配置编辑或保存入口", async () => {
  renderInspector(
    <NodeInspector
      id="flow"
      revisionId="old-revision"
      node={worker}
      readOnly
    />,
  );
  expect(screen.getByRole("textbox", { name: "节点标题" })).toHaveAttribute(
    "readonly",
  );
  expect(screen.getByRole("textbox", { name: "Prompt" })).toHaveAttribute(
    "readonly",
  );
  expect(
    screen.queryByRole("combobox", { name: "指派伙伴" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByTestId("composer-runtime-control"),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "保存" }),
  ).not.toBeInTheDocument();
  expect(requests).toHaveLength(0);
});

it("仅修改权限保留流程的 CLI、账号和模型继承", async () => {
  const user = userEvent.setup();
  renderInspector(
    <NodeInspector
      id="flow"
      revisionId="revision"
      defaults={{
        runtimeId: "claude",
        accountId: "work",
        model: "model-b",
        reasoning: "high",
      }}
      node={{
        ...worker,
        execution: { resolve: { kind: "waker", wakerId: "one" } },
      }}
    />,
  );
  await waitFor(() =>
    expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
      "Claude",
    ),
  );
  expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
    "工作账号",
  );
  await waitFor(() =>
    expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
      "模型 B",
    ),
  );
  const permissions = screen.getByTestId("composer-permission-control");
  await waitFor(() => expect(permissions).toBeEnabled());
  await user.click(permissions);
  await user.click(screen.getByRole("menuitemradio", { name: /完全访问/ }));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]).toHaveProperty("edit.fields", {
    "resolve.permissionLevel": "full-access",
  });
});

it("未保存草稿切到同节点的历史版本时展示冻结内容", async () => {
  const user = userEvent.setup();
  const view = renderInspector(
    <NodeInspector id="flow" revisionId="current" node={worker} />,
  );
  const title = screen.getByRole("textbox", { name: "节点标题" });
  await user.clear(title);
  await user.type(title, "未保存的草稿");
  view.rerender(
    <NodeInspector
      id="flow"
      revisionId="old"
      node={{ ...worker, label: "历史标题" }}
      readOnly
    />,
  );
  expect(title).toHaveValue("历史标题");
  expect(title).toHaveAttribute("readonly");
  expect(requests).toHaveLength(0);
});

it("保存期间切历史版本后，旧请求完成不覆盖历史内容", async () => {
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  server.use(
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        requests.push(await request.json());
        await pending;
        return HttpResponse.json({ revisionId: "new" });
      },
    ),
  );
  const user = userEvent.setup();
  const view = renderInspector(
    <NodeInspector id="flow" revisionId="current" node={worker} />,
  );
  const title = screen.getByRole("textbox", { name: "节点标题" });
  await user.clear(title);
  await user.type(title, "保存中的新标题");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  view.rerender(
    <NodeInspector
      id="flow"
      revisionId="old"
      node={{ ...worker, label: "历史标题" }}
      readOnly
    />,
  );
  finish();
  await waitFor(() => expect(title).not.toBeDisabled());
  expect(title).toHaveValue("历史标题");
  expect(screen.queryByText("已保存")).not.toBeInTheDocument();
});

it("后台修订的执行配置字段不会被本地其他字段草稿覆盖", async () => {
  const user = userEvent.setup();
  const view = renderInspector(
    <NodeInspector id="flow" revisionId="revision" node={worker} />,
  );
  const partner = screen.getByRole("combobox", { name: "指派伙伴" });
  await waitFor(() => expect(partner).toBeEnabled());
  await user.click(partner);
  await user.click(screen.getByRole("option", { name: "复核伙伴" }));
  view.rerender(
    <NodeInspector
      id="flow"
      revisionId="remote-revision"
      node={{
        ...worker,
        execution: {
          ...worker.execution,
          resolve: {
            ...worker.execution.resolve,
            permissionLevel: "full-access",
          },
        },
      }}
    />,
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]).toMatchObject({
    expectedRevisionId: "remote-revision",
    edit: {
      fields: { "resolve.wakerId": "two" },
    },
  });
});

it("保存成功后的回写保留保存期间读到的更新版本字段", async () => {
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  server.use(
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        requests.push(await request.json());
        await pending;
        return HttpResponse.json({ revisionId: "saved-revision" });
      },
    ),
  );
  const user = userEvent.setup();
  const view = renderInspector(
    <NodeInspector id="flow" revisionId="revision" node={worker} />,
  );
  const title = screen.getByRole("textbox", { name: "节点标题" });
  await user.clear(title);
  await user.type(title, "已保存标题");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  view.rerender(
    <NodeInspector
      id="flow"
      revisionId="newer-revision"
      node={{
        ...worker,
        label: "已保存标题",
        execution: {
          ...worker.execution,
          resolve: {
            ...worker.execution.resolve,
            permissionLevel: "full-access",
          },
        },
      }}
    />,
  );
  finish();
  await screen.findByText("已保存");
  const partner = screen.getByRole("combobox", { name: "指派伙伴" });
  await user.click(partner);
  await user.click(screen.getByRole("option", { name: "复核伙伴" }));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]).toMatchObject({
    expectedRevisionId: "newer-revision",
    edit: {
      fields: { "resolve.wakerId": "two" },
    },
  });
});
