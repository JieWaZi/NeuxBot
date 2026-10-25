import { ChevronRight, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

import type { GithubComJieWaZiNeuxBotServerApisSkillsWakerSkillResponse } from "@/shared/api/generated/types.gen";
import type { GithubComJieWaZiNeuxBotServerApisMcpConnectorResponse } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";

import {
  wakerOverviewConnectorPreviewLimit,
  wakerOverviewSkillPreviewLimit,
} from "./waker-overview-limits";
import { WakerAccentSection } from "./waker-accent-block";

type WakerSkillResponse =
  GithubComJieWaZiNeuxBotServerApisSkillsWakerSkillResponse;
type WakerConnectorResponse =
  GithubComJieWaZiNeuxBotServerApisMcpConnectorResponse;

type WakerSkillsToolsSectionProps = {
  wakerId: string;
  skills: WakerSkillResponse[];
  connectors: WakerConnectorResponse[];
  connectorsError: boolean;
  connectorsPending: boolean;
  skillsError: boolean;
  skillsPending: boolean;
  onAddConnector: () => void;
  onAddSkill: () => void;
};

function PreviewChip({ label, title }: { label: string; title?: string }) {
  return (
    <span
      className="bg-surface-scene type-ui border-border-subtle inline-flex max-w-full rounded-md border px-2.5 py-1 font-medium [overflow-wrap:anywhere]"
      title={title}
    >
      {label}
    </span>
  );
}

function PreviewOverflowLink({
  count,
  label,
  to,
}: {
  count: number;
  label: string;
  to: string;
}) {
  if (count <= 0) return null;
  return (
    <Button size="sm" variant="ghost" asChild>
      <Link to={to} aria-label={label}>
        {label}
        <ChevronRight />
      </Link>
    </Button>
  );
}

export function WakerSkillsToolsSection({
  wakerId,
  skills,
  connectors,
  connectorsError,
  connectorsPending,
  skillsError,
  skillsPending,
  onAddConnector,
  onAddSkill,
}: WakerSkillsToolsSectionProps) {
  const { t } = useTranslation("waker");
  const installedSkills = skills.filter((item) => item.skill?.id);
  const enabledSkills = installedSkills.filter(
    (item) => item.enabled !== false,
  );
  const installedConnectors = connectors.filter((item) => item.connector?.id);
  const enabledConnectors = installedConnectors.filter(
    (item) => item.enabled !== false,
  );

  const previewSkills = enabledSkills.slice(0, wakerOverviewSkillPreviewLimit);
  const hiddenSkillCount = enabledSkills.length - previewSkills.length;
  const previewConnectors = enabledConnectors.slice(
    0,
    wakerOverviewConnectorPreviewLimit,
  );
  const hiddenConnectorCount =
    enabledConnectors.length - previewConnectors.length;

  return (
    <section aria-label={t("detail.skillsTools")} className="grid gap-8">
      <WakerAccentSection
        title={t("detail.mySkills", {
          enabled: enabledSkills.length,
          total: installedSkills.length,
        })}
        description={t("detail.skillsLead")}
        actions={
          <Button size="sm" variant="ghost" asChild>
            <Link to={`/wakers/${wakerId}/skills`}>
              {t("detail.manage")}
              <ChevronRight />
            </Link>
          </Button>
        }
        contentClassName="space-y-4"
      >
        {skillsPending ? (
          <p className="text-muted-foreground type-body">
            {t("detail.skillsLoading")}
          </p>
        ) : skillsError ? (
          <p className="text-destructive type-body" role="alert">
            {t("detail.skillsLoadError")}
          </p>
        ) : enabledSkills.length > 0 ? (
          <div className="flex flex-wrap items-center gap-2">
            {previewSkills.map((item) => {
              const skill = item.skill;
              if (!skill?.id) return null;
              return (
                <PreviewChip
                  key={skill.id}
                  label={skill.name || t("detail.unnamedSkill")}
                  title={skill.description ?? undefined}
                />
              );
            })}
            <PreviewOverflowLink
              count={hiddenSkillCount}
              label={t("detail.moreSkills", { count: hiddenSkillCount })}
              to={`/wakers/${wakerId}/skills`}
            />
          </div>
        ) : (
          <p className="text-muted-foreground type-body">
            {t("detail.noSkills")}
          </p>
        )}

        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="surface" onClick={onAddSkill}>
            <Plus />
            {t("detail.addSkill")}
          </Button>
        </div>
      </WakerAccentSection>

      <WakerAccentSection
        className="min-w-0"
        title={t("detail.myConnectors", {
          enabled: enabledConnectors.length,
          total: installedConnectors.length,
        })}
        description={t("detail.connectorsLead")}
        actions={
          <Button size="sm" variant="ghost" asChild>
            <Link to={`/wakers/${wakerId}/connectors`}>
              {t("detail.manage")}
              <ChevronRight />
            </Link>
          </Button>
        }
        contentClassName="space-y-4"
      >
        {connectorsPending ? (
          <p className="text-muted-foreground type-body">
            {t("detail.connectorsLoading")}
          </p>
        ) : connectorsError ? (
          <p className="text-destructive type-body" role="alert">
            {t("detail.connectorsLoadError")}
          </p>
        ) : enabledConnectors.length ? (
          <div className="flex flex-wrap items-center gap-2">
            {previewConnectors.map((item) => (
              <PreviewChip
                key={item.connector?.id}
                label={item.connector?.name || t("detail.unnamedConnector")}
                title={item.connector?.description ?? undefined}
              />
            ))}
            <PreviewOverflowLink
              count={hiddenConnectorCount}
              label={t("detail.moreConnectors", {
                count: hiddenConnectorCount,
              })}
              to={`/wakers/${wakerId}/connectors`}
            />
          </div>
        ) : (
          <p className="text-muted-foreground type-body">
            {t("detail.noConnectors")}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="surface" onClick={onAddConnector}>
            <Plus />
            {t("detail.addConnector")}
          </Button>
        </div>
      </WakerAccentSection>
    </section>
  );
}
