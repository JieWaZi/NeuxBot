import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
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
              sdk: "ally.workflow",
              description: "",
              phases: [],
              nodes: [],
              controls: [],
              edges: [],
              scripts: [],
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
              title: "参数验收",
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

it.each([true, false])(
  "没有创作聊天时独立解析运行配置，可用=%s",
  async (available) => {
    let body: unknown;
    server.use(
      http.get(`${origin}/api/v1/runtimes`, () =>
        HttpResponse.json({
          resources: available
            ? [
                {
                  id: "codex",
                  enabled: true,
                  state: "ready",
                  isDefault: true,
                  defaultModel: "native",
                  models: [
                    {
                      id: "native",
                      name: "Native",
                      permissionLevels: ["default"],
                    },
                  ],
                },
              ]
            : [],
        }),
      ),
      http.get(`${origin}/api/v1/ai-accounts`, () =>
        HttpResponse.json({ resources: [] }),
      ),
      http.get(`${origin}/api/v1/runtimes/codex/models`, () =>
        HttpResponse.json({
          runtimeId: "codex",
          accountId: "system",
          state: "fresh",
          defaultModel: "native",
          models: [
            {
              id: "native",
              name: "Native",
              permissionLevels: ["default"],
            },
          ],
        }),
      ),
      http.post(`${origin}/api/v1/workflows/flow/runs`, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ id: "run-defaults" });
      }),
    );
    const container = document.createElement("div");
    document.body.append(container);
    const rendered = render(
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
            onClose={() => {}}
            revision={{
              id: "version-1",
              manifest: {
                sdk: "ally.workflow",
                title: "无会话工作流",
                description: "",
                controls: [],
                edges: [],
                nodes: [
                  {
                    id: "review",
                    kind: "worker",
                    label: "审核",
                    inputs: [],
                    execution: {
                      resolve: { kind: "waker", wakerId: "reviewer" },
                    },
                  },
                ],
                phases: [],
                scripts: [],
              },
            }}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    try {
      const run = within(container).getByRole("button", { name: "运行" });
      if (available) {
        await waitFor(() => expect(run).toBeEnabled());
        await userEvent.click(run);
        await waitFor(() =>
          expect(body).toMatchObject({
            defaults: {
              runtimeId: "codex",
              accountId: "system",
              model: "native",
              permissionLevel: "default",
            },
          }),
        );
      } else {
        expect(await within(container).findByRole("alert")).toHaveTextContent(
          "没有可用的运行配置",
        );
        expect(run).toBeDisabled();
        expect(body).toBeUndefined();
      }
    } finally {
      rendered.unmount();
      container.remove();
    }
  },
);

it.each([false, true])(
  "没有全局智能体时，纯本地或已绑定智能体流程可运行，绑定=%s",
  async (bound) => {
    let body: unknown;
    server.use(
      http.post(`${origin}/api/v1/workflows/flow/runs`, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ id: "offline-run" });
      }),
    );
    const container = document.createElement("div");
    document.body.append(container);
    const rendered = render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter>
          <RunForm
            id="flow"
            container={container}
            onClose={() => {}}
            revision={{
              id: "version-local",
              manifest: {
                sdk: "ally.workflow",
                title: "本地",
                description: "",
                controls: [],
                edges: [],
                phases: [],
                scripts: [],
                nodes: bound
                  ? [
                      {
                        id: "worker",
                        kind: "worker",
                        label: "审核",
                        inputs: [],
                        execution: {
                          resolve: {
                            kind: "waker",
                            wakerId: "reviewer",
                            runtimeId: "codex",
                          },
                        },
                      },
                    ]
                  : [
                      {
                        id: "local",
                        kind: "functionTask",
                        label: "整理数据",
                        inputs: [],
                        execution: {
                          language: "javascript",
                          executionMode: "pure",
                          codeDigest: "digest",
                        },
                      },
                    ],
              },
            }}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    try {
      const run = within(container).getByRole("button", { name: "运行" });
      expect(run).toBeEnabled();
      await userEvent.click(run);
      await waitFor(() =>
        expect(body).toMatchObject({
          revisionId: "version-local",
          defaults: {},
        }),
      );
    } finally {
      rendered.unmount();
      container.remove();
    }
  },
);
