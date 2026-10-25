import * as PopoverPrimitive from "@radix-ui/react-popover";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

export const Popover = PopoverPrimitive.Root;
export const PopoverTrigger = PopoverPrimitive.Trigger;
export const PopoverAnchor = PopoverPrimitive.Anchor;
export const PopoverClose = PopoverPrimitive.Close;

type PopoverContentProps = ComponentProps<typeof PopoverPrimitive.Content> & {
  variant?: "default" | "navigation" | "summary" | "selection";
  width?: "default" | "compact" | "wide" | "trigger";
};

export function PopoverContent({
  align = "center",
  children,
  className,
  collisionPadding = 16,
  sideOffset = 6,
  variant = "default",
  width = "default",
  ...props
}: PopoverContentProps) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        data-motion="popup"
        align={align}
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn(
          "border-border-default bg-surface-raised shadow-menu z-50 max-h-[var(--radix-popover-content-available-height)] max-w-[calc(100vw-32px)] origin-(--radix-popover-content-transform-origin) overscroll-contain border outline-none",
          variant === "navigation"
            ? "flex min-h-0 w-auto overflow-hidden rounded-2xl p-1"
            : cn(
                "overflow-y-auto",
                variant === "summary"
                  ? "rounded-2xl p-5"
                  : variant === "selection"
                    ? "rounded-xl p-3"
                    : "rounded-lg p-3",
                width === "trigger"
                  ? "w-(--radix-popover-trigger-width)"
                  : width === "wide"
                    ? "w-100"
                    : width === "compact"
                      ? "w-64"
                      : "w-80",
              ),
          className,
        )}
        {...props}
      >
        <NativeOverlayPresence />
        {children}
      </PopoverPrimitive.Content>
    </PopoverPrimitive.Portal>
  );
}
