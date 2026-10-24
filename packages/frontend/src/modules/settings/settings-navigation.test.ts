import { describe, expect, it } from "vitest";
import {
  settingsNavigation,
  settingsNavigationGroups,
  settingsSectionOf,
} from "./settings-navigation";

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

it("MCP 与 API 并列在发布集成分组，恢复独立设置分类", () => {
  expect(settingsSectionOf("integration-mcp")).toBe("integration-mcp");
  expect(
    settingsNavigationGroups
      .find((group) => group.titleKey === "groups.integration")
      ?.items.map((item) => item.key),
  ).toEqual(["integration-api", "integration-mcp"]);
  expect(settingsSectionOf("unknown")).toBe("general");
});
