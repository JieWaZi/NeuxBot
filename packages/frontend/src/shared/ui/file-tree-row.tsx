import { ChevronRight } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/shared/lib/cn";

/** 文件树的统一行密度、缩进、焦点与选中样式，不承载文件系统行为。 */
export function FileTreeRow({
  depth = 0,
  directory = false,
  expanded,
  selected,
  icon,
  children,
  className,
  ...props
}: Omit<ComponentProps<"button">, "children"> & {
  depth?: number;
  directory?: boolean;
  expanded?: boolean;
  selected?: boolean;
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <button
      {...props}
      type="button"
      role={props.role ?? "treeitem"}
      aria-expanded={directory ? expanded : undefined}
      aria-selected={selected}
      className={cn(
        "focus-visible:ring-ring type-body min-h-row-default hover:bg-muted grid min-w-0 flex-1 items-center gap-1 rounded-sm pr-2 text-left outline-none focus-visible:ring-2",
        directory
          ? "grid-cols-[16px_16px_minmax(0,1fr)]"
          : "grid-cols-[16px_minmax(0,1fr)]",
        selected && "bg-muted",
        className,
      )}
      style={{
        paddingLeft: `${(directory ? 8 : 24) + depth * 16}px`,
        ...props.style,
      }}
    >
      {directory && (
        <ChevronRight
          aria-hidden="true"
          strokeWidth={1.5}
          className={cn(
            "text-muted-foreground size-icon-sm transition-transform motion-reduce:transition-none",
            expanded && "rotate-90",
          )}
        />
      )}
      {icon}
      {children}
    </button>
  );
}
