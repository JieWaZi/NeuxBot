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
  "远端Wiki维护不支持时明确拒绝且不回退本机 $backend",
  async (ref) => {
    await expect(
      knowledgeAdapter(ref).maintainWiki({
        path: { libraryId: "kb" },
        body: {
          requestId: "maintenance",
          message: "核查来源",
          runtimeId: "codex",
        },
      }),
    ).rejects.toMatchObject({ status: 501 });
  },
);

it("本机Wiki维护固定库身份并返回独立知识任务回执", async () => {
  const bodies: unknown[] = [];
  server.use(
    http.post(`${root}/maintain`, async ({ request }) => {
      bodies.push(await request.json());
      return HttpResponse.json({
        receipt: { jobId: "wiki-job", state: "accepted" },
      });
    }),
  );
  const result = await knowledgeAdapter(local).maintainWiki({
    path: { libraryId: "other" },
    body: {
      requestId: "maintenance",
      message: "核查来源",
      runtimeId: "codex",
    },
  });
  expect(result.data).toMatchObject({
    receipt: { jobId: "wiki-job", state: "accepted" },
  });
  expect(bodies).toEqual([
    {
      requestId: "maintenance",
      message: "核查来源",
      runtimeId: "codex",
    },
  ]);
});
