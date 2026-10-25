import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import { knowledgeAdapter } from "@/shared/api/knowledge";

const origin = "http://wiki-maintenance.test";
const local = {
  backend: "local",
  connectionId: "local",
  knowledgeBaseId: "kb",
};
const remote = {
  backend: "custom",
  connectionId: "connection",
  knowledgeBaseId: "kb",
};
const root = `${origin}/api/v1/knowledge/libraries/kb`;
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

it.each([remote, { ...remote, backend: "weknora" }])(
  "远端维护聊天不支持时明确拒绝且不回退本机 $backend",
  async (ref) => {
    await expect(
      knowledgeAdapter(ref).sendMaintenanceMessage({
        path: { libraryId: "kb" },
        body: {
          requestId: "maintenance",
          pageId: "one",
          expectedVersion: 3,
          text: "核查来源",
          runtimeId: "codex",
          permissionLevel: "default",
        },
      }),
    ).rejects.toMatchObject({ status: 501 });
  },
);

it("本机维护聊天固定库身份并传递普通执行接纳结果", async () => {
  const bodies: unknown[] = [];
  server.use(
    http.post(`${root}/maintenance-chats`, async ({ request }) => {
      bodies.push(await request.json());
      return HttpResponse.json({
        chat: { id: "chat" },
        execution: { id: "execution" },
      });
    }),
  );
  const result = await knowledgeAdapter(local).sendMaintenanceMessage({
    path: { libraryId: "other" },
    body: {
      requestId: "maintenance",
      pageId: "one",
      expectedVersion: 3,
      text: "核查来源",
      runtimeId: "codex",
      permissionLevel: "default",
    },
  });
  expect(result.data).toMatchObject({
    chat: { id: "chat" },
    execution: { id: "execution" },
  });
  expect(bodies).toEqual([
    {
      requestId: "maintenance",
      pageId: "one",
      expectedVersion: 3,
      text: "核查来源",
      runtimeId: "codex",
      permissionLevel: "default",
    },
  ]);
});
