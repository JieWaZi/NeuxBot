import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

type ToolbarProps = ComponentProps<"div"> & {
  leading?: ReactNode;
  trailing?: ReactNode;
  leadingClassName?: string;
  trailingClassName?: string;
  divided?: boolean;
  size?: "sm" | "md";
};

export function Toolbar({
  children,
  className,
  divided = true,
  size = "sm",
  leading,
  trailing,
  leadingClassName,
  trailingClassName,
  ...props
}: ToolbarProps) {
  return (
    <div
      className={cn(
        "pb-3",
        divided && "border-border-subtle border-b",
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          trailing
            ? "flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2"
            : "flex items-center",
          size === "md" ? "min-h-control-md" : "min-h-control-sm",
        )}
      >
        <div
          className={cn(
            "flex max-w-full min-w-0 shrink-0 flex-wrap items-center gap-3",
            size === "md" ? "min-h-control-md" : "min-h-control-sm",
            leadingClassName,
          )}
        >
          {leading ?? children}
        </div>
        {trailing ? (
          <div
            className={cn(
              "ml-auto flex max-w-full min-w-0 flex-1 flex-wrap items-center justify-end gap-2",
              trailingClassName,
            )}
          >
            {trailing}
          </div>
        ) : null}
      </div>
    </div>
  );
}
