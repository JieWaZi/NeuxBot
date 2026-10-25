import { useTranslation } from "react-i18next";

import {
  NavigationItem,
  NavigationPanel,
  NavigationSection,
} from "@/shared/ui/navigation-panel";
import {
  settingsNavigationGroups,
  type SettingsSection,
} from "../settings-navigation";

export function SettingsContextSidebar({
  activeSection,
  onSelect,
}: {
  activeSection: SettingsSection;
  onSelect: (section: SettingsSection) => void;
}) {
  const { t } = useTranslation("appSettings");

  return (
    <aside className="min-h-0 min-w-0 overflow-hidden">
      <NavigationPanel
        aria-label={t("navigationLabel")}
        className="scrollbar-hidden min-h-0 overflow-y-auto bg-transparent py-1"
      >
        {settingsNavigationGroups.map((group) => (
          <NavigationSection key={group.titleKey} title={t(group.titleKey)}>
            {group.items.map(({ icon: Icon, key, labelKey }) => (
              <NavigationItem
                key={key}
                active={activeSection === key}
                icon={<Icon strokeWidth={1.5} />}
                label={t(labelKey)}
                onClick={() => onSelect(key)}
              />
            ))}
          </NavigationSection>
        ))}
      </NavigationPanel>
    </aside>
  );
}
