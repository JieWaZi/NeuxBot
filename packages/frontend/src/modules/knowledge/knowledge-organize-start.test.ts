import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { beforeAll, afterAll, expect, it } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { emptyLibrary } from "./knowledge-model";
import {
  createKnowledgeOrganizeIntent,
  createKnowledgeOrganizeStart,
} from "./knowledge-organize-start";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: "http://ally.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterAll(() => server.close());
it.each(["full-access", "auto", "default"])(
  "%s 聊天权限不进入模型请求，重复接纳复用同一知识回执",
  async (permissionLevel) => {
    const requests: Record<string, unknown>[] = [];
    server.use(
      http.post(
        "http://ally.test/api/v1/knowledge/libraries/kb/organize",
        async ({ request }) => {
          requests.push((await request.json()) as Record<string, unknown>);
          return HttpResponse.json({
            receipt: { ...acceptedJobReceipt, requestId: "same-request" },
          });
        },
      ),
    );
    const selection = {
      runtimeId: "codex",
      accountId: "system",
      model: "model",
      reasoning: "high",
      permissionLevel,
    };
    const start = createKnowledgeOrganizeStart({
      library: emptyLibrary("kb", "库", "general"),
      batch: ["source"],
      selection,
      intent: createKnowledgeOrganizeIntent(),
      perform: async (_key, action) => {
        await action("same-request");
      },
      message: "真实用户请求",
    });
    expect(requests).toHaveLength(0);
    const [first, second] = await Promise.all([
      start.execute(),
      start.execute(),
    ]);
    await start.execute();
    expect(requests).toHaveLength(1);
    expect(requests[0]).toMatchObject({
      message: "真实用户请求",
      sourceVersionIds: ["source"],
      runtimeId: "codex",
    });
    expect(requests[0]).not.toHaveProperty("permissionLevel");
    expect(requests[0]).not.toHaveProperty("wakerId");
    expect(requests[0]).toHaveProperty("reasoning", "high");
    expect(first).toEqual(second);
  },
);

it("文档重建携带显式标记和当前原件，复用接纳身份而不退化为普通整理", async () => {
  const requests: Record<string, unknown>[] = [];
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        requests.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({
          receipt: { ...acceptedJobReceipt, requestId: "rebuild-request" },
        });
      },
    ),
  );
  const selection = {
    runtimeId: "codex",
    accountId: "system",
    model: "model",
    reasoning: "",
    permissionLevel: "full-access",
  };
  const start = createKnowledgeOrganizeStart({
    library: emptyLibrary("kb", "库", "general"),
    batch: ["current-version"],
    selection,
    intent: createKnowledgeOrganizeIntent(),
    perform: async (_key, action) => {
      await action("rebuild-request");
    },
    message: "重建",
    rebuild: true,
  });
  await start.execute();
  await start.execute();
  expect(requests).toHaveLength(1);
  expect(requests[0]).toMatchObject({
    rebuild: true,
    sourceVersionIds: ["current-version"],
    requestId: "rebuild-request",
  });
});

it("重建收到无操作回执时不误报接纳成功", async () => {
  server.use(
    http.post("http://ally.test/api/v1/knowledge/libraries/kb/organize", () =>
      HttpResponse.json({ noop: true }),
    ),
  );
  const selection = {
    runtimeId: "codex",
    accountId: "system",
    model: "model",
    reasoning: "",
    permissionLevel: "full-access",
  };
  const start = createKnowledgeOrganizeStart({
    library: emptyLibrary("kb", "库", "general"),
    batch: ["processed-version"],
    selection,
    intent: createKnowledgeOrganizeIntent(),
    perform: async (_key, action) => {
      await action("rebuild-noop");
    },
    message: "重建",
    rebuild: true,
    notAcceptedMessage: "重建尚未接纳",
  });
  await expect(start.execute()).rejects.toThrow("重建尚未接纳");
});

const acceptedJobReceipt = {
  requestId: "accepted-request",
  libraryId: "kb",
  resourceId: "accepted-job",
  jobId: "accepted-job",
  version: 1,
  state: "accepted",
};

function createStart(rebuild = false) {
  const selection = {
    runtimeId: "codex",
    accountId: "system",
    model: "model",
    reasoning: "",
    permissionLevel: "full-access",
  };
  return createKnowledgeOrganizeStart({
    library: emptyLibrary("kb", "库", "general"),
    batch: ["source"],
    selection,
    intent: createKnowledgeOrganizeIntent(),
    perform: async (_key, action) => {
      await action("accepted-request");
    },
    message: "整理当前文档",
    rebuild,
    notAcceptedMessage: "整理尚未接纳",
  });
}

it.each([false, true])(
  "真实任务回执在尚无Execution时接纳成功并复用原回执（重建=%s）",
  async (rebuild) => {
    let posts = 0;
    server.use(
      http.post(
        "http://ally.test/api/v1/knowledge/libraries/kb/organize",
        () => {
          posts++;
          return HttpResponse.json({
            receipt: acceptedJobReceipt,
            noop: false,
          });
        },
      ),
    );
    const start = createStart(rebuild);
    const [first, second] = await Promise.all([
      start.execute(),
      start.execute(),
    ]);
    expect(first.receipt).toEqual(acceptedJobReceipt);
    expect(second).toBe(first);
    expect(start.accepted()).toBe(first);
    expect(await start.execute()).toBe(first);
    expect(posts).toBe(1);
  },
);

it.each([
  {},
  {
    acceptedExecution: { chat: { id: "chat" }, execution: { id: "execution" } },
  },
  { receipt: null },
  { receipt: { ...acceptedJobReceipt, requestId: "other-request" } },
  { receipt: { ...acceptedJobReceipt, libraryId: "other-library" } },
  { receipt: { ...acceptedJobReceipt, jobId: "" } },
  { receipt: { ...acceptedJobReceipt, resourceId: "other-job" } },
  { receipt: { ...acceptedJobReceipt, state: "no-op" } },
  { receipt: { ...acceptedJobReceipt, version: 0 } },
])("缺失或损坏的任务接纳回执不会报告成功：%j", async (response) => {
  server.use(
    http.post("http://ally.test/api/v1/knowledge/libraries/kb/organize", () =>
      HttpResponse.json(response),
    ),
  );
  const start = createStart();
  await expect(start.execute()).rejects.toThrow("整理尚未接纳");
  expect(start.accepted()).toBeUndefined();
});

it("普通整理的无新增回执保留成功语义", async () => {
  server.use(
    http.post("http://ally.test/api/v1/knowledge/libraries/kb/organize", () =>
      HttpResponse.json({ noop: true }),
    ),
  );
  const start = createStart();
  await expect(start.execute()).resolves.toEqual({ noop: true });
});
