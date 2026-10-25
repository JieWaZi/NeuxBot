import { cva, type VariantProps } from "class-variance-authority";
import { useRef, type KeyboardEvent, type ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { OverviewCard } from "./overview-card";

const segmentedControlVariants = cva("items-stretch", {
  variants: {
    appearance: {
      segments:
        "bg-button-fill inline-flex w-fit gap-0.5 rounded-control p-0.5",
      cards:
        "grid w-full grid-cols-[repeat(auto-fit,minmax(min(100%,14rem),1fr))] gap-3",
      underline: "inline-flex min-w-0 items-stretch gap-4",
      resources:
        "grid w-full grid-cols-1 gap-3 @[36rem]/resource-page:grid-cols-2 @[56rem]/resource-page:grid-cols-3",
    },
    size: {
      sm: "type-ui",
      md: "type-body",
    },
    distribution: {
      content: "",
      fill: "w-full",
    },
  },
  compoundVariants: [
    { appearance: "segments", distribution: "fill", class: "flex" },
    { appearance: "segments", size: "sm", class: "h-control-sm" },
    {
      appearance: "segments",
      size: "md",
      class: "h-control-md p-[3px]",
    },
  ],
  defaultVariants: {
    appearance: "segments",
    size: "md",
    distribution: "content",
  },
});

const segmentedOptionVariants = cva(
  "duration-quick ease-ui focus-visible:ring-status-success-content flex min-w-0 font-normal transition-[background-color,border-color,color] outline-none focus-visible:ring-2 [&_svg]:shrink-0",
  {
    variants: {
      appearance: {
        segments:
          "[&_svg]:size-icon-md h-full items-center justify-center gap-1.5 rounded-control-inner px-2",
        cards:
          "border-border-subtle bg-surface-raised hover:bg-surface-tertiary [&_svg]:size-icon-lg items-center gap-3 rounded-md border p-4 text-left aria-checked:border-accent-border",
        underline:
          "border-transparent aria-checked:border-content-primary min-h-control-md items-center justify-center border-b-2 px-1",
      },
      size: {
        sm: "px-2",
        md: "px-3",
      },
      distribution: {
        content: "",
        fill: "flex-1",
      },
    },
    compoundVariants: [{ appearance: "cards", class: "px-4" }],
    defaultVariants: {
      appearance: "segments",
      size: "md",
      distribution: "content",
    },
  },
);

type SegmentedOption<T extends string> = {
  label: string;
  value: T;
  icon?: ReactNode;
  description?: string;
  disabled?: boolean;
};

type SegmentedControlProps<T extends string> = VariantProps<
  typeof segmentedControlVariants
> & {
  value: T;
  options: readonly SegmentedOption<T>[];
  onValueChange: (value: T) => void;
  ariaLabel: string;
  className?: string;
  disabled?: boolean;
};

export function SegmentedControl<T extends string>({
  appearance = "segments",
  ariaLabel,
  className,
  disabled = false,
  distribution,
  onValueChange,
  options,
  size,
  value,
}: SegmentedControlProps<T>) {
  const segmentRefs = useRef(new Map<string, HTMLButtonElement>());

  const moveSelection = (
    event: KeyboardEvent<HTMLButtonElement>,
    currentIndex: number,
  ) => {
    const enabledOptions = options
      .map((option, index) => ({ index, option }))
      .filter(({ option }) => !option.disabled);
    if (enabledOptions.length === 0) return;

    const currentEnabledIndex = enabledOptions.findIndex(
      ({ index }) => index === currentIndex,
    );
    let target: (typeof enabledOptions)[number] | undefined;

    if (event.key === "ArrowRight" || event.key === "ArrowLeft") {
      const offset = event.key === "ArrowRight" ? 1 : -1;
      const nextIndex =
        (currentEnabledIndex + offset + enabledOptions.length) %
        enabledOptions.length;
      target = enabledOptions[nextIndex];
    } else if (event.key === "Home") {
      target = enabledOptions[0];
    } else if (event.key === "End") {
      target = enabledOptions[enabledOptions.length - 1];
    } else {
      return;
    }

    if (!target) return;
    event.preventDefault();
    onValueChange(target.option.value);
    segmentRefs.current.get(target.option.value)?.focus();
  };

  return (
    <div
      aria-disabled={disabled || undefined}
      className={cn(
        segmentedControlVariants({ appearance, size, distribution }),
        appearance === "resources" &&
          options.length === 4 &&
          "@[56rem]/resource-page:grid-cols-4",
        className,
      )}
      role="radiogroup"
      aria-label={ariaLabel}
    >
      {options.map((option, index) => {
        const selected = option.value === value;
        if (appearance === "resources") {
          return (
            <OverviewCard
              key={option.value}
              ref={(node) => {
                if (node) segmentRefs.current.set(option.value, node);
                else segmentRefs.current.delete(option.value);
              }}
              role="radio"
              aria-checked={selected}
              selected={selected}
              label={option.label}
              icon={option.icon}
              description={option.description}
              disabled={disabled || option.disabled}
              tabIndex={selected ? 0 : -1}
              onClick={() => onValueChange(option.value)}
              onKeyDown={(event) => moveSelection(event, index)}
            />
          );
        }
        return (
          <button
            key={option.value}
            ref={(node) => {
              if (node) segmentRefs.current.set(option.value, node);
              else segmentRefs.current.delete(option.value);
            }}
            type="button"
            role="radio"
            aria-label={appearance === "cards" ? option.label : undefined}
            aria-description={
              appearance === "cards" ? option.description : undefined
            }
            aria-checked={selected}
            disabled={disabled || option.disabled}
            tabIndex={selected ? 0 : -1}
            onClick={() => onValueChange(option.value)}
            onKeyDown={(event) => moveSelection(event, index)}
            className={cn(
              segmentedOptionVariants({ appearance, size, distribution }),
              appearance === "underline"
                ? selected
                  ? "text-content-primary font-medium"
                  : "text-content-secondary hover:text-content-primary"
                : selected
                  ? "bg-surface-raised text-content-primary font-medium"
                  : "text-button-content hover:bg-button-fill-hover active:bg-button-fill-pressed",
              option.disabled && "pointer-events-none opacity-45",
            )}
          >
            {option.icon}
            {appearance === "cards" ? (
              <span className="grid min-w-0 gap-1">
                <span className="type-body font-medium">{option.label}</span>
                {option.description ? (
                  <span className="text-content-secondary type-ui">
                    {option.description}
                  </span>
                ) : null}
              </span>
            ) : (
              <span className="whitespace-nowrap">{option.label}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}
