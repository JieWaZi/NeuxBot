import * as HoverCardPrimitive from "@radix-ui/react-hover-card";
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

// shadcn/ui HoverCard composition, adapted to NeuxBot tokens and native overlays.
// Source: https://github.com/shadcn-ui/ui/blob/main/apps/v4/registry/new-york-v4/ui/hover-card.tsx
export const HoverCard = HoverCardPrimitive.Root;
export const HoverCardTrigger = HoverCardPrimitive.Trigger;

export function HoverCardContent({
  className,
  align = "center",
  sideOffset = 6,
  children,
  ...props
}: ComponentProps<typeof HoverCardPrimitive.Content>) {
  return (
    <HoverCardPrimitive.Portal>
      <HoverCardPrimitive.Content
        data-slot="hover-card-content"
        data-motion="popup"
        align={align}
        sideOffset={sideOffset}
        className={cn(
          "border-border-default bg-surface-raised shadow-menu z-50 max-h-[min(20rem,var(--radix-hover-card-content-available-height))] w-100 max-w-[calc(100vw-32px)] origin-(--radix-hover-card-content-transform-origin) overflow-y-auto overscroll-contain rounded-2xl border p-5 outline-none",
          className,
        )}
        {...props}
      >
        <NativeOverlayPresence />
        {children}
      </HoverCardPrimitive.Content>
    </HoverCardPrimitive.Portal>
  );
}
