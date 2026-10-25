import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { saveBlob } from "@/shared/platform";
import { WorkflowArtifacts } from "./workflow-artifacts";

vi.mock("@/shared/platform", () => ({ saveBlob: vi.fn(async () => true) }));
const origin = "http://workflow-download.test";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

it("按明确的文件结果身份下载，不依赖预览类型", async () => {
  const user = userEvent.setup();
  let requestedId: string | null = null;
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/runs/run/file`, ({ request }) => {
      requestedId = new URL(request.url).searchParams.get("fileId");
      return new HttpResponse("binary spreadsheet bytes", {
        headers: { "Content-Type": "application/octet-stream" },
      });
    }),
  );
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WorkflowArtifacts
        id="flow"
        runId="run"
        values={[]}
        files={[
          { id: "output-id", name: "预算.xlsx", size: 24, available: true },
        ]}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "下载文件" }));
  await waitFor(() => expect(saveBlob).toHaveBeenCalled());
  expect(requestedId).toBe("output-id");
  const call = vi.mocked(saveBlob).mock.calls[0];
  expect(call?.[1]).toBe("预算.xlsx");
  expect(await call?.[0].text()).toBe("binary spreadsheet bytes");
});

it("文件不可用时保留说明并禁用下载", () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WorkflowArtifacts
        id="flow"
        runId="run"
        values={[]}
        files={[{ id: "output-id", name: "预算.xlsx", available: false }]}
      />
    </QueryClientProvider>,
  );
  expect(screen.getByRole("button", { name: "下载文件" })).toBeDisabled();
  expect(
    screen.getByText("文件已移除或不在本次运行的允许目录内。"),
  ).toBeVisible();
});
