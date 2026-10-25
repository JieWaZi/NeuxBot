import * as SliderPrimitive from "@radix-ui/react-slider";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

// 基于 shadcn/ui Slider，保留 Radix 的拖动、键盘与可访问性行为。
// 来源：https://ui.shadcn.com/r/styles/new-york/slider.json
export function Slider({
  className,
  variant = "default",
  marks,
  thumbLabel,
  thumbValueText,
  highlighted = false,
  ...props
}: ComponentProps<typeof SliderPrimitive.Root> & {
  variant?: "default" | "stepped";
  marks?: readonly string[];
  thumbLabel?: string;
  thumbValueText?: string;
  highlighted?: boolean;
}) {
  return (
    <SliderPrimitive.Root
      data-stepped={variant === "stepped" || undefined}
      className={cn(
        "relative flex w-full touch-none items-center select-none data-[disabled]:opacity-45",
        variant === "stepped" ? "h-control-lg" : "h-control-md",
        className,
      )}
      {...props}
    >
      <SliderPrimitive.Track
        className={cn(
          "bg-muted relative w-full grow overflow-hidden rounded-full",
          variant === "stepped" ? "h-control-sm" : "h-1.5",
        )}
      >
        <SliderPrimitive.Range
          data-slot="slider-range"
          className={cn(
            "duration-standard ease-ui absolute h-full transition-[left,right,background-color] motion-reduce:transition-none",
            highlighted
              ? "bg-control-highlight/65"
              : variant === "stepped"
                ? "bg-control-highlight/50"
                : "bg-control-highlight",
          )}
        />
        {marks ? (
          <span aria-hidden className="pointer-events-none absolute inset-0">
            {marks.map((mark, index) => (
              <span
                key={mark}
                className="bg-content-secondary/35 absolute top-1/2 size-1 -translate-x-1/2 -translate-y-1/2 rounded-full"
                style={{
                  left: `calc(var(--spacing-icon-xl) / 2 + (100% - var(--spacing-icon-xl)) * ${index / Math.max(marks.length - 1, 1)})`,
                }}
              />
            ))}
          </span>
        ) : null}
      </SliderPrimitive.Track>
      <SliderPrimitive.Thumb
        data-slot="slider-thumb"
        aria-label={thumbLabel}
        aria-valuetext={thumbValueText}
        className={cn(
          "duration-standard ease-ui focus-visible:ring-ring focus-visible:ring-offset-canvas block shadow-xs transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:pointer-events-none motion-reduce:transition-none",
          variant === "stepped"
            ? "size-icon-xl rounded-full transition-[background-color,scale] hover:scale-110 motion-reduce:hover:scale-100"
            : "border-border-default size-4 rounded-full border",
          "bg-surface-raised",
        )}
      />
    </SliderPrimitive.Root>
  );
}
