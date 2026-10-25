import { BarChart3, Package, Folder, History } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useLocation } from "react-router-dom";

import { DetailDockNavigation } from "@/shared/ui/detail-dock";

const sections = [
  { key: "overview", icon: BarChart3 },
  { key: "projects", icon: Folder },
  { key: "memory", icon: History },
  { key: "resources", icon: Package },
] as const;

export function PartnerDetailDock({ partnerId }: { partnerId: string }) {
  const { t } = useTranslation("common");
  const { pathname } = useLocation();
  const root = `/wakers/${partnerId}`;

  return (
    <DetailDockNavigation
      label={t("partnerNavigation.partnerMenu")}
      items={sections.map(({ icon, key }) => {
        const href = key === "overview" ? root : `${root}/${key}`;
        const active =
          (key === "resources" &&
            /^\/wakers\/[^/]+\/(resources|plugins|skills|connectors)(?:\/|$)/.test(
              pathname,
            )) ||
          pathname === href ||
          (pathname.startsWith(`${href}/`) && key !== "overview");
        return {
          id: key,
          icon,
          href,
          active,
          label: t(`partnerNavigation.sections.${key}`),
        };
      })}
    />
  );
}
