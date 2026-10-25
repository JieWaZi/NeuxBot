import { cva, type VariantProps } from "class-variance-authority";
import { ChevronDown } from "lucide-react";
import type { ButtonHTMLAttributes } from "react";

import { cn } from "@/shared/lib/cn";

export const fieldControlTriggerVariants = cva(
  "border-field-border bg-field duration-quick ease-ui flex items-center justify-between gap-2 rounded-md border text-foreground transition-[background-color,border-color,box-shadow] outline-none hover:border-field-border-hover hover:bg-field-hover focus:border-field-border-active focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50 [&>span]:min-w-0 [&>span]:truncate",
  {
    variants: {
      size: {
        sm: "h-control-sm px-2 type-ui",
        md: "h-control-md px-3 type-body",
        lg: "h-control-lg px-3 type-label",
      },
      width: {
        full: "w-full",
        filter:
          "min-w-field-filter-min max-w-field-filter basis-field-filter-min grow",
        dateRange: "max-w-field-date-range w-auto shrink-0",
      },
    },
    defaultVariants: {
      size: "md",
      width: "full",
    },
  },
);

export type FieldControlTriggerProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof fieldControlTriggerVariants> & {
    showChevron?: boolean;
  };

/** 与 SelectTrigger 相同外观，供 Popover / DropdownMenu 等表单型触发器复用。 */
export function FieldControlTrigger({
  children,
  className,
  showChevron = true,
  size,
  type = "button",
  width,
  ...props
}: FieldControlTriggerProps) {
  return (
    <button
      type={type}
      className={cn(fieldControlTriggerVariants({ size, width }), className)}
      {...props}
    >
      {children}
      {showChevron ? (
        <ChevronDown
          className="text-muted-foreground size-icon-sm shrink-0"
          strokeWidth={1.5}
          aria-hidden
        />
      ) : null}
    </button>
  );
}
