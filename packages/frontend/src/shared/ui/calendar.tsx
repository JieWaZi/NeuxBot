import { ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";
import type { ComponentProps } from "react";
import { DayPicker } from "react-day-picker";

import { cn } from "@/shared/lib/cn";

type CalendarProps = ComponentProps<typeof DayPicker>;

export function Calendar({
  className,
  classNames,
  showOutsideDays = true,
  ...props
}: CalendarProps) {
  return (
    <DayPicker
      showOutsideDays={showOutsideDays}
      className={cn("w-full", className)}
      classNames={{
        months: "relative flex flex-col",
        month: "grid gap-3",
        month_caption: "relative flex h-control-sm items-center justify-center",
        caption_label: "type-body font-medium",
        nav: "absolute inset-x-0 top-0 flex items-center justify-between",
        button_previous:
          "border-border bg-surface hover:bg-muted focus-visible:ring-ring z-10 grid size-control-sm place-items-center rounded-sm border outline-none focus-visible:ring-2 disabled:opacity-45",
        button_next:
          "border-border bg-surface hover:bg-muted focus-visible:ring-ring z-10 grid size-control-sm place-items-center rounded-sm border outline-none focus-visible:ring-2 disabled:opacity-45",
        month_grid: "w-full border-collapse",
        weekdays: "grid grid-cols-7",
        weekday:
          "text-muted-foreground type-caption grid h-control-sm place-items-center font-normal",
        week: "mt-1 grid grid-cols-7",
        day: "type-ui relative grid h-control-md w-full place-items-center p-0 text-center",
        day_button:
          "hover:bg-muted focus-visible:ring-ring grid size-8 place-items-center rounded-sm outline-none focus-visible:ring-2 aria-selected:opacity-100",
        range_start:
          "bg-muted rounded-l-sm [&>button]:!bg-primary [&>button]:!text-primary-foreground",
        range_middle:
          "bg-muted rounded-none [&>button]:!bg-transparent [&>button]:!text-foreground",
        range_end:
          "bg-muted rounded-r-sm [&>button]:!bg-primary [&>button]:!text-primary-foreground",
        selected: "[&>button]:bg-primary [&>button]:text-primary-foreground",
        today: "[&>button]:border-border-strong [&>button]:border",
        outside: "text-muted-foreground/55",
        disabled: "text-muted-foreground/45 pointer-events-none",
        hidden: "invisible",
        ...classNames,
      }}
      components={{
        Chevron: ({ orientation }) => {
          const Icon =
            orientation === "left"
              ? ChevronLeft
              : orientation === "right"
                ? ChevronRight
                : ChevronDown;
          return <Icon className="size-icon-sm" strokeWidth={1.5} />;
        },
      }}
      {...props}
    />
  );
}
