import { describe, expect, it } from "vitest";
import { settingsNavigation, settingsSectionOf } from "./settings-navigation";

describe("API 集成设置导航", () => {
  it("恢复 API 集成路由且不重复入口", () => {
    expect(settingsSectionOf("integration-api")).toBe("integration-api");
    expect(
      settingsNavigation.filter(
        (item) => String(item.key) === "integration-api",
      ),
    ).toHaveLength(1);
  });
});

it("IM 与 API 保留独立入口，MCP 归入 API", () => {
  expect(settingsSectionOf("integration-im")).toBe("integration-im");
  expect(settingsSectionOf("integration-mcp")).toBe("integration-api");
  expect(settingsSectionOf("unknown")).toBe("general");
});

it("智能体包含账号管理，不保留独立账号分类或旧路由兼容", () => {
  expect(settingsSectionOf("agents")).toBe("agents");
  expect(settingsNavigation.map((item) => item.key)).toEqual([
    "general",
    "agents",
    "integration-im",
    "integration-api",
  ]);
  expect(settingsSectionOf("ai-accounts")).toBe("general");
  expect(settingsSectionOf("runtime-environments")).toBe("general");
});
