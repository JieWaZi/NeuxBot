import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";

type CornerLauncherButtonProps = Omit<
  ComponentProps<"button">,
  "children" | "type"
> & {
  icon: ReactNode;
  indicator?: ReactNode;
  label: string;
  wide?: boolean;
};

export function CornerLauncherButton({
  className,
  icon,
  indicator,
  label,
  wide,
  ...props
}: CornerLauncherButtonProps) {
  return (
    <Button
      aria-label={label}
      {...props}
      className={cn("group/launcher relative flex", className)}
      shape="corner"
      size="corner-launcher"
      type="button"
      variant="plain"
    >
      {/* 展开的标签仍贴在底边，只有角落图标占用较高的命中区。 */}
      <span
        aria-hidden="true"
        className={cn(
          "bg-control-launcher text-control-launcher-content group-hover/launcher:bg-control-launcher-hover group-hover/launcher:text-control-launcher-content-hover group-active/launcher:bg-control-launcher-pressed group-focus-visible/launcher:bg-control-launcher-hover group-focus-visible/launcher:text-control-launcher-content-hover h-control-corner-launcher w-control-corner-launcher duration-quick ease-ui shadow-menu absolute right-0 bottom-0 flex items-center overflow-hidden rounded-tl-lg transition-[width,background-color,color,box-shadow] motion-reduce:transition-none",
          wide
            ? "group-hover/launcher:w-40 group-focus-visible/launcher:w-40"
            : "group-hover/launcher:w-control-launcher group-focus-visible/launcher:w-control-launcher",
        )}
      >
        <span className="size-control-corner-launcher grid shrink-0 place-items-center">
          {icon}
        </span>
        <span className="type-ui duration-quick min-w-0 -translate-x-1 truncate pr-4 opacity-0 transition-[opacity,transform] group-hover/launcher:translate-x-0 group-hover/launcher:opacity-100 group-focus-visible/launcher:translate-x-0 group-focus-visible/launcher:opacity-100 motion-reduce:transition-none">
          {label}
        </span>
        {indicator}
      </span>
    </Button>
  );
}
