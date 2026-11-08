import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

export function NavigationPanel({
  appearance = "default",
  title,
  className,
  children,
  ...props
}: Omit<ComponentProps<"nav">, "title"> & {
  appearance?: "default" | "catalog" | "settings";
  title?: ReactNode;
}) {
  return (
    <nav
      className={cn(
        "flex min-h-0 flex-col",
        appearance === "settings"
          ? "bg-surface-scene text-content-primary h-full gap-4"
          : appearance === "catalog"
            ? "bg-resource-canvas text-resource-content h-full gap-1 px-3 pt-4 pb-3"
            : "bg-surface-chrome text-content-primary",
        className,
      )}
      {...props}
    >
      {title && <h2 className="type-label mb-3 px-2 font-semibold">{title}</h2>}
      {children}
    </nav>
  );
}

export function NavigationSection({
  appearance = "default",
  className,
  children,
  title,
  titleClassName,
  ...props
}: Omit<ComponentProps<"section">, "title"> & {
  title?: ReactNode;
  titleClassName?: string;
  appearance?: "default" | "catalog" | "settings";
}) {
  return (
    <section
      className={cn(
        appearance !== "default" ? "min-w-0" : "px-2 py-2",
        className,
      )}
      {...props}
    >
      {title ? (
        <div
          className={cn(
            appearance === "settings"
              ? "text-content-secondary type-ui mb-2 px-3"
              : appearance === "catalog"
                ? "text-resource-muted type-resource-caption mt-4 mb-1 px-2"
                : "text-content-caption type-caption px-2 py-1",
            titleClassName,
          )}
        >
          {title}
        </div>
      ) : null}
      <div className={appearance !== "default" ? "grid gap-1" : "grid gap-0.5"}>
        {children}
      </div>
    </section>
  );
}

type NavigationItemProps = ComponentProps<"button"> & {
  active?: boolean;
  density?: "default" | "navigation" | "catalog" | "settings";
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
        "duration-quick ease-ui focus-visible:ring-ring grid w-full grid-cols-[20px_minmax(0,1fr)_auto] items-center gap-2 rounded-md text-left transition-[background-color,color] outline-none focus-visible:ring-2",
        density === "settings"
          ? "[&_svg]:size-icon-md min-h-control-hit-target px-3"
          : density === "catalog"
            ? "catalog-focus [&>span:first-child>svg]:size-icon-md min-h-row-catalog px-2"
            : density === "navigation"
              ? "[&_svg]:size-icon-md min-h-row-navigation px-3"
              : "[&_svg]:size-icon-md min-h-row-default px-2",
        density === "catalog" || density === "settings"
          ? active
            ? "bg-resource-hover text-resource-content"
            : "text-resource-secondary hover:bg-resource-hover hover:text-resource-content"
          : active
            ? "bg-selection-surface text-content-primary"
            : "text-content-secondary hover:bg-action-quiet-hover hover:text-content-primary",
        className,
      )}
      {...props}
    >
      <span className="grid place-items-center">{icon}</span>
      <span className="min-w-0">
        <span
          className={cn(
            "block truncate",
            density === "settings"
              ? "type-resource-body"
              : density === "catalog"
                ? "type-resource-body"
                : "type-body font-medium",
            (density === "catalog" || density === "settings") &&
              active &&
              "font-medium",
          )}
        >
          {label}
        </span>
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
