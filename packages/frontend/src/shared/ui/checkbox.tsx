import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import { cva, type VariantProps } from "class-variance-authority";
import { Check, Minus } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

const checkboxVariants = cva(
  "border-field-border bg-field text-control-highlight-foreground duration-quick ease-ui focus-visible:ring-ring focus-visible:ring-offset-canvas shrink-0 rounded-sm border transition-[background-color,border-color,box-shadow] outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-45 data-[state=checked]:border-control-highlight data-[state=checked]:bg-control-highlight data-[state=indeterminate]:border-control-highlight data-[state=indeterminate]:bg-control-highlight",
  {
    variants: {
      size: {
        sm: "size-4 [&_svg]:size-icon-xs",
        md: "size-5 [&_svg]:size-icon-sm",
      },
    },
    defaultVariants: {
      size: "md",
    },
  },
);

type CheckboxProps = ComponentProps<typeof CheckboxPrimitive.Root> &
  VariantProps<typeof checkboxVariants>;

export function Checkbox({ className, size, ...props }: CheckboxProps) {
  return (
    <CheckboxPrimitive.Root
      className={cn(checkboxVariants({ size }), className)}
      {...props}
    >
      <CheckboxPrimitive.Indicator className="grid place-items-center">
        {props.checked === "indeterminate" ? (
          <Minus strokeWidth={1.5} />
        ) : (
          <Check strokeWidth={1.5} />
        )}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}
