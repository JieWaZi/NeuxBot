import type { ComponentProps, ReactNode } from "react";
import { ArrowLeft } from "lucide-react";

import { cn } from "@/shared/lib/cn";
import { Button } from "./button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./tooltip";

type PageHeaderProps = Omit<ComponentProps<"header">, "title"> & {
  title: ReactNode;
  description?: ReactNode;
  eyebrow?: ReactNode;
  actions?: ReactNode;
  layout?: "list" | "detail" | "form";
  visual?: ReactNode;
  appearance?: "default" | "compact";
  status?: ReactNode;
  /** 由页面提供确定的列表入口；未提供时保留普通标题区。 */
  backAction?: { label: string; onClick: () => void };
};

function PageHeaderBack({
  action,
}: {
  action: NonNullable<PageHeaderProps["backAction"]>;
}) {
  return (
    <div
      data-slot="page-header-back"
      className="duration-standard ease-ui w-0 shrink-0 overflow-hidden opacity-0 transition-[width,opacity] group-focus-within/page-title:w-12 group-focus-within/page-title:opacity-100 group-hover/page-title:w-12 group-hover/page-title:opacity-100 motion-reduce:transition-none"
    >
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              aria-label={action.label}
              onClick={action.onClick}
              className="duration-standard ease-ui -translate-x-2 transition-[transform,background-color,color] group-focus-within/page-title:translate-x-0 group-hover/page-title:translate-x-0 focus-visible:ring-offset-0 focus-visible:ring-inset motion-reduce:transform-none motion-reduce:transition-none"
            >
              <ArrowLeft />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{action.label}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>
  );
}

function PageHeaderTitleBlock({
  backAction,
  description,
  eyebrow,
  title,
  visual,
}: Pick<
  PageHeaderProps,
  "backAction" | "description" | "eyebrow" | "title" | "visual"
>) {
  const content = (
    <div className={cn("min-w-0", backAction && "flex-1")}>
      {eyebrow ? (
        <div className="text-content-caption type-caption mb-1">{eyebrow}</div>
      ) : null}
      <h1 className="type-page-title font-semibold tracking-[-0.01em]">
        {title}
      </h1>
      {description ? (
        <div className="text-content-secondary type-body mt-1 break-words">
          {description}
        </div>
      ) : null}
    </div>
  );
  return backAction ? (
    <div
      data-slot="page-header-identity"
      className="group/page-title flex min-w-0 items-start"
    >
      <PageHeaderBack action={backAction} />
      {visual ? <div className="mr-4 shrink-0">{visual}</div> : null}
      {content}
    </div>
  ) : (
    content
  );
}

export function PageHeader({
  actions,
  appearance = "default",
  backAction,
  children,
  className,
  description,
  eyebrow,
  layout = "list",
  title,
  status,
  visual,
  ...props
}: PageHeaderProps) {
  if (appearance === "compact")
    return (
      <header
        className={cn(
          "@container/page-header flex min-w-0 flex-wrap items-center gap-3",
          className,
        )}
        {...props}
      >
        <div
          className={cn(
            "group/page-title flex min-w-0 items-center",
            layout === "form" ? "flex-auto" : "flex-1 basis-60",
          )}
        >
          {backAction && <PageHeaderBack action={backAction} />}
          {visual && <div className="mr-3 shrink-0">{visual}</div>}
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <h1 className="type-resource-heading text-resource-content min-w-0 font-semibold break-words">
                {title}
              </h1>
              {status}
            </div>
            {description && (
              <div className="type-resource-caption text-resource-muted mt-1 min-w-0">
                {description}
              </div>
            )}
          </div>
        </div>
        {actions && (
          <div className="ml-auto flex max-w-full flex-wrap items-center justify-end gap-2">
            {actions}
          </div>
        )}
        {children}
      </header>
    );
  if (layout === "detail" && visual) {
    return (
      <header
        className={cn(
          "@container/page-header flex flex-wrap items-start gap-4",
          className,
        )}
        {...props}
      >
        {!backAction ? <div className="shrink-0">{visual}</div> : null}
        <div className="min-w-0 flex-1 basis-80">
          <div className="flex flex-wrap items-start gap-x-6 gap-y-4">
            <div className="min-w-0 flex-1 basis-64">
              <PageHeaderTitleBlock
                backAction={backAction}
                visual={backAction ? visual : undefined}
                description={description}
                eyebrow={eyebrow}
                title={title}
              />
            </div>
            {actions ? (
              <div className="flex max-w-full shrink-0 flex-wrap items-center gap-2">
                {actions}
              </div>
            ) : null}
          </div>
          {children}
        </div>
      </header>
    );
  }

  return (
    <header
      className={cn(
        "@container/page-header flex flex-wrap items-start justify-between gap-4",
        layout === "list" && "min-h-20",
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "min-w-0 flex-1 basis-60",
          visual && "flex items-start gap-4",
        )}
      >
        {visual && !backAction ? (
          <div className="shrink-0">{visual}</div>
        ) : null}
        <div className="min-w-0 flex-1">
          <PageHeaderTitleBlock
            backAction={backAction}
            visual={backAction ? visual : undefined}
            description={description}
            eyebrow={eyebrow}
            title={title}
          />
          {children}
        </div>
      </div>
      {actions ? (
        <div className="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          {actions}
        </div>
      ) : null}
    </header>
  );
}
