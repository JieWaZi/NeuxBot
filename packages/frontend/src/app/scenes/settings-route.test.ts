import { describe, expect, it } from "vitest";

import {
  legacyIMSettingsHref,
  integrationSettingsHref,
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
    expect(legacyIMSettingsHref("/im", "")).toBe(
      "/settings/integrations?tab=im",
    );
    expect(legacyIMSettingsHref("/im/connection-1", "?chat=binding-1")).toBe(
      "/settings/integrations?connection=connection-1&binding=binding-1&tab=im",
    );
    expect(legacyIMSettingsHref("/workflows/one", "")).toBeUndefined();
    expect(legacyIMSettingsHref("/im/bad%", "")).toBe(
      "/settings/integrations?connection=bad%25&tab=im",
    );
    expect(settingsSectionFromPath("/settings/integration-im")).toBe(
      "integrations",
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

it("智能体详情深链保留实际配置身份并复用唯一设置 Scene", () => {
  expect(settingsSectionFromPath("/settings/agents")).toBe("agents");
  const tab = routeSceneForLocation(
    { pathname: "/settings/agents", search: "?agent=codex-profile" },
    labels,
  );
  expect(tab).toMatchObject({
    id: "settings",
    kind: "settings",
    mountKey: "settings",
    href: "/settings/agents?agent=codex-profile",
  });
});

it.each(["api", "mcp"])("旧 %s 服务入口保留参数并直接打开对应详情", (tab) => {
  expect(
    integrationSettingsHref(
      `/settings/integration-${tab}`,
      "?keysPage=3&keep=value",
    ),
  ).toBe(
    `/settings/integrations?keysPage=3&keep=value&tab=${tab}&detail=service`,
  );
});
it("集成详情 Scene 保存平台和服务身份，移除临时授权定位", () => {
  expect(
    routeSceneForLocation(
      {
        pathname: "/settings/integrations",
        search: "?tab=im&platform=feishu&connection=one&binding=chat",
      },
      labels,
    ).href,
  ).toBe("/settings/integrations?tab=im&platform=feishu");
  expect(
    routeSceneForLocation(
      { pathname: "/settings/integrations", search: "?tab=mcp&detail=service" },
      labels,
    ).href,
  ).toBe("/settings/integrations?tab=mcp&detail=service");
});
