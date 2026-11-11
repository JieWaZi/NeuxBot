import { describe, expect, it } from "vitest";
import { settingsNavigation, settingsSectionOf } from "./settings-navigation";

describe("API 集成设置导航", () => {
  it("恢复 API 集成路由且不重复入口", () => {
    expect(settingsSectionOf("integration-api")).toBe("integrations");
    expect(
      settingsNavigation.filter((item) => String(item.key) === "integrations"),
    ).toHaveLength(1);
  });
});

it("旧 IM、API 与 MCP 入口统一归属集成", () => {
  expect(settingsSectionOf("integration-im")).toBe("integrations");
  expect(settingsSectionOf("integration-mcp")).toBe("integrations");
  expect(settingsSectionOf("unknown")).toBe("general");
});

it("智能体包含账号管理，不保留独立账号分类或旧路由兼容", () => {
  expect(settingsSectionOf("agents")).toBe("agents");
  expect(settingsNavigation.map((item) => item.key)).toEqual([
    "general",
    "agents",
    "integrations",
  ]);
  expect(settingsSectionOf("ai-accounts")).toBe("general");
  expect(settingsSectionOf("runtime-environments")).toBe("general");
});
