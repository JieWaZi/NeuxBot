import { BarChart3, Package, Folder, History } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";

import {
  NavigationItem,
  NavigationPanel,
  NavigationSection,
} from "@/shared/ui/navigation-panel";

const sections = [
  { key: "overview", icon: BarChart3 },
  { key: "projects", icon: Folder },
  { key: "memory", icon: History },
  { key: "resources", icon: Package },
] as const;

/** 伙伴详情原有四项分区的左侧导航；名称、内容与数据仍由所属页面持有。 */
export function PartnerContextSidebar({ partnerId }: { partnerId: string }) {
  const { t } = useTranslation("common");
  const { pathname } = useLocation();
  const root = `/wakers/${partnerId}`;
  return (
    <NavigationPanel
      appearance="catalog"
      title={t("partnerNavigation.partner")}
      aria-label={t("partnerNavigation.partnerMenu")}
    >
      <NavigationSection appearance="catalog">
        {sections.map(({ icon: Icon, key }) => {
          const href = key === "overview" ? root : `${root}/${key}`;
          const active =
            (key === "resources" &&
              /^\/wakers\/[^/]+\/(resources|plugins|skills|connectors)(?:\/|$)/.test(
                pathname,
              )) ||
            pathname === href ||
            (pathname.startsWith(`${href}/`) && key !== "overview");
          const label = t(`partnerNavigation.sections.${key}`);
          return (
            <NavigationItem
              key={key}
              asChild
              density="catalog"
              active={active}
              icon={<Icon aria-hidden="true" strokeWidth={1.5} />}
              label={label}
            >
              <Link
                to={href}
                aria-label={label}
                aria-current={active ? "page" : undefined}
              />
            </NavigationItem>
          );
        })}
      </NavigationSection>
    </NavigationPanel>
  );
}
