import { Bot, KeyRound, Settings2, Plug, MessageCircle } from "lucide-react";

export type SettingsSection =
  | "general"
  | "ai-accounts"
  | "runtime-environments"
  | "integration-api"
  | "integration-im";

export const settingsNavigationGroups = [
  {
    titleKey: "groups.app",
    items: [{ key: "general", labelKey: "sections.general", icon: Settings2 }],
  },
  {
    titleKey: "groups.ai",
    items: [
      { key: "ai-accounts", labelKey: "sections.accounts", icon: KeyRound },
      {
        key: "runtime-environments",
        labelKey: "sections.runtimeEnvironments",
        icon: Bot,
      },
    ],
  },
  {
    titleKey: "groups.integration",
    items: [
      {
        key: "integration-im",
        labelKey: "sections.integrationIM",
        icon: MessageCircle,
      },
      {
        key: "integration-api",
        labelKey: "sections.integrationAPI",
        icon: Plug,
      },
    ],
  },
] as const satisfies ReadonlyArray<{
  titleKey: string;
  items: ReadonlyArray<{
    key: SettingsSection;
    labelKey: string;
    icon: typeof Settings2;
  }>;
}>;

export const settingsNavigation = settingsNavigationGroups.flatMap(
  (group) =>
    [...group.items] as ReadonlyArray<{
      key: SettingsSection;
      labelKey: string;
      icon: typeof Settings2;
    }>,
);

export function settingsSectionOf(value?: string): SettingsSection {
  if (value === "integration-mcp") return "integration-api";
  return value === "runtime-environments" ||
    value === "ai-accounts" ||
    value === "integration-api" ||
    value === "integration-im"
    ? value
    : "general";
}
