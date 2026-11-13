import * as ContextMenuPrimitive from "@radix-ui/react-context-menu";
import { ChevronRight } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { menuContentClass, menuItemClass } from "./dropdown-menu";
import { NativeOverlayPresence } from "./native-overlay";

export const ContextMenu = ContextMenuPrimitive.Root;
export const ContextMenuSub = ContextMenuPrimitive.Sub;

const contextMenuContentClass = cn(
  menuContentClass,
  "max-h-[min(320px,var(--radix-context-menu-content-available-height))] origin-(--radix-context-menu-content-transform-origin)",
);
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
        className={cn(contextMenuContentClass, className)}
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
  variant,
  ...props
}: ComponentProps<typeof ContextMenuPrimitive.Item> & {
  variant?: "destructive";
}) {
  return (
    <ContextMenuPrimitive.Item
      className={cn(
        menuItemClass,
        variant === "destructive" &&
          "text-destructive data-[highlighted]:bg-destructive/8 data-[highlighted]:text-destructive",
        className,
      )}
      {...props}
    />
  );
}

export function ContextMenuSeparator(
  props: ComponentProps<typeof ContextMenuPrimitive.Separator>,
) {
  return (
    <ContextMenuPrimitive.Separator
      className="bg-border -mx-1 my-1 h-px"
      {...props}
    />
  );
}

export function ContextMenuSubTrigger({
  children,
  className,
  ...props
}: ComponentProps<typeof ContextMenuPrimitive.SubTrigger>) {
  return (
    <ContextMenuPrimitive.SubTrigger
      className={cn(menuItemClass, "data-[state=open]:bg-muted", className)}
      {...props}
    >
      {children}
      <ChevronRight className="text-muted-foreground ml-auto shrink-0" />
    </ContextMenuPrimitive.SubTrigger>
  );
}

export function ContextMenuSubContent({
  children,
  className,
  collisionPadding = 16,
  ...props
}: ComponentProps<typeof ContextMenuPrimitive.SubContent>) {
  return (
    <ContextMenuPrimitive.Portal>
      <ContextMenuPrimitive.SubContent
        data-motion="popup"
        collisionPadding={collisionPadding}
        className={cn(contextMenuContentClass, className)}
        {...props}
      >
        <NativeOverlayPresence />
        {children}
      </ContextMenuPrimitive.SubContent>
    </ContextMenuPrimitive.Portal>
  );
}
