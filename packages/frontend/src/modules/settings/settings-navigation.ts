import { Bot, Settings2, Plug } from "lucide-react";

export type SettingsSection = "general" | "agents" | "integrations";
export type IntegrationTab = "im" | "api" | "mcp";

export const settingsNavigation = [
  { key: "general", labelKey: "sections.general", icon: Settings2 },
  { key: "agents", labelKey: "sections.runtimeEnvironments", icon: Bot },
  { key: "integrations", labelKey: "sections.integrations", icon: Plug },
] as const satisfies ReadonlyArray<{
  key: SettingsSection;
  labelKey: string;
  icon: typeof Settings2;
}>;

export function settingsSectionOf(value?: string): SettingsSection {
  if (
    [
      "integrations",
      "integration-im",
      "integration-api",
      "integration-mcp",
    ].includes(value ?? "")
  )
    return "integrations";
  return value === "agents" ? value : "general";
}

/** 旧入口仍可定位所属详情类型；显式类型优先，参数由原 URL 持有。 */
export function integrationTabOf(
  pathname: string,
  params: URLSearchParams,
): IntegrationTab {
  const tab = params.get("tab");
  if (tab === "im" || tab === "api" || tab === "mcp") return tab;
  return pathname.endsWith("integration-mcp")
    ? "mcp"
    : pathname.endsWith("integration-api")
      ? "api"
      : "im";
}

/** 返回集成总览仅清理详情定位，保留密钥分页等原查询条件。 */
export function integrationOverviewParams(params: URLSearchParams) {
  const next = new URLSearchParams(params);
  for (const key of [
    "tab",
    "platform",
    "detail",
    "connection",
    "binding",
    "chat",
  ])
    next.delete(key);
  return next;
}
