import { BarChart3, Package, Folder, History } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { NavigationPanel } from "@/shared/ui/navigation-panel";

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
      aria-label={t("partnerNavigation.partnerMenu")}
      className="gap-0.5 bg-transparent p-3"
    >
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
          <Button
            key={key}
            asChild
            size="lg"
            variant="ghost"
            className={cn(
              "w-full justify-start",
              active &&
                "bg-shell-control-active text-content-primary hover:bg-shell-control-active",
            )}
          >
            <Link
              to={href}
              aria-label={label}
              aria-current={active ? "page" : undefined}
            >
              <Icon aria-hidden="true" strokeWidth={1.5} />
              <span className="truncate">{label}</span>
            </Link>
          </Button>
        );
      })}
    </NavigationPanel>
  );
}
