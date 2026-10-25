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
          result: "最终结果",
          flowContext: { triggerSource: "agent" },
        },
        manifest: { meta: { phases: [{ id: "fetch", title: "读取评论" }] } },
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
          input: [{ data: "真实评论输入" }],
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
  expect(screen.getByText("对话调用")).toBeInTheDocument();
  expect(detail).not.toHaveBeenCalled();
  await user.click(summary);
  expect(await screen.findByText("真实评论输出")).toBeInTheDocument();
  expect(screen.getByText("真实评论输入")).toBeInTheDocument();
  expect(screen.queryByText("null")).not.toBeInTheDocument();
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
