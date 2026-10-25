import {
  ChevronDown,
  ChevronRight,
  Table2,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router-dom";

import { cn } from "@/shared/lib/cn";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

type ChatGroupHeaderProps = {
  action?: { current: boolean; href: string; label: string };
  collapsed: boolean;
  icon?: LucideIcon;
  label: string;
  onActionOpened?: () => void;
  onToggle: () => void;
};

export function ChatGroupHeader({
  action,
  collapsed,
  icon: Icon,
  label,
  onActionOpened,
  onToggle,
}: ChatGroupHeaderProps) {
  return (
    <div className="group/group hover:bg-muted h-row-compact flex w-full items-center rounded-sm">
      <button
        type="button"
        aria-expanded={!collapsed}
        onClick={onToggle}
        className={cn(
          "group/toggle text-muted-foreground hover:text-foreground focus-visible:ring-ring type-ui flex h-full min-w-0 flex-1 items-center rounded-sm px-3 text-left outline-none focus-visible:ring-2",
          Icon && "gap-2",
        )}
      >
        {Icon ? <Icon className="size-icon-md" strokeWidth={1.5} /> : null}
        <span className="flex min-w-0 items-center gap-1">
          <span className="truncate">{label}</span>
          {collapsed ? (
            <ChevronRight
              className="size-icon-sm shrink-0 opacity-0 transition-opacity group-hover/group:opacity-100 group-focus-visible/toggle:opacity-100"
              strokeWidth={1.5}
            />
          ) : (
            <ChevronDown
              className="size-icon-sm shrink-0 opacity-0 transition-opacity group-hover/group:opacity-100 group-focus-visible/toggle:opacity-100"
              strokeWidth={1.5}
            />
          )}
        </span>
      </button>
      {action ? (
        <TooltipProvider delayDuration={350}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Link
                to={action.href}
                aria-current={action.current ? "page" : undefined}
                aria-label={action.label}
                className="text-muted-foreground hover:text-foreground focus-visible:ring-ring size-control-sm mr-1 grid shrink-0 place-items-center rounded-sm opacity-0 transition-opacity outline-none group-hover/group:opacity-100 focus-visible:opacity-100 focus-visible:ring-2"
                onClick={onActionOpened}
              >
                <Table2 className="size-icon-sm" strokeWidth={1.5} />
              </Link>
            </TooltipTrigger>
            <TooltipContent side="top">{action.label}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ) : null}
    </div>
  );
}
