import { zhCN } from "date-fns/locale";
import { CalendarDays } from "lucide-react";
import { useState } from "react";
import type { DateRange } from "react-day-picker";

import { localDateFromInput, localDateToInput } from "@/shared/lib/local-date";
import { Button } from "@/shared/ui/button";
import { Calendar } from "@/shared/ui/calendar";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";

type DateRangePickerLabels = {
  clear: string;
  empty: string;
  help: string;
  title: string;
  trigger: string;
};

type DateRangePickerProps = {
  className?: string;
  from: string;
  labels: DateRangePickerLabels;
  onChange: (from: string, to: string) => void;
  to: string;
};

const monthDayFormatter = new Intl.DateTimeFormat("zh-CN", {
  month: "long",
  day: "numeric",
});
const fullDateFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "long",
  day: "numeric",
});

function rangeLabel(from: Date | undefined, to: Date | undefined) {
  if (!from) return undefined;
  if (!to) return `${fullDateFormatter.format(from)}起`;
  if (from.getFullYear() === to.getFullYear()) {
    return `${monthDayFormatter.format(from)} – ${monthDayFormatter.format(to)}`;
  }
  return `${fullDateFormatter.format(from)} – ${fullDateFormatter.format(to)}`;
}

export function DateRangePicker({
  className,
  from,
  labels,
  onChange,
  to,
}: DateRangePickerProps) {
  const [open, setOpen] = useState(false);
  const [draftRange, setDraftRange] = useState<DateRange>();
  const [selectingEnd, setSelectingEnd] = useState(false);
  const fromDate = from ? localDateFromInput(from) : undefined;
  const toDate = to ? localDateFromInput(to) : undefined;
  const selected: DateRange | undefined = fromDate
    ? { from: fromDate, to: toDate }
    : undefined;
  const label = rangeLabel(fromDate, toDate) ?? labels.empty;

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) {
          setDraftRange(selected);
          setSelectingEnd(false);
        }
      }}
    >
      <PopoverTrigger asChild>
        <FieldControlTrigger
          type="button"
          size="sm"
          width="dateRange"
          showChevron={false}
          aria-label={labels.trigger}
          className={className}
        >
          <span className="truncate">{label}</span>
          <CalendarDays
            className="text-muted-foreground size-icon-sm shrink-0"
            strokeWidth={1.5}
            aria-hidden
          />
        </FieldControlTrigger>
      </PopoverTrigger>
      <PopoverContent align="end">
        <div className="mb-2">
          <p className="type-body font-medium">{labels.title}</p>
          <p className="text-muted-foreground type-caption mt-1">
            {labels.help}
          </p>
        </div>
        <Calendar
          mode="range"
          locale={zhCN}
          weekStartsOn={1}
          defaultMonth={toDate ?? fromDate ?? new Date()}
          selected={draftRange}
          onSelect={(_, triggerDate) => {
            if (!selectingEnd || !draftRange?.from) {
              setDraftRange({ from: triggerDate, to: undefined });
              setSelectingEnd(true);
              return;
            }
            const nextRange =
              triggerDate < draftRange.from
                ? { from: triggerDate, to: draftRange.from }
                : { from: draftRange.from, to: triggerDate };
            setDraftRange(nextRange);
            setSelectingEnd(false);
            onChange(
              localDateToInput(nextRange.from),
              localDateToInput(nextRange.to),
            );
            setOpen(false);
          }}
        />
        {from || to ? (
          <div className="border-border mt-2 flex justify-end border-t pt-2">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                onChange("", "");
                setOpen(false);
              }}
            >
              {labels.clear}
            </Button>
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}
