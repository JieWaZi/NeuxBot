import { useState, type ReactNode } from "react";
import { ExternalLink } from "lucide-react";
import { openExternalURL } from "@/shared/platform";
import { useTranslation } from "react-i18next";

import {
  ResourceCatalogField,
  ResourceCatalogFields,
} from "@/shared/ui/resource-catalog";
import { DetailSection } from "@/shared/ui/detail-section";

type SkillMetadataCardProps = {
  description?: string;
  source?: ReactNode;
  category?: ReactNode;
  sourceURL?: string;
  version?: string;
  updatedAt?: number;
};

/** 对应 MarketplaceSkillPage 的 SkillInformation：简介与真实事实同在信息 Card。 */
export function SkillMetadataCard({
  description,
  source,
  category,
  sourceURL,
  version,
  updatedAt,
}: SkillMetadataCardProps) {
  const { t, i18n } = useTranslation("skill");
  const [linkError, setLinkError] = useState(false);
  const externalURL =
    sourceURL && /^https?:\/\//.test(sourceURL) ? sourceURL : undefined;
  if (!description && !source && !category && !version && !updatedAt)
    return null;
  return (
    <DetailSection appearance="catalog" title={t("detail.information")}>
      <div className="grid gap-3 px-4 py-3">
        {description && (
          <p className="type-resource-body text-resource-content">
            {description}
          </p>
        )}
        {(source || category || version || updatedAt) && (
          <ResourceCatalogFields>
            {source && (
              <ResourceCatalogField label={t("detail.metadataSource")}>
                {externalURL ? (
                  <a
                    href={externalURL}
                    className="focus-visible:ring-ring inline-flex max-w-full items-center gap-1 rounded-xs outline-none hover:underline focus-visible:ring-2"
                    onClick={(event) => {
                      if (
                        event.metaKey ||
                        event.ctrlKey ||
                        event.shiftKey ||
                        event.altKey
                      )
                        return;
                      event.preventDefault();
                      setLinkError(false);
                      void openExternalURL(externalURL).catch(() =>
                        setLinkError(true),
                      );
                    }}
                  >
                    {source}
                    <ExternalLink className="size-icon-sm shrink-0" />
                  </a>
                ) : (
                  source
                )}
              </ResourceCatalogField>
            )}
            {category && (
              <ResourceCatalogField label={t("category.label")}>
                {category}
              </ResourceCatalogField>
            )}
            {version && (
              <ResourceCatalogField label={t("detail.version")}>
                {version}
              </ResourceCatalogField>
            )}
            {updatedAt ? (
              <ResourceCatalogField label={t("detail.updated")}>
                {new Intl.DateTimeFormat(i18n.resolvedLanguage, {
                  dateStyle: "medium",
                }).format(new Date(updatedAt))}
              </ResourceCatalogField>
            ) : null}
          </ResourceCatalogFields>
        )}
        {linkError && (
          <p role="alert" className="type-resource-body text-destructive">
            {t("plugin:failed")}
          </p>
        )}
      </div>
    </DetailSection>
  );
}
