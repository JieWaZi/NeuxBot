import { describe, expect, it } from "vitest";

import {
  legacyIMSettingsHref,
  settingsSectionFromPath,
} from "./settings-route";
import {
  routeSceneForLocation,
  type RouteSceneLabels,
} from "./app-route-scene-model";

const labels: RouteSceneLabels = {
  resources: "资源库",
  chat: "聊天",
  detail: "详情",
  newChat: "新聊天",
  page: "页面",
  partner: "伙伴",
  settings: "设置",
  team: "团队",
  teamChat: "团队聊天",
};

describe("IM 设置路由", () => {
  it("旧列表与连接链接转到发布集成，并保留聊天授权定位", () => {
    expect(legacyIMSettingsHref("/im", "")).toBe("/settings/integration-im");
    expect(legacyIMSettingsHref("/im/connection-1", "?chat=binding-1")).toBe(
      "/settings/integration-im?connection=connection-1&binding=binding-1",
    );
    expect(legacyIMSettingsHref("/workflows/one", "")).toBeUndefined();
    expect(legacyIMSettingsHref("/im/bad%", "")).toBe(
      "/settings/integration-im?connection=bad%25",
    );
    expect(settingsSectionFromPath("/settings/integration-im")).toBe(
      "integration-im",
    );
  });

  it("旧链接、不同设置分类共用设置 Scene，标签不恢复临时详情", () => {
    for (const pathname of [
      "/im/one",
      "/settings/integration-im",
      "/settings/general",
    ]) {
      const tab = routeSceneForLocation(
        { pathname, search: "?connection=one&binding=chat-one" },
        labels,
      );
      expect(tab).toMatchObject({
        id: "settings",
        kind: "settings",
        label: "设置",
      });
      expect(tab.href).not.toContain("connection=");
      expect(tab.href).not.toContain("binding=");
    }
  });
});
