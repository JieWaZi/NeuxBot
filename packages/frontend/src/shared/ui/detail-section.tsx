import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { Card } from "./card";
import { SurfacePatternPanel } from "./surface-pattern-panel";

type DetailSectionProps = Omit<ComponentProps<"section">, "title"> & {
  title: ReactNode;
  actions?: ReactNode;
  /** plain 用于独立状态；muted 用于配置；wash 用于资料阅读。 */
  surface?: "plain" | "muted" | "wash";
};

/** 详情分区统一标题、留白和单层表面，标题与操作始终位于内容上方。 */
export function DetailSection({
  title,
  actions,
  children,
  className,
  surface = "plain",
  ...props
}: DetailSectionProps) {
  return (
    <section className={cn("grid min-w-0 gap-4", className)} {...props}>
      <header className="min-h-control-md @container/page-header flex flex-wrap items-center justify-between gap-3">
        <h2 className="type-title min-w-0 font-semibold [overflow-wrap:anywhere]">
          {title}
        </h2>
        {actions ? (
          <div className="flex max-w-full flex-wrap items-center gap-2">
            {actions}
          </div>
        ) : null}
      </header>
      {surface === "wash" ? (
        <SurfacePatternPanel className="p-5" pattern="wash" surface="scene">
          {children}
        </SurfacePatternPanel>
      ) : surface === "muted" ? (
        <Card variant="section">{children}</Card>
      ) : (
        children
      )}
    </section>
  );
}
