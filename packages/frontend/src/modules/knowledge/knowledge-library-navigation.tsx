import { History, Layers3, Settings2, SlidersHorizontal } from "lucide-react";
import {
  NavigationItem,
  NavigationPanel,
  NavigationSection,
} from "@/shared/ui/navigation-panel";
import { useCopy } from "./use-copy";

export type KnowledgeLibrarySection =
  "basic" | "models" | "chunking" | "activity";

/** 加载和编辑共用导航，保持分区名称、图标与布局一致。 */
export function KnowledgeLibraryNavigation({
  mode,
  section = "basic",
  disabled = false,
  onSectionChange,
}: {
  mode: "create" | "edit";
  section?: KnowledgeLibrarySection;
  disabled?: boolean;
  onSectionChange?: (section: KnowledgeLibrarySection) => void;
}) {
  const t = useCopy();
  const sections = [
    { key: "basic" as const, label: t("basicSettings"), icon: Settings2 },
    {
      key: "models" as const,
      label: t("modelSettings"),
      icon: SlidersHorizontal,
    },
    { key: "chunking" as const, label: t("chunkingSection"), icon: Layers3 },
    ...(mode === "edit"
      ? [
          {
            key: "activity" as const,
            label: t("activitySection"),
            icon: History,
          },
        ]
      : []),
  ];
  return (
    <aside className="bg-surface-chrome border-border-subtle w-44 shrink-0 overflow-y-auto border-r">
      <NavigationPanel
        aria-label={t("knowledgeSettingsNavigation")}
        className="bg-transparent"
      >
        <NavigationSection>
          {sections.map(({ key, label, icon: Icon }) => (
            <NavigationItem
              key={key}
              label={label}
              icon={<Icon />}
              active={section === key}
              disabled={disabled}
              onClick={() => onSectionChange?.(key)}
            />
          ))}
        </NavigationSection>
      </NavigationPanel>
    </aside>
  );
}
