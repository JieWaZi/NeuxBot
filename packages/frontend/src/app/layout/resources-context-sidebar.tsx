import { Cable, Package, Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  NavigationItem,
  NavigationPanel,
  NavigationSection,
} from "@/shared/ui/navigation-panel";
import type { resourceCategoryFromParams } from "../pages/resources-navigation";

type ResourceCategory = ReturnType<typeof resourceCategoryFromParams>;

export function ResourcesContextSidebar({
  category,
  onCategoryChange,
}: {
  readonly category: ResourceCategory;
  readonly onCategoryChange: (
    category: "plugins" | "skills" | "connectors",
  ) => void;
}) {
  const { t } = useTranslation("plugin");
  return (
    <aside className="min-h-0 min-w-0 overflow-y-auto">
      <NavigationPanel aria-label={t("title")} className="bg-transparent">
        <NavigationSection>
          {(
            [
              { id: "plugins", icon: Package },
              { id: "skills", icon: Sparkles },
              { id: "connectors", icon: Cable },
            ] as const
          ).map(({ id, icon: Icon }) => (
            <NavigationItem
              key={id}
              active={category === id}
              icon={<Icon strokeWidth={1.5} />}
              label={t(id)}
              onClick={() => onCategoryChange(id)}
            />
          ))}
        </NavigationSection>
      </NavigationPanel>
    </aside>
  );
}
