import { afterEach, expect, it, vi } from "vitest";
import {
  readOrganizePreferences,
  saveOrganizePreferences,
} from "./knowledge-organize-preferences";

const ref = { backend: "local", connectionId: "local", knowledgeBaseId: "kb" };
afterEach(() => {
  vi.restoreAllMocks();
});

it("按完整知识库身份隔离选择，局部修改保留其他偏好", () => {
  saveOrganizePreferences(ref, {
    selection: {
      runtimeId: "codex",
      accountId: "system",
      model: "chosen",
      reasoning: "high",
    },
  });
  saveOrganizePreferences(ref, { requirements: "保留限制" });
  expect(readOrganizePreferences(ref)).toEqual({
    selection: {
      runtimeId: "codex",
      accountId: "system",
      model: "chosen",
      reasoning: "high",
    },
    requirements: "保留限制",
  });
  expect(readOrganizePreferences({ ...ref, connectionId: "other" })).toEqual(
    {},
  );
  expect(readOrganizePreferences({ ...ref, knowledgeBaseId: "other" })).toEqual(
    {},
  );
});

it("前端重新启动后丢弃临时值，不向浏览器存储写入", async () => {
  const write = vi.spyOn(Storage.prototype, "setItem");
  saveOrganizePreferences(ref, { requirements: "临时要求" });
  expect(write).not.toHaveBeenCalled();
  vi.resetModules();
  const restarted = await import("./knowledge-organize-preferences");
  expect(restarted.readOrganizePreferences(ref)).toEqual({});
});
