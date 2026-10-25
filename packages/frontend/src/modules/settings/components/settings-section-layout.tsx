import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { PageHeader } from "@/shared/ui/page-header";

export function SettingsContentShell({
  actions,
  children,
  className,
  description,
  title,
}: {
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  description?: ReactNode;
  title: ReactNode;
}) {
  return (
    <div className={cn("mx-auto w-full max-w-4xl", className)}>
      <PageHeader
        layout="detail"
        title={title}
        description={description}
        actions={actions}
      />
      <div className="mt-8 flex flex-col gap-8">{children}</div>
    </div>
  );
}

export function SettingsRow({
  children,
  className,
  content,
  description,
  title,
  ...props
}: Omit<ComponentProps<"section">, "title" | "content"> & {
  description?: ReactNode;
  content?: ReactNode;
  title: ReactNode;
}) {
  return (
    <section
      className={cn(
        "border-border border-t pt-8 first:border-t-0 first:pt-0",
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "flex flex-wrap justify-between gap-6",
          description ? "items-start" : "items-center",
        )}
      >
        <div className="min-w-0 flex-1 basis-60">
          <h2 className="type-label text-content-primary font-normal">
            {title}
          </h2>
          {description ? (
            <p className="text-content-secondary type-body mt-1">
              {description}
            </p>
          ) : null}
        </div>
        {children ? (
          <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
            {children}
          </div>
        ) : null}
      </div>
      {content}
    </section>
  );
}

export function SettingsGroup({
  children,
  className,
  description,
  title,
  trailing,
}: {
  children: ReactNode;
  className?: string;
  description?: ReactNode;
  title: ReactNode;
  trailing?: ReactNode;
}) {
  return (
    <section className={cn("flex flex-col gap-3", className)}>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <h3 className="text-content-secondary type-ui font-medium">
            {title}
          </h3>
          {description ? (
            <p className="text-content-secondary type-body mt-1">
              {description}
            </p>
          ) : null}
        </div>
        {trailing}
      </div>
      {children}
    </section>
  );
}
