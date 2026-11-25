import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { listWakersOptions, type Waker } from "@/modules/waker";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { cn } from "@/shared/lib/cn";
import { StaticAvatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";
import { Skeleton } from "@/shared/ui/skeleton";

const partnerPageSize = 24;

type Partner = Waker & { id: string };

export function workBoardPartnerPageOptions(pageNum: number, query?: string) {
  return listWakersOptions(
    withLocalErrorSurface({
      query: { pageNum, pageSize: partnerPageSize, query },
    }),
  );
}

function WorkBoardPartnersLoading({
  inTrigger = false,
}: {
  inTrigger?: boolean;
}) {
  const { t } = useTranslation("workBoard");
  const [revealed, setRevealed] = useState(false);
  useEffect(() => {
    if (!inTrigger) return;
    const timer = window.setTimeout(() => setRevealed(true), 140);
    return () => window.clearTimeout(timer);
  }, [inTrigger]);
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={t("loadingPartners")}
      className={
        inTrigger
          ? "pointer-events-none absolute inset-0 grid place-items-center"
          : undefined
      }
    >
      <div hidden={inTrigger && !revealed}>
        <Skeleton className={inTrigger ? "h-3 w-12" : "h-4 w-28"} />
      </div>
    </div>
  );
}

export function WorkBoardPartnerPicker({
  selected,
  onSelect,
  disabled,
  pending,
}: {
  selected?: Waker;
  onSelect: (partner: Partner) => void;
  disabled: boolean;
  pending: boolean;
}) {
  const { t } = useTranslation("workBoard");
  const selectedLabel = useId();
  const [open, setOpen] = useState(false);
  const unavailable = disabled || pending;
  const visible = open && !unavailable;
  if (open && unavailable) setOpen(false);
  const selectedName = selected ? selected.name || t("waker") : undefined;
  return (
    <div className="relative flex min-w-0">
      <Popover open={visible} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            shape="capsule"
            disabled={unavailable}
            className={cn("max-w-36", pending && "[&>*]:invisible")}
            aria-label={t("choosePartner")}
            aria-describedby={selected ? selectedLabel : undefined}
            title={selectedName}
            onKeyDown={(event) => {
              if (event.key === "ArrowDown" && !visible) {
                event.preventDefault();
                setOpen(true);
              }
            }}
          >
            {selected ? (
              <StaticAvatar
                name={selectedName ?? ""}
                src={selected.avatar}
                size="xs"
              />
            ) : null}
            <span id={selectedLabel} className="min-w-0 truncate">
              {selectedName ?? t("choosePartner")}
            </span>
            <ChevronDown className="text-content-secondary shrink-0" />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          side="top"
          align="end"
          width="compact"
          aria-label={t("choosePartner")}
        >
          {visible ? (
            <PartnerResults
              selectedId={selected?.id}
              onSelect={(partner) => {
                onSelect(partner);
                setOpen(false);
              }}
            />
          ) : null}
        </PopoverContent>
      </Popover>
      {pending ? <WorkBoardPartnersLoading inTrigger /> : null}
    </div>
  );
}

function PartnerResults({
  selectedId,
  onSelect,
}: {
  selectedId?: string;
  onSelect: (partner: Partner) => void;
}) {
  const { t } = useTranslation("workBoard");
  const [text, setText] = useState("");
  const [{ term, page }, setSearch] = useState({ term: "", page: 1 });
  const results = useRef<HTMLUListElement>(null);
  const searchField = useRef<HTMLDivElement>(null);
  const nextTerm = text.trim();
  const searching = nextTerm !== term;
  useEffect(() => {
    if (!searching) return;
    const timer = window.setTimeout(
      () => setSearch({ term: nextTerm, page: 1 }),
      250,
    );
    return () => window.clearTimeout(timer);
  }, [nextTerm, searching]);
  const query = useQuery(workBoardPartnerPageOptions(page, term || undefined));
  const partners = query.data?.resources ?? [];
  const hasNext = page * partnerPageSize < (query.data?.totalSize ?? 0);
  const canPaginate = page > 1 || hasNext;
  const searchInput = () => searchField.current?.querySelector("input");

  function moveOption(event: KeyboardEvent<HTMLDivElement>) {
    if (
      event.nativeEvent.isComposing ||
      (event.key !== "ArrowDown" && event.key !== "ArrowUp")
    )
      return;
    const options = [...(results.current?.querySelectorAll("button") ?? [])];
    const fromSearch = event.target === searchInput();
    const index = options.findIndex(
      (option) => option === document.activeElement,
    );
    if ((!fromSearch && index < 0) || !options.length) return;
    event.preventDefault();
    if (event.key === "ArrowUp" && index === 0) {
      searchInput()?.focus();
      return;
    }
    if (fromSearch) {
      (event.key === "ArrowDown" ? options[0] : options.at(-1))?.focus();
      return;
    }
    const offset = event.key === "ArrowDown" ? 1 : -1;
    options[(index + offset + options.length) % options.length]?.focus();
  }

  return (
    <div className="grid gap-2" onKeyDown={moveOption}>
      <div ref={searchField}>
        <SearchField
          size="sm"
          width="full"
          autoFocus
          aria-label={t("partnerSearch")}
          placeholder={t("partnerSearch")}
          clearLabel={t("clearSearch")}
          value={text}
          onChange={(event) => setText(event.target.value)}
        />
      </div>
      {searching || query.isPending ? (
        <WorkBoardPartnersLoading />
      ) : query.isError && !query.data ? (
        <div
          role="alert"
          className="text-destructive type-ui flex flex-wrap items-center gap-2"
        >
          {localApiErrorMessage(query.error, t("loadFailed"))}
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => void query.refetch()}
          >
            {t("retry")}
          </Button>
        </div>
      ) : partners.length ? (
        <ul ref={results} className="max-h-64 overflow-y-auto">
          {partners.flatMap((partner) => {
            if (!partner.id) return [];
            const id = partner.id;
            const name = partner.name || t("waker");
            return [
              <li key={id}>
                <Button
                  type="button"
                  size="row"
                  variant="choice"
                  className="focus-visible:ring-inset"
                  aria-pressed={id === selectedId}
                  title={name}
                  onClick={() => onSelect({ ...partner, id, name })}
                >
                  <StaticAvatar name={name} src={partner.avatar} size="xs" />
                  <span className="min-w-0 truncate">{name}</span>
                </Button>
              </li>,
            ];
          })}
        </ul>
      ) : (
        <p className="text-content-secondary type-ui py-2">
          {t(term ? "noMatchingPartners" : "noPartners")}
        </p>
      )}
      {canPaginate && !searching ? (
        <div className="flex items-center justify-between gap-2">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={page <= 1}
            onClick={() => {
              searchInput()?.focus();
              setSearch({ term, page: page - 1 });
            }}
          >
            {t("previous")}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={!hasNext}
            onClick={() => {
              searchInput()?.focus();
              setSearch({ term, page: page + 1 });
            }}
          >
            {t("next")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
