import { ChevronDown } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";

type ActivityTone = "neutral" | "info" | "success" | "warning" | "danger";

const toneClass: Record<ActivityTone, string> = {
  neutral: "bg-surface-tertiary text-content-secondary",
  info: "bg-status-info-surface text-status-info-content",
  success: "bg-status-success-surface text-status-success-content",
  warning: "bg-status-warning-surface text-status-warning-content",
  danger: "bg-status-danger-surface text-status-danger-content",
};

type ActivityItemProps = Omit<ComponentProps<"div">, "title"> & {
  title: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  actions?: ReactNode;
  tone?: ActivityTone;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
};

export function ActivityItem({
  actions,
  children,
  className,
  description,
  expanded,
  icon,
  onExpandedChange,
  title,
  tone = "neutral",
  ...props
}: ActivityItemProps) {
  const expandable = children !== undefined;
  const header = (
    <div className="min-h-row-default grid grid-cols-[28px_minmax(0,1fr)_auto] items-center gap-2 px-2">
      <span
        className={cn(
          "size-control-sm [&_svg]:size-icon-sm grid place-items-center rounded-md",
          toneClass[tone],
        )}
      >
        {icon}
      </span>
      <span className="min-w-0 text-left">
        <span className="type-body block truncate font-medium">{title}</span>
        {description ? (
          <span className="text-content-secondary type-caption block truncate">
            {description}
          </span>
        ) : null}
      </span>
      <span className="flex shrink-0 items-center gap-1">
        {actions}
        {expandable ? (
          <ChevronDown
            className="text-content-muted size-icon-sm transition-transform group-data-[state=open]/activity:rotate-180"
            strokeWidth={1.5}
            aria-hidden="true"
          />
        ) : null}
      </span>
    </div>
  );

  if (!expandable) {
    return (
      <div
        className={cn(
          "border-border-subtle bg-surface-raised rounded-lg border",
          className,
        )}
        {...props}
      >
        {header}
      </div>
    );
  }

  return (
    <Collapsible
      open={expanded}
      onOpenChange={onExpandedChange}
      className={cn(
        "group/activity border-border-subtle bg-surface-raised rounded-lg border",
        className,
      )}
      {...props}
    >
      <CollapsibleTrigger className="focus-visible:ring-ring w-full rounded-lg outline-none focus-visible:ring-2">
        {header}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="border-border-subtle border-t px-3 py-2">
          {children}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
