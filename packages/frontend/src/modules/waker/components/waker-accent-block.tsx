import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

import { WakerPatternPanel } from "./waker-pattern-panel";

type WakerAccentBlockProps = ComponentProps<"div"> & {
  children: ReactNode;
  /** 工作方式等多行条目时使用 */
  divided?: boolean;
};

/** 概览页灰底内容块：圆角、点阵纹理，落在白底分组上 */
export function WakerAccentBlock({
  children,
  className,
  divided = false,
  ...props
}: WakerAccentBlockProps) {
  return (
    <WakerPatternPanel
      className={cn("rounded-lg", !divided && "p-4", className)}
      pattern="dots"
      surface="card"
      {...props}
    >
      {divided ? (
        <div className="divide-border-subtle divide-y [&>*]:p-4">
          {children}
        </div>
      ) : (
        children
      )}
    </WakerPatternPanel>
  );
}

type WakerAccentSectionProps = {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
};

/** 标题 + 可选说明/操作 + 内容区（通常包在 {@link WakerAccentBlock} 内） */
export function WakerAccentSection({
  title,
  description,
  actions,
  children,
  className,
  contentClassName,
}: WakerAccentSectionProps) {
  return (
    <section className={className}>
      <header className="flex min-w-0 flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="type-label font-semibold">{title}</h2>
          {description ? (
            <p className="text-content-secondary type-caption mt-1">
              {description}
            </p>
          ) : null}
        </div>
        {actions}
      </header>
      <div className={cn("mt-3 min-w-0", contentClassName)}>{children}</div>
    </section>
  );
}
