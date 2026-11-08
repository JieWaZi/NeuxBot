import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsSkillVersion as ApiSkillVersion } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { DetailSection } from "@/shared/ui/detail-section";

type SkillVersionHistoryProps = {
  currentVersion: string;
  readOnly?: boolean;
  embedded?: boolean;
  currentVersionId: string;
  onVersionClick: (version: ApiSkillVersion) => void;
  versions: ApiSkillVersion[];
};

export function SkillVersionHistory({
  currentVersionId,
  onVersionClick,
  versions,
  readOnly = false,
  embedded = false,
}: SkillVersionHistoryProps) {
  const { t } = useTranslation("skill");

  const rows = (
    <div className="grid gap-1 p-2">
      {versions.map((version) => {
        const current = version.id === currentVersionId;
        return (
          <button
            key={version.id}
            type="button"
            aria-label={t(
              readOnly ? "detail.viewVersion" : "detail.viewVersionDiff",
              {
                version: version.version,
              },
            )}
            onClick={() => onVersionClick(version)}
            className={cn(
              "focus-visible:ring-ring duration-quick flex w-full flex-wrap items-center justify-between gap-3 rounded-md px-3 py-3 text-left transition-colors outline-none focus-visible:ring-2 motion-reduce:transition-none",
              current
                ? "bg-accent-surface hover:bg-accent-surface-strong"
                : "hover:bg-resource-hover",
            )}
          >
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="type-label font-medium">
                  v{version.version}
                </span>
                {current ? (
                  <span className="bg-accent-surface-strong text-content-primary type-caption rounded-full px-2 py-1">
                    {t("detail.current")}
                  </span>
                ) : null}
              </div>
              {(!readOnly || version.notes) && (
                <p className="text-content-secondary type-body mt-1 [overflow-wrap:anywhere] whitespace-pre-wrap">
                  {version.notes || operationLabel(version.operation, t)}
                </p>
              )}
            </div>
            {(!readOnly || Boolean(version.createdAt)) && (
              <span className="text-muted-foreground type-caption shrink-0">
                {version.createdAt
                  ? new Intl.DateTimeFormat("zh-CN", {
                      month: "short",
                      day: "numeric",
                    }).format(new Date(version.createdAt))
                  : "-"}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
  return embedded ? (
    <div>
      <h3 className="type-resource-body text-resource-secondary px-4 pt-3">
        {t("detail.versionHistory")}
      </h3>
      {rows}
    </div>
  ) : (
    <DetailSection appearance="catalog" title={t("detail.versionHistory")}>
      {rows}
    </DetailSection>
  );
}

function operationLabel(
  operation: string | undefined,
  t: (key: string) => string,
): string {
  if (operation === "edit") return t("detail.operation.edit");
  if (operation === "pull") return t("detail.operation.pull");
  if (operation === "restore") return t("detail.operation.restore");
  return t("detail.operation.import");
}
