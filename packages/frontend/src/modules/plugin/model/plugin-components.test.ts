import { describe, it, expect } from "vitest";
import { pluginComponents } from "./plugin-components";
import type { PluginDetail } from "./plugin-view";

const resources: PluginDetail[] = [
  {
    plugin: { id: "plugin", name: "办公室" },
    ready: false,
    installations: [
      { wakerId: "w1", versionId: "old", enabled: false },
      { wakerId: "w2", versionId: "new", enabled: true },
    ],
    versions: [
      {
        id: "new",
        package: {
          skills: [
            { key: "notes", name: "新笔记", description: "会议记录" },
            { key: "calendar", name: "日程" },
          ],
          servers: [{ key: "calendar-mcp" }],
        },
      },
      {
        id: "old",
        package: {
          skills: [{ key: "notes", name: "旧笔记" }],
          servers: [{ key: "notes-mcp" }],
        },
      },
    ],
  },
];
describe("pluginComponents", () => {
  it("uses the installed version for a partner and retains disabled or unauthorized capabilities", () => {
    const result = pluginComponents(resources, "skills", "", "w1");
    expect(result.map((item) => [item.name, item.versionId])).toEqual([
      ["旧笔记", "old"],
    ]);
    expect(pluginComponents(resources, "connectors", "", "w1")[0]?.name).toBe(
      "notes-mcp",
    );
    expect(pluginComponents(resources, "skills", "", "uninstalled")).toEqual(
      [],
    );
  });
  it("counts only installations whose saved version contains the component", () => {
    const result = pluginComponents(resources, "skills", "");
    expect(
      result.map((item) => [item.name, item.installations.length]),
    ).toEqual([
      ["新笔记", 2],
      ["日程", 1],
    ]);
  });
  it("searches component content and the owning plugin name", () => {
    expect(
      pluginComponents(resources, "skills", "会议").map((item) => item.name),
    ).toEqual(["新笔记"]);
    expect(pluginComponents(resources, "skills", "办公室")).toHaveLength(2);
    expect(pluginComponents(resources, "skills", "missing")).toEqual([]);
  });
});

it("uses the installed snapshot for global components after a newer version has only been prepared", () => {
  const result = pluginComponents(
    [
      {
        plugin: { id: "p" },
        resolvedVersionId: "old",
        installations: [{ wakerId: "w1", versionId: "old", enabled: false }],
        versions: [
          {
            id: "prepared",
            package: { skills: [{ key: "new", name: "未安装能力" }] },
          },
          {
            id: "old",
            package: { skills: [{ key: "old", name: "实际安装能力" }] },
          },
        ],
      },
    ],
    "skills",
    "",
  );
  expect(result.map((item) => [item.name, item.versionId])).toEqual([
    ["实际安装能力", "old"],
  ]);
});
