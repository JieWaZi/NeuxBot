import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

export function WorkspaceSurfaceHeader({
  className,
  ...props
}: ComponentProps<"header">) {
  return (
    <header
      className={cn(
        "border-border-subtle bg-surface-panel h-row-navigation flex min-w-0 shrink-0 items-center gap-1 border-b px-2",
        className,
      )}
      {...props}
    />
  );
}
