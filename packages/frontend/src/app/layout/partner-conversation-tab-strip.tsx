import { hashKey, useQuery } from "@tanstack/react-query";
import type { ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { mainConversationOptions } from "@/modules/chat/api/main-conversation";
import type { Waker } from "@/modules/waker";
import { ApiError } from "@/shared/api/errors";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { StaticAvatar } from "@/shared/ui/avatar";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import type {
  AppSceneTab,
  ChatSceneOwner,
} from "@/shared/workspace/app-scene-model";
import { mainConversationTab } from "../scenes/main-conversation-route";

type Props = Omit<
  ComponentProps<typeof SceneTabStrip>,
  "onClose" | "onCloseOthers"
> & {
  owner?: ChatSceneOwner;
  partners: readonly Waker[];
  loadingPartners: boolean;
  showingSource: boolean;
  missingQueries: ReadonlySet<string>;
  onCloseOthers: (id: string, keepId?: string) => void;
  onOpenMain: (owner: ChatSceneOwner) => void;
  onClose: (id: string, fallbackTab?: Omit<AppSceneTab, "lastUsed">) => void;
};

/** 在同一个键盘可导航的页签栏中保留伙伴入口；普通聊天沿用现有 Scene。 */
export function PartnerConversationTabStrip({
  owner,
  partners,
  loadingPartners,
  showingSource,
  missingQueries,
  onOpenMain,
  onCloseOthers,
  onClose,
  ...strip
}: Props) {
  const { t } = useTranslation("chat");
  const partnerId = owner?.kind === "waker" ? owner.id : undefined;
  const listed = partners.find((waker) => waker.id === partnerId);
  const detail = useQuery({
    ...getWakerOptions({ path: { wakerId: partnerId ?? "" } }),
    enabled: Boolean(partnerId && !listed && !loadingPartners),
  });
  if (owner?.kind !== "waker")
    return (
      <SceneTabStrip
        {...strip}
        onClose={onClose}
        onCloseOthers={onCloseOthers}
      />
    );

  const partner = listed ?? detail.data;
  const label = partner?.name?.trim() || t("mainConversation");
  const main = mainConversationTab(owner, label);
  const ordinary = strip.tabs.filter((tab) => tab.id !== main.id);
  if (
    (detail.error instanceof ApiError && detail.error.status === 404) ||
    missingQueries.has(
      hashKey(
        mainConversationOptions({
          subjectType: owner.kind,
          subjectId: owner.id,
        }).queryKey,
      ),
    )
  ) {
    return (
      <SceneTabStrip
        {...strip}
        trailing={undefined}
        tabs={strip.tabs.map((tab) =>
          tab.id === main.id
            ? { ...tab, notFound: true, closeable: true }
            : tab,
        )}
        onClose={onClose}
        onCloseOthers={onCloseOthers}
      />
    );
  }
  return (
    <SceneTabStrip
      {...strip}
      tabs={[
        {
          ...main,
          fixed: true,
          closeable: false,
          description: t("mainConversation"),
          indicator: (
            <StaticAvatar name={label} src={partner?.avatar} size="sm" />
          ),
        },
        ...ordinary,
      ]}
      onCloseOthers={(id) => onCloseOthers(id, main.id)}
      onSelect={(id) => {
        if (id !== main.id) strip.onSelect(id);
        else if (strip.activeTabId !== main.id || showingSource)
          onOpenMain(owner);
      }}
      onClose={(id) =>
        onClose(
          id,
          ordinary.length === 1 && ordinary[0]?.id === id ? main : undefined,
        )
      }
    />
  );
}
