import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { CompanionAvatar } from "@/shared/companion/companion-avatar";
import type { CompanionControl } from "@/shared/companion/companion-avatar";
import type { CompanionActivity } from "@/shared/companion/activity";
import {
  companionRoaming,
  type CompanionMotion,
} from "@/shared/companion/roaming";
import { Button } from "@/shared/ui/button";

type DockSelection = {
  avatar: string;
  name: string;
  trigger: HTMLElement;
  scope: string;
};
const CompanionDock = createContext<{
  selected: DockSelection | null;
  activity: CompanionActivity;
  show: (avatar: string, name: string, trigger: HTMLElement) => void;
  hide: () => void;
} | null>(null);

/** 互动选择只属于当前聊天；沿用输入区的会话范围，首条消息持久化不重置形象。 */
export function CompanionInteractionProvider({
  children,
  scope,
  activeAvatar,
  activity,
}: {
  children: ReactNode;
  scope: string;
  activeAvatar: string;
  activity: CompanionActivity;
}) {
  const [selection, setSelection] = useState<DockSelection | null>(null);
  const selected = selection?.scope === scope ? selection : null;
  return (
    <CompanionDock
      value={{
        selected,
        activity: selected?.avatar === activeAvatar ? activity : "idle",
        show: (avatar, name, trigger) =>
          setSelection({ avatar, name, trigger, scope }),
        hide: () => {
          setSelection(null);
          if (selected?.trigger.isConnected) selected.trigger.focus();
        },
      }}
    >
      {children}
    </CompanionDock>
  );
}

export function useCompanionDock() {
  return useContext(CompanionDock);
}

/** 伙伴从输入框上沿悬浮到消息之上；透明区域不占布局或拦截鼠标。 */
export function CompanionInteraction() {
  const dock = useCompanionDock();
  if (!dock?.selected) return null;
  return (
    <DockedCompanion
      key={dock.selected.avatar}
      selection={dock.selected}
      activity={dock.activity}
      onHide={dock.hide}
    />
  );
}

function DockedCompanion({
  selection,
  activity,
  onHide,
}: {
  selection: DockSelection;
  activity: CompanionActivity;
  onHide: () => void;
}) {
  const { t } = useTranslation("common");
  const control = useRef<CompanionControl>(null);
  const track = useRef<HTMLDivElement>(null);
  const actor = useRef<HTMLDivElement>(null);
  const roaming = useRef<ReturnType<typeof companionRoaming> | null>(null);
  // 保持命令适配器稳定，避免流式消息刷新时重建形象和动画。
  const motion = useMemo<CompanionMotion>(
    () => ({
      move: (action) => roaming.current?.move(action) ?? false,
      stop: () => roaming.current?.stop(),
    }),
    [],
  );
  useEffect(() => {
    if (track.current && actor.current)
      roaming.current = companionRoaming(track.current, actor.current);
    control.current?.interact();
    return () => {
      roaming.current?.destroy();
      roaming.current = null;
    };
  }, []);
  return (
    <div
      role="region"
      aria-label={t("companion.expanded", { name: selection.name })}
      data-companion-dock
      className="pointer-events-none relative h-0 w-full"
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          onHide();
        }
      }}
    >
      <div
        ref={track}
        className="h-avatar-docked pointer-events-none absolute right-0 bottom-0 left-1/2 z-10 translate-y-6"
      >
        <div
          ref={actor}
          data-companion-actor
          className="group/companion size-avatar-docked pointer-events-none absolute top-0 left-0 max-w-full"
        >
          {/* 只有 SVG 实际绘制区域接收指针，透明画布继续透传给消息区。 */}
          <CompanionAvatar
            ref={control}
            avatar={selection.avatar}
            activity={activity}
            preview
            motion={motion}
            interactiveLabel={t("companion.touch", { name: selection.name })}
            interactiveTitle={t("companion.expandedHint")}
          />
          <div className="pointer-events-auto invisible absolute top-0 right-0 p-1 opacity-0 group-focus-within/companion:visible group-focus-within/companion:opacity-100 group-hover/companion:visible group-hover/companion:opacity-100">
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={t("companion.hide")}
              onClick={onHide}
            >
              <X />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
