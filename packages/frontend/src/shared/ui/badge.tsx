import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 rounded-full border px-2 py-0.5 type-caption font-medium whitespace-nowrap transition-colors [&>svg]:size-icon-xs",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary/12 text-primary",
        destructive: "border-destructive/20 bg-destructive/10 text-destructive",
        secondary: "border-transparent bg-muted text-muted-foreground",
        outline: "border-border text-muted-foreground",
        success: "border-success/20 bg-success/12 text-success",
        warning: "border-warning/25 bg-warning/12 text-warning",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

type BadgeProps = ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean };

/** shadcn Badge：供紧凑状态、范围和策略标签复用。 */
export function Badge({
  asChild = false,
  className,
  variant,
  ...props
}: BadgeProps) {
  const Component = asChild ? Slot : "span";
  return (
    <Component
      data-slot="badge"
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  );
}
