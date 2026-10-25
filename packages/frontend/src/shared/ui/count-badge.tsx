import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

import { Badge } from "./badge";

type CountBadgeProps = Omit<ComponentProps<typeof Badge>, "children"> & {
  count: number;
  max?: number;
};

/** 紧凑数字角标，供导航和列表展示运行中或待处理数量。 */
export function CountBadge({
  className,
  count,
  max = 9,
  variant = "success",
  ...props
}: CountBadgeProps) {
  if (count <= 0) return null;

  const label = count > max ? `${max}+` : String(count);

  return (
    <Badge
      variant={variant}
      className={cn(
        "type-caption h-4 min-w-4 rounded-full border-0 px-1 py-0 font-semibold tabular-nums",
        className,
      )}
      {...props}
    >
      {label}
    </Badge>
  );
}
