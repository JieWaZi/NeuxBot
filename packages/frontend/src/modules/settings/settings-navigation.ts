import { Bot, Settings2, Plug, MessageCircle } from "lucide-react";

export type SettingsSection =
  "general" | "agents" | "integration-api" | "integration-im";

export const settingsNavigation = [
  { key: "general", labelKey: "sections.general", icon: Settings2 },
  { key: "agents", labelKey: "sections.runtimeEnvironments", icon: Bot },
  {
    key: "integration-im",
    labelKey: "sections.integrationIM",
    icon: MessageCircle,
  },
  { key: "integration-api", labelKey: "sections.integrationAPI", icon: Plug },
] as const satisfies ReadonlyArray<{
  key: SettingsSection;
  labelKey: string;
  icon: typeof Settings2;
}>;

export function settingsSectionOf(value?: string): SettingsSection {
  if (value === "integration-mcp") return "integration-api";
  return value === "agents" ||
    value === "integration-api" ||
    value === "integration-im"
    ? value
    : "general";
}
