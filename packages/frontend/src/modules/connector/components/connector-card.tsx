import { Cable, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ResourceListItem } from "@/shared/ui/resource-list-item";
import { Button } from "@/shared/ui/button";
import { Switch } from "@/shared/ui/switch";
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
      icon={
        <span className="bg-resource-surface text-resource-muted size-resource-icon grid shrink-0 place-items-center rounded-lg">
          <Cable className="size-resource-logo" strokeWidth={1.5} />
        </span>
      }
      to={`${wakerId ? `/wakers/${wakerId}` : "/resources"}/connectors/${connector.id}`}
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
        ) : onInstall ? (
          <Button
            size="catalog"
            variant="catalog-secondary"
            disabled={disabled}
            onClick={onInstall}
          >
            <Plus />
            {t("plugin:install")}
          </Button>
        ) : undefined
      }
    />
  );
}
