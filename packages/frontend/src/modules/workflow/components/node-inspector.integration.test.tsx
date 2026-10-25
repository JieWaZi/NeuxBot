import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { NodeInspector } from "./node-inspector";
import { chatWorkspaceHandlers } from "../test/chat-workspace-handlers";

const origin = "http://node-draft.test";
const server = setupServer();
type InspectorProps = Parameters<typeof NodeInspector>[0];

beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderInspector(props: InspectorProps) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = (next: InspectorProps) => (
    <QueryClientProvider client={queryClient}>
      <NodeInspector {...next} />
    </QueryClientProvider>
  );
  const rendered = render(view(props));
  return (next: InspectorProps) => rendered.rerender(view(next));
}

it("节点权限可以选择、保存，并从新版本恢复", async () => {
  const submissions: unknown[] = [];
  server.use(
    ...chatWorkspaceHandlers(origin),
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        submissions.push(await request.json());
        return HttpResponse.json({ revisionId: "revision-2" });
      },
    ),
  );
  const props: InspectorProps = {
    id: "flow",
    revisionId: "revision-1",
    node: {
      id: "worker",
      kind: "worker",
      labelTemplate: "整理关注项",
      options: {
        resolve: {
          kind: "waker",
          wakerId: "fixture",
          permissionLevel: "default",
        },
      },
    },
  };
  const rerender = renderInspector(props);
  const user = userEvent.setup();
  const permission = await screen.findByTestId("composer-permission-control");
  await waitFor(() => expect(permission).toBeEnabled());
  await user.click(permission);
  await user.click(screen.getByRole("menuitemradio", { name: /完全访问/ }));
  await waitFor(() =>
    expect(submissions).toEqual([
      expect.objectContaining({
        edit: expect.objectContaining({
          fields: expect.objectContaining({
            resolve: expect.objectContaining({
              permissionLevel: "full-access",
            }),
          }),
        }),
      }),
    ]),
  );
  rerender({
    ...props,
    revisionId: "revision-2",
    node: {
      ...props.node,
      options: {
        resolve: {
          kind: "waker",
          wakerId: "fixture",
          permissionLevel: "full-access",
        },
      },
    },
  });
  expect(permission).toHaveTextContent("完全访问");
  rerender({
    ...props,
    revisionId: "revision-3",
    node: {
      ...props.node,
      options: {
        resolve: { kind: "waker", wakerId: "fixture", permissionLevel: "auto" },
      },
    },
  });
  expect(permission).toHaveTextContent("自动审批");
});

it("同一节点远端更新保留未提交指令、同步未编辑标题，保存使用最新CAS", async () => {
  const submissions: unknown[] = [];
  server.use(
    http.put(
      `${origin}/api/v1/workflows/flow/configuration`,
      async ({ request }) => {
        submissions.push(await request.json());
        return HttpResponse.json({ revisionId: "revision-3" });
      },
    ),
  );
  const props: InspectorProps = {
    id: "flow",
    revisionId: "revision-1",
    node: {
      id: "question",
      kind: "askUser",
      labelTemplate: "原始标题",
      promptTemplate: '"原始指令"',
    },
  };
  const rerender = renderInspector(props);
  const user = userEvent.setup();
  const prompt = screen.getByRole("textbox", { name: "Prompt" });
  await user.clear(prompt);
  await user.type(prompt, "本地未提交指令");
  rerender({
    ...props,
    revisionId: "revision-2",
    node: {
      ...props.node,
      labelTemplate: "远端标题",
      promptTemplate: '"远端指令"',
    },
  });
  expect(screen.getByRole("textbox", { name: "Prompt" })).toBe(prompt);
  expect(prompt).toHaveValue("本地未提交指令");
  expect(screen.getByRole("textbox", { name: "节点标题" })).toHaveValue(
    "远端标题",
  );
  await user.tab();
  await waitFor(() =>
    expect(submissions).toEqual([
      expect.objectContaining({
        expectedRevisionId: "revision-2",
        edit: {
          target: "node",
          id: "question",
          fields: {},
          prompt: "本地未提交指令",
        },
      }),
    ]),
  );
});

it("同节点新版本保留代码草稿，切换节点或definition身份才重置", async () => {
  const props: InspectorProps = {
    id: "flow",
    revisionId: "revision-1",
    node: {
      id: "calculate",
      kind: "functionTask",
      labelTemplate: "计算",
      options: { code: "return 1" },
    },
  };
  const rerender = renderInspector(props);
  const user = userEvent.setup();
  const code = screen.getByRole("textbox", { name: "代码" });
  await user.clear(code);
  await user.type(code, "return 2");
  const remote = {
    ...props,
    revisionId: "revision-2",
    node: { ...props.node, options: { code: "return 3" } },
  };
  rerender(remote);
  expect(code).toHaveValue("return 2");
  rerender({ ...remote, node: { ...remote.node, id: "other" } });
  expect(code).toHaveValue("return 3");
  await user.clear(code);
  await user.type(code, "return 4");
  rerender({
    ...remote,
    id: "another-flow",
    node: { ...remote.node, id: "other" },
  });
  expect(code).toHaveValue("return 3");
});

it("阶段新版本只同步未编辑字段，保留阶段描述草稿", async () => {
  const props: InspectorProps = {
    id: "flow",
    revisionId: "revision-1",
    phase: { id: "prepare", title: "准备", detail: "原始描述" },
  };
  const rerender = renderInspector(props);
  const user = userEvent.setup();
  const detail = screen.getByRole("textbox", { name: "描述" });
  await user.clear(detail);
  await user.type(detail, "本地描述");
  rerender({
    ...props,
    revisionId: "revision-2",
    phase: { ...props.phase, title: "远端阶段标题", detail: "远端描述" },
  });
  expect(detail).toHaveValue("本地描述");
  expect(screen.getByRole("textbox", { name: "阶段标题" })).toHaveValue(
    "远端阶段标题",
  );
});
