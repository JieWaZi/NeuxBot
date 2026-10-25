import { beforeEach, describe, expect, it } from "vitest";

import {
  clampContextSidebarWidth,
  collapsedPartnerIds,
  forgetTeamThread,
  moveItem,
  readPartnerNavigationPreferences,
  rememberTeamThread,
  reconcilePartnerOrder,
  type PartnerNavigationPreferences,
} from "./partner-navigation-preferences";

const preferences: PartnerNavigationPreferences = {
  collapsedGroups: {},
  contextSidebarWidth: 240,
  currentPartnerId: "",
  currentRailKey: "",
  entityMode: "waker",
  globalSidebarCollapsed: false,
  lastTeamThreads: {},
  railExpanded: false,
  railOrder: [],
};

describe("partner navigation preferences", () => {
  beforeEach(() => window.localStorage.clear());

  it("保留有效手动顺序并把新伙伴追加到末尾", () => {
    expect(
      reconcilePartnerOrder(
        ["waker-a", "waker-b", "waker-c"],
        ["deleted", "waker-b", "waker-a"],
      ),
    ).toEqual(["waker-b", "waker-a", "waker-c"]);
  });

  it("收起时用第五个位置展示顺序之外的当前伙伴", () => {
    expect(
      collapsedPartnerIds(["a", "b", "c", "d", "e", "f", "g"], "g"),
    ).toEqual(["a", "b", "c", "d", "g"]);
  });

  it("支持拖拽目标稳定排序", () => {
    expect(moveItem(["a", "b", "c"], "c", "a")).toEqual(["c", "a", "b"]);
  });

  it("把上下文侧栏宽度限制在可用范围", () => {
    expect(clampContextSidebarWidth(180)).toBe(216);
    expect(clampContextSidebarWidth(284.6)).toBe(285);
    expect(clampContextSidebarWidth(520)).toBe(360);
  });

  it("按团队记住并清理上次会话", () => {
    const remembered = rememberTeamThread(preferences, "team-a", "thread-a");
    expect(remembered.lastTeamThreads).toEqual({ "team-a": "thread-a" });
    expect(forgetTeamThread(remembered, "team-a", "another-thread")).toBe(
      remembered,
    );
    expect(
      forgetTeamThread(remembered, "team-a", "thread-a").lastTeamThreads,
    ).toEqual({});
  });

  it("应用重启后从本地偏好恢复各团队最后会话并丢弃无效值", () => {
    window.localStorage.setItem(
      "ally.partner-navigation",
      JSON.stringify({
        ...preferences,
        lastTeamThreads: {
          "team-a": "thread-a",
          "team-empty": "",
          "team-invalid": 42,
        },
      }),
    );

    expect(readPartnerNavigationPreferences().lastTeamThreads).toEqual({
      "team-a": "thread-a",
    });
  });

  it("恢复双栏偏好时约束上下文宽度并兼容旧折叠字段", () => {
    window.localStorage.setItem(
      "ally.partner-navigation",
      JSON.stringify({
        contextSidebarWidth: 999,
        entityMode: "team",
        sidebarCollapsed: true,
      }),
    );

    expect(readPartnerNavigationPreferences()).toMatchObject({
      contextSidebarWidth: 360,
      entityMode: "team",
      globalSidebarCollapsed: true,
    });
  });
});
