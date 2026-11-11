import { useTranslation } from "react-i18next";
import { useState } from "react";

import {
  NavigationItem,
  NavigationPanel,
  NavigationSection,
} from "@/shared/ui/navigation-panel";
import {
  settingsNavigation,
  type SettingsSection,
} from "../settings-navigation";
import { SearchField } from "@/shared/ui/search-field";

export function SettingsContextSidebar({
  activeSection,
  onSelect,
}: {
  activeSection: SettingsSection;
  onSelect: (section: SettingsSection) => void;
}) {
  const { t } = useTranslation("appSettings");
  const [query, setQuery] = useState("");
  const search = query.trim().toLocaleLowerCase();
  const items = settingsNavigation.filter((item) =>
    t(item.labelKey).toLocaleLowerCase().includes(search),
  );

  return (
    <aside className="bg-surface-scene flex h-full min-h-0 min-w-0 flex-col gap-1 overflow-hidden px-3 pt-4">
      <SearchField
        width="full"
        aria-label={t("search")}
        placeholder={t("search")}
        clearLabel={t("clearSearch")}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <NavigationPanel
        appearance="settings"
        aria-label={t("navigationLabel")}
        className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto pt-3 pb-3"
      >
        <NavigationSection appearance="settings">
          {items.map(({ icon: Icon, key, labelKey }) => (
            <NavigationItem
              key={key}
              active={activeSection === key}
              density="settings"
              icon={<Icon strokeWidth={1.5} />}
              label={t(labelKey)}
              onClick={() => onSelect(key)}
            />
          ))}
        </NavigationSection>
        {!items.length ? (
          <p role="status" className="type-ui text-content-secondary px-2 py-4">
            {t("noResults")}
          </p>
        ) : null}
      </NavigationPanel>
    </aside>
  );
}
