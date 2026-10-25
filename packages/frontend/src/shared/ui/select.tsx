import * as SelectPrimitive from "@radix-ui/react-select";
import type { VariantProps } from "class-variance-authority";
import { Check, ChevronDown, ChevronUp } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { fieldControlTriggerVariants } from "@/shared/ui/field-control-trigger";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

export const Select = SelectPrimitive.Root;
export const SelectValue = SelectPrimitive.Value;

type SelectTriggerProps = ComponentProps<typeof SelectPrimitive.Trigger> &
  VariantProps<typeof fieldControlTriggerVariants>;

export function SelectTrigger({
  children,
  className,
  size,
  width,
  ...props
}: SelectTriggerProps) {
  return (
    <SelectPrimitive.Trigger
      className={cn(fieldControlTriggerVariants({ size, width }), className)}
      {...props}
    >
      {children}
      <SelectPrimitive.Icon asChild>
        <ChevronDown
          className="text-muted-foreground size-icon-sm shrink-0"
          strokeWidth={1.5}
        />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  );
}

export function SelectContent({
  children,
  className,
  collisionPadding = 16,
  position = "popper",
  sideOffset = 6,
  ...props
}: ComponentProps<typeof SelectPrimitive.Content>) {
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        data-motion="popup"
        position={position}
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn(
          "border-border-default bg-surface-raised shadow-menu z-50 max-h-[min(320px,var(--radix-select-content-available-height,320px))] max-w-[calc(100vw-32px)] min-w-[min(var(--radix-select-trigger-width),calc(100vw-32px))] origin-(--radix-select-content-transform-origin) overflow-hidden rounded-md border",
          className,
        )}
        {...props}
      >
        <NativeOverlayPresence />
        <SelectPrimitive.ScrollUpButton className="text-muted-foreground bg-surface-elevated h-row-compact flex cursor-default items-center justify-center">
          <ChevronUp className="size-icon-sm" strokeWidth={1.5} />
        </SelectPrimitive.ScrollUpButton>
        <SelectPrimitive.Viewport className="p-1">
          {children}
        </SelectPrimitive.Viewport>
        <SelectPrimitive.ScrollDownButton className="text-muted-foreground bg-surface-elevated h-row-compact flex cursor-default items-center justify-center">
          <ChevronDown className="size-icon-sm" strokeWidth={1.5} />
        </SelectPrimitive.ScrollDownButton>
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  );
}

export function SelectItem({
  children,
  className,
  ...props
}: ComponentProps<typeof SelectPrimitive.Item>) {
  return (
    <SelectPrimitive.Item
      className={cn(
        "type-ui min-h-row-compact data-[state=checked]:bg-selection-surface data-[state=checked]:text-content-primary data-[highlighted]:bg-muted data-[highlighted]:text-foreground relative flex w-full cursor-pointer items-center rounded-md py-1 pr-8 pl-2 outline-none select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-45",
        className,
      )}
      {...props}
    >
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator className="absolute right-2 grid place-items-center">
        <Check className="size-icon-sm" strokeWidth={1.5} />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}
