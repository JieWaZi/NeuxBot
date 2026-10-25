import { LockKeyhole, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Switch } from "@/shared/ui/switch";
import { Checkbox } from "@/shared/ui/checkbox";
import {
  ResourceItemAction,
  ResourceListItem,
} from "@/shared/ui/resource-list-item";
import {
  ResourceManagementCard,
  ResourcePartnerUsage,
  ResourceMoreAction,
} from "@/shared/ui/resource-management-card";
import { DropdownMenuItem } from "@/shared/ui/dropdown-menu";
import { PluginIcon } from "./plugin-icon";
import {
  pluginDisplayName,
  pluginInstalledVersion,
  pluginSummary,
  pluginNeedsAuthorization,
  type PluginDetail,
  type PluginCatalogEntry,
} from "../model/plugin-view";
export function PluginCard({
  detail,
  catalog,
  wakerId = "",
  onInstall,
  onUpdate,
  onToggle,
  onRemove,
  pending = false,
  selected = false,
  onSelectedChange,
}: {
  detail?: PluginDetail;
  catalog?: PluginCatalogEntry;
  wakerId?: string;
  onInstall: () => void;
  onUpdate?: () => void;
  onToggle?: (enabled: boolean) => void;
  onRemove?: () => void;
  pending?: boolean;
  selected?: boolean;
  onSelectedChange?: (selected: boolean) => void;
}) {
  const { t } = useTranslation("plugin");
  const relation = detail?.installations?.find(
    (item) => item.wakerId === wakerId,
  );
  const version = pluginInstalledVersion(detail, wakerId);
  const pkg = catalog?.package ?? version?.package;
  const needsAuthorization = pluginNeedsAuthorization(pkg, detail);
  const mine = Boolean(detail && !catalog);
  const name = pluginDisplayName(pkg, detail?.plugin?.name);
  const to = catalog
    ? `/resources/plugins/catalog/${catalog.key}`
    : wakerId
      ? `/wakers/${wakerId}/plugins/${detail?.plugin?.id}`
      : `/resources/plugins/${detail?.plugin?.id}`;
  const description = pluginSummary(
    pkg,
    detail?.plugin?.description,
    catalog
      ? (catalog.sourceKey ?? `catalog:${catalog.key}`)
      : detail?.plugin?.sourceKey,
  );
  const installations = (detail?.installations ?? []).filter(
    (item) => !wakerId || item.wakerId === wakerId,
  );
  if (mine)
    return (
      <ResourceManagementCard
        title={name}
        description={description}
        icon={<PluginIcon pkg={pkg} />}
        to={to}
        label={t("viewPlugin", { name })}
        selected={selected}
        selection={
          onSelectedChange && (
            <Checkbox
              size="sm"
              checked={selected}
              aria-label={t("selectNamed", { name })}
              onCheckedChange={(checked) => onSelectedChange(checked === true)}
            />
          )
        }
        control={
          <div className="flex items-center gap-2">
            {detail?.update?.available && onUpdate && (
              <Button
                variant="ghost"
                size="sm"
                disabled={pending}
                onClick={onUpdate}
              >
                {t("updateAvailable")}
              </Button>
            )}
            {relation && onToggle && (
              <Switch
                hitArea="comfortable"
                checked={relation.enabled ?? false}
                disabled={pending}
                aria-label={t("enable")}
                onClick={() => onToggle(!relation.enabled)}
              />
            )}
          </div>
        }
        usage={
          <ResourcePartnerUsage
            count={installations.length}
            partners={installations.map((item) => ({
              id: item.wakerId ?? "",
              name: detail?.partnerNames?.[item.wakerId ?? ""] || t("unknown"),
              avatar: detail?.partnerAvatars?.[item.wakerId ?? ""],
            }))}
          />
        }
        actions={
          <>
            {needsAuthorization && (
              <span className="type-caption text-warning flex items-center gap-1">
                <LockKeyhole className="size-icon-sm" />
                {t("requiresAuthorization")}
              </span>
            )}
            <ResourceMoreAction to={to} name={name}>
              {(!wakerId || needsAuthorization) && (
                <DropdownMenuItem onSelect={onInstall}>
                  {needsAuthorization ? <LockKeyhole /> : <Plus />}
                  {t(needsAuthorization ? "authorize" : "installToPartners")}
                </DropdownMenuItem>
              )}
              {onRemove && (
                <DropdownMenuItem
                  className="text-destructive data-[highlighted]:text-destructive"
                  onSelect={onRemove}
                >
                  <Trash2 />
                  {t(wakerId ? "uninstall" : "remove")}
                </DropdownMenuItem>
              )}
            </ResourceMoreAction>
          </>
        }
      />
    );
  return (
    <ResourceListItem
      title={name}
      description={description}
      descriptionLines={1}
      icon={<PluginIcon pkg={pkg} />}
      to={to}
      label={t("viewPlugin", { name })}
      actions={
        needsAuthorization ? (
          <Button
            variant="ghost"
            size="sm"
            disabled={pending}
            aria-label={t("authorizeNamed", { name })}
            onClick={onInstall}
          >
            {t("authorize")}
          </Button>
        ) : (
          <ResourceItemAction
            variant="ghost"
            disabled={pending}
            label={t("installNamed", { name })}
            onClick={onInstall}
          >
            <Plus />
          </ResourceItemAction>
        )
      }
    />
  );
}
