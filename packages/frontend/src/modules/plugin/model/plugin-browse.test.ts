import { expect, it } from "vitest";
import { i18n } from "@/shared/i18n";
import {
  PLUGIN_CATEGORY_LABELS,
  pluginBrowseCategory,
  pluginBrowseGroups,
  pluginBrowseHref,
  type PluginBrowseEntry,
} from "./plugin-browse";

it("opens a local uninstalled plugin while keeping prepared catalog packages on their catalog identity", () => {
  expect(
    pluginBrowseHref({
      pluginId: "local-plugin",
      sourceKey: "local-skills:local-plugin",
      installedCount: 0,
    }),
  ).toBe("/resources/plugins/local-plugin");
  expect(
    pluginBrowseHref({
      pluginId: "prepared-plugin",
      catalogKey: "office",
      installedCount: 0,
    }),
  ).toBe("/resources/plugins/catalog/office");
  expect(
    pluginBrowseHref({
      pluginId: "installed-plugin",
      catalogKey: "office",
      installedCount: 1,
    }),
  ).toBe("/resources/plugins/installed-plugin");
});
it("groups the three published packages from their actual free-string metadata", () => {
  const entries: PluginBrowseEntry[] = [
    {
      sourceKey: "catalog:github",
      name: "GitHub",
      category: "Developer Tools",
    },
    { sourceKey: "catalog:feishu", name: "飞书", category: "Productivity" },
    { sourceKey: "catalog:wecom", name: "企业微信", category: "Productivity" },
  ];
  const groups = pluginBrowseGroups(entries);
  expect(
    groups.map((group) => [
      group.category,
      group.entries.map((entry) => entry.name),
    ]),
  ).toEqual([
    ["coding", ["GitHub"]],
    ["productivity", ["飞书", "企业微信"]],
  ]);
  const t = i18n.getFixedT("zh-CN", "plugin");
  expect(
    groups.map((group) => t(PLUGIN_CATEGORY_LABELS[group.category])),
  ).toEqual(["编程", "办公"]);
});
it("normalizes only known host keys and explicit labels without guessing from a plugin name", () => {
  expect(pluginBrowseCategory("  DEVELOPER TOOLS ")).toBe("coding");
  expect(pluginBrowseCategory(" Data & Analytics ")).toBe("data-analytics");
  expect(pluginBrowseCategory(" DATA-ANALYTICS ")).toBe("data-analytics");
  expect(pluginBrowseCategory("Productivity")).toBe("productivity");
  for (const value of ["constructor", "toString", "__proto__"])
    expect(pluginBrowseCategory(value)).toBe("other");
  expect(pluginBrowseCategory(undefined)).toBe("other");
  expect(pluginBrowseCategory("Unknown custom category")).toBe("other");
  expect(
    pluginBrowseGroups([
      { name: "Developer Tools", category: "Unknown custom category" },
    ])[0]?.category,
  ).toBe("other");
});
