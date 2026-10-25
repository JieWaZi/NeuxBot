import { Check } from "lucide-react";
import { useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { consumeOverlayWheelScroll } from "@/shared/lib/overlay-scroll-wheel";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";

const supportedZones = Intl.supportedValuesOf("timeZone");

/** 只选择 IANA 时区，保留历史别名；搜索内容不会成为表单值。 */
export function TimeZonePicker({
  id,
  value,
  onChange,
  required = false,
  disabled = false,
  "aria-label": ariaLabel,
}: {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
  disabled?: boolean;
  "aria-label"?: string;
}) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const listId = useId();
  const listRef = useRef<HTMLDivElement>(null);
  const zones = useMemo(
    () =>
      [
        ...new Set(["UTC", ...supportedZones, ...(value ? [value] : [])]),
      ].sort(),
    [value],
  );
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const matches = zones.filter((zone) =>
    zone
      .toLocaleLowerCase()
      .replaceAll("_", " ")
      .includes(normalizedQuery.replaceAll("_", " ")),
  );
  const focusOption = (index: number) => {
    const options =
      listRef.current?.querySelectorAll<HTMLButtonElement>("[role=option]");
    if (options?.length) {
      options[Math.max(0, Math.min(options.length - 1, index))]?.focus();
    }
  };
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery("");
      }}
    >
      <PopoverTrigger asChild>
        <FieldControlTrigger
          id={id}
          role="combobox"
          aria-label={ariaLabel || t("timeZonePicker.choose")}
          aria-required={required}
          aria-controls={open ? listId : undefined}
          aria-haspopup="listbox"
          aria-expanded={open}
          disabled={disabled}
        >
          <span>{value || t("timeZonePicker.choose")}</span>
        </FieldControlTrigger>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        width="trigger"
        className="flex flex-col p-2"
      >
        <SearchField
          width="full"
          containerClassName="shrink-0"
          aria-label={t("timeZonePicker.search")}
          placeholder={t("timeZonePicker.search")}
          clearLabel={t("timeZonePicker.clear")}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown") {
              event.preventDefault();
              focusOption(0);
            }
          }}
        />
        <div
          ref={listRef}
          id={listId}
          role="listbox"
          aria-label={ariaLabel || t("timeZonePicker.choose")}
          className="mt-2 max-h-56 min-h-0 overflow-y-auto overscroll-contain"
          onWheel={consumeOverlayWheelScroll}
        >
          {matches.map((zone, index) => (
            <button
              key={zone}
              type="button"
              role="option"
              aria-selected={value === zone}
              tabIndex={-1}
              className="type-ui hover:bg-action-quiet-hover focus:bg-action-quiet-hover text-content-primary min-h-row-compact flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1 text-left outline-none"
              onClick={() => {
                onChange(zone);
                setOpen(false);
                setQuery("");
              }}
              onKeyDown={(event) => {
                if (
                  ["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)
                ) {
                  event.preventDefault();
                  focusOption(
                    event.key === "Home"
                      ? 0
                      : event.key === "End"
                        ? matches.length - 1
                        : index + (event.key === "ArrowDown" ? 1 : -1),
                  );
                }
              }}
            >
              <span className="truncate">{zone}</span>
              {value === zone ? (
                <Check aria-hidden className="size-icon-sm shrink-0" />
              ) : null}
            </button>
          ))}
          {!matches.length ? (
            <p className="type-ui text-content-secondary px-2 py-3">
              {t("timeZonePicker.empty")}
            </p>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}
