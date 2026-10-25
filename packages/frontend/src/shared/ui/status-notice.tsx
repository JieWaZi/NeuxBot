import type { ComponentProps } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/shared/lib/cn";

const noticeVariants = cva("type-body rounded-lg border p-4", {
  variants: {
    tone: {
      warning:
        "border-status-warning-border bg-status-warning-surface text-content-secondary",
      success:
        "border-status-success-border bg-status-success-surface text-status-success-content",
    },
  },
  defaultVariants: { tone: "warning" },
});

/** 就近结果与提示共用容器层级，语义颜色表达当前状态。 */
export function StatusNotice({
  tone = "warning",
  className,
  ...props
}: ComponentProps<"div"> & VariantProps<typeof noticeVariants>) {
  return (
    <div
      role={tone === "success" ? "status" : undefined}
      className={cn(noticeVariants({ tone }), className)}
      {...props}
    />
  );
}
