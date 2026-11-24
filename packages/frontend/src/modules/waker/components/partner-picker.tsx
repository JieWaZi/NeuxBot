import { useQuery } from "@tanstack/react-query";
import { useEffect, useId, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { listWakersOptions, getWakerOptions } from "../api/waker-queries";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { RadioGroup, RadioGroupItem } from "@/shared/ui/radio-group";
import { Checkbox } from "@/shared/ui/checkbox";
import { Input } from "@/shared/ui/input";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import {
  FieldControlTrigger,
  type FieldControlTriggerProps,
} from "@/shared/ui/field-control-trigger";
import { QueryState, QueryRefreshError } from "@/shared/ui/query-state";
import { ResourcePartnerAvatar } from "@/shared/ui/resource-management-card";

export function PartnerPicker({
  selected,
  onChange,
  installed = [],
  onlyPartner,
  onNames,
  variant = "list",
  selectionMode = "multiple",
  label,
  triggerContent,
  triggerProps,
  disabled = false,
}: {
  variant?: "list" | "select";
  selectionMode?: "single" | "multiple";
  disabled?: boolean;
  label?: string;
  triggerContent?: ReactNode;
  triggerProps?: Pick<
    FieldControlTriggerProps,
    "appearance" | "size" | "width"
  >;
  selected: string[];
  onChange: (ids: string[]) => void;
  installed?: string[];
  onlyPartner?: string;
  onNames?: (names: Record<string, string>) => void;
}) {
  const { t } = useTranslation("waker");
  const [open, setOpen] = useState(false);
  const selectionLabel = useId();
  const showSettingsControl = triggerProps?.appearance === "settings";
  const confirmOnActivation = showSettingsControl && variant === "select";
  const needsData =
    variant === "list" ||
    open ||
    triggerContent === undefined ||
    Boolean(onNames);
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const library = useQuery({
    ...listWakersOptions(
      withLocalErrorSurface({
        query: { pageNum: page, pageSize: 50, query: search },
      }),
    ),
    enabled: needsData && !onlyPartner,
  });
  const owner = useQuery({
    ...getWakerOptions(
      withLocalErrorSurface({ path: { wakerId: onlyPartner ?? "pending" } }),
    ),
    enabled: needsData && Boolean(onlyPartner),
  });
  const query = onlyPartner ? owner : library;
  const partners = onlyPartner
    ? owner.data
      ? [owner.data]
      : []
    : (library.data?.resources ?? []).filter((item) => item.id);
  useEffect(() => {
    const items = onlyPartner
      ? owner.data
        ? [owner.data]
        : []
      : (library.data?.resources ?? []);
    onNames?.(
      Object.fromEntries(
        items
          .filter((item) => item.id)
          .map((item) => [item.id ?? "", item.name ?? ""]),
      ),
    );
  }, [onlyPartner, owner.data, library.data, onNames]);
  const content = (
    <div className="space-y-3">
      {!onlyPartner && (
        <Input
          aria-label={t("search")}
          value={search}
          placeholder={t("search")}
          disabled={disabled}
          onChange={(event) => {
            setSearch(event.target.value);
            setPage(1);
          }}
        />
      )}
      {!query.data ? (
        <QueryState
          loadingLayout="content"
          pending={query.isPending}
          error={query.error}
          onRetry={() => void query.refetch()}
        />
      ) : partners.length === 0 ? (
        <p className="text-muted-foreground type-body">
          {t("picker.noPartners")}
        </p>
      ) : (
        <PartnerChoiceList
          mode={selectionMode}
          selected={selected[0] ?? ""}
          onChange={(id) => {
            onChange([id]);
            if (variant === "select") setOpen(false);
          }}
        >
          {partners.map((partner) => {
            const id = partner.id;
            if (!id) return null;
            const existing = installed.includes(id);
            return (
              <label
                key={id}
                className="hover:bg-surface-subtle min-h-control-md rounded-control flex cursor-pointer items-center gap-3 px-2 py-2"
              >
                {selectionMode === "single" ? (
                  <RadioGroupItem
                    value={id}
                    aria-label={partner.name}
                    disabled={existing || disabled}
                    // Radix usually selects the radio that receives arrow-key focus.
                    onFocus={
                      confirmOnActivation
                        ? (event) => event.preventDefault()
                        : undefined
                    }
                    onKeyDown={
                      confirmOnActivation
                        ? (event) => {
                            if (event.key !== "Enter") return;
                            event.preventDefault();
                            event.currentTarget.click();
                          }
                        : undefined
                    }
                    onClick={
                      confirmOnActivation && selected.includes(id)
                        ? () => setOpen(false)
                        : undefined
                    }
                  />
                ) : (
                  <Checkbox
                    aria-label={partner.name}
                    checked={existing || selected.includes(id)}
                    disabled={existing || disabled}
                    onCheckedChange={(checked) =>
                      onChange(
                        checked
                          ? [...selected, id]
                          : selected.filter((value) => value !== id),
                      )
                    }
                  />
                )}
                <ResourcePartnerAvatar
                  name={partner.name ?? ""}
                  avatar={partner.avatar}
                  size="xs"
                />
                <span className="type-body min-w-0 flex-1 truncate">
                  {partner.name}
                </span>
                {existing && (
                  <span className="type-caption text-muted-foreground">
                    {t("picker.installed")}
                  </span>
                )}
              </label>
            );
          })}
        </PartnerChoiceList>
      )}
      {query.data && query.isError && (
        <QueryRefreshError
          error={query.error}
          pending={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      )}
      {!onlyPartner && (library.data?.totalSize ?? 0) > 50 && (
        <div className="flex items-center justify-between gap-3">
          <Button
            variant="ghost"
            disabled={disabled || page === 1}
            onClick={() => setPage(page - 1)}
          >
            {t("picker.previous")}
          </Button>
          <span className="type-caption">
            {t("picker.page", { page, count: library.data?.totalSize })}
          </span>
          <Button
            variant="ghost"
            disabled={disabled || page * 50 >= (library.data?.totalSize ?? 0)}
            onClick={() => setPage(page + 1)}
          >
            {t("picker.next")}
          </Button>
        </div>
      )}
    </div>
  );
  if (variant === "list") return content;
  const names = partners
    .filter((partner) => selected.includes(partner.id ?? ""))
    .map((partner) => partner.name);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <FieldControlTrigger
          {...triggerProps}
          disabled={disabled}
          aria-label={label ?? t("picker.title")}
          aria-describedby={selected.length ? selectionLabel : undefined}
        >
          <span id={selectionLabel} className="inline-flex min-w-0">
            {triggerContent ??
              (selected.length
                ? names.length === selected.length
                  ? names.join("、")
                  : t("picker.selectedPartners", { count: selected.length })
                : t(
                    selectionMode === "single"
                      ? "picker.title"
                      : "picker.placeholder",
                  ))}
          </span>
        </FieldControlTrigger>
      </PopoverTrigger>
      <PopoverContent
        align={showSettingsControl ? "end" : "start"}
        width={showSettingsControl ? "compact" : "trigger"}
        variant={showSettingsControl ? "selection" : "default"}
        aria-label={label ?? t("picker.title")}
        className={showSettingsControl ? undefined : "max-h-80"}
      >
        {content}
      </PopoverContent>
    </Popover>
  );
}

function PartnerChoiceList({
  mode,
  selected,
  onChange,
  children,
}: {
  mode: "single" | "multiple";
  selected: string;
  onChange: (id: string) => void;
  children: ReactNode;
}) {
  return mode === "single" ? (
    <RadioGroup value={selected} onValueChange={onChange} className="space-y-1">
      {children}
    </RadioGroup>
  ) : (
    <div className="space-y-1">{children}</div>
  );
}
