import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter } from "react-router-dom";
import { afterAll, afterEach, beforeAll, beforeEach, expect, it } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { InstanceDetails } from "./instance-details";

const origin = "http://instance-feedback.test";
const endpoint = `${origin}/api/v1/workflows/flow/runs/run/instances/instance`;
const server = setupServer();
const instanceSnapshot = () => ({
  instance: {
    id: "instance",
    kind: "worker",
    chatId: "worker-chat",
    executionId: "execution-current",
    input: [{ data: "执行当前审核" }],
  },
  execution: { id: "execution-current", runtimeId: "codex", model: "model" },
  chat: {
    chat: {
      id: "worker-chat",
      activeExecutionId: "execution-current",
      pendingApprovals: [
        {
          approvalId: "approval-current",
          title: "写入发布文件",
          options: [
            { id: "allow-once", name: "批准这次写入", kind: "allow_once" },
          ],
        },
      ],
      pendingElicitations: [
        {
          elicitationId: "question-current",
          mode: "form",
          message: "填写发布说明",
          requestedSchema: {
            type: "object",
            properties: { note: { type: "string", title: "发布说明" } },
            required: ["note"],
          },
        },
      ],
    },
  },
});
let snapshot = instanceSnapshot();
const approvals: unknown[] = [];
const answers: unknown[] = [];

beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  approvals.length = 0;
  answers.length = 0;
  snapshot = instanceSnapshot();
  server.use(
    http.get(endpoint, () => HttpResponse.json(snapshot)),
    http.put(
      `${origin}/api/v1/chats/worker-chat/approvals/approval-current`,
      async ({ request }) => {
        approvals.push(await request.json());
        snapshot = {
          ...snapshot,
          chat: { chat: { ...snapshot.chat?.chat, pendingApprovals: [] } },
        };
        return HttpResponse.json({ success: true });
      },
    ),
    http.put(
      `${origin}/api/v1/chats/worker-chat/elicitations/question-current`,
      async ({ request }) => {
        answers.push(await request.json());
        snapshot = {
          ...snapshot,
          chat: { chat: { ...snapshot.chat?.chat, pendingElicitations: [] } },
        };
        return HttpResponse.json({ success: true });
      },
    ),
  );
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderInstance() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <InstanceDetails id="flow" runId="run" instanceId="instance" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return queryClient;
}

it("真实空返回说明没有返回内容，不制造 null 指令和输出块", async () => {
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        instance: {
          id: "instance",
          kind: "functionTask",
          status: "succeeded",
          completedAt: 100,
          input: [],
          output: null,
        },
      }),
    ),
  );
  renderInstance();
  expect(await screen.findByText("无返回内容")).toBeInTheDocument();
  expect(screen.queryByText("指令")).not.toBeInTheDocument();
  expect(screen.queryByText("null")).not.toBeInTheDocument();
});

it("首次取数有共享骨架，失败可重试，后台失败保留已加载详情", async () => {
  const user = userEvent.setup();
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({ message: "暂时不可用" }, { status: 503 }),
    ),
  );
  const queryClient = renderInstance();
  expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
  await screen.findByRole("alert");
  server.use(http.get(endpoint, () => HttpResponse.json(snapshot)));
  await user.click(screen.getByRole("button", { name: "重试" }));
  const instruction = await screen.findByText("执行当前审核");
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({ message: "后台刷新失败" }, { status: 503 }),
    ),
  );
  await act(async () => {
    await queryClient.invalidateQueries();
  });
  await screen.findByRole("alert");
  expect(screen.getByText("执行当前审核")).toBe(instruction);
});

it("当前Execution存在时仍显示并提交原生审批与问答", async () => {
  const user = userEvent.setup();
  renderInstance();
  await user.click(await screen.findByRole("button", { name: /允许本次/ }));
  await waitFor(() => expect(approvals).toEqual([{ optionId: "allow-once" }]));
  await user.type(
    screen.getByRole("textbox", { name: "发布说明" }),
    "可以发布",
  );
  await user.click(screen.getByRole("button", { name: "继续" }));
  await waitFor(() =>
    expect(answers).toEqual([
      { content: { note: "可以发布" }, declined: false },
    ]),
  );
});

it("Chat切到另一Execution后，旧instance不展示新执行交互", async () => {
  snapshot = {
    ...snapshot,
    chat: {
      chat: { ...snapshot.chat?.chat, activeExecutionId: "execution-later" },
    },
  };
  renderInstance();
  await screen.findByText("执行当前审核");
  expect(
    screen.queryByRole("button", { name: /允许本次/ }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("填写发布说明")).not.toBeInTheDocument();
});

it("结果不确定作为失败证据展示，不再要求人工核对或再次停止", async () => {
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        instance: {
          id: "instance",
          kind: "worker",
          status: "failed",
          completedAt: 100,
          input: [{ data: "整理关注项" }],
          failure: { data: { code: "EFFECT_OUTCOME_UNKNOWN" } },
          options: { skippable: true },
        },
        effect: { id: "effect", status: "unknown", attemptGeneration: 1 },
      }),
    ),
  );
  renderInstance();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "此次执行未能取得可靠结果，已按失败结束",
  );
  expect(screen.getByText(/EFFECT_OUTCOME_UNKNOWN/)).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /核对|停止|跳过/ }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
});
