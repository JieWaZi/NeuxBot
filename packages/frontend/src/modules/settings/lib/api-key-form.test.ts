import { describe, expect, it } from "vitest";
import { apiKeyFormSchema, toAPIKeyGrants } from "./api-key-form";

const base = {
  name: "集成",
  expiresAt: "",
  knowledgeIDs: ["kb-a"],
  knowledgeCapabilities: ["retrieve"],
  taskIDs: [],
  taskCapabilities: [],
};

describe("集成 Key 显式授权", () => {
  it("拒绝空组、未知能力、通配范围与缺少读取的写入或整理", () => {
    for (const fields of [
      { knowledgeIDs: [] },
      { knowledgeCapabilities: ["full_access"] },
      { knowledgeIDs: ["*"] },
      { knowledgeCapabilities: ["ingest"] },
      { knowledgeCapabilities: ["organize"] },
      { taskIDs: ["task-a"], taskCapabilities: [] },
    ])
      expect(apiKeyFormSchema.safeParse({ ...base, ...fields }).success).toBe(
        false,
      );
  });
  it("独立保存能力乘明确资源，整理不会隐含写入，去重不扩展未来资源", () => {
    const parsed = apiKeyFormSchema.parse({
      ...base,
      knowledgeIDs: ["kb-a", "kb-a", "kb-b"],
      knowledgeCapabilities: ["retrieve", "organize"],
      taskIDs: ["task-a"],
      taskCapabilities: ["automatic_task.results"],
    });
    expect(toAPIKeyGrants(parsed)).toEqual([
      {
        resourceKind: "knowledge",
        resourceIds: ["kb-a", "kb-b"],
        capabilities: ["retrieve", "organize"],
      },
      {
        resourceKind: "automatic_task",
        resourceIds: ["task-a"],
        capabilities: ["automatic_task.results"],
      },
    ]);
  });
  it("过期时间必须在未来，不能接受无效日期", () => {
    expect(
      apiKeyFormSchema.safeParse({ ...base, expiresAt: "2000-01-01T00:00" })
        .success,
    ).toBe(false);
    expect(
      apiKeyFormSchema.safeParse({ ...base, expiresAt: "invalid" }).success,
    ).toBe(false);
  });
});
