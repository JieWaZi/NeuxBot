import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ButtonHTMLAttributes } from "react";

import { cn } from "@/shared/lib/cn";

export const buttonVariants = cva(
  "duration-quick ease-ui inline-flex items-center justify-center rounded-control font-medium transition-[background-color,border-color,color,box-shadow,opacity] outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-canvas disabled:pointer-events-none disabled:opacity-45",
  {
    variants: {
      variant: {
        primary:
          "bg-button-primary text-primary-foreground hover:bg-button-primary-hover active:bg-button-primary-pressed",
        secondary:
          "border-button-outline bg-button-fill text-button-content hover:bg-button-fill-hover active:bg-button-fill-pressed border",
        subtle:
          "bg-button-fill text-button-content hover:bg-button-fill-hover active:bg-button-fill-pressed",
        surface:
          "bg-surface-scene text-foreground hover:bg-action-quiet-hover active:bg-action-quiet-pressed",
        ghost:
          "text-action-quiet hover:bg-action-quiet-hover hover:text-foreground active:bg-action-quiet-pressed",
        text: "text-action-quiet hover:text-foreground active:text-foreground",
        link: "text-link hover:text-link-hover text-left whitespace-normal",
        // 说明性图标仅强调颜色，悬停与按下不增加背景表面。
        plain: "text-action-quiet hover:text-foreground active:text-foreground",
        choice:
          "text-foreground hover:bg-action-quiet-hover active:bg-action-quiet-pressed aria-pressed:bg-selection-surface",
        launcher:
          "bg-control-launcher text-control-launcher-content shadow-menu hover:bg-control-launcher-hover hover:text-control-launcher-content-hover active:bg-control-launcher-pressed justify-start gap-0 overflow-hidden",
        "danger-quiet":
          "text-destructive hover:bg-destructive/8 active:bg-destructive/12",
        danger:
          "bg-destructive text-destructive-foreground hover:bg-destructive/88",
        success: "bg-success text-success-foreground hover:bg-success/88",
        accent:
          "bg-accent-surface text-accent-content hover:bg-accent-surface-strong active:bg-accent-border",
      },
      size: {
        option:
          "min-h-control-sm w-full items-start justify-start gap-2 px-2 py-1.5 type-body [&_svg:not([data-companion-avatar])]:size-icon-md",
        row: "min-h-row-compact w-full items-center justify-start gap-3 px-0 py-1.5 type-body [&_svg:not([data-companion-avatar])]:size-icon-md",
        sm: "h-button-sm gap-1.5 px-3 type-ui [&_svg:not([data-companion-avatar])]:size-icon-xs",
        md: "h-button-md gap-1.5 px-4 type-ui [&_svg:not([data-companion-avatar])]:size-icon-sm",
        lg: "h-button-lg gap-2 px-5 type-body [&_svg:not([data-companion-avatar])]:size-icon-md",
        "icon-sm":
          "size-control-sm rounded-sm p-0 [&_svg:not([data-companion-avatar])]:size-icon-sm",
        icon: "size-control-md rounded-sm p-0 [&_svg:not([data-companion-avatar]):not([data-decoration])]:size-icon-md",
        dock: "size-dock-button rounded-full p-0 [&_svg:not([data-companion-avatar])]:size-icon-md",
        "icon-lg":
          "size-control-lg rounded-sm p-0 [&_svg:not([data-companion-avatar])]:size-icon-lg",
      },
      shape: {
        default: null,
        circle: "rounded-full",
        capsule: "rounded-2xl",
        corner: "rounded-none rounded-tl-lg",
      },
      attached: {
        start: "relative rounded-r-none focus-visible:z-10",
        end: "relative rounded-l-none focus-visible:z-10",
      },
    },
    compoundVariants: [
      {
        variant: "success",
        attached: "end",
        class: "border-l border-primary-foreground/20",
      },
    ],
    defaultVariants: {
      variant: "primary",
      size: "md",
      shape: "default",
    },
  },
);

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  };

export function Button({
  asChild = false,
  attached,
  className,
  shape,
  size,
  variant,
  ...props
}: ButtonProps) {
  const Component = asChild ? Slot : "button";
  return (
    <Component
      className={cn(
        buttonVariants({ attached, shape, size, variant }),
        className,
      )}
      {...props}
    />
  );
}
