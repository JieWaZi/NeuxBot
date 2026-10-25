import type { LucideIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { cn } from "@/shared/lib/cn";
import { Button } from "./button";
import { Dock, DockIcon } from "./dock";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./tooltip";

type DetailDockItem = {
  id: string;
  label: string;
  icon: LucideIcon;
  active: boolean;
} & (
  { href: string; onSelect?: never } | { href?: never; onSelect: () => void }
);

/** 统一详情导航的透明表面、图标命中区与当前项；业务层只提供导航数据。 */
export function DetailDockNavigation({
  label,
  items,
}: {
  label: string;
  items: DetailDockItem[];
}) {
  return (
    <nav aria-label={label} className="pointer-events-auto">
      <TooltipProvider>
        <Dock direction="middle">
          {items.map(({ icon: Icon, ...item }) => (
            <DockIcon key={item.id}>
              <Tooltip>
                <TooltipTrigger asChild>
                  {item.href !== undefined ? (
                    <Button
                      asChild
                      variant="ghost"
                      size="dock"
                      shape="circle"
                      className={cn(item.active && "text-content-primary")}
                    >
                      <Link
                        to={item.href}
                        aria-label={item.label}
                        aria-current={item.active ? "page" : undefined}
                      >
                        <Icon aria-hidden="true" strokeWidth={1.5} />
                      </Link>
                    </Button>
                  ) : (
                    <Button
                      variant="ghost"
                      size="dock"
                      shape="circle"
                      aria-label={item.label}
                      aria-pressed={item.active}
                      className={cn(item.active && "text-content-primary")}
                      onClick={item.onSelect}
                    >
                      <Icon aria-hidden="true" strokeWidth={1.5} />
                    </Button>
                  )}
                </TooltipTrigger>
                <TooltipContent side="top">{item.label}</TooltipContent>
              </Tooltip>
            </DockIcon>
          ))}
        </Dock>
      </TooltipProvider>
    </nav>
  );
}
