import {
  legacyIMSettingsHref,
  integrationSettingsHref,
  settingsSectionFromPath,
} from "../scenes/settings-route";

export type AppModuleId =
  | "partners"
  | "work-board"
  | "usage"
  | "resources"
  | "knowledge"
  | "automatic-tasks"
  | "settings";

export type AppSidebarKind =
  "partners" | "resources" | "knowledge" | "settings" | "none";

export type AppNavigationRoute = {
  readonly moduleId: AppModuleId;
  readonly sidebarKind: AppSidebarKind;
  readonly parentHref: string;
};

export type AppNavigationLocation = {
  readonly pathname: string;
  readonly search: string;
};

const moduleHrefs: Readonly<Record<AppModuleId, string>> = {
  partners: "/chat",
  "work-board": "/work-board",
  usage: "/usage",
  resources: "/resources",
  knowledge: "/knowledge",
  "automatic-tasks": "/automatic-tasks",
  settings: "/settings/general",
};

function route(
  moduleId: AppModuleId,
  sidebarKind: AppSidebarKind,
  parentHref = moduleHrefs[moduleId],
): AppNavigationRoute {
  return { moduleId, sidebarKind, parentHref };
}

/** 模块归属仅描述壳层导航，不参与 Scene 身份或领域资源生命周期。 */
export function resolveAppNavigation({
  pathname,
  search,
}: AppNavigationLocation): AppNavigationRoute {
  const params = new URLSearchParams(search);
  const waker = /^\/wakers\/([^/]+)(?:\/([^/]+))?/.exec(pathname);
  if (waker?.[1]) {
    const root = `/wakers/${waker[1]}`;
    const section = waker[2];
    const pluginComponent =
      /^\/wakers\/[^/]+\/plugins\/([^/]+)\/(?:skills|connectors)\/[^/]+$/.exec(
        pathname,
      );
    const parentHref = pluginComponent?.[1]
      ? `${root}/plugins/${pluginComponent[1]}`
      : section &&
          /^\/wakers\/[^/]+\/[^/]+\/[^/]+/.test(pathname) &&
          ["projects", "skills", "connectors"].includes(section)
        ? `${root}/${section}`
        : root;
    return route("partners", "partners", parentHref);
  }
  const team = /^\/teams\/([^/]+)(?:\/threads\/([^/]+)|\/chats)?$/.exec(
    pathname,
  );
  if (team?.[1])
    return route("partners", "partners", `/teams/${team[1]}/chats`);
  if (
    pathname === "/teams" ||
    (pathname === "/wakers" && params.get("tab") === "teams")
  ) {
    return route("partners", "partners", "/wakers?tab=teams");
  }
  if (
    pathname === "/" ||
    pathname === "/chat" ||
    pathname === "/wakers" ||
    /^\/chats\/[^/]+$/.test(pathname)
  ) {
    return route("partners", "partners");
  }

  const settingsSection = settingsSectionFromPath(pathname);
  if (settingsSection)
    return route("settings", "settings", `/settings/${settingsSection}`);
  if (/^\/knowledge(?:\/[^/]+)?$/.test(pathname))
    return route("knowledge", "knowledge");
  const workflow = /^\/workflows(?:\/([^/]+)(?:\/runs\/([^/]+))?)?$/.exec(
    pathname,
  );
  if (workflow)
    return route(
      "resources",
      workflow[1] ? "none" : "resources",
      workflow[2]
        ? `/workflows/${workflow[1]}`
        : "/resources?category=workflows",
    );
  if (pathname === "/resources" || pathname.startsWith("/resources/")) {
    if (pathname === "/resources" && params.get("category") === "workflows")
      return route("resources", "resources", "/resources?category=workflows");
    const category = /^\/resources\/skills(?:\/|$)/.test(pathname)
      ? "skills"
      : /^\/resources\/connectors(?:\/|$)/.test(pathname)
        ? "connectors"
        : undefined;
    return route(
      "resources",
      "resources",
      category ? `/resources?category=${category}` : moduleHrefs.resources,
    );
  }
  if (pathname === "/work-board") return route("work-board", "none");
  if (pathname === "/usage") return route("usage", "none");
  if (/^\/automatic-tasks(?:\/[^/]+)?$/.test(pathname))
    return route("automatic-tasks", "none");
  // 未知路由由既有 Routes 回到首页；壳层同步使用首页的伙伴导航。
  return route("partners", "partners");
}

export type AppModuleTargets = {
  /** 已验证的最近聊天，或交由原聊天入口查询的当前伙伴。 */
  readonly partnerHref?: string;
  /** 由唯一设置 Scene 或原有设置偏好提供的最近位置。 */
  readonly settingsHref?: string;
};

function localLocation(
  href: string | undefined,
): AppNavigationLocation | undefined {
  if (!href?.startsWith("/") || href.startsWith("//") || /[\\#\r\n]/.test(href))
    return undefined;
  const queryIndex = href.indexOf("?");
  return queryIndex < 0
    ? { pathname: href, search: "" }
    : { pathname: href.slice(0, queryIndex), search: href.slice(queryIndex) };
}

/** 目标存在性属于调用方的既有事实；此处只校验路由家族并保留 URL 条件。 */
export function resolveAppModuleHref(
  moduleId: AppModuleId,
  targets: AppModuleTargets = {},
): string {
  if (moduleId === "partners") {
    const location = localLocation(targets.partnerHref);
    if (
      location &&
      (/^\/chats\/[^/]+$/.test(location.pathname) ||
        (location.pathname === "/chat" &&
          !new URLSearchParams(location.search).has("draftId")) ||
        /^\/teams\/[^/]+\/threads\/[^/]+$/.test(location.pathname))
    ) {
      return `${location.pathname}${location.search}`;
    }
  }
  if (moduleId === "settings") {
    const location = localLocation(targets.settingsHref);
    if (location) {
      const section = settingsSectionFromPath(location.pathname);
      if (section)
        return (
          legacyIMSettingsHref(location.pathname, location.search) ??
          integrationSettingsHref(location.pathname, location.search) ??
          `/settings/${section}${location.search}`
        );
    }
  }
  return moduleHrefs[moduleId];
}
