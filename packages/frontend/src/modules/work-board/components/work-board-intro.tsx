import { useQueries, useQuery } from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { NewChatComposer } from "@/modules/chat";
import { listWakersOptions } from "@/modules/waker";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { defaultCompanionAvatar } from "@/shared/companion/appearance";
import { StaticAvatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { WorkBoardAvatars } from "./work-board-avatars";

const partnerPageSize = 100;

function TitlePartner({ children }: { children?: ReactNode }) {
  return (
    <>
      <span className="sr-only">{children}</span>
      <StaticAvatar name="" src={defaultCompanionAvatar} size="sm" />
    </>
  );
}

export function WorkBoardIntro({ active }: { active: boolean }) {
  const { t } = useTranslation("workBoard");
  const labelId = useId();
  const [scope] = useState(() => `draft:work-board:${crypto.randomUUID()}`);
  const [selectedId, setSelectedId] = useState("");
  const [showAllPartners, setShowAllPartners] = useState(false);
  const wakers = useQuery({
    ...listWakersOptions({ query: { pageNum: 1, pageSize: partnerPageSize } }),
    enabled: active,
  });
  const additionalPageCount = showAllPartners
    ? Math.max(
        0,
        Math.ceil((wakers.data?.totalSize ?? 0) / partnerPageSize) - 1,
      )
    : 0;
  const additionalPages = useQueries({
    queries: Array.from({ length: additionalPageCount }, (_, index) => ({
      ...listWakersOptions(
        withLocalErrorSurface({
          query: { pageNum: index + 2, pageSize: partnerPageSize },
        }),
      ),
      enabled: active,
    })),
  });
  const partners = Array.from(
    new Map(
      [wakers, ...additionalPages].flatMap(({ data }) =>
        (data?.resources ?? []).flatMap((waker) =>
          waker.id
            ? [
                [
                  waker.id,
                  { ...waker, id: waker.id, name: waker.name || t("waker") },
                ] as const,
              ]
            : [],
        ),
      ),
    ).values(),
  );
  const partnerError = [wakers, ...additionalPages].find(
    (page) => page.isError,
  );
  const selected = selectedId
    ? partners.find((partner) => partner.id === selectedId)
    : (partners.find((partner) => partner.isDefault) ?? partners[0]);
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
          renderPartnerPicker={(pending) => (
            <Select
              value={selected?.id ?? ""}
              onValueChange={setSelectedId}
              onOpenChange={(open) => {
                if (open) setShowAllPartners(true);
              }}
              disabled={!partners.length || pending}
            >
              <SelectTrigger
                appearance="composer"
                className="max-w-36"
                aria-label={t("choosePartner")}
              >
                <SelectValue
                  className="min-w-0 truncate"
                  placeholder={t("choosePartner")}
                >
                  {selected && (
                    <span className="flex min-w-0 items-center gap-1.5">
                      <StaticAvatar
                        name={selected.name}
                        src={selected.avatar}
                        size="xs"
                      />
                      <span className="truncate">{selected.name}</span>
                    </span>
                  )}
                </SelectValue>
              </SelectTrigger>
              <SelectContent side="top" align="end">
                {partners.map((partner) => (
                  <SelectItem
                    key={partner.id}
                    value={partner.id}
                    textValue={partner.name}
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <StaticAvatar
                        name={partner.name}
                        src={partner.avatar}
                        size="xs"
                      />
                      <span className="truncate">{partner.name}</span>
                    </span>
                  </SelectItem>
                ))}
                {additionalPages.some((page) => page.isPending) ? (
                  <div
                    role="status"
                    aria-busy="true"
                    className="text-content-secondary type-ui px-2 py-1"
                  >
                    {t("loadingPartners")}
                  </div>
                ) : null}
              </SelectContent>
            </Select>
          )}
        />
        {partnerError ? (
          <div
            role="alert"
            className="text-destructive type-ui flex items-center gap-2"
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
