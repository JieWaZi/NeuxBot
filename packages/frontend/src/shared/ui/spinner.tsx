import type { HTMLAttributes } from "react";

import { cn } from "@/shared/lib/cn";

export function Spinner({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "agent-grid-spinner text-muted-foreground size-icon-sm shrink-0",
        className,
      )}
      role="status"
      {...props}
    >
      {Array.from({ length: 9 }, (_, index) => (
        <span key={index} />
      ))}
    </div>
  );
}
