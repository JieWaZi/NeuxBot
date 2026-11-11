import { ChevronRight } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { PageHeader } from "@/shared/ui/page-header";

export function SettingsContentShell({
  actions,
  children,
  className,
  description,
  showHeader = true,
  title,
}: {
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  description?: ReactNode;
  showHeader?: boolean;
  title?: ReactNode;
}) {
  return (
    <div className={cn("flex w-full min-w-0 flex-col gap-6", className)}>
      {showHeader && title ? (
        <PageHeader
          layout="detail"
          title={title}
          description={description}
          actions={actions}
        />
      ) : title ? (
        <h1 className="sr-only">{title}</h1>
      ) : null}
      {children}
    </div>
  );
}

export function SettingsRow({
  children,
  className,
  content,
  description,
  grouped = false,
  title,
  ...props
}: Omit<ComponentProps<"section">, "title" | "content"> & {
  description?: ReactNode;
  grouped?: boolean;
  content?: ReactNode;
  title: ReactNode;
}) {
  return (
    <section
      className={cn(
        "min-w-0 px-4 py-3",
        grouped
          ? "before:bg-border-subtle relative before:absolute before:inset-x-4 before:top-0 before:h-px first:before:hidden"
          : "bg-resource-surface rounded-lg",
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "min-h-row-default flex flex-wrap justify-between gap-x-4 gap-y-3",
          description ? "items-start" : "items-center",
        )}
      >
        <div className="min-w-0 flex-1 basis-48">
          <h2 className="type-label text-content-primary font-medium">
            {title}
          </h2>
          {description ? (
            <p className="text-content-secondary type-resource-body mt-0.5">
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
    <section className={cn("flex min-w-0 flex-col gap-2", className)}>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <h3 className="text-content-secondary type-resource-body font-normal">
            {title}
          </h3>
          {description ? (
            <p className="text-content-secondary type-ui mt-1">{description}</p>
          ) : null}
        </div>
        {trailing}
      </div>
      {children}
    </section>
  );
}

/** 设置中的整行二级导航，与普通设置行共用密度、表面和焦点。 */
export function SettingsNavigationRow({
  title,
  description,
  visual,
  onClick,
  ariaLabel,
  trailing,
}: {
  title: ReactNode;
  description?: ReactNode;
  visual?: ReactNode;
  trailing?: ReactNode;
  onClick: () => void;
  ariaLabel?: string;
}) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      onClick={onClick}
      className="before:bg-border-subtle min-h-row-detail hover:bg-action-quiet-hover focus-visible:ring-ring relative flex w-full items-center gap-4 px-4 py-3 text-left outline-none before:absolute before:inset-x-4 before:top-0 before:h-px first:before:hidden focus-visible:ring-2 focus-visible:ring-inset"
    >
      {visual}
      <span className="min-w-0 flex-1">
        <span className="type-label text-content-primary block font-medium">
          {title}
        </span>
        {description ? (
          <span className="type-resource-body text-content-secondary block">
            {description}
          </span>
        ) : null}
      </span>
      {trailing}
      <ChevronRight
        aria-hidden="true"
        className="size-icon-md text-content-tertiary shrink-0"
      />
    </button>
  );
}

/** 设置分区唯一填充表面，子项各自拥有内缩分隔。 */
export function SettingsCard({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="settings-card"
      className={cn(
        "bg-resource-surface min-w-0 overflow-hidden rounded-lg",
        className,
      )}
      {...props}
    />
  );
}

/** 危险操作保留完整业务影响及所属动作，不引入另一套红色卡片。 */
export function SettingsDangerZone({
  title,
  actionTitle,
  description,
  action,
}: {
  title: ReactNode;
  actionTitle: ReactNode;
  description: ReactNode;
  action: ReactNode;
}) {
  return (
    <SettingsGroup title={title}>
      <SettingsCard>
        <SettingsRow grouped title={actionTitle} description={description}>
          {action}
        </SettingsRow>
      </SettingsCard>
    </SettingsGroup>
  );
}
