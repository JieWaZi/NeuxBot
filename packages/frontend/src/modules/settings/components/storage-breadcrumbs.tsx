import { useTranslation } from "react-i18next";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";

export function StorageBreadcrumbs({
  partnerTitle,
}: {
  partnerTitle?: string;
}) {
  const { t } = useTranslation("appSettings");
  const storage = t("sections.storage");
  return (
    <SceneBreadcrumbs
      ariaLabel={t("sceneLabel")}
      items={
        partnerTitle
          ? [
              { label: storage, href: "/settings/storage" },
              { label: partnerTitle },
            ]
          : [{ label: storage }]
      }
    />
  );
}
