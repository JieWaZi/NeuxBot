import { useTranslation } from "react-i18next";

import { ReadOnlyField } from "@/shared/ui/read-only-field";
import { DetailSection } from "@/shared/ui/detail-section";

type SkillMetadataCardProps = {
  description: string;
  name: string;
  source: string;
};

export function SkillMetadataCard({
  description,
  name,
  source,
}: SkillMetadataCardProps) {
  const { t } = useTranslation("skill");

  return (
    <DetailSection className="mt-6" title={t("detail.metadata")} surface="wash">
      <div className="grid gap-5">
        <dl className="grid min-w-0 grid-cols-2 gap-x-8 gap-y-4">
          <ReadOnlyField
            label={
              <span className="font-medium">{t("detail.metadataName")}</span>
            }
          >
            <span className="type-label font-medium">{name || "-"}</span>
          </ReadOnlyField>
          <ReadOnlyField
            label={
              <span className="font-medium">{t("detail.metadataSource")}</span>
            }
          >
            {source || "-"}
          </ReadOnlyField>
        </dl>
        <dl className="min-w-0">
          <ReadOnlyField
            label={
              <span className="font-medium">
                {t("detail.metadataDescription")}
              </span>
            }
          >
            {description || "-"}
          </ReadOnlyField>
        </dl>
      </div>
    </DetailSection>
  );
}
