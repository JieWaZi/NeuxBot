import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { RunForm } from "./run-form";
import type { ExecutionDefaults } from "../model/types";

const origin = "http://workflow-run-form.test";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  server.resetHandlers();
  localStorage.clear();
});
afterAll(() => server.close());

it("运行只收集参数，区分必填与选填并沿用已保存配置", async () => {
  const user = userEvent.setup();
  const accepted = vi.fn();
  const defaults: ExecutionDefaults = {
    runtimeId: "codex",
    model: "saved-model",
    permissionLevel: "default",
  };
  let body: unknown;
  server.use(
    http.post(`${origin}/api/v1/workflows/flow/runs`, async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({ id: "run-1" });
    }),
  );
  const container = document.createElement("div");
  document.body.append(container);
  const view = (
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
      <MemoryRouter>
        <RunForm
          id="flow"
          container={container}
          onClose={() => accepted(false)}
          defaults={defaults}
          revision={{
            id: "version-1",
            manifest: {
              meta: {
                title: "参数验收",
                inputSchema: {
                  type: "object",
                  required: ["material"],
                  properties: {
                    material: {
                      type: "string",
                      title: "会议材料",
                      description: "粘贴本次会议要整理的材料。",
                      "x-ui-control": "textarea",
                    },
                    note: {
                      type: "string",
                      title: "补充说明",
                      description: "填写需要额外关注的事项。",
                      "x-ui-control": "input",
                    },
                  },
                },
              },
            },
          }}
        />
      </MemoryRouter>
    </QueryClientProvider>
  );
  const rendered = render(view);
  const dialog = container;
  expect(within(dialog).queryByText("执行配置")).not.toBeInTheDocument();
  expect(within(dialog).queryByRole("combobox")).not.toBeInTheDocument();
  const required = within(dialog).getByRole("textbox", { name: "会议材料" });
  const optional = within(dialog).getByRole("textbox", { name: "补充说明" });
  expect(required).toBeRequired();
  expect(optional).not.toBeRequired();
  const requiredLabel = dialog.querySelector(`label[for="${required.id}"]`);
  const optionalLabel = dialog.querySelector(`label[for="${optional.id}"]`);
  expect(requiredLabel?.querySelector("span")).toHaveTextContent("*");
  expect(requiredLabel?.querySelector("span")).toHaveClass("text-destructive");
  expect(optionalLabel).not.toHaveTextContent("*");
  await user.click(within(dialog).getByRole("button", { name: "运行" }));
  expect(body).toBeUndefined();
  await user.type(required, "本周会议材料");
  await user.click(within(dialog).getByRole("button", { name: "运行" }));
  await vi.waitFor(() => expect(accepted).toHaveBeenCalledWith(false));
  expect(body).toMatchObject({
    revisionId: "version-1",
    args: { material: "本周会议材料" },
    defaults,
  });
  expect(body).toHaveProperty("requestId", expect.any(String));
  const firstRequest = body;
  await user.click(within(dialog).getByRole("button", { name: "运行" }));
  await vi.waitFor(() => expect(accepted).toHaveBeenCalledTimes(2));
  expect(body).not.toEqual(firstRequest);
  rendered.unmount();
  render(view);
  expect(screen.getByRole("textbox", { name: "会议材料" })).toHaveValue(
    "本周会议材料",
  );
  container.remove();
});
