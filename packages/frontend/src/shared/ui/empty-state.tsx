import type { LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

type EmptyStateProps = Omit<ComponentProps<"div">, "title"> & {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  icon?: LucideIcon;
  density?: "compact" | "default";
};

export function EmptyState({
  actions,
  className,
  density = "default",
  description,
  icon: Icon,
  title,
  ...props
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        "grid place-items-center px-4 text-center",
        density === "compact" ? "min-h-44" : "min-h-64",
        className,
      )}
      {...props}
    >
      <div className="max-w-[400px]">
        {Icon ? (
          <span className="bg-surface-tertiary mx-auto grid size-10 place-items-center rounded-xl">
            <Icon
              className="text-content-muted size-icon-lg"
              strokeWidth={1.5}
              aria-hidden="true"
            />
          </span>
        ) : null}
        <p className="type-body mt-3 font-medium">{title}</p>
        {description ? (
          <div className="text-content-secondary type-caption mt-1">
            {description}
          </div>
        ) : null}
        {actions ? (
          <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
            {actions}
          </div>
        ) : null}
      </div>
    </div>
  );
}
