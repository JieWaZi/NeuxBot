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

it("IM 与 API 并列在发布集成分组，MCP 归入 API", () => {
  expect(settingsSectionOf("integration-im")).toBe("integration-im");
  expect(settingsSectionOf("integration-mcp")).toBe("integration-api");
  expect(
    settingsNavigationGroups
      .find((group) => group.titleKey === "groups.integration")
      ?.items.map((item) => item.key),
  ).toEqual(["integration-im", "integration-api"]);
  expect(settingsSectionOf("unknown")).toBe("general");
});
