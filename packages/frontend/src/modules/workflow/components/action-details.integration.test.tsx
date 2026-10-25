import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import type { Manifest } from "../model/types";
import { ActionDetails } from "./action-details";

const origin = "http://workflow-action-details.test";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderDetails(manifest: Manifest, revisionId = "saved-version") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = (version: string) => (
    <QueryClientProvider client={queryClient}>
      <ActionDetails
        id="flow"
        revisionId={version}
        node={{ id: "read-comments", kind: "action" }}
        manifest={manifest}
      />
    </QueryClientProvider>
  );
  const result = render(view(revisionId));
  return (version: string) => result.rerender(view(version));
}

it("调用代码区合并实际内置处理函数和实现，保持只读并随版本更新", async () => {
  const handler = "async (ctx, input) => { return input.comments; }";
  const call = "action(COMMENT_ACTION, { input: args })";
  const versions: Array<string | null> = [];
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/source`, ({ request }) => {
      const revision = new URL(request.url).searchParams.get("revisionId");
      versions.push(revision);
      return HttpResponse.json({
        revisionId: revision,
        actionDetails: {
          "read-comments": {
            actionId: "comments",
            callSource: call,
            handlerSource:
              revision === "saved-version"
                ? handler
                : "async (ctx, input) => { return []; }",
          },
        },
      });
    }),
  );
  const rerender = renderDetails({
    meta: {
      actions: [
        { id: "unrelated", handler: "wrong" },
        {
          id: "comments",
          label: "获取评论",
          description: "分页读取评论",
          type: "builtin",
          handler: "fetchComments",
        },
      ],
    },
  });
  const code = await screen.findByRole("textbox", {
    name: "调用代码与参数",
  });
  const target =
    '// id: "comments"\n// type: "builtin"\n// handler: "fetchComments"';
  expect(code).toHaveValue(`${target}\n\n${call}\n\n${handler}`);
  expect(code).toHaveAttribute("readonly");
  expect(screen.getAllByRole("textbox")).toHaveLength(1);
  expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  expect(screen.queryByText("获取评论")).not.toBeInTheDocument();
  expect(screen.queryByText("分页读取评论")).not.toBeInTheDocument();
  await userEvent.setup().type(code, "unexpected edit");
  expect(code).toHaveValue(`${target}\n\n${call}\n\n${handler}`);
  rerender("history-version");
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "调用代码与参数" })).toHaveValue(
      `${target}\n\n${call}\n\nasync (ctx, input) => { return []; }`,
    ),
  );
  expect(
    screen.getByRole("textbox", { name: "调用代码与参数" }),
  ).toHaveAttribute("readonly");
  expect(versions).toEqual(["saved-version", "history-version"]);
});

it("脚本调用区保留真实程序和动态参数，仅引用外部文件", async () => {
  const call =
    "action('comments', { args: ['scripts/comments.py', JSON.stringify(args)] })";
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/source`, () =>
      HttpResponse.json({
        actionDetails: {
          "read-comments": { actionId: "comments", callSource: call },
        },
      }),
    ),
  );
  renderDetails({
    meta: {
      actions: [
        {
          id: "comments",
          label: "获取评论",
          type: "script",
          executable: "python3",
          workingDirectory: "workflow_scope",
        },
      ],
    },
  });
  expect(
    await screen.findByRole("textbox", { name: "调用代码与参数" }),
  ).toHaveValue(
    `// id: "comments"\n// type: "script"\n// executable: "python3"\n// workingDirectory: "workflow_scope"\n\n${call}`,
  );
  expect(screen.getAllByRole("textbox")).toHaveLength(1);
  expect(screen.queryByText("可执行程序")).not.toBeInTheDocument();
  expect(
    screen.queryByText(
      "参数见下方调用代码；外部脚本文件仅展示引用，不读取文件正文。",
    ),
  ).not.toBeInTheDocument();
});

it("HTTP 展示请求限制和包含运行时表达式的请求配置", async () => {
  const call =
    "action('comments', { url: args.url, method: 'POST', body: args.body })";
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/source`, () =>
      HttpResponse.json({
        actionDetails: {
          "read-comments": { actionId: "comments", callSource: call },
        },
      }),
    ),
  );
  renderDetails({
    meta: {
      actions: [
        {
          id: "comments",
          type: "http",
          allowedOrigins: ["https://example.com"],
          methods: ["POST"],
        },
      ],
    },
  });
  expect(
    await screen.findByRole("textbox", { name: "调用代码与参数" }),
  ).toHaveValue(
    `// id: "comments"\n// type: "http"\n// allowedOrigins: ["https://example.com"]\n// methods: ["POST"]\n\n${call}`,
  );
  expect(screen.getAllByRole("textbox")).toHaveLength(1);
});

it("加载失败可在面板内重新加载", async () => {
  let failed = true;
  server.use(
    http.get(`${origin}/api/v1/workflows/flow/source`, () =>
      failed
        ? new HttpResponse(null, { status: 500 })
        : HttpResponse.json({
            actionDetails: {
              "read-comments": {
                actionId: "comments",
                callSource: "action('comments', {})",
              },
            },
          }),
    ),
  );
  renderDetails({
    meta: {
      actions: [{ id: "comments", type: "script", executable: "python3" }],
    },
  });
  await screen.findByRole("alert");
  failed = false;
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "重新加载" }));
  await screen.findByRole("textbox", { name: "调用代码与参数" });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
