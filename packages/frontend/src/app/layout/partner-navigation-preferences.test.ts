import { beforeEach, describe, expect, it } from "vitest";

import {
  clampContextSidebarWidth,
  pinnedAppModules,
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

  it("支持拖拽目标稳定排序", () => {
    expect(moveItem(["a", "b", "c"], "c", "a", "before")).toEqual([
      "c",
      "a",
      "b",
    ]);
  });

  it("按可见拖动方向落到目标指定的一侧", () => {
    const stored = ["a", "b", "c", "d"];
    // 分组后的可见顺序是 a/c/b/d，与持久顺序不同。
    expect(moveItem(stored, "c", "b", "after")).toEqual(["a", "b", "c", "d"]);
    expect(moveItem(stored, "b", "c", "before")).toEqual(["a", "b", "c", "d"]);
  });

  it("把上下文侧栏宽度限制在可用范围", () => {
    expect(clampContextSidebarWidth(80)).toBe(216);
    expect(clampContextSidebarWidth(180)).toBe(216);
    expect(clampContextSidebarWidth(284.6)).toBe(285);
    expect(clampContextSidebarWidth(520)).toBe(400);
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
      contextSidebarWidth: 400,
      entityMode: "team",
      globalSidebarCollapsed: false,
    });
  });
});

it("旧偏好缺少宽度时使用280且保留伙伴排序、分组和最近团队", () => {
  window.localStorage.setItem(
    "ally.partner-navigation",
    JSON.stringify({
      railOrder: ["waker:b", "waker:a"],
      collapsedGroups: { "waker:a": ["folder:temporary"] },
      lastTeamThreads: { t: "thread" },
      sidebarCollapsed: true,
    }),
  );
  expect(readPartnerNavigationPreferences()).toMatchObject({
    contextSidebarWidth: 280,
    globalSidebarCollapsed: false,
    railOrder: ["waker:b", "waker:a"],
    collapsedGroups: { "waker:a": ["folder:temporary"] },
    lastTeamThreads: { t: "thread" },
  });
  expect(clampContextSidebarWidth(Number.NaN)).toBe(280);
});

it("忽略旧收起字段，并把过小旧宽度钳制到216，不丢弃实体顺序", () => {
  window.localStorage.setItem(
    "ally.partner-navigation",
    JSON.stringify({
      contextSidebarWidth: 88,
      globalSidebarCollapsed: true,
      sidebarCollapsed: true,
      railOrder: ["waker:b"],
    }),
  );
  expect(readPartnerNavigationPreferences()).toMatchObject({
    contextSidebarWidth: 216,
    globalSidebarCollapsed: false,
    railOrder: ["waker:b"],
  });
});

it("Rail默认顺序与pin选择恢复，未知版本的boolean选择仍保留", () => {
  expect(pinnedAppModules()).toEqual(["usage"]);
  window.localStorage.setItem(
    "ally.partner-navigation",
    JSON.stringify({
      railPins: {
        "work-board": true,
        knowledge: true,
        workflows: false,
        future: true,
        broken: "true",
      },
    }),
  );
  const saved = readPartnerNavigationPreferences();
  expect(saved.railPins).toEqual({
    "work-board": true,
    knowledge: true,
    workflows: false,
    future: true,
  });
  expect(pinnedAppModules(saved.railPins)).toEqual(["usage", "knowledge"]);
});
