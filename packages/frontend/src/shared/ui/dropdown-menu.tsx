import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import { Check, ChevronRight } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

export const DropdownMenu = DropdownMenuPrimitive.Root;
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;
export const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup;
export const DropdownMenuSub = DropdownMenuPrimitive.Sub;

const menuContentClass =
  "border-border-default bg-surface-raised origin-(--radix-dropdown-menu-content-transform-origin) z-50 max-h-[min(320px,var(--radix-dropdown-menu-content-available-height))] max-w-[calc(100vw-32px)] min-w-[160px] overflow-y-auto overscroll-contain rounded-lg border p-1 shadow-menu";

export function DropdownMenuContent({
  children,
  className,
  collisionPadding = 16,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        data-motion="popup"
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn(menuContentClass, className)}
        {...props}
      >
        <NativeOverlayPresence />
        {children}
      </DropdownMenuPrimitive.Content>
    </DropdownMenuPrimitive.Portal>
  );
}

export function DropdownMenuSubContent({
  children,
  className,
  collisionPadding = 16,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.SubContent>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.SubContent
        data-motion="popup"
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn(menuContentClass, className)}
        {...props}
      >
        <NativeOverlayPresence />
        {children}
      </DropdownMenuPrimitive.SubContent>
    </DropdownMenuPrimitive.Portal>
  );
}

const menuItemClass =
  "type-ui min-h-row-compact flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 outline-none select-none [&>svg]:size-icon-sm data-[disabled]:pointer-events-none data-[disabled]:opacity-45 data-[state=checked]:bg-selection-surface data-[state=checked]:text-content-primary data-[highlighted]:bg-muted data-[highlighted]:text-foreground";

export function DropdownMenuLabel({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Label>) {
  return (
    <DropdownMenuPrimitive.Label
      className={cn(
        "border-border text-muted-foreground type-caption min-h-row-compact -mx-1 mb-1 flex items-center border-b px-3",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      className={cn("bg-border -mx-1 my-1 h-px", className)}
      {...props}
    />
  );
}

export function DropdownMenuItem({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Item>) {
  return (
    <DropdownMenuPrimitive.Item
      className={cn(menuItemClass, className)}
      {...props}
    />
  );
}

export function DropdownMenuCheckboxItem({
  children,
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.CheckboxItem>) {
  return (
    <DropdownMenuPrimitive.CheckboxItem
      className={cn(menuItemClass, className)}
      {...props}
    >
      {children}
      <DropdownMenuPrimitive.ItemIndicator className="ml-auto shrink-0">
        <Check className="size-icon-sm" strokeWidth={1.5} />
      </DropdownMenuPrimitive.ItemIndicator>
    </DropdownMenuPrimitive.CheckboxItem>
  );
}

export function DropdownMenuRadioItem({
  children,
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.RadioItem>) {
  return (
    <DropdownMenuPrimitive.RadioItem
      className={cn(menuItemClass, className)}
      {...props}
    >
      {children}
      <DropdownMenuPrimitive.ItemIndicator className="ml-auto shrink-0">
        <Check className="size-icon-sm" strokeWidth={1.5} />
      </DropdownMenuPrimitive.ItemIndicator>
    </DropdownMenuPrimitive.RadioItem>
  );
}

type DropdownMenuSubTriggerProps = ComponentProps<
  typeof DropdownMenuPrimitive.SubTrigger
> & {
  // value 是在子菜单标题右侧展示的当前选中值。
  value?: string;
};

export function DropdownMenuSubTrigger({
  children,
  className,
  value,
  ...props
}: DropdownMenuSubTriggerProps) {
  return (
    <DropdownMenuPrimitive.SubTrigger
      className={cn(menuItemClass, "data-[state=open]:bg-muted", className)}
      {...props}
    >
      <span className="[&>svg]:size-icon-sm flex min-w-0 flex-1 items-center gap-2 truncate [&>svg]:shrink-0">
        {children}
      </span>
      {value ? (
        <span className="text-muted-foreground type-caption max-w-[120px] min-w-0 shrink-0 truncate">
          {value}
        </span>
      ) : null}
      <ChevronRight
        className="text-muted-foreground size-icon-sm shrink-0"
        strokeWidth={1.5}
      />
    </DropdownMenuPrimitive.SubTrigger>
  );
}
