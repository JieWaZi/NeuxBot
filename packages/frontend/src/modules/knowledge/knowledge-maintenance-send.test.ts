import { expect, it } from "vitest";
import { ApiError } from "@/shared/api/errors";
import { createWikiMaintenanceSender } from "./knowledge-maintenance-send";

it("未知接纳结果沿同一请求重试且固定打开时的库、页和版本，接纳后续聊使用原Chat", async () => {
  const calls: unknown[] = [];
  const resource = {
    backend: "local",
    connectionId: "local",
    knowledgeBaseId: "kb",
  };
  let fail = true;
  const sender = createWikiMaintenanceSender({
    resource,
    pageId: "one",
    expectedVersion: 3,
    perform: async (options) => {
      calls.push(options);
      if (fail) throw new ApiError("timeout", "NETWORK", 504);
      return { data: { chat: { id: "chat" }, execution: { id: "execution" } } };
    },
  });
  resource.knowledgeBaseId = "other";
  const selection = {
    runtimeId: "codex",
    accountId: "account",
    model: "model",
    reasoning: "high",
    permissionLevel: "default",
  };
  await expect(sender.send("核查问题", [], selection)).rejects.toMatchObject({
    status: 504,
  });
  fail = false;
  const accepted = await sender.send("核查问题", [], selection);
  expect(calls[0]).toEqual(calls[1]);
  expect(calls[0]).toMatchObject({
    path: { libraryId: "kb" },
    body: {
      pageId: "one",
      expectedVersion: 3,
      text: "核查问题",
    },
  });
  expect(accepted.chat?.id).toBe("chat");
  await sender.send("继续核查", [], selection);
  expect(calls[2]).toMatchObject({
    body: { chatId: "chat", pageId: "one", text: "继续核查" },
  });
});

it("未确认接纳前不允许更换输入或执行账号，以免生成第二次修订", async () => {
  const calls: unknown[] = [];
  const sender = createWikiMaintenanceSender({
    resource: {
      backend: "local",
      connectionId: "local",
      knowledgeBaseId: "kb",
    },
    pageId: "one",
    expectedVersion: 2,
    perform: async (options) => {
      calls.push(options);
      throw new ApiError("timeout", "NETWORK", 504);
    },
  });
  await expect(
    sender.send("原请求", [], { runtimeId: "codex" }),
  ).rejects.toMatchObject({ status: 504 });
  await expect(
    sender.send("变更输入", [], { runtimeId: "codex" }),
  ).rejects.toMatchObject({ status: 409 });
  expect(calls).toHaveLength(1);
});

it("修订入口只回读已入库来源，携带外部附件时发送前明确拒绝", async () => {
  const calls: unknown[] = [];
  const sender = createWikiMaintenanceSender({
    resource: {
      backend: "local",
      connectionId: "local",
      knowledgeBaseId: "kb",
    },
    pageId: "one",
    expectedVersion: 2,
    perform: async (options) => {
      calls.push(options);
      return { data: { chat: { id: "chat" }, execution: { id: "execution" } } };
    },
  });
  await expect(
    sender.send("核查", ["/outside/file.md"], { runtimeId: "codex" }),
  ).rejects.toMatchObject({ status: 400 });
  expect(calls).toHaveLength(0);
});

it.each([
  { code: "KnowledgeConflict", status: 409 },
  { code: "KnowledgeForbidden", status: 403 },
  { code: "KnowledgeNotFound", status: 404 },
])(
  "未知接纳结果重试遇到 $status 拒绝时仍保留原请求",
  async ({ code, status }) => {
    const calls: unknown[] = [];
    const sender = createWikiMaintenanceSender({
      resource: {
        backend: "local",
        connectionId: "local",
        knowledgeBaseId: "kb",
      },
      pageId: "one",
      expectedVersion: 3,
      perform: async (options) => {
        calls.push(options);
        if (calls.length === 1) throw new ApiError("timeout", "NETWORK", 504);
        if (calls.length === 2) throw new ApiError("明确拒绝", code, status);
        return {
          data: { chat: { id: "chat" }, execution: { id: "execution" } },
        };
      },
    });
    await expect(
      sender.send("核查问题", [], { runtimeId: "codex" }),
    ).rejects.toMatchObject({ status: 504 });
    expect(sender.canRefreshRejectedVersion()).toBe(false);
    await expect(
      sender.send("核查问题", [], { runtimeId: "codex" }),
    ).rejects.toMatchObject({ status });
    expect(sender.canRefreshRejectedVersion()).toBe(false);
    await sender.send("核查问题", [], { runtimeId: "codex" });
    expect(calls[1]).toEqual(calls[0]);
    expect(calls[2]).toEqual(calls[0]);
    expect(sender.canRefreshRejectedVersion()).toBe(false);
  },
);

it.each(["KnowledgeConflict", "40900009501"])(
  "明确首次版本冲突 %s 允许重新打开，直接重试仍保持原版本",
  async (code) => {
    const calls: unknown[] = [];
    const sender = createWikiMaintenanceSender({
      resource: {
        backend: "local",
        connectionId: "local",
        knowledgeBaseId: "kb",
      },
      pageId: "one",
      expectedVersion: 3,
      perform: async (options) => {
        calls.push(options);
        if (calls.length === 1)
          throw new ApiError("version conflict", code, 409);
        return {
          data: { chat: { id: "chat" }, execution: { id: "execution" } },
        };
      },
    });
    expect(sender.canRefreshRejectedVersion()).toBe(false);
    await expect(
      sender.send("核查问题", [], { runtimeId: "codex" }),
    ).rejects.toMatchObject({ status: 409 });
    expect(sender.canRefreshRejectedVersion()).toBe(true);
    await sender.send("核查问题", [], { runtimeId: "codex" });
    expect(calls[0]).toMatchObject({ body: { expectedVersion: 3 } });
    expect(calls[1]).toMatchObject({ body: { expectedVersion: 3 } });
    expect(sender.canRefreshRejectedVersion()).toBe(false);
  },
);
