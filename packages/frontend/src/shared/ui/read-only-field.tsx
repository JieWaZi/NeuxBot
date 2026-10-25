import type { ReactNode } from "react";
import { cn } from "@/shared/lib/cn";

/** 详情中的只读字段：标签 12/16、正文 13/20，长路径与标识可完整换行。 */
export function ReadOnlyField({
  label,
  children,
  className,
  layout = "stacked",
}: {
  label: ReactNode;
  children: ReactNode;
  className?: string;
  layout?: "stacked" | "row";
}) {
  return (
    <div
      className={cn(
        "min-w-0",
        layout === "row" &&
          "grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] items-center gap-x-3",
        className,
      )}
    >
      <dt className="text-content-secondary type-ui">{label}</dt>
      <dd
        className={cn(
          "type-body min-w-0 [overflow-wrap:anywhere] whitespace-pre-wrap",
          layout === "stacked" && "mt-1",
        )}
      >
        {children}
      </dd>
    </div>
  );
}
