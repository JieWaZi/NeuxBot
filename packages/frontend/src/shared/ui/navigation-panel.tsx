import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

export function NavigationPanel({
  className,
  ...props
}: ComponentProps<"nav">) {
  return (
    <nav
      className={cn(
        "bg-surface-chrome text-content-primary flex min-h-0 flex-col",
        className,
      )}
      {...props}
    />
  );
}

export function NavigationSection({
  className,
  children,
  title,
  titleClassName,
  ...props
}: Omit<ComponentProps<"section">, "title"> & {
  title?: ReactNode;
  titleClassName?: string;
}) {
  return (
    <section className={cn("px-2 py-2", className)} {...props}>
      {title ? (
        <div
          className={cn(
            "text-content-caption type-caption px-2 py-1",
            titleClassName,
          )}
        >
          {title}
        </div>
      ) : null}
      <div className="grid gap-0.5">{children}</div>
    </section>
  );
}

type NavigationItemProps = ComponentProps<"button"> & {
  active?: boolean;
  density?: "default" | "navigation";
  icon?: ReactNode;
  label: ReactNode;
  description?: ReactNode;
  trailing?: ReactNode;
};

export function NavigationItem({
  active = false,
  className,
  density = "default",
  description,
  icon,
  label,
  trailing,
  ...props
}: NavigationItemProps) {
  return (
    <button
      type="button"
      aria-current={active ? "page" : undefined}
      className={cn(
        "duration-quick ease-ui focus-visible:ring-ring [&_svg]:size-icon-md grid w-full grid-cols-[20px_minmax(0,1fr)_auto] items-center gap-2 rounded-md text-left transition-[background-color,color] outline-none focus-visible:ring-2",
        density === "navigation"
          ? "min-h-row-navigation px-3"
          : "min-h-row-default px-2",
        active
          ? "bg-selection-surface text-content-primary"
          : "text-content-secondary hover:bg-action-quiet-hover hover:text-content-primary",
        className,
      )}
      {...props}
    >
      <span className="grid place-items-center">{icon}</span>
      <span className="min-w-0">
        <span className="type-body block truncate font-medium">{label}</span>
        {description ? (
          <span className="text-content-caption type-caption block truncate">
            {description}
          </span>
        ) : null}
      </span>
      {trailing ? <span className="shrink-0">{trailing}</span> : null}
    </button>
  );
}
