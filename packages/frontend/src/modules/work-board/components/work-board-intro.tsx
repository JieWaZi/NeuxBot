import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { NewChatComposer } from "@/modules/chat";
import type { Waker } from "@/modules/waker";
import { ApiError } from "@/shared/api/errors";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { defaultCompanionAvatar } from "@/shared/companion/appearance";
import { StaticAvatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { WorkBoardAvatars } from "./work-board-avatars";
import {
  WorkBoardPartnerPicker,
  WorkBoardPartnersLoading,
  workBoardPartnerPageOptions,
} from "./work-board-partner-picker";

function TitlePartner({ children }: { children?: ReactNode }) {
  return (
    <>
      <span className="sr-only">{children}</span>
      <StaticAvatar name="" src={defaultCompanionAvatar} size="sm" />
    </>
  );
}

function partnerDetailOptions(wakerId: string) {
  const options = { path: { wakerId } };
  return {
    ...getWakerOptions(withLocalErrorSurface(options)),
    queryKey: getWakerOptions(options).queryKey,
  };
}

function missingPartner(error: unknown) {
  return error instanceof ApiError && error.status === 404;
}

export function WorkBoardIntro({ active }: { active: boolean }) {
  const { t } = useTranslation("workBoard");
  const queryClient = useQueryClient();
  const labelId = useId();
  const [scope] = useState(() => `draft:work-board:${crypto.randomUUID()}`);
  const [selectedId, setSelectedId] = useState("");
  const wakers = useQuery({
    ...workBoardPartnerPageOptions(1),
    enabled: active,
  });
  const partners = (wakers.data?.resources ?? []).flatMap((waker) =>
    waker.id
      ? [{ ...waker, id: waker.id, name: waker.name || t("waker") }]
      : [],
  );
  const defaultPartner =
    partners.find((partner) => partner.isDefault) ?? partners[0];
  // 列表排序与刷新只影响候选，草稿收件人由已确定的伙伴 ID 持有。
  if (!selectedId && defaultPartner) setSelectedId(defaultPartner.id);
  const detailOptions = partnerDetailOptions(selectedId || "pending");
  const detail = useQuery({
    ...detailOptions,
    enabled: active && Boolean(selectedId),
    initialData: () => partners.find((partner) => partner.id === selectedId),
    initialDataUpdatedAt: 0,
    retry: (failures, error) => !missingPartner(error) && failures < 2,
  });
  useQueryEvents({
    topic: "entities",
    resource: { kind: "waker", id: selectedId },
    queryKeys: [detailOptions.queryKey],
    eventTypes: ["entities.changed"],
    enabled: Boolean(selectedId),
    active,
  });
  const removed = missingPartner(detail.error);
  const selected = !removed && detail.data?.id ? detail.data : undefined;
  const choosePartner = (partner: Waker & { id: string }) => {
    const { queryKey } = partnerDetailOptions(partner.id);
    const known = queryClient.getQueryState(queryKey);
    if (!known?.data && !missingPartner(known?.error)) {
      // 搜索结果已足以开始草稿，精确详情在后台核对和刷新。
      queryClient.setQueryData(queryKey, partner, { updatedAt: 0 });
    }
    setSelectedId(partner.id);
  };
  const pending = wakers.isPending || Boolean(selectedId && detail.isPending);
  const partnerError =
    !removed && detail.isError ? detail : wakers.isError ? wakers : undefined;

  return (
    <header className="mb-8 grid min-w-0 grid-cols-[minmax(0,44rem)] items-end justify-center gap-4 @[52rem]/resource-page:grid-cols-[minmax(0,1fr)_minmax(0,min(60%,44rem))_minmax(0,1fr)]">
      {partners.length ? (
        <WorkBoardAvatars partners={partners} active={active} side="left" />
      ) : null}
      <section
        aria-labelledby={labelId}
        className="grid min-w-0 content-start gap-3 @[52rem]/resource-page:col-start-2"
      >
        <h1 id={labelId} className="type-display text-center font-semibold">
          <Trans
            t={t}
            i18nKey="startChat"
            components={{ partner: <TitlePartner /> }}
          />
        </h1>
        <NewChatComposer
          scopeKey={scope}
          active={active}
          partner={selected}
          renderPartnerPicker={(sending) => (
            <WorkBoardPartnerPicker
              selected={selected}
              onSelect={choosePartner}
              disabled={
                !active ||
                sending ||
                wakers.isPending ||
                (!partners.length && !wakers.isError && !selectedId)
              }
            />
          )}
        />
        {removed ? (
          <p role="alert" className="text-destructive type-ui">
            {t("selectedPartnerUnavailable")}
          </p>
        ) : partnerError ? (
          <div
            role="alert"
            className="text-destructive type-ui flex flex-wrap items-center gap-2"
          >
            {localApiErrorMessage(partnerError.error, t("loadFailed"))}
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => void partnerError.refetch()}
            >
              {t("retry")}
            </Button>
          </div>
        ) : pending ? (
          <WorkBoardPartnersLoading />
        ) : wakers.isSuccess && !partners.length ? (
          <p className="text-content-secondary type-ui">{t("noPartners")}</p>
        ) : null}
      </section>
      {partners.length ? (
        <WorkBoardAvatars partners={partners} active={active} side="right" />
      ) : null}
    </header>
  );
}
