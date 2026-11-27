import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Navigate, useSearchParams } from "react-router-dom";
import { listWakersOptions } from "@/modules/waker";
import { readChatLastWakerId } from "@/shared/lib/chat-navigation";
import { mainConversationHref } from "@/shared/lib/main-conversation-navigation";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { readPartnerNavigationPreferences } from "../layout/partner-navigation-preferences";
import { WelcomeScene } from "./welcome-scene";

/** 伙伴工作台的默认入口只解析主体。带 draftId 或 Chat ID 的显式地址由普通聊天负责。 */
export function MainConversationEntry({ current }: { current: boolean }) {
  const active = useRouteSceneActive();
  const [params] = useSearchParams();
  const requested = params.get("wakerId")?.trim();
  const { t } = useTranslation(["chat", "common"]);
  const partners = useQuery({
    ...listWakersOptions({ query: { pageNum: 1, pageSize: 100 } }),
    enabled: current && active && !requested,
  });
  if (!current || !active) return null;
  const list = partners.data?.resources ?? [];
  const preferred =
    readPartnerNavigationPreferences().currentPartnerId ||
    readChatLastWakerId();
  const id =
    requested ||
    list.find((partner) => partner.id === preferred)?.id ||
    list.find((partner) => partner.id)?.id;
  if (id)
    return (
      <Navigate replace to={mainConversationHref({ kind: "waker", id })} />
    );
  if (partners.isError)
    return (
      <div className="grid h-full place-content-center gap-2">
        <p role="alert">
          {t("common:partnerNavigation.entitiesUnavailable", {
            name: t("common:partnerNavigation.partner"),
          })}
        </p>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => void partners.refetch()}
        >
          {t("chat:entryRetry")}
        </Button>
      </div>
    );
  if (partners.isPending)
    return (
      <div
        role="status"
        aria-busy="true"
        aria-label={t("common:partnerNavigation.loadingEntities", {
          name: t("common:partnerNavigation.partner"),
        })}
        className="p-4"
      >
        <Skeleton className="h-6 w-32" />
      </div>
    );
  return <WelcomeScene />;
}
