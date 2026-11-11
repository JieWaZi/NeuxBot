import { Package, Sparkles, TriangleAlert, Unplug } from "lucide-react";
import { useTranslation } from "react-i18next";
import { DiagramFlowHorizontal } from "@/shared/ui/diagram-flow-horizontal";
import {
  usePluginBrowse,
  pluginBrowseHref,
  PluginIcon,
} from "@/modules/plugin";
import { Button } from "@/shared/ui/button";
import {
  NavigationItem,
  NavigationPanel,
  NavigationSection,
} from "@/shared/ui/navigation-panel";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import type { resourceCategoryFromParams } from "../pages/resources-navigation";

type ResourceCategory = ReturnType<typeof resourceCategoryFromParams>;

export function ResourcesContextSidebar({
  category,
  onCategoryChange,
  currentPath = "",
  onPluginSelect,
  active = true,
}: {
  readonly category: ResourceCategory;
  readonly onCategoryChange: (category: ResourceCategory) => void;
  readonly currentPath?: string;
  readonly onPluginSelect?: (href: string) => void;
  readonly active?: boolean;
}) {
  const { t } = useTranslation("plugin");
  const installed = usePluginBrowse({ installedOnly: true, active });
  const isCurrent = (href: string) =>
    currentPath === href || currentPath.startsWith(`${href}/`);
  const currentShortcut = installed.entries.some((entry) =>
    isCurrent(pluginBrowseHref(entry)),
  );
  return (
    <aside className="min-h-0 min-w-0 overflow-y-auto">
      <NavigationPanel
        appearance="catalog"
        title={t("title")}
        aria-label={t("title")}
      >
        <NavigationSection appearance="catalog">
          {(
            [
              { id: "plugins", icon: Package },
              { id: "skills", icon: Sparkles },
              { id: "connectors", icon: Unplug },
              { id: "workflows", icon: DiagramFlowHorizontal },
            ] as const
          ).map(({ id, icon: Icon }) => (
            <NavigationItem
              density="catalog"
              key={id}
              active={category === id && !currentShortcut}
              aria-current={
                category === id
                  ? currentShortcut
                    ? "location"
                    : "page"
                  : undefined
              }
              icon={<Icon strokeWidth={1.5} />}
              label={t(id === "workflows" ? "workflow:title" : id)}
              onClick={() => onCategoryChange(id)}
            />
          ))}
        </NavigationSection>
        {installed.entries.length > 0 && (
          <NavigationSection appearance="catalog" title={t("installed")}>
            {installed.entries.map((entry) => {
              const href = pluginBrowseHref(entry);
              return (
                <NavigationItem
                  density="catalog"
                  key={entry.sourceKey}
                  active={isCurrent(href)}
                  icon={
                    <PluginIcon
                      icon={entry.icon}
                      icons={entry.icons}
                      size="navigation"
                    />
                  }
                  label={entry.name}
                  trailing={
                    entry.requiresAuthorization && (
                      <TriangleAlert
                        aria-label={t("requiresAuthorization")}
                        className="text-warning"
                      />
                    )
                  }
                  onClick={() => onPluginSelect?.(href)}
                />
              );
            })}
          </NavigationSection>
        )}
        {installed.isError && (
          <div className="space-y-2 px-4 py-2">
            <p role="alert" className="type-caption text-destructive">
              {localApiErrorMessage(installed.error, t("failed"))}
            </p>
            <Button
              variant="catalog-ghost"
              size="catalog"
              disabled={installed.isFetching}
              onClick={() => void installed.refetch()}
            >
              {t("retry")}
            </Button>
          </div>
        )}
      </NavigationPanel>
    </aside>
  );
}
