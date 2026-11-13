import { expect, it } from "vitest";
import { resolveNewChatExecution } from "./new-chat-execution";

it("独立账号缺少模型目录时仍可从完全访问改为默认权限", () => {
  const params: Parameters<typeof resolveNewChatExecution>[0] = {
    runtime: {
      id: "claude",
      defaultAccountId: "system",
      models: [{ id: "shared-model", permissionLevels: ["default"] }],
    },
    accountId: "managed",
    model: "shared-model",
    catalog: [],
    configuredPermission: "auto",
  };
  const initial = resolveNewChatExecution(params);
  expect(initial.permission).toBe("full-access");
  expect(initial.permissionOptions.has("default")).toBe(true);
  expect(
    resolveNewChatExecution({ ...params, permission: "default" }).permission,
  ).toBe("default");
});

it("伙伴推理只属于其原模型，明确选择默认推理时保留空值", () => {
  const params = {
    accountId: "system",
    configuredPermission: "default",
    partnerDefault: { model: "original", reasoning: "low" },
  } as const;
  expect(
    resolveNewChatExecution({ ...params, catalog: [], model: "original" })
      .reasoning,
  ).toBe("low");
  expect(
    resolveNewChatExecution({
      ...params,
      catalog: [],
      model: "original",
      reasoning: "",
    }).reasoning,
  ).toBe("");
  expect(
    resolveNewChatExecution({ ...params, catalog: [], model: "other" })
      .reasoning,
  ).toBe("");
});

it("按当前模型的已知权限回退并保留用户明确的选择", () => {
  const params: Parameters<typeof resolveNewChatExecution>[0] = {
    runtime: { id: "claude" },
    accountId: "system",
    model: "selected",
    catalog: [{ id: "selected", permissionLevels: ["default"] }],
    configuredPermission: "auto",
  };
  expect(resolveNewChatExecution(params).permission).toBe("default");
  const explicit = resolveNewChatExecution({
    ...params,
    permission: "full-access",
  });
  expect(explicit.permission).toBe("full-access");
  expect(explicit.permissionUnsupported).toBe(true);
});
