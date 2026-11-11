import { Check, Plus, TriangleAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { ResourceListItem } from "@/shared/ui/resource-list-item";
import { ResourceCatalogStatus } from "@/shared/ui/resource-catalog";
import { PluginIcon } from "./plugin-icon";
import {
  pluginBrowseHref,
  type PluginBrowseEntry,
} from "../model/plugin-browse";
import {
  pluginDisplayName,
  pluginInstalledVersion,
  pluginSummary,
  type PluginDetail,
} from "../model/plugin-view";

/** 条目只展示身份与当前状态；维护操作由详情承载。 */
export function PluginCard({
  entry,
  detail,
  wakerId = "",
  onInstall,
  onUpdate,
  pending = false,
}: {
  entry?: PluginBrowseEntry;
  detail?: PluginDetail;
  wakerId?: string;
  onInstall?: () => void;
  onUpdate?: () => void;
  pending?: boolean;
}) {
  const { t } = useTranslation("plugin");
  const pkg = pluginInstalledVersion(detail, wakerId)?.package;
  const name = entry
    ? (entry.name ?? "")
    : pluginDisplayName(pkg, detail?.plugin?.name);
  const description = entry
    ? entry.description
    : pluginSummary(pkg, detail?.plugin?.description);
  const to = entry
    ? pluginBrowseHref(entry)
    : `/wakers/${encodeURIComponent(wakerId)}/plugins/${encodeURIComponent(detail?.plugin?.id ?? "")}`;
  const installed = entry
    ? (entry.installedCount ?? 0) > 0
    : Boolean(
        detail?.installations?.some((relation) => relation.wakerId === wakerId),
      );
  return (
    <ResourceListItem
      appearance="row"
      headingLevel={3}
      title={name}
      description={description}
      descriptionLines={1}
      icon={
        <PluginIcon
          icon={entry?.icon}
          icons={entry?.icons}
          pkg={pkg}
          category={entry?.category ?? detail?.plugin?.categories?.[0]}
        />
      }
      to={to}
      label={t("viewPlugin", { name })}
      actions={
        entry?.requiresAuthorization && onInstall ? (
          <Button
            size="catalog"
            variant="catalog-outline"
            disabled={pending}
            aria-label={t("authorizeNamed", { name })}
            onClick={onInstall}
          >
            <TriangleAlert aria-hidden="true" />
            {t("authorize")}
          </Button>
        ) : entry?.update?.available && onUpdate ? (
          <Button
            size="catalog"
            variant="catalog-secondary"
            disabled={pending}
            aria-label={t("updateNamed", { name })}
            onClick={onUpdate}
          >
            {t("updateAvailable")}
          </Button>
        ) : installed ? (
          <ResourceCatalogStatus>
            <Check aria-hidden="true" className="size-icon-md text-success" />
            {t("installed")}
          </ResourceCatalogStatus>
        ) : onInstall ? (
          <Button
            size="catalog"
            variant="catalog-secondary"
            disabled={pending}
            aria-label={t("installNamed", { name })}
            onClick={onInstall}
          >
            <Plus aria-hidden="true" />
            {t("install")}
          </Button>
        ) : undefined
      }
    />
  );
}
