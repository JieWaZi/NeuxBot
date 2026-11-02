import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { RunResultDialog } from "./run-result-dialog";

const origin = "http://run-result.test";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

it("展开摘要步骤时读取真实输入输出，不把列表省略字段显示为 null", async () => {
  const user = userEvent.setup();
  const detail = vi.fn();
  const endpoint = `${origin}/api/v1/workflows/flow/runs/run`;
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        run: {
          id: "run",
          status: "succeeded",
          completedAt: 100,
          args: { client: "客户 A", enabled: false, nested: { count: 0 } },
          result: { summary: "最终结果", items: ["第一项", "第二项"] },
          flowContext: { triggerSource: "agent" },
        },
        manifest: {
          sdk: "ally.workflow",
          title: "测试工作流",
          description: "",
          nodes: [],
          controls: [],
          edges: [],
          scripts: [],
          phases: [{ detail: "", id: "fetch", title: "读取评论" }],
        },
        files: [
          { id: "ready-file", name: "report.md", size: 2048, available: true },
          {
            id: "missing-file",
            name: "missing.csv",
            size: 0,
            available: false,
          },
        ],
        instances: [
          {
            id: "step",
            phaseId: "fetch",
            kind: "functionTask",
            label: "整理评论结果",
            status: "succeeded",
            completedAt: 100,
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/workflows/flow/events/history`, () =>
      HttpResponse.json({ resources: [] }),
    ),
    http.get(`${endpoint}/instances/step`, () => {
      detail();
      return HttpResponse.json({
        instance: {
          id: "step",
          kind: "functionTask",
          status: "succeeded",
          completedAt: 100,
          input: [{ data: { input: "真实评论输入", code: "private-code" } }],
          output: "真实评论输出",
        },
      });
    }),
  );
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <RunResultDialog id="flow" runId="run" open onOpenChange={vi.fn()} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  const summary = await screen.findByText(/整理评论结果/);
  const inputs = screen.getByText(/"client": "客户 A"/);
  expect(JSON.parse(inputs.textContent ?? "")).toEqual({
    client: "客户 A",
    enabled: false,
    nested: { count: 0 },
  });
  const output = screen.getByText(/"summary": "最终结果"/);
  expect(JSON.parse(output.textContent ?? "")).toEqual({
    summary: "最终结果",
    items: ["第一项", "第二项"],
  });
  expect(inputs.closest("pre")).toHaveClass("max-h-80", "overflow-auto");
  expect(output.closest("pre")).toHaveClass("max-h-80", "overflow-auto");
  expect(screen.getByText("对话调用")).toBeInTheDocument();
  expect(detail).not.toHaveBeenCalled();
  expect(screen.getByText("report.md")).toBeInTheDocument();
  expect(screen.getByText("2 KB")).toBeInTheDocument();
  expect(screen.getByText("missing.csv")).toBeInTheDocument();
  expect(
    screen.getByText("文件已移除或不在本次运行的允许目录内。"),
  ).toBeInTheDocument();
  await user.click(summary);
  expect(await screen.findByText('"真实评论输出"')).toBeInTheDocument();
  expect(screen.getByText('"真实评论输入"')).toBeInTheDocument();
  expect(screen.queryByText("null")).not.toBeInTheDocument();
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));

vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: () => {} }));
