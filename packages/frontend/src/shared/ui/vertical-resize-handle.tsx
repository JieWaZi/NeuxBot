import type { KeyboardEventHandler, MouseEventHandler } from "react";

import { cn } from "@/shared/lib/cn";

type VerticalResizeHandleProps = {
  ariaLabel: string;
  appearance?: "line" | "hover";
  ariaValueMax?: number;
  ariaValueMin?: number;
  ariaValueNow?: number;
  className?: string;
  onKeyDown?: KeyboardEventHandler<HTMLDivElement>;
  onMouseDown: MouseEventHandler<HTMLDivElement>;
};

export function VerticalResizeHandle({
  ariaLabel,
  appearance = "line",
  ariaValueMax,
  ariaValueMin,
  ariaValueNow,
  className,
  onKeyDown,
  onMouseDown,
}: VerticalResizeHandleProps) {
  return (
    <div className={cn("group relative w-px shrink-0", className)}>
      <span
        className={cn(
          "group-hover:bg-primary/30 group-active:bg-primary/50 group-has-[:focus-visible]:bg-primary/50 absolute inset-0 transition-colors",
          appearance === "line" ? "bg-border" : "bg-transparent",
        )}
      />
      <div
        className="absolute inset-y-0 -right-1.5 -left-1.5 z-10 cursor-col-resize outline-none"
        role="separator"
        aria-orientation="vertical"
        aria-label={ariaLabel}
        aria-valuemax={ariaValueMax}
        aria-valuemin={ariaValueMin}
        aria-valuenow={ariaValueNow}
        tabIndex={onKeyDown ? 0 : undefined}
        onKeyDown={onKeyDown}
        onMouseDown={onMouseDown}
      />
    </div>
  );
}
