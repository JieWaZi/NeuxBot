import type { ComponentProps, ReactNode } from "react";
import { Slot } from "@radix-ui/react-slot";
import { cn } from "@/shared/lib/cn";

/** 源端分类标题与轻量列表之间的统一间距。 */
export function ResourceCatalogGroup({
  title,
  count,
  actions,
  children,
  className,
  ...props
}: Omit<ComponentProps<"section">, "title"> & {
  title: ReactNode;
  count?: number;
  actions?: ReactNode;
}) {
  return (
    <section
      className={cn(
        "border-resource-border grid min-w-0 gap-3 border-t pt-4",
        className,
      )}
      {...props}
    >
      <div className="flex min-w-0 items-center justify-between gap-2">
        <h2 className="type-resource-row-title text-resource-content flex min-w-0 items-center gap-2 font-semibold">
          {title}
          {count != null && (
            <span className="type-resource-body text-resource-muted font-normal">
              {count}
            </span>
          )}
        </h2>
        {actions && <div className="shrink-0">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

/** 详情 Card 中共享的行尺寸；交互由真实链接或按钮提供。 */
export function ResourceCatalogRow({
  asChild = false,
  className,
  ...props
}: ComponentProps<"div"> & { asChild?: boolean }) {
  const Component = asChild ? Slot : "div";
  return (
    <Component
      className={cn(
        "catalog-focus text-resource-content relative flex min-h-16 min-w-0 items-center gap-4 px-4 py-3",
        className,
      )}
      {...props}
    />
  );
}

export function ResourceCatalogRows({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "[&>*+*]:before:border-resource-border min-w-0 [&>*+*]:before:absolute [&>*+*]:before:inset-x-4 [&>*+*]:before:top-0 [&>*+*]:before:border-t",
        className,
      )}
      {...props}
    />
  );
}

export function ResourceCatalogFields({
  className,
  ...props
}: ComponentProps<"dl">) {
  return (
    <dl
      className={cn(
        "grid min-w-0 grid-cols-[minmax(120px,max-content)_minmax(0,1fr)] gap-x-6 gap-y-2",
        className,
      )}
      {...props}
    />
  );
}

export function ResourceCatalogField({
  label,
  children,
}: {
  label: ReactNode;
  children: ReactNode;
}) {
  return (
    <>
      <dt className="type-resource-caption text-resource-muted min-w-0 [overflow-wrap:anywhere]">
        {label}
      </dt>
      <dd className="type-resource-body text-resource-content min-w-0 [overflow-wrap:anywhere]">
        {children}
      </dd>
    </>
  );
}

/** 安装事实和授权事实由调用方确定；仅负责源端状态槽排版。 */
export function ResourceCatalogStatus({
  children,
  className,
  pill = false,
  tone = "neutral",
  ...props
}: ComponentProps<"span"> & {
  pill?: boolean;
  tone?: "neutral" | "success" | "warning";
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center whitespace-nowrap",
        pill
          ? "type-resource-caption bg-resource-hover gap-1 rounded-full border border-transparent px-2 py-0.5 font-medium"
          : "type-resource-row-description h-control-catalog gap-1.5 px-2",
        tone === "success"
          ? "text-success"
          : tone === "warning"
            ? "text-warning"
            : "text-resource-muted",
        className,
      )}
      {...props}
    >
      {pill && (
        <span
          aria-hidden="true"
          className="size-1.5 rounded-full bg-current [[data-status=unavailable]_&]:border [[data-status=unavailable]_&]:border-current [[data-status=unavailable]_&]:bg-transparent"
        />
      )}
      {children}
    </span>
  );
}
