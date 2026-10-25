import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { Card } from "./card";

type OverviewCardProps = Omit<
  ComponentProps<"button">,
  "children" | "value"
> & {
  label: string;
  icon?: ReactNode;
  description?: string;
  value?: ReactNode;
  selected?: boolean;
};

/** 分类与统计共用右上方光晕，字重独立表达选中。 */
export function OverviewCard({
  className,
  label,
  icon,
  description,
  value,
  selected = false,
  role,
  ...props
}: OverviewCardProps) {
  const metric = value !== undefined;
  const hint = description ? (
    <span
      className={cn(
        "text-content-secondary type-ui duration-standard ease-ui min-w-0 translate-y-1 break-words opacity-0 transition-[opacity,transform] group-hover/overview:translate-y-0 group-hover/overview:opacity-100 group-focus-visible/overview:translate-y-0 group-focus-visible/overview:opacity-100 motion-reduce:translate-y-0 motion-reduce:transition-none",
        metric
          ? "ml-auto text-right"
          : "group-data-[state=selected]/overview:translate-y-0 group-data-[state=selected]/overview:opacity-100",
        !metric && icon && "pl-8",
      )}
    >
      {description}
    </span>
  ) : null;
  return (
    <Card
      asChild
      variant="section"
      className={cn(
        "group/overview focus-visible:ring-status-success-content relative isolate flex cursor-pointer flex-col gap-3 overflow-hidden p-4 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset disabled:pointer-events-none disabled:opacity-45",
        metric && "min-h-28",
        className,
      )}
    >
      <button
        type="button"
        role={role}
        aria-label={label}
        aria-description={description}
        aria-pressed={role === "radio" ? undefined : selected}
        data-state={selected ? "selected" : undefined}
        {...props}
      >
        <span
          aria-hidden="true"
          className="overview-card-glow duration-standard ease-ui pointer-events-none absolute inset-0 -z-10 opacity-0 transition-opacity group-hover/overview:opacity-100 group-focus-visible/overview:opacity-100 group-data-[state=selected]/overview:opacity-100 motion-reduce:transition-none"
        />
        <span className="flex min-w-0 items-center gap-3">
          {icon ? (
            <span
              aria-hidden="true"
              className="text-content-secondary [&_svg]:size-icon-lg shrink-0"
            >
              {icon}
            </span>
          ) : null}
          <span
            className={cn(
              "type-body min-w-0 flex-1 break-words",
              selected ? "font-semibold" : "font-medium",
            )}
          >
            {label}
          </span>
        </span>
        {metric ? (
          <span className="mt-auto flex min-w-0 flex-wrap items-end justify-between gap-x-3 gap-y-1">
            <span className="type-display min-w-0 font-semibold break-all tabular-nums">
              {value}
            </span>
            {hint}
          </span>
        ) : (
          hint
        )}
      </button>
    </Card>
  );
}
