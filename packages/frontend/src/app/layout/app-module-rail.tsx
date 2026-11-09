import {
  BookOpen,
  Bot,
  ChartNoAxesCombined,
  Clock3,
  Ellipsis,
  Package,
  Settings,
} from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuPinnableItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { cn } from "@/shared/lib/cn";
import type { AppModuleId } from "./app-navigation-model";
import {
  pinnedAppModules,
  type AppPinnableModuleId,
} from "./partner-navigation-preferences";

type AppModuleRailProps = {
  readonly activeModule: AppModuleId;
  readonly onNavigate: (moduleId: AppModuleId) => void;
  readonly pinnedModules?: readonly AppPinnableModuleId[];
  readonly onPinChange?: (
    moduleId: AppPinnableModuleId,
    pinned: boolean,
  ) => void;
};

/** OpenBot v0.62.0/fcb35746：packages/ui/src/features/window/PageRail.tsx、page-rail.css；入口按 B03 映射。 */
export function AppModuleRail({
  activeModule,
  onNavigate,
  pinnedModules = pinnedAppModules(),
  onPinChange,
}: AppModuleRailProps) {
  const { t } = useTranslation("common");
  const [moreOpen, setMoreOpen] = useState(false);
  const moreRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuAnchorTop = useRef(0);
  // pin 会移动 More；浮层保持打开位置，使指针和键盘仍指向原来的行。
  useLayoutEffect(() => {
    if (moreOpen && menuRef.current && moreRef.current)
      menuRef.current.style.translate = `0 ${menuAnchorTop.current - moreRef.current.getBoundingClientRect().top}px`;
  }, [moreOpen, pinnedModules]);
  const pages = [
    { id: "usage", label: t("usage:title"), icon: ChartNoAxesCombined },
    { id: "knowledge", label: t("knowledge:knowledge"), icon: BookOpen },
    { id: "automatic-tasks", label: t("automaticTask:title"), icon: Clock3 },
  ] as const;
  const moreActive = pages.some(
    ({ id }) => id === activeModule && !pinnedModules.includes(id),
  );
  const moduleButton = ({
    id,
    label,
    icon: Icon,
  }: {
    id: AppModuleId;
    label: string;
    icon: typeof Bot;
  }) => (
    <Tooltip key={id}>
      <TooltipTrigger asChild>
        <Button
          type="button"
          size="module-rail"
          variant="ghost"
          aria-label={label}
          aria-current={activeModule === id ? "page" : undefined}
          className={cn(
            "hover:bg-shell-control-hover shrink-0",
            activeModule === id &&
              "bg-shell-control-active text-content-primary hover:bg-shell-control-active",
          )}
          onClick={() => onNavigate(id)}
        >
          <Icon strokeWidth={1.5} />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  );
  return (
    <TooltipProvider delayDuration={350}>
      <nav
        aria-label={t("appNavigation.modules")}
        className="app-module-rail bg-surface-module-rail flex h-full min-h-0 w-(--shell-rail-width) flex-col items-center overflow-y-auto"
        data-app-module-rail
        data-desktop-drag-region
      >
        <div
          className="flex shrink-0 flex-col items-center gap-1.5"
          data-desktop-no-drag-region
        >
          {moduleButton({
            id: "partners",
            label: t("partnerNavigation.partner"),
            icon: Bot,
          })}
          {moduleButton({
            id: "resources",
            label: t("plugin:title"),
            icon: Package,
          })}
          {pages
            .filter(({ id }) => pinnedModules.includes(id))
            .map(moduleButton)}
          <DropdownMenu
            open={moreOpen}
            onOpenChange={(open) => {
              if (open)
                menuAnchorTop.current =
                  moreRef.current?.getBoundingClientRect().top ?? 0;
              setMoreOpen(open);
            }}
          >
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger ref={moreRef} asChild>
                  <Button
                    type="button"
                    size="module-rail"
                    variant="ghost"
                    aria-label={t("appNavigation.more")}
                    aria-current={moreActive ? "page" : undefined}
                    className={cn(
                      "hover:bg-shell-control-hover shrink-0",
                      moreActive &&
                        "bg-shell-control-active text-content-primary hover:bg-shell-control-active",
                    )}
                  >
                    <Ellipsis strokeWidth={1.5} />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent side="right">
                {t("appNavigation.more")}
              </TooltipContent>
            </Tooltip>
            <DropdownMenuContent
              ref={menuRef}
              side="right"
              align="start"
              aria-label={t("appNavigation.more")}
              className="app-module-menu"
            >
              {pages.map(({ id, label, icon: Icon }) => (
                <DropdownMenuPinnableItem
                  key={id}
                  aria-current={activeModule === id ? "page" : undefined}
                  onSelect={() => onNavigate(id)}
                  pinned={pinnedModules.includes(id)}
                  pinLabel={t("appNavigation.pinModule", { name: label })}
                  onPinnedChange={
                    onPinChange
                      ? (pinned) => onPinChange(id, pinned)
                      : undefined
                  }
                >
                  <Icon strokeWidth={1.5} />
                  {label}
                </DropdownMenuPinnableItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div
          className="mt-auto flex shrink-0 flex-col items-center gap-1.5 pt-4"
          data-desktop-no-drag-region
        >
          {moduleButton({
            id: "settings",
            label: t("partnerNavigation.settings"),
            icon: Settings,
          })}
        </div>
      </nav>
    </TooltipProvider>
  );
}
