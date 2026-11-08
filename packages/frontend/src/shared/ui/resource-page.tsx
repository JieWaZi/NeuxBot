import {
  createContext,
  useContext,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { Slottable } from "@radix-ui/react-slot";

import { cn } from "@/shared/lib/cn";
import { Button } from "./button";
import { PageHeader } from "./page-header";
import { SearchField } from "./search-field";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  TooltipProvider,
} from "./tooltip";
import { Toolbar } from "./toolbar";

export function PageActionLabel({
  className,
  ...props
}: ComponentProps<"span">) {
  return (
    <span
      className={cn("hidden @[32rem]/page-header:inline", className)}
      {...props}
    />
  );
}

/**
 * 页面操作按语义决定样式与文案：维护类（edit / delete）仅图标，带文案的业务动作用 primary / secondary。
 * 详情页 PageHeader 操作区从左到右：状态（开关与说明）→ 维护 → 主 CTA（intent="primary"）在最右。
 */
export function PageAction({
  asChild = false,
  hitArea = "default",
  intent,
  appearance = "default",
  label,
  children,
  ...props
}: Omit<ComponentProps<typeof Button>, "size"> & {
  hitArea?: "default" | "comfortable";
  appearance?: "default" | "catalog";
  intent?: "primary" | "secondary" | "edit" | "delete" | "revoke";
  label?: string;
}) {
  const accessibleLabel = label ?? props["aria-label"];
  const comfortable = Boolean(label) || hitArea === "comfortable";
  const iconOnly = intent === "edit" || intent === "delete";
  const variant =
    appearance === "catalog" && intent !== "delete" && intent !== "revoke"
      ? intent === "primary"
        ? "catalog-primary"
        : intent === "edit"
          ? "catalog-ghost"
          : "catalog-secondary"
      : intent === "delete" || intent === "revoke"
        ? "danger-quiet"
        : intent === "edit"
          ? "ghost"
          : intent === "primary"
            ? "primary"
            : intent === "secondary"
              ? "secondary"
              : label
                ? "secondary"
                : props.variant;
  const action = (
    <Button
      {...props}
      asChild={asChild}
      variant={variant}
      aria-label={accessibleLabel}
      data-page-action={comfortable ? "true" : undefined}
      data-variant={comfortable ? (variant ?? "primary") : undefined}
      className={cn(
        appearance !== "catalog" &&
          (iconOnly
            ? "w-control-md gap-0 px-0"
            : "gap-2 px-2 @[32rem]/page-header:px-3"),
        comfortable && appearance !== "catalog" && "page-action min-w-9",
        props.className,
      )}
      size={
        appearance === "catalog"
          ? iconOnly
            ? "catalog-icon"
            : "catalog"
          : "md"
      }
    >
      {asChild ? <Slottable>{children}</Slottable> : children}
      {label && !iconOnly ? (
        <span className="whitespace-nowrap">{label}</span>
      ) : null}
    </Button>
  );
  return accessibleLabel && comfortable ? (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>{action}</TooltipTrigger>
        <TooltipContent>{accessibleLabel}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    action
  );
}

export function ResourcePageHeader({
  title,
  description,
  actions,
  appearance = "default",
}: {
  title: string;
  description: ReactNode;
  actions?: ReactNode;
  appearance?: "default" | "catalog";
}) {
  if (appearance === "catalog")
    return (
      <header className="text-resource-content grid min-w-0 gap-1 pb-6">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <h1 className="type-resource-title font-semibold">{title}</h1>
          {actions}
        </div>
        {description && (
          <div className="type-label text-resource-muted">{description}</div>
        )}
      </header>
    );
  return (
    <PageHeader title={title} description={description} actions={actions} />
  );
}

export function ResourcePageToolbar({
  children,
  filters,
}: {
  children?: ReactNode;
  filters?: ReactNode;
}) {
  return (
    <Toolbar
      size="md"
      className="pb-2"
      leading={children}
      trailing={filters}
      leadingClassName="max-w-full min-w-0"
      trailingClassName="basis-full @[36rem]/resource-page:basis-0"
    />
  );
}

export function ResourcePageSearchField(
  props: Omit<
    ComponentProps<typeof SearchField>,
    "containerClassName" | "size" | "width"
  >,
) {
  return <SearchField {...props} size="sm" width="toolbar" />;
}

const ResourcePageFooterHost = createContext<HTMLDivElement | null | undefined>(
  undefined,
);

export function ResourcePageSurface({
  children,
  className,
  contentClassName,
  contentLayout = "scroll",
  appearance = "default",
  ...props
}: ComponentProps<"div"> & {
  contentClassName?: string;
  contentLayout?: "scroll" | "pane" | "form";
  appearance?: "default" | "catalog";
}) {
  const [footerHost, setFooterHost] = useState<HTMLDivElement | null>(null);
  return (
    <ResourcePageFooterHost.Provider value={footerHost}>
      <div
        data-slot="resource-page-surface"
        data-appearance={appearance}
        data-content-layout={contentLayout}
        className={cn(
          "@container/resource-page flex h-full min-h-0 min-w-0 flex-col overflow-hidden",
          appearance === "catalog"
            ? "bg-resource-canvas text-resource-content"
            : "bg-surface-scene",
          className,
        )}
        {...props}
      >
        <div
          data-slot="resource-page-viewport"
          className={cn(
            "min-h-0 flex-1 px-4 @[42.5rem]/resource-page:px-6",
            appearance === "catalog"
              ? cn(
                  "pt-resource-content-top",
                  contentLayout !== "pane" && "pb-8",
                )
              : "pt-shell-content-top",
            contentLayout !== "pane"
              ? cn(
                  "scrollbar-hidden scroll-pb-scene-bottom overflow-y-auto",
                  appearance === "default" && "pb-scene-bottom",
                )
              : "overflow-hidden",
            contentClassName,
          )}
        >
          {appearance === "catalog" ? (
            <div
              data-slot="resource-page-content"
              className={cn(
                "max-w-resource-catalog @container/resource-catalog mx-auto w-full min-w-0",
                contentLayout === "form" && "flex min-h-full flex-col",
                contentLayout === "pane" && "flex h-full min-h-0 flex-col",
              )}
            >
              {children}
            </div>
          ) : (
            children
          )}
        </div>
        <div
          ref={setFooterHost}
          data-slot="resource-page-footer-host"
          className="shrink-0"
        />
      </div>
    </ResourcePageFooterHost.Provider>
  );
}

/** 保存操作挂到所属页面底部，与正文滚动区分开；弹窗保留自身 footer。 */
export function ResourcePageFooter({
  children,
  className,
  appearance = "page",
  ...props
}: ComponentProps<"footer"> & { appearance?: "page" | "dialog" }) {
  const host = useContext(ResourcePageFooterHost);
  const footer = (
    <footer
      data-slot="resource-page-footer"
      {...props}
      className={cn(
        "shrink-0 px-4 @[42.5rem]/resource-page:px-6",
        appearance === "page"
          ? "bg-resource-surface border-resource-border min-h-15 border-t py-3"
          : "py-4",
        className,
      )}
    >
      <div className="max-w-resource-catalog mx-auto flex w-full min-w-0 flex-wrap items-center justify-between gap-3">
        {children}
      </div>
    </footer>
  );
  if (appearance === "dialog" || host === undefined) return footer;
  return host ? createPortal(footer, host) : null;
}

export function ResourceCardGrid({
  className,
  appearance = "default",
  ...props
}: ComponentProps<"div"> & { appearance?: "default" | "catalog" }) {
  return (
    <div
      className={cn(
        "grid grid-cols-1",
        appearance === "catalog"
          ? "gap-x-6 gap-y-1 @[41.5rem]/resource-catalog:grid-cols-2"
          : "gap-3 @[36rem]/resource-page:grid-cols-2 @[56rem]/resource-page:grid-cols-3",
        className,
      )}
      {...props}
    />
  );
}

export function ResourcePageLibraryLabel({
  installed,
  count,
}: {
  installed: string;
  count: string;
}) {
  return (
    <div className="flex min-w-max items-center gap-3 whitespace-nowrap">
      <h2 className="type-body font-medium">{installed}</h2>
      <span className="text-content-secondary type-caption">{count}</span>
    </div>
  );
}
