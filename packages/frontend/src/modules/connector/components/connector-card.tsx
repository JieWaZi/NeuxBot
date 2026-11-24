import { useTranslation } from "react-i18next";
import { resourceLibraryDetailPath } from "@/shared/lib/resource-navigation";
import {
  ResourceInstallActions,
  ResourceListItem,
} from "@/shared/ui/resource-list-item";
import { Switch } from "@/shared/ui/switch";
import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";
import type { ConnectorSummary } from "../model/types";

type Props = {
  connector: ConnectorSummary;
  disabled?: boolean;
  onInstall?: () => void;
  onEnabledChange: (enabled: boolean) => void;
  wakerId: string;
};

export function ConnectorCard({
  connector,
  disabled,
  onInstall,
  onEnabledChange,
  wakerId,
}: Props) {
  const { t } = useTranslation("connector");
  const systemManaged = connector.activationPolicy === "workflow_context";
  return (
    <ResourceListItem
      appearance="row"
      title={connector.name}
      description={connector.description}
      icon={<ResourceCategoryIcon category={connector.categories?.[0]} />}
      to={
        wakerId
          ? `/wakers/${encodeURIComponent(wakerId)}/connectors/${encodeURIComponent(connector.id)}`
          : resourceLibraryDetailPath(
              "connectors",
              connector.id,
              connector.ownerWakerId,
            )
      }
      label={t("card.view", { name: connector.name })}
      actions={
        wakerId ? (
          systemManaged ? (
            <span className="text-resource-muted type-caption">
              {t("systemManaged")}
            </span>
          ) : (
            <Switch
              hitArea="comfortable"
              checked={connector.enabled}
              disabled={disabled}
              aria-label={t(
                connector.enabled ? "card.disable" : "card.enable",
                { name: connector.name },
              )}
              onClick={() => onEnabledChange(!connector.enabled)}
            />
          )
        ) : (
          <ResourceInstallActions
            installedCount={connector.affectedWakerCount}
            disabled={disabled}
            onInstall={onInstall}
          />
        )
      }
    />
  );
}
