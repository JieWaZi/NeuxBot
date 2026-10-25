import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import type { ComponentProps } from "react";
import { Check } from "lucide-react";
import { cn } from "@/shared/lib/cn";

// shadcn/ui RadioGroup composition, adapted to NeuxBot field tokens.
// Source: https://github.com/shadcn-ui/ui/blob/main/apps/v4/registry/new-york-v4/ui/radio-group.tsx
export function RadioGroup({
  className,
  ...props
}: ComponentProps<typeof RadioGroupPrimitive.Root>) {
  return (
    <RadioGroupPrimitive.Root
      data-slot="radio-group"
      className={cn("grid gap-2", className)}
      {...props}
    />
  );
}

export function RadioGroupItem({
  className,
  ...props
}: ComponentProps<typeof RadioGroupPrimitive.Item>) {
  return (
    <RadioGroupPrimitive.Item
      data-slot="radio-group-item"
      className={cn(
        "border-field-border bg-field text-control-highlight focus-visible:ring-ring focus-visible:ring-offset-canvas size-5 shrink-0 rounded-full border outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-45",
        className,
      )}
      {...props}
    >
      <RadioGroupPrimitive.Indicator className="grid place-items-center">
        <span className="bg-control-highlight size-2.5 rounded-full" />
      </RadioGroupPrimitive.Indicator>
    </RadioGroupPrimitive.Item>
  );
}

/** 分层选择浮层中的整行单选项，选中标记固定在右侧。 */
export function RadioGroupOption({
  children,
  className,
  ...props
}: ComponentProps<typeof RadioGroupPrimitive.Item>) {
  return (
    <RadioGroupPrimitive.Item
      className={cn(
        "type-ui hover:bg-muted focus-visible:bg-muted focus-visible:ring-ring data-[state=checked]:bg-selection-surface data-[state=checked]:text-content-primary min-h-row-compact flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset disabled:pointer-events-none disabled:opacity-45",
        className,
      )}
      {...props}
    >
      <span className="min-w-0 flex-1">{children}</span>
      <RadioGroupPrimitive.Indicator className="shrink-0">
        <Check className="size-icon-sm" strokeWidth={1.5} />
      </RadioGroupPrimitive.Indicator>
    </RadioGroupPrimitive.Item>
  );
}
