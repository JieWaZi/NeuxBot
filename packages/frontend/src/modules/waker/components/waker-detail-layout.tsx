import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import {
  SceneBreadcrumbs,
  type SceneBreadcrumbItem,
} from "@/shared/ui/scene-breadcrumbs";
import { getWakerOptions } from "../api/waker-queries";

type Section =
  | "resources"
  | "plugins"
  | "connectors"
  | "chats"
  | "memory"
  | "overview"
  | "projects"
  | "skills";

type Props = {
  active: Section;
  children: ReactNode;
  breadcrumbItems?: readonly SceneBreadcrumbItem[];
  showBreadcrumbs?: boolean;
};

export function WakerDetailLayout({
  active,
  children,
  breadcrumbItems,
  showBreadcrumbs = true,
}: Props) {
  const { t } = useTranslation();
  const { wakerId = "" } = useParams();
  // 只观察页面与侧栏已经取得的父实体；深链缺数据时保留类型占位。
  const parent = useQuery({
    ...getWakerOptions({ path: { wakerId } }),
    enabled: false,
  });
  const name = parent.data?.name || t("common:partnerNavigation.partner");
  const sectionLabels: Record<Section, string> = {
    overview: t("common:partnerNavigation.partner"),
    projects: t("project:title"),
    memory: t("memory:title"),
    skills: t("skill:title"),
    connectors: t("connector:title"),
    plugins: t("plugin:plugins"),
    resources: t("plugin:title"),
    chats: t("chat:conversationTab"),
  };
  const items: readonly SceneBreadcrumbItem[] =
    active === "overview"
      ? [
          { label: t("common:partnerNavigation.wakers"), href: "/wakers" },
          { label: name },
        ]
      : [
          ...(wakerId
            ? [
                {
                  label: t("common:partnerNavigation.wakers"),
                  href: "/wakers",
                },
                { label: name, href: `/wakers/${wakerId}` },
              ]
            : []),
          ...(breadcrumbItems ?? [{ label: sectionLabels[active] }]),
        ];
  return (
    <main
      className="bg-surface-scene h-full min-w-0 overflow-hidden"
      data-partner-section={active}
    >
      {showBreadcrumbs ? (
        <SceneBreadcrumbs ariaLabel={sectionLabels[active]} items={items} />
      ) : null}
      {children}
    </main>
  );
}
