import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ResourceListItem } from "@/shared/ui/resource-list-item";
import { Button } from "@/shared/ui/button";
import { Switch } from "@/shared/ui/switch";
import { SkillIcon } from "./skill-icon";
import type { SkillSummary } from "../model/types";

type Props = {
  onInstall?: () => void;
  disabled?: boolean;
  skill: SkillSummary;
  wakerId: string;
  onEnabledChange: (enabled: boolean) => void;
};

export function SkillCard({
  disabled,
  onInstall,
  onEnabledChange,
  skill,
  wakerId,
}: Props) {
  const { t } = useTranslation("skill");
  const resourceTo = wakerId
    ? `/wakers/${wakerId}/skills/${skill.id}`
    : `/resources/skills/${skill.id}`;
  return (
    <ResourceListItem
      appearance="row"
      title={skill.name}
      description={
        wakerId && !skill.enabled ? t("disabled") : skill.description
      }
      icon={<SkillIcon />}
      to={resourceTo}
      label={t("card.view", { name: skill.name })}
      actions={
        wakerId ? (
          <Switch
            hitArea="comfortable"
            checked={skill.enabled}
            disabled={disabled}
            aria-label={t(skill.enabled ? "card.disable" : "card.enable", {
              name: skill.name,
            })}
            onClick={() => onEnabledChange(!skill.enabled)}
          />
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
