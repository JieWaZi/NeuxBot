import type { AppSceneTab } from "@/shared/workspace/app-scene-model";
import {
  legacyIMSettingsHref,
  settingsSectionFromPath,
} from "./settings-route";

export type RouteSceneLocation = {
  pathname: string;
  search: string;
};

export type RouteSceneLabels = {
  knowledge?: string;
  resources: string;
  workBoard?: string;
  im?: string;
  automaticTask?: string;
  workflow?: string;
  chat: string;
  detail: string;
  newChat: string;
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
): Omit<AppSceneTab, "lastUsed"> {
  return {
    closeable: true,
    href: `${location.pathname}${location.search}`,
    id,
    kind: "route",
    label,
    mountKey: id,
  };
}

export function routeSceneForLocation(
  location: RouteSceneLocation,
  labels: RouteSceneLabels,
): Omit<AppSceneTab, "lastUsed"> {
  const { pathname, search } = location;
  if (settingsSectionFromPath(pathname)) {
    const href =
      legacyIMSettingsHref(pathname, search) || `${pathname}${search}`;
    const [settingsPath = "", settingsSearch = ""] = href.split("?");
    const params = new URLSearchParams(settingsSearch);
    // 临时详情仅属于当前打开动作，切回设置标签时不恢复已离开的弹窗。
    params.delete("connection");
    params.delete("binding");
    params.delete("chat");
    return {
      ...settingsSceneTab(labels.settings),
      href: `${settingsPath}${params.size ? `?${params}` : ""}`,
    };
  }
  const knowledge = /^\/knowledge(?:\/([^/]+))?$/.exec(pathname);
  if (knowledge) {
    const params = new URLSearchParams(search);
    const id =
      knowledge[1] ||
      (params.get("view") !== "library" ? params.get("library") : "");
    if (!id)
      return routeTab("knowledge", labels.knowledge || labels.page, location);
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
    );
  }
  if (
    pathname === "/workflows" ||
    pathname === "/resources" ||
    pathname.startsWith("/resources/")
  )
    return routeTab("resources", labels.resources, location);
  if (pathname === "/work-board")
    return routeTab("work-board", labels.workBoard || labels.page, location);
  const workflow = /^\/workflows(?:\/([^/]+)(?:\/runs\/([^/]+))?)?$/.exec(
    pathname,
  );
  if (workflow)
    return routeTab(
      workflow[1] === "new"
        ? `workflow-draft:${new URLSearchParams(search).get("draftId") || "default"}`
        : workflow[1]
          ? `workflow:${workflow[1]}`
          : "workflows",
      labels.workflow || labels.page,
      location,
    );
  const globalAutomatic = /^\/automatic-tasks(?:\/([^/]+))?$/.exec(pathname);
  if (globalAutomatic)
    return routeTab(
      "automatic-tasks",
      labels.automaticTask || labels.page,
      location,
    );
  const automatic = /^\/wakers\/([^/]+)\/automatic-tasks(?:\/([^/]+))?$/.exec(
    pathname,
  );
  if (automatic?.[1]) {
    return routeTab(
      "automatic-tasks",
      labels.automaticTask || labels.page,
      location,
    );
  }
  const params = new URLSearchParams(search);
  const chat = /^\/chats\/([^/]+)$/.exec(pathname);
  if (chat?.[1]) return routeTab(`chat:${chat[1]}`, labels.chat, location);

  if (pathname === "/" || pathname === "/chat") {
    const draftId = params.get("draftId");
    const wakerId = params.get("wakerId") || "default";
    return routeTab(
      draftId ? `chat-draft:${draftId}` : `chat-entry:${wakerId}`,
      labels.newChat,
      location,
    );
  }

  if (pathname === "/wakers") {
    const teamMode = params.get("tab") === "teams";
    return routeTab(
      teamMode ? "management:teams" : "management:wakers",
      teamMode ? labels.team : labels.partner,
      location,
    );
  }

  const team = /^\/teams\/([^/]+)(?:\/threads\/([^/]+)|\/chats)?$/.exec(
    pathname,
  );
  if (team?.[1]) {
    if (pathname.endsWith("/chats")) {
      return routeTab(`team:${team[1]}:chats`, labels.teamChat, location);
    }
    if (team[2]) {
      return routeTab(
        `team:${team[1]}:thread:${team[2]}`,
        labels.teamChat,
        location,
      );
    }
    // 草稿使用独立身份，首发创建 Thread 后才能像伙伴草稿一样原地替换。
    const draftId = params.get("draftId") || "default";
    return routeTab(
      `team-draft:${team[1]}:${draftId}`,
      labels.teamChat,
      location,
    );
  }

  const waker = /^\/wakers\/([^/]+)(?:\/([^/]+))?/.exec(pathname);
  const wakerId = waker?.[1];
  if (wakerId) {
    const section = waker[2] || "overview";
    const resource = wakerResourceSections.has(section);
    return routeTab(
      resource ? `waker:${wakerId}:resources` : `waker:${wakerId}:${section}`,
      resource ? labels.detail : labels.partner,
      location,
    );
  }

  return routeTab(`route:${pathname}`, labels.page, location);
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
