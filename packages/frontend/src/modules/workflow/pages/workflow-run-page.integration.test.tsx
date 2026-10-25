import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { Link, MemoryRouter, Route, Routes } from "react-router-dom";
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
import { installCanvasDOM } from "../test/canvas-dom";
import { client } from "@/shared/api/generated/client.gen";
import type {
  GetWorkflowRunResponse,
  GetWorkflowInstanceResponse,
} from "@/shared/api/generated/types.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { WorkflowRunView } from "../components/run-view";

const subscription = vi.hoisted(() => ({
  refresh: undefined as (() => void) | undefined,
}));
vi.mock("@/shared/api/events", () => ({
  subscribeWorkflow: (
    _id: string,
    options: { onEvent: () => void; onState?: (state: "open") => void },
  ) => {
    subscription.refresh = options.onEvent;
    options.onState?.("open");
    return () => {
      subscription.refresh = undefined;
    };
  },
}));

const api = "http://workflow.test/api/v1/workflows/flow-1";
const server = setupServer();
let snapshot: GetWorkflowRunResponse;
const answers: unknown[] = [];
const permissions: unknown[] = [];
const instance: GetWorkflowInstanceResponse = {
  instance: {
    id: "actual-1",
    nodeId: "analyse",
    label: "客户 A 风险分析",
    path: "root/environments/item:0",
    phaseId: "review",
    kind: "worker",
    status: "succeeded",
    completedAt: 1,
    chatId: "worker-chat",
    output: { data: { highRisk: true } },
  },
};

let restoreCanvasDOM = () => {};

beforeAll(() => {
  client.setConfig({ baseUrl: "http://workflow.test" });
  server.listen({ onUnhandledRequest: "error" });
  restoreCanvasDOM = installCanvasDOM();
});
beforeEach(() => {
  answers.length = 0;
  permissions.length = 0;
  snapshot = {
    run: {
      id: "run-1",
      definitionId: "flow-1",
      status: "waiting",
      revisionId: "frozen-1",
      revisionOrdinal: 2,
      version: 1,
      args: { client: "A" },
    },
    manifest: {
      meta: {
        title: "发布预审",
        phases: [
          { id: "review", title: "环境预审", detail: "客户环境的风险分析" },
        ],
      },
      nodes: [
        {
          id: "analyse",
          labelTemplate: "风险分析模板",
          kind: "worker",
          phaseId: "review",
          options: { resolve: { kind: "waker", wakerId: "real-waker" } },
        },
        {
          id: "unreached",
          labelTemplate: "尚未开始的写入",
          kind: "worker",
          phaseId: "review",
        },
      ],
      groups: [{ id: "environments", kind: "pipeline" }],
    },
    instances: [instance.instance ?? {}],
    instanceCount: 1,
    nodeCounts: [
      {
        nodeId: "analyse",
        instanceId: "actual-1",
        status: "succeeded",
        count: 1,
      },
    ],
    humanRequests: [
      {
        id: "human-1",
        instanceId: "actual-1",
        status: "waiting",
        prompt: "是否批准客户 A 的风险？",
        options: {
          options: [
            { value: "approve", label: "批准" },
            { value: "deny", label: "否决" },
          ],
          defaultValue: "deny",
          allowSkip: true,
        },
      },
    ],
    operationRequests: [
      {
        id: "operation-1",
        instanceId: "actual-1",
        status: "waiting",
        description: "在已批准目录写验收文件",
        descriptor: { argv: ["tool", "write"] },
        descriptorDigest: "immutable-digest",
      },
    ],
  };
  server.use(
    http.get("http://workflow.test/api/v1/wakers/real-waker", () =>
      HttpResponse.json({ id: "real-waker", name: "验收伙伴" }),
    ),
    http.get(`${api}/runs/run-1`, () => HttpResponse.json(snapshot)),
    http.get(`${api}/runs/run-1/instances/actual-1`, () =>
      HttpResponse.json(instance),
    ),
    http.post(
      `${api}/runs/run-1/humans/human-1/answer`,
      async ({ request }) => {
        answers.push(await request.json());
        snapshot = { ...snapshot, humanRequests: [] };
        return HttpResponse.json({ success: true });
      },
    ),
    http.post(
      `${api}/runs/run-1/operations/operation-1/answer`,
      async ({ request }) => {
        permissions.push(await request.json());
        snapshot = { ...snapshot, operationRequests: [] };
        return HttpResponse.json({ success: true });
      },
    ),
    http.post(`${api}/runs/run-1/cancel`, () => {
      snapshot = {
        ...snapshot,
        run: { ...snapshot.run, status: "cancelled", completedAt: 2 },
      };
      return HttpResponse.json({ success: true });
    }),
  );
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  restoreCanvasDOM();
  server.close();
  vi.unstubAllGlobals();
});

function renderRun() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <MemoryRouter initialEntries={["/workflows/flow-1/runs/run-1"]}>
          <Routes>
            <Route
              path="/workflows/:workflowId/runs/:runId"
              element={
                <>
                  <Link to="/workflows/flow-1/runs/run-1?result=1">
                    查看执行结果
                  </Link>
                  <WorkflowRunView id="flow-1" runId="run-1" />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

it("真实执行画布登记当前运行身份且切换后不保留旧提示抑制范围", async () => {
  snapshot.humanRequests = [];
  snapshot.operationRequests = [];
  const view = renderRun();
  await screen.findByRole("button", { name: "输入参数" });
  expect(
    document.querySelector("[data-notification-workflow-run]"),
  ).toHaveAttribute("data-notification-workflow-run", "run-1");
  view.unmount();
  expect(document.querySelector("[data-notification-workflow-run]")).toBeNull();
});

it("执行输入节点使用冻结参数，弹窗只读且不读取保存输入或默认值", async () => {
  snapshot.humanRequests = [];
  snapshot.operationRequests = [];
  snapshot.manifest = {
    ...snapshot.manifest,
    inputPropertyOrder: { "": ["client", "enabled", "count", "note"] },
    meta: {
      ...snapshot.manifest?.meta,
      inputSchema: {
        type: "object",
        properties: {
          client: { type: "string", title: "客户", default: "默认客户" },
          enabled: { type: "boolean", title: "发送通知", default: true },
          count: { type: "integer", title: "条目数", default: 10 },
          note: { type: "string", title: "备注" },
        },
      },
    },
  };
  snapshot.run = {
    ...snapshot.run,
    args: {
      client: "实际客户",
      enabled: false,
      count: 0,
      note: "本次运行备注",
    },
  };
  localStorage.setItem(
    "ally.workflow.last-run-inputs.flow-1",
    JSON.stringify({
      schema: JSON.stringify(snapshot.manifest.meta?.inputSchema, null, 2),
      args: { client: "上次保存的客户" },
    }),
  );
  const user = userEvent.setup();
  renderRun();
  const input = await screen.findByRole("button", { name: "输入参数" });
  expect(within(input).getByText("实际客户")).toBeInTheDocument();
  expect(within(input).getByText("false")).toBeInTheDocument();
  expect(within(input).getByText("0")).toBeInTheDocument();
  expect(within(input).getByText("还有 1 项参数")).toBeInTheDocument();
  expect(input).not.toHaveAttribute("data-running-shimmer");
  expect(input).not.toHaveAttribute("data-workflow-node");
  await user.click(input);
  const dialog = screen.getByRole("dialog", { name: "运行输入" });
  expect(dialog).toHaveAttribute("data-workflow-input-details", "true");
  expect(dialog).not.toHaveClass("fixed");
  expect(document.body).not.toHaveAttribute("data-scroll-locked");
  expect(within(dialog).getByText("本次运行备注")).toBeInTheDocument();
  expect(within(dialog).queryByText("默认客户")).not.toBeInTheDocument();
  expect(within(dialog).queryByText("上次保存的客户")).not.toBeInTheDocument();
  expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
  expect(
    within(dialog).queryByRole("button", { name: "运行" }),
  ).not.toBeInTheDocument();
  await user.keyboard("{Escape}");
  await waitFor(() =>
    expect(
      screen.queryByRole("dialog", { name: "运行输入" }),
    ).not.toBeInTheDocument(),
  );
});

it("后台查询失败保留运行画布与人工输入草稿，就近重试后继续使用原浮层", async () => {
  const user = userEvent.setup();
  renderRun();
  await screen.findByText("是否批准客户 A 的风险？", undefined, {
    timeout: 5000,
  });
  const node = screen.getByRole("button", { name: "选择 风险分析模板" });
  const input = screen.getByRole("textbox", {
    name: "也可以直接填写修改意见或其他回答",
  });
  await user.type(input, "先保留当前结果");
  server.use(
    http.get(`${api}/runs/run-1`, () =>
      HttpResponse.json({ message: "临时断线" }, { status: 503 }),
    ),
  );
  act(() => subscription.refresh?.());
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "选择 风险分析模板" })).toBe(node);
  expect(input).toHaveValue("先保留当前结果");
  server.use(http.get(`${api}/runs/run-1`, () => HttpResponse.json(snapshot)));
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(
    screen.getByRole("textbox", { name: "也可以直接填写修改意见或其他回答" }),
  ).toBe(input);
});

it("默认答案不自动提交；业务跳过与操作许可分别提交真实契约", async () => {
  const user = userEvent.setup();
  renderRun();
  await screen.findByText("是否批准客户 A 的风险？", undefined, {
    timeout: 5000,
  });
  expect(answers).toEqual([]);
  expect(permissions).toEqual([]);
  await user.click(screen.getByRole("button", { name: "跳过" }));
  await waitFor(() => expect(answers).toHaveLength(1));
  expect(answers[0]).toMatchObject({
    skip: true,
    value: "deny",
    requestId: expect.any(String),
  });
  expect(permissions).toEqual([]);
  await user.click(screen.getByRole("button", { name: "拒绝此次操作" }));
  await waitFor(() => expect(permissions).toHaveLength(1));
  expect(permissions[0]).toMatchObject({
    allowed: false,
    descriptorDigest: "immutable-digest",
    requestId: expect.any(String),
  });
});

it("人工输入支持五种控件，预填默认值只在用户确认后提交类型正确的答案", async () => {
  const user = userEvent.setup();
  snapshot = {
    ...snapshot,
    operationRequests: [],
    humanRequests: [
      {
        id: "human-1",
        instanceId: "actual-1",
        status: "waiting",
        prompt: "填写会议资料",
        options: {
          schema: {
            type: "object",
            properties: {
              name: {
                type: "string",
                title: "会议名称",
                "x-ui-control": "input",
              },
              format: {
                type: "string",
                title: "材料格式",
                enum: ["brief", "full"],
                "x-ui-control": "radio",
                "x-ui-labels": ["简洁", "完整"],
              },
              topics: {
                type: "array",
                title: "准备内容",
                items: { type: "string", enum: ["agenda", "materials"] },
                "x-ui-control": "checkboxes",
              },
              confirmed: {
                type: "boolean",
                title: "确认资料",
                "x-ui-control": "switch",
              },
              notes: {
                type: "string",
                title: "补充说明",
                "x-ui-control": "textarea",
              },
            },
            required: ["name", "format", "topics", "confirmed", "notes"],
          },
          defaultValue: {
            name: "周会",
            format: "brief",
            topics: ["agenda"],
            confirmed: false,
            notes: "",
          },
        },
      },
    ],
  };
  renderRun();
  const name = await screen.findByLabelText(/会议名称/, undefined, {
    timeout: 5000,
  });
  expect(name).toHaveValue("周会");
  expect(answers).toEqual([]);
  await user.clear(name);
  await user.type(name, "研发周会");
  await user.click(screen.getByRole("radio", { name: "完整" }));
  await user.click(screen.getByRole("checkbox", { name: "materials" }));
  await user.click(screen.getByRole("switch", { name: /确认资料/ }));
  const notes = screen.getByLabelText(/补充说明/);
  expect(notes.tagName).toBe("TEXTAREA");
  await user.type(notes, "第一行{Enter}第二行");
  expect(answers).toEqual([]);
  await user.click(screen.getByRole("button", { name: "提交答案" }));
  await waitFor(() => expect(answers).toHaveLength(1));
  expect(answers[0]).toMatchObject({
    skip: false,
    value: {
      name: "研发周会",
      format: "full",
      topics: ["agenda", "materials"],
      confirmed: true,
      notes: "第一行\n第二行",
    },
  });
});

it("画布区分未执行模板，节点展示实际执行项，SSE 刷新最终业务结论", async () => {
  const user = userEvent.setup();
  snapshot = { ...snapshot, humanRequests: [], operationRequests: [] };
  server.use(
    http.get(`${api}/runs/run-1/instances/actual-1`, () =>
      HttpResponse.json({
        ...instance,
        chat: { chat: { id: "worker-chat" } },
      }),
    ),
  );
  renderRun();
  await screen.findByText("尚未开始的写入");
  expect(await screen.findByText("验收伙伴")).toBeInTheDocument();
  expect(screen.getByText("未执行")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "选择 风险分析模板" }));
  expect(
    await screen.findByRole("link", { name: "打开完整执行" }),
  ).toHaveAttribute("href", "/chats/worker-chat");
  const nodeDialog = screen.getByRole("dialog", { name: "客户 A 风险分析" });
  expect(
    within(nodeDialog).queryByRole("heading", { name: "对话" }),
  ).not.toBeInTheDocument();
  expect(
    within(nodeDialog).queryByRole("button", { name: "展开对话" }),
  ).not.toBeInTheDocument();
  expect(
    within(nodeDialog).queryByRole("textbox", { name: "输入消息" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("尚未开始的写入")).toBeInTheDocument();
  snapshot = {
    ...snapshot,
    humanRequests: [],
    operationRequests: [],
    run: {
      ...snapshot.run,
      status: "succeeded",
      completedAt: 2,
      result: { decision: "不予发布", verified: false },
    },
  };
  act(() => subscription.refresh?.());
  await user.click(screen.getByRole("link", { name: "查看执行结果" }));
  expect(
    await screen.findByRole("dialog", { name: "执行详情 · 发布预审" }),
  ).toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByText(/"decision": "不予发布"/)).toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("button", { name: "停止运行" }),
  ).not.toBeInTheDocument();
});

it("停止需要确认，并在服务端终态返回后禁用操作", async () => {
  const user = userEvent.setup();
  renderRun();
  await screen.findByText("是否批准客户 A 的风险？", undefined, {
    timeout: 5000,
  });
  await user.click(screen.getByRole("button", { name: "停止运行" }));
  const dialog = screen.getByRole("dialog", { name: "停止这次运行？" });
  await user.click(within(dialog).getByRole("button", { name: "停止运行" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("button", { name: "停止运行" }),
  ).not.toBeInTheDocument();
});

it("失败运行提供此次运行的优化入口，不显示停止或人工对账", async () => {
  snapshot = {
    ...snapshot,
    run: { ...snapshot.run, status: "failed", completedAt: 100 },
    humanRequests: [],
    operationRequests: [],
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={["/workflows/flow-1/runs/run-1"]}>
        <TooltipProvider>
          <WorkflowRunView id="flow-1" runId="run-1" />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(
    await screen.findByRole("link", { name: "优化此次工作流" }),
  ).toHaveAttribute("href", "/workflows/flow-1?tab=canvas&optimizeRun=run-1");
  expect(
    screen.queryByRole("button", { name: /停止运行|核对执行结果|再次停止/ }),
  ).not.toBeInTheDocument();
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
