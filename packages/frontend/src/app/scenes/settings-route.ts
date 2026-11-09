import { settingsSectionOf, type SettingsSection } from "@/modules/settings";

/** 旧 IM 深链继续定位连接与聊天授权，统一交给唯一设置 Scene 展示。 */
export function legacyIMSettingsHref(pathname: string, search: string) {
  const match = /^\/im(?:\/([^/]+))?$/.exec(pathname);
  if (!match) return undefined;
  const params = new URLSearchParams(search);
  if (match[1]) {
    let connectionId = match[1];
    try {
      connectionId = decodeURIComponent(connectionId);
    } catch {
      // 保留旧链接中的原始标识，由详情请求按普通无效记录处理。
    }
    params.set("connection", connectionId);
  }
  if (params.has("chat")) {
    params.set("binding", params.get("chat") || "");
    params.delete("chat");
  }
  params.set("tab", "im");
  return `/settings/integrations?${params}`;
}

export function settingsSectionFromPath(
  pathname: string,
): SettingsSection | undefined {
  if (/^\/im(?:\/[^/]+)?$/.test(pathname)) return "integrations";
  const match = /^\/settings(?:\/([^/]+))?$/.exec(pathname);
  return match ? settingsSectionOf(match[1]) : undefined;
}

/** 将旧发布集成路径归一到集成 Scene，保留原查询条件。 */
export function integrationSettingsHref(pathname: string, search: string) {
  const legacy = /^\/settings\/integration-(im|api|mcp)$/.exec(pathname);
  if (!legacy) return undefined;
  const params = new URLSearchParams(search);
  if (!params.has("tab")) params.set("tab", legacy[1] ?? "im");
  if (legacy[1] === "api" || legacy[1] === "mcp")
    params.set("detail", "service");
  return `/settings/integrations?${params}`;
}
