import { mainConversationScene } from "./main-conversation-route";
import { resolveAppNavigation } from "../layout/app-navigation-model";
import type {
  AppSceneBackground,
  AppSceneTab,
} from "@/shared/workspace/app-scene-model";
import {
  integrationSettingsHref,
  legacyIMSettingsHref,
} from "./settings-route";

export type RouteSceneLocation = {
  key?: string;
  pathname: string;
  search: string;
  state?: unknown;
};

export type RouteSceneLabels = {
  knowledge?: string;
  resources: string;
  workBoard?: string;
  usage?: string;
  im?: string;
  automaticTask?: string;
  workflow?: string;
  chat: string;
  mainConversation?: string;
  detail: string;
  newChat: string;
  newWaker?: string;
  page: string;
  partner: string;
  settings: string;
  team: string;
  teamChat: string;
};

const wakerResourceSections = new Set([
  "resources",
  "plugins",
  "connectors",
  "memory",
  "overview",
  "projects",
  "skills",
]);

function routeTab(
  id: string,
  label: string,
  location: RouteSceneLocation,
  background: AppSceneBackground,
): Omit<AppSceneTab, "lastUsed"> {
  return {
    background,
    closeable: true,
    href: `${location.pathname}${location.search}`,
    id,
    kind: "route",
    label,
    mountKey: id,
    navigationState: location.state,
  };
}

export function routeSceneForLocation(
  location: RouteSceneLocation,
  labels: RouteSceneLabels,
): Omit<AppSceneTab, "lastUsed"> {
  const { pathname, search } = location;
  const navigation = resolveAppNavigation(location);
  if (navigation.moduleId === "settings") {
    const href =
      legacyIMSettingsHref(pathname, search) ||
      integrationSettingsHref(pathname, search) ||
      `${pathname}${search}`;
    const [settingsPath = "", settingsSearch = ""] = href.split("?");
    const params = new URLSearchParams(settingsSearch);
    // 临时详情仅属于当前打开动作，切回设置标签时不恢复已离开的弹窗。
    params.delete("connection");
    params.delete("binding");
    params.delete("chat");
    return {
      ...settingsSceneTab(labels.settings),
      navigationState: location.state,
      href: `${settingsPath}${params.size ? `?${params}` : ""}`,
    };
  }
  const knowledge = /^\/knowledge(?:\/([^/]+))?$/.exec(pathname);
  if (navigation.moduleId === "knowledge" && knowledge) {
    const params = new URLSearchParams(search);
    const id =
      knowledge[1] ||
      (params.get("view") !== "library" ? params.get("library") : "");
    if (!id)
      return routeTab(
        "knowledge",
        labels.knowledge || labels.page,
        location,
        "suspend",
      );
    const identity = [
      params.get("backend") || "local",
      params.get("connection") || "local",
      id,
    ]
      .map(encodeURIComponent)
      .join(":");
    return routeTab(
      `knowledge:${identity}`,
      params.get("title") || labels.knowledge || labels.page,
      location,
      "suspend",
    );
  }
  if (navigation.moduleId === "usage")
    return routeTab("usage", labels.usage || labels.page, location, "suspend");
  if (navigation.moduleId === "work-board")
    return routeTab(
      "work-board",
      labels.workBoard || labels.page,
      location,
      "preserve",
    );
  const workflow = /^\/workflows\/([^/]+)(?:\/runs\/([^/]+))?$/.exec(pathname);
  if (workflow)
    return routeTab(
      workflow[1] === "new"
        ? `workflow-draft:${new URLSearchParams(search).get("draftId") || "default"}`
        : `workflow:${workflow[1]}`,
      labels.workflow || labels.page,
      location,
      "preserve",
    );
  if (navigation.moduleId === "resources")
    return routeTab("resources", labels.resources, location, "suspend");
  const globalAutomatic = /^\/automatic-tasks(?:\/([^/]+))?$/.exec(pathname);
  if (navigation.moduleId === "automatic-tasks" && globalAutomatic)
    return routeTab(
      "automatic-tasks",
      labels.automaticTask || labels.page,
      location,
      "suspend",
    );
  const automatic = /^\/wakers\/([^/]+)\/automatic-tasks(?:\/([^/]+))?$/.exec(
    pathname,
  );
  if (automatic?.[1]) {
    return routeTab(
      "automatic-tasks",
      labels.automaticTask || labels.page,
      location,
      "suspend",
    );
  }
  const params = new URLSearchParams(search);
  const main = mainConversationScene(
    location,
    labels.mainConversation || labels.chat,
  );
  if (main) return main;
  const chat = /^\/chats\/([^/]+)$/.exec(pathname);
  if (chat?.[1])
    return routeTab(`chat:${chat[1]}`, labels.chat, location, "preserve");

  if (
    pathname === "/" ||
    pathname === "/chat" ||
    pathname === "/wakers" ||
    pathname === "/teams"
  ) {
    const draftId = params.get("draftId");
    const wakerId = params.get("wakerId") || "default";
    return routeTab(
      draftId ? `chat-draft:${draftId}` : `chat-entry:${wakerId}`,
      labels.newChat,
      location,
      "preserve",
    );
  }

  const team = /^\/teams\/([^/]+)(?:\/threads\/([^/]+)|\/chats)?$/.exec(
    pathname,
  );
  if (team?.[1]) {
    if (pathname.endsWith("/chats")) {
      return routeTab(
        `team:${team[1]}:chats`,
        labels.teamChat,
        location,
        "suspend",
      );
    }
    if (team[2]) {
      return routeTab(
        `team:${team[1]}:thread:${team[2]}`,
        labels.teamChat,
        location,
        "preserve",
      );
    }
    // 草稿使用独立身份，首发创建 Thread 后才能像伙伴草稿一样原地替换。
    const draftId = params.get("draftId") || "default";
    return routeTab(
      `team-draft:${team[1]}:${draftId}`,
      labels.teamChat,
      location,
      "preserve",
    );
  }

  if (pathname === "/wakers/new")
    return routeTab(
      "waker-create",
      labels.newWaker ?? labels.partner,
      location,
      "suspend",
    );

  const waker = /^\/wakers\/([^/]+)(?:\/([^/]+))?/.exec(pathname);
  const wakerId = waker?.[1];
  if (wakerId) {
    const section = waker[2] || "overview";
    const resource = wakerResourceSections.has(section);
    return routeTab(
      resource ? `waker:${wakerId}:resources` : `waker:${wakerId}:${section}`,
      resource ? labels.detail : labels.partner,
      location,
      "suspend",
    );
  }

  return routeTab(`route:${pathname}`, labels.page, location, "preserve");
}

export function settingsSceneTab(label: string): Omit<AppSceneTab, "lastUsed"> {
  return {
    closeable: true,
    id: "settings",
    kind: "settings",
    label,
    mountKey: "settings",
  };
}
