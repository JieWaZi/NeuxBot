import * as ContextMenuPrimitive from "@radix-ui/react-context-menu";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { menuContentClass, menuItemClass } from "./dropdown-menu";
import { NativeOverlayPresence } from "./native-overlay";

export const ContextMenu = ContextMenuPrimitive.Root;
export function ContextMenuTrigger({
  onKeyDown,
  ...props
}: ComponentProps<typeof ContextMenuPrimitive.Trigger>) {
  return (
    <ContextMenuPrimitive.Trigger
      {...props}
      onKeyDown={(event) => {
        onKeyDown?.(event);
        if (event.defaultPrevented) return;
        if (
          event.key !== "ContextMenu" &&
          !(event.shiftKey && event.key === "F10")
        )
          return;
        event.preventDefault();
        const rect = event.currentTarget.getBoundingClientRect();
        event.currentTarget.dispatchEvent(
          new MouseEvent("contextmenu", {
            bubbles: true,
            cancelable: true,
            clientX: rect.left,
            clientY: rect.bottom,
          }),
        );
      }}
    />
  );
}

export function ContextMenuContent({
  children,
  className,
  collisionPadding = 16,
  ...props
}: ComponentProps<typeof ContextMenuPrimitive.Content>) {
  return (
    <ContextMenuPrimitive.Portal>
      <ContextMenuPrimitive.Content
        data-motion="popup"
        collisionPadding={collisionPadding}
        className={cn(
          menuContentClass,
          "max-h-[min(320px,var(--radix-context-menu-content-available-height))] origin-(--radix-context-menu-content-transform-origin)",
          className,
        )}
        {...props}
      >
        <NativeOverlayPresence />
        {children}
      </ContextMenuPrimitive.Content>
    </ContextMenuPrimitive.Portal>
  );
}

export function ContextMenuItem({
  className,
  ...props
}: ComponentProps<typeof ContextMenuPrimitive.Item>) {
  return (
    <ContextMenuPrimitive.Item
      className={cn(menuItemClass, className)}
      {...props}
    />
  );
}
