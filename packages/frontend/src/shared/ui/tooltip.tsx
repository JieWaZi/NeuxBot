import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

export const Tooltip = TooltipPrimitive.Root;
export const TooltipProvider = TooltipPrimitive.Provider;
export const TooltipTrigger = TooltipPrimitive.Trigger;

export function TooltipContent({
  children,
  className,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        data-motion="presence"
        sideOffset={sideOffset}
        className={cn(
          "border-border-default bg-surface-raised type-ui text-foreground z-50 max-w-60 rounded-md border px-3 py-2 shadow-sm",
          className,
        )}
        {...props}
      >
        <NativeOverlayPresence />
        {children}
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  );
}
