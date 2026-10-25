import { cva, type VariantProps } from "class-variance-authority";
import type { InputHTMLAttributes } from "react";

import { cn } from "@/shared/lib/cn";

const inputVariants = cva(
  "placeholder:text-content-muted duration-quick ease-ui w-full border text-foreground transition-[background-color,border-color] outline-none aria-invalid:border-destructive aria-invalid:hover:border-destructive aria-invalid:focus:border-destructive disabled:opacity-50",
  {
    variants: {
      appearance: {
        default:
          "border-field-border bg-field hover:border-field-border-hover hover:bg-field-hover focus:border-field-border-active",
        quiet:
          "border-transparent bg-surface-panel hover:bg-field-hover focus:border-field-border-active",
      },
      shape: {
        default: "rounded-md",
        pill: "rounded-full",
      },
      size: {
        sm: "h-control-sm px-2 type-ui",
        md: "h-control-md px-3 type-body",
        lg: "h-control-lg px-3 type-label",
      },
    },
    defaultVariants: {
      appearance: "default",
      shape: "default",
      size: "sm",
    },
  },
);

type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "size"> &
  VariantProps<typeof inputVariants>;

export function Input({
  appearance,
  className,
  shape,
  size,
  ...props
}: InputProps) {
  return (
    <input
      className={cn(inputVariants({ appearance, shape, size }), className)}
      {...props}
    />
  );
}
