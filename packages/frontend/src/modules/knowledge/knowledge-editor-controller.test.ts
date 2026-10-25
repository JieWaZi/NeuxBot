import { expect, it } from "vitest";
import { ApiError } from "@/shared/api/errors";
import { createKnowledgeEditorController } from "./knowledge-editor-controller";
import { emptyLibrary } from "./knowledge-model";

it("成功保存保留服务器返回的身份和版本，下一次提交以新版本为基线", async () => {
  const library = emptyLibrary("kb", "旧名称", "general");
  library.revision = 2;
  const controller = createKnowledgeEditorController(
    library,
    { runtimeId: "codex", model: "old" },
    3,
  );
  const desired = {
    ...controller.getSnapshot().draft,
    library: { ...library, name: "新名称" },
    execution: { runtimeId: "codex", model: "new" },
  };
  await controller.submit(desired, async (_command, current) => {
    current.acknowledgeLibrary({ ...desired.library, revision: 8 });
    current.acknowledgeExecution(desired.execution, 9);
  });
  expect(controller.getSnapshot().saved.library.revision).toBe(8);
  expect(controller.getSnapshot().draft.library.revision).toBe(8);
  expect(controller.getSnapshot().executionVersion).toBe(9);
  await controller.submit(controller.getSnapshot().draft, async (command) => {
    expect(command.libraryVersion).toBe(8);
    expect(command.executionVersion).toBe(9);
  });
});

it("新建已有权威库身份后即使后续确定4xx，也保留原命令而不换身份再建库", async () => {
  const controller = createKnowledgeEditorController();
  const draft = {
    ...controller.getSnapshot().draft,
    library: emptyLibrary("", "部分创建库", "general"),
    execution: { runtimeId: "codex", model: "model" },
  };
  let original = "";
  await controller.submit(draft, async (command, current) => {
    original = command.coreRequestId;
    current.acknowledgeLibrary({
      ...draft.library,
      id: "real-kb",
      revision: 1,
    });
    throw new ApiError("后续配置冲突", "CONFLICT", 409);
  });
  expect(controller.getSnapshot().command?.coreRequestId).toBe(original);
  controller.edit({
    ...draft,
    library: { ...draft.library, name: "不应另建" },
  });
  await controller.submit(controller.getSnapshot().draft, async (command) => {
    expect(command.coreRequestId).toBe(original);
    expect(command.draft.library.name).toBe("部分创建库");
  });
  expect(controller.getSnapshot().saved.library.id).toBe("real-kb");
});

it("Embedding确认后清理已提交密钥，后续确定拒绝重试不重写已成功配置", async () => {
  const controller = createKnowledgeEditorController(
    emptyLibrary("kb", "库", "general"),
  );
  const desired = {
    ...controller.getSnapshot().draft,
    embedding: {
      baseUrl: "https://embedding.example/v1",
      model: "own-embedding",
      apiKey: "pending-private-key",
      hasApiKey: false,
    },
  };
  await controller.submit(desired, async (command, current) => {
    current.acknowledgeEmbedding(
      { ...desired.embedding, hasApiKey: true },
      1,
      3,
      command.embeddingRequestId,
    );
    expect(current.getSnapshot().draft.embedding.apiKey).toBe("");
    expect(current.getSnapshot().command?.draft.embedding.apiKey).toBe("");
    throw new ApiError("执行配置被拒绝", "INVALID", 400);
  });
  expect(controller.getSnapshot().command).toBeUndefined();
  expect(controller.getSnapshot().draft.embedding).toEqual({
    ...desired.embedding,
    apiKey: "",
    hasApiKey: true,
  });
  await controller.submit(
    controller.getSnapshot().draft,
    async (command, current) => {
      const saved = current.getSnapshot().saved;
      const value = command.draft.embedding;
      const embeddingChanged =
        value.baseUrl !== saved.embedding.baseUrl ||
        value.model !== saved.embedding.model ||
        Boolean(value.apiKey);
      expect(embeddingChanged).toBe(false);
      expect(command.libraryVersion).toBe(3);
    },
  );
});

it("Embedding尚未确认时保留密钥与原请求身份以核验未知写入", async () => {
  const controller = createKnowledgeEditorController(
    emptyLibrary("kb", "库", "general"),
  );
  const desired = {
    ...controller.getSnapshot().draft,
    embedding: {
      baseUrl: "https://embedding.example/v1",
      model: "own-embedding",
      apiKey: "unconfirmed-private-key",
      hasApiKey: false,
    },
  };
  let requestId = "";
  await controller.submit(desired, async (command) => {
    requestId = command.embeddingRequestId;
    throw new ApiError("回执未知", "NETWORK", 500);
  });
  expect(controller.getSnapshot().draft.embedding.apiKey).toBe(
    "unconfirmed-private-key",
  );
  expect(controller.getSnapshot().command?.embeddingRequestId).toBe(requestId);
  expect(controller.getSnapshot().command?.draft.embedding.apiKey).toBe(
    "unconfirmed-private-key",
  );
});
