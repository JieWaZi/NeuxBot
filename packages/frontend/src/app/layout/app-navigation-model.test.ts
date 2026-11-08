import { describe, expect, it } from "vitest";

import {
  resolveAppModuleHref,
  resolveAppNavigation,
} from "./app-navigation-model";

function resolve(href: string) {
  const url = new URL(href, "https://neuxbot.invalid");
  return resolveAppNavigation({ pathname: url.pathname, search: url.search });
}

describe("应用模块归属", () => {
  it.each([
    ["/wakers/w1/projects/p1", "partners", "partners", "/wakers/w1/projects"],
    [
      "/wakers/w1/resources?category=workflows",
      "partners",
      "partners",
      "/wakers/w1",
    ],
    ["/wakers/w1/plugins/p1", "partners", "partners", "/wakers/w1"],
    [
      "/wakers/w1/plugins/p1/skills/s1",
      "partners",
      "partners",
      "/wakers/w1/plugins/p1",
    ],
    ["/wakers/w1/automatic-tasks/task1", "partners", "partners", "/wakers/w1"],
    ["/teams/t1/threads/c1", "partners", "partners", "/teams/t1/chats"],
    ["/teams", "partners", "partners", "/wakers?tab=teams"],
    ["/wakers?tab=teams", "partners", "partners", "/wakers?tab=teams"],
    ["/chats/c1?wakerId=w1", "partners", "partners", "/wakers"],
    ["/resources/plugins/p1/skills/s1", "resources", "resources", "/resources"],
    [
      "/resources/plugins/p1/connectors/c1",
      "resources",
      "resources",
      "/resources",
    ],
    [
      "/resources/plugins/catalog/github/connectors/github",
      "resources",
      "resources",
      "/resources",
    ],
    [
      "/resources/plugins/catalog/office/skills/guide",
      "resources",
      "resources",
      "/resources",
    ],
    [
      "/resources/skills/s1",
      "resources",
      "resources",
      "/resources?category=skills",
    ],
    [
      "/resources/connectors/c1",
      "resources",
      "resources",
      "/resources?category=connectors",
    ],
    ["/resources/plugins/p1", "resources", "resources", "/resources"],
    [
      "/resources?category=workflows&search=demo",
      "resources",
      "resources",
      "/resources?category=workflows",
    ],
    ["/workflows", "resources", "resources", "/resources?category=workflows"],
    [
      "/workflows/new?draftId=d1",
      "resources",
      "none",
      "/resources?category=workflows",
    ],
    ["/workflows/f1/runs/r1", "resources", "none", "/workflows/f1"],
    ["/workflows/f1", "resources", "none", "/resources?category=workflows"],
    ["/work-board", "work-board", "none", "/work-board"],
    ["/usage?groupBy=model", "usage", "none", "/usage"],
    [
      "/knowledge/k1?library=k1&view=wiki",
      "knowledge",
      "knowledge",
      "/knowledge",
    ],
    ["/automatic-tasks/task1", "automatic-tasks", "none", "/automatic-tasks"],
    [
      "/settings/integration-mcp?search=demo",
      "settings",
      "settings",
      "/settings/integration-api",
    ],
    ["/settings/unknown", "settings", "settings", "/settings/general"],
    [
      "/im/connection1?chat=binding1",
      "settings",
      "settings",
      "/settings/integration-im",
    ],
    ["/unknown", "partners", "partners", "/wakers"],
    ["/resources-other", "partners", "partners", "/wakers"],
    ["/workflows/f1/unknown", "partners", "partners", "/wakers"],
  ])("%s 由 %s 提供导航", (href, moduleId, sidebarKind, parentHref) => {
    expect(resolve(href)).toEqual({ moduleId, sidebarKind, parentHref });
  });
});

describe("模块入口目标", () => {
  it.each([
    ["partners", "/wakers"],
    ["work-board", "/work-board"],
    ["usage", "/usage"],
    ["resources", "/resources"],
    ["knowledge", "/knowledge"],
    ["automatic-tasks", "/automatic-tasks"],
    ["settings", "/settings/general"],
  ] as const)("%s 首次或已删除目标回实际列表", (moduleId, href) => {
    expect(resolveAppModuleHref(moduleId)).toBe(href);
  });

  it.each([
    "/chats/c1?wakerId=w1&search=demo",
    "/teams/t1/threads/c1?search=demo",
  ])("恢复已有有效聊天 %s 并保留参数", (partnerHref) => {
    expect(resolveAppModuleHref("partners", { partnerHref })).toBe(partnerHref);
  });

  it("旧设置入口恢复有效分区并保留参数", () => {
    expect(
      resolveAppModuleHref("settings", {
        settingsHref: "/settings/integration-mcp?search=demo",
      }),
    ).toBe("/settings/integration-api?search=demo");
    expect(
      resolveAppModuleHref("settings", {
        settingsHref: "/im/conn%20one?chat=binding1&search=demo",
      }),
    ).toBe(
      "/settings/integration-im?search=demo&connection=conn+one&binding=binding1",
    );
  });

  it.each([
    "/resources",
    "/unknown",
    "/wakers/w1/projects/p1",
    "/chat?draftId=d1",
    "https://example.com/chats/c1",
    "//example.com/chats/c1",
    "/\\example.com/chats/c1",
  ])("非法或非聊天伙伴目标 %s 回伙伴列表", (partnerHref) => {
    expect(resolveAppModuleHref("partners", { partnerHref })).toBe("/wakers");
  });

  it.each(["/resources", "/unknown", "https://example.com/settings/general"])(
    "跨模块或非站内设置目标 %s 回默认分区",
    (settingsHref) => {
      expect(resolveAppModuleHref("settings", { settingsHref })).toBe(
        "/settings/general",
      );
    },
  );
});
