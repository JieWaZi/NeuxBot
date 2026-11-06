import { ArrowLeft, ArrowRight } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { ChatSceneOwner } from "@/shared/workspace/app-scene-model";
import { Button } from "@/shared/ui/button";
import { SceneChromeLayout } from "@/shared/ui/scene-chrome-portal";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import {
  nearestEntityHistory,
  recordEntityHistory,
  type EntityHistory,
} from "./entity-navigation-history";

/** 侧栏的返回记录实体身份；页面覆盖和聊天子页签不参与。 */
export function useAppNavigationHistory({
  owner,
  available,
  onSelect,
  onPageBack,
}: {
  owner?: ChatSceneOwner;
  available: (owner: ChatSceneOwner) => boolean;
  onSelect: (owner: ChatSceneOwner) => void;
  onPageBack?: () => void;
}) {
  const [{ history, pendingTarget }, setHistory] = useState<{
    history: EntityHistory;
    pendingTarget?: number;
  }>(() => ({
    history: recordEntityHistory({ entries: [], index: -1 }, owner),
  }));
  const recorded = recordEntityHistory(history, owner, pendingTarget);
  if (recorded !== history) {
    setHistory({ history: recorded });
  }
  const back = nearestEntityHistory(recorded, -1, available);
  const forward = nearestEntityHistory(recorded, 1, available);
  const go = (index: number | undefined) => {
    if (index === undefined) return;
    const entry = recorded.entries[index];
    if (!entry) return;
    setHistory({ history: recorded, pendingTarget: index });
    onSelect(entry);
  };
  return {
    canGoBack: Boolean(onPageBack) || back !== undefined,
    canGoForward: forward !== undefined,
    onGoBack: () => (onPageBack ? onPageBack() : go(back)),
    onGoForward: () => go(forward),
  };
}

export function AppNavigationHistoryControls({
  canGoBack,
  canGoForward,
  onGoBack,
  onGoForward,
}: {
  canGoBack: boolean;
  canGoForward: boolean;
  onGoBack: () => void;
  onGoForward: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <div className="flex shrink-0 items-center" data-desktop-no-drag-region>
      {(
        [
          ["back", ArrowLeft, canGoBack, onGoBack],
          ["forward", ArrowRight, canGoForward, onGoForward],
        ] as const
      ).map(([key, Icon, enabled, onClick]) => (
        <Tooltip key={key}>
          <TooltipTrigger asChild>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              aria-label={t(`titlebar.${key}`)}
              disabled={!enabled}
              onClick={onClick}
            >
              <Icon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t(`titlebar.${key}`)}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}

/** 覆盖页的局部顶栏；真实详情路径由所属页面贡献。 */
export function AppTitlebar({ breadcrumbs }: { breadcrumbs?: ReactNode }) {
  return (
    <header data-app-titlebar className="h-(--shell-titlebar-height) min-w-0">
      <SceneChromeLayout actions={null}>
        <div
          className="type-ui text-content-primary min-w-0 flex-1 truncate px-2"
          data-titlebar-breadcrumbs
        >
          {breadcrumbs}
        </div>
      </SceneChromeLayout>
    </header>
  );
}

/** 这些详情由所属页面的既有 Query 贡献完整顶栏，Shell 只提供其挂载槽。 */
export function pageOwnsSceneHeader(pathname: string) {
  return (
    /^\/settings(?:\/|$)/.test(pathname) ||
    /^\/wakers\/[^/]+(?:\/|$)/.test(pathname) ||
    /^\/teams\/[^/]+\/chats$/.test(pathname) ||
    /^\/resources\/.+/.test(pathname) ||
    /^\/knowledge\/[^/]+$/.test(pathname) ||
    /^\/workflows\/[^/]+(?:\/runs\/[^/]+)?$/.test(pathname) ||
    /^\/automatic-tasks\/[^/]+$/.test(pathname)
  );
}
