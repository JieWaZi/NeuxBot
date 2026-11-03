import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { RetryRunDialog } from "./retry-run-dialog";
const server = setupServer();
const origin = "http://retry-plan.test";
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
it.each([true, false])(
  "原版本重试展示准确计划并阻止未知效果重跑（允许=%s）",
  async (allowed) => {
    const bodies: unknown[] = [];
    server.use(
      http.get(`${origin}/api/v1/workflows/flow/runs/source`, () =>
        HttpResponse.json({
          manifest: { nodes: [{ id: "worker", label: "历史步骤标题" }] },
        }),
      ),
      http.get(`${origin}/api/v1/workflows/flow/runs/source/retry-plan`, () =>
        HttpResponse.json({
          runId: "source",
          revisionId: "original",
          expectedVersion: 7,
          allowed,
          items: [
            {
              instanceId: "instance",
              nodeId: "worker",
              action: allowed ? "evaluate_reuse" : "reconcile",
              reason: "按冻结证据核对",
            },
          ],
        }),
      ),
      http.post(
        `${origin}/api/v1/workflows/flow/runs/source/retry`,
        async ({ request }) => {
          bodies.push(await request.json());
          return HttpResponse.json({ id: "new-run" });
        },
      ),
    );
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter>
          <RetryRunDialog id="flow" runId="source" />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "重新运行" }));
    const dialog = screen.getByRole("dialog", { name: "原版本重试计划" });
    await within(dialog).findByText("按冻结证据核对");
    await within(dialog).findByText("历史步骤标题");
    expect(within(dialog).queryByText("worker")).not.toBeInTheDocument();
    const submit = within(dialog).getByRole("button", { name: "重新运行" });
    if (!allowed) {
      expect(submit).toBeDisabled();
      expect(bodies).toEqual([]);
      return;
    }
    await userEvent.click(submit);
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({
      requestId: expect.any(String),
      expectedVersion: 7,
    });
  },
);
