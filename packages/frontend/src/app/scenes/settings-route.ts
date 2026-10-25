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
  return `/settings/integration-im${params.size ? `?${params}` : ""}`;
}

export function settingsSectionFromPath(
  pathname: string,
): SettingsSection | undefined {
  if (/^\/im(?:\/[^/]+)?$/.test(pathname)) return "integration-im";
  const match = /^\/settings(?:\/([^/]+))?$/.exec(pathname);
  return match ? settingsSectionOf(match[1]) : undefined;
}
