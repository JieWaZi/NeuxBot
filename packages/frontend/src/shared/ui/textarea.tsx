import { cva, type VariantProps } from "class-variance-authority";
import type { ReactNode, TextareaHTMLAttributes } from "react";

import { cn } from "@/shared/lib/cn";

const textareaVariants = cva(
  "border-field-border bg-field placeholder:text-content-muted duration-quick ease-ui w-full resize-none rounded-md border text-foreground transition-[background-color,border-color,box-shadow] outline-none aria-invalid:border-destructive aria-invalid:hover:border-destructive aria-invalid:focus:border-destructive hover:border-field-border-hover hover:bg-field-hover focus:border-field-border-active focus:ring-2 focus:ring-ring disabled:opacity-50",
  {
    variants: {
      appearance: {
        field: null,
        embedded:
          "rounded-none border-0 bg-transparent hover:bg-transparent focus:ring-0",
      },
      size: {
        sm: "min-h-20 px-2 py-2 type-ui",
        md: "min-h-24 px-3 py-2 type-body",
        lg: "min-h-30 px-3 py-3 type-label",
      },
    },
    defaultVariants: {
      size: "md",
      appearance: "field",
    },
  },
);

type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> &
  VariantProps<typeof textareaVariants> & {
    /** 复合指令输入的内部底栏；与文字输入共享同一个字段边界。 */
    footer?: ReactNode;
  };

export function Textarea({
  className,
  size,
  appearance = "field",
  footer,
  ...props
}: TextareaProps) {
  if (footer) {
    return (
      <div
        className={
          appearance === "field"
            ? "border-field-border bg-field duration-quick ease-ui hover:border-field-border-hover focus-within:border-field-border-active rounded-md border transition-[background-color,border-color]"
            : undefined
        }
      >
        <textarea
          className={cn(
            textareaVariants({ size, appearance }),
            "border-0 bg-transparent hover:bg-transparent focus:ring-0",
            className,
          )}
          {...props}
        />
        <div className="flex flex-wrap items-center justify-end gap-1 p-2">
          {footer}
        </div>
      </div>
    );
  }
  return (
    <textarea
      className={cn(textareaVariants({ size, appearance }), className)}
      {...props}
    />
  );
}
