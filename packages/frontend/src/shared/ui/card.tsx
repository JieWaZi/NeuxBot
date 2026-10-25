import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

// shadcn/ui Card composition, adapted to NeuxBot surfaces and semantic containers.
// Source: https://github.com/shadcn-ui/ui/blob/main/apps/v4/registry/new-york-v4/ui/card.tsx
const cardVariants = cva("bg-card text-foreground min-w-0 rounded-xl", {
  variants: {
    variant: {
      default: "flex flex-col gap-5 py-5",
      section: "p-5",
      floating: "bg-surface-scene shadow-md flex flex-col gap-5 py-5",
      resource:
        "group/resource-card [--ring:var(--status-success-content)] duration-quick hover:bg-card-hover data-[state=selected]:bg-accent-surface flex flex-col overflow-hidden transition-colors motion-reduce:transition-none",
    },
  },
  defaultVariants: { variant: "default" },
});

export function Card({
  asChild = false,
  className,
  variant,
  ...props
}: ComponentProps<"div"> &
  VariantProps<typeof cardVariants> & { asChild?: boolean }) {
  const Component = asChild ? Slot : "div";
  return (
    <Component
      data-slot="card"
      className={cn(cardVariants({ variant }), className)}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "flex flex-wrap items-center justify-between gap-3 px-5",
        className,
      )}
      {...props}
    />
  );
}

export function CardTitle({ className, ...props }: ComponentProps<"h2">) {
  return (
    <h2
      data-slot="card-title"
      className={cn("type-title min-w-0 font-semibold", className)}
      {...props}
    />
  );
}

export function CardContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn("min-w-0 px-5", className)}
      {...props}
    />
  );
}

/** 内容区响应整张资源卡的悬停，避免覆盖链接或独立操作导致明度不一致。 */
export function CardBody({
  asChild = false,
  className,
  ...props
}: ComponentProps<"div"> & { asChild?: boolean }) {
  const Component = asChild ? Slot : "div";
  return (
    <Component
      data-slot="card-body"
      className={cn(
        "duration-quick group-hover/resource-card:bg-surface-subtle min-w-0 transition-colors motion-reduce:transition-none",
        className,
      )}
      {...props}
    />
  );
}

export function CardFooter({ className, ...props }: ComponentProps<"footer">) {
  return (
    <footer
      data-slot="card-footer"
      className={cn(
        "min-h-row-navigation mt-auto flex items-center gap-3 px-5",
        className,
      )}
      {...props}
    />
  );
}
