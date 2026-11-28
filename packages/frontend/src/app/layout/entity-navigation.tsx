import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragMoveEvent,
} from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
  type SortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ArrowLeftRight,
  CircleAlert,
  LoaderCircle,
  Plus,
  Search,
  UsersRound,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import { TeamAvatarStack } from "@/modules/team";
import type { Waker } from "@/modules/waker";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as ChannelDetail } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

import type { ExecutorAttention } from "@/modules/workspaceattention";
import {
  companionAttention,
  ConversationAttentionMark,
  conversationAttentionMark,
} from "@/shared/ui/conversation-attention";
import type {
  NavigationDropPlacement,
  PartnerNavigationPreferences,
  WakerNavigationGroup,
} from "./partner-navigation-preferences";

export type NavigationEntity =
  | {
      id: string;
      key: string;
      kind: "team";
      team: ChannelDetail;
      attention?: ExecutorAttention;
    }
  | {
      id: string;
      key: string;
      kind: "waker";
      waker: Waker;
      attention?: ExecutorAttention;
    };

type NavigationWaker = Extract<NavigationEntity, { kind: "waker" }>;
type NavigationDropTarget = {
  targetId: string;
  placement: NavigationDropPlacement;
  keyboard: boolean;
};

type EntityNavigationProps = {
  canReorder?: boolean;
  collapsed: boolean;
  entityStatus?: "error" | "loading" | "ready";
  floating?: boolean;
  items: NavigationEntity[];
  mode: PartnerNavigationPreferences["entityMode"];
  onManageEntities: (mode: PartnerNavigationPreferences["entityMode"]) => void;
  onModeChange: (mode: PartnerNavigationPreferences["entityMode"]) => void;
  onReorder: (
    sourceId: string,
    targetId: string,
    placement: NavigationDropPlacement,
  ) => void;
  onRetryGroupImport?: () => void;
  onSelectPartner: (partnerId: string) => void;
  onSelectTeam: (channelId: string) => void;
  onShowcaseWaker?: (waker: Waker, origin: HTMLElement) => void;
  selectedKey: string;
  history?: ReactNode;
  sidebarSearch?: string;
  wakerGroups?: readonly WakerNavigationGroup[];
  renderWakerRow?: (item: NavigationWaker, row: ReactElement) => ReactElement;
  renderTeamRow?: (
    item: Extract<NavigationEntity, { kind: "team" }>,
    row: ReactElement,
  ) => ReactElement;
};

const modeSwitchDurationMs = 220;
const dropGroupHighlight =
  "isolate before:border-accent before:bg-accent-surface before:pointer-events-none before:absolute before:inset-x-0 before:top-0 before:-z-10 before:border-x";
const partnerRailIndicatorHeight = {
  hover: 8,
  selected: 20,
} as const;

function EntityFeedback({
  collapsed,
  label,
  status,
}: {
  collapsed: boolean;
  label: string;
  status: "empty" | "error" | "loading";
}) {
  const content =
    status === "loading" ? (
      <LoaderCircle
        className="size-icon-md animate-spin"
        strokeWidth={1.5}
        aria-hidden="true"
      />
    ) : status === "error" ? (
      <CircleAlert
        className="text-destructive size-icon-md"
        strokeWidth={1.5}
        aria-hidden="true"
      />
    ) : (
      <UsersRound
        className="size-icon-md"
        strokeWidth={1.5}
        aria-hidden="true"
      />
    );
  return collapsed ? (
    <span
      aria-label={label}
      className="text-content-muted size-control-md grid place-items-center"
      role={status === "error" ? "alert" : "status"}
    >
      {content}
    </span>
  ) : (
    <p
      className="text-content-muted type-ui px-2 py-4 text-center"
      role={status === "error" ? "alert" : "status"}
    >
      {label}
    </p>
  );
}

function TeamEntityAvatar({
  team,
  size,
}: {
  team: ChannelDetail;
  size: "md" | "navigation";
}) {
  const members = team.members ?? [];
  if (!members.some((member) => member.waker?.id)) {
    return (
      <span
        className={cn(
          "bg-muted text-muted-foreground grid place-items-center overflow-hidden rounded-sm",
          size === "navigation" ? "size-avatar-navigation" : "size-avatar-md",
        )}
        aria-hidden="true"
      >
        <UsersRound className="size-icon-md" strokeWidth={1.5} />
      </span>
    );
  }
  return <TeamAvatarStack members={members} size={size} />;
}

// 单人与组合头像都在槽内保留绘图余量，角标以主体右上部为中心。
const avatarAttentionPosition =
  "absolute right-2 top-3 inline-flex translate-x-1/2 -translate-y-1/2";

function EntityAvatar({
  item,
  interactive = false,
  size = "md",
  onDoubleInteract,
  onOpenAction,
}: {
  item: NavigationEntity;
  interactive?: boolean;
  size?: "md" | "navigation";
  onDoubleInteract?: (trigger: HTMLElement) => void;
  onOpenAction?: () => void;
}) {
  const { t } = useTranslation("common");
  const mark = entityMark(item);
  const attention =
    item.kind === "waker" ? companionAttention(mark) : undefined;
  const actionLabel =
    mark?.kind === "action"
      ? t(
          mark.request
            ? `partnerNavigation.request.${mark.request}`
            : "partnerNavigation.needsAction",
        )
      : undefined;
  return (
    <span className="relative inline-flex shrink-0">
      {item.kind === "waker" ? (
        <Avatar
          interactive={interactive}
          attention={attention}
          activity={
            mark?.kind === "action"
              ? "waiting"
              : mark?.kind === "working"
                ? "working"
                : mark?.kind === "failed"
                  ? "failed"
                  : "idle"
          }
          name={item.waker.name ?? t("partnerNavigation.partner")}
          onDoubleInteract={onDoubleInteract}
          shape="square"
          src={item.waker.avatar}
          size={size}
        />
      ) : (
        <TeamEntityAvatar team={item.team} size={size} />
      )}
      {mark?.kind === "action" && onOpenAction ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={onOpenAction}
              aria-label={actionLabel}
              className={cn(
                avatarAttentionPosition,
                "focus-visible:ring-ring rounded-full outline-none before:absolute before:-inset-1.5 focus-visible:ring-2",
                attention &&
                  (size === "navigation"
                    ? "size-icon-sm top-2.5"
                    : "size-icon-sm top-2"),
              )}
              data-avatar-attention
            >
              {attention ? null : (
                <ConversationAttentionMark mark={mark} variant="avatar" />
              )}
            </button>
          </TooltipTrigger>
          <TooltipContent>{actionLabel}</TooltipContent>
        </Tooltip>
      ) : mark && !attention ? (
        <span
          className={cn(avatarAttentionPosition, "pointer-events-none")}
          data-avatar-attention
        >
          {item.kind === "team" && mark.kind === "working" ? (
            <span
              role="img"
              aria-label={t("partnerNavigation.working")}
              title={t("partnerNavigation.working")}
              data-conversation-attention="working"
              className="bg-accent-secondary ring-surface-panel size-1.5 rounded-full ring-2 motion-safe:animate-pulse"
            />
          ) : (
            <ConversationAttentionMark mark={mark} variant="avatar" />
          )}
        </span>
      ) : null}
    </span>
  );
}

function entityLabel(item: NavigationEntity, fallback: string) {
  return item.kind === "waker"
    ? item.waker.name || fallback
    : item.team.channel?.name || fallback;
}

function entityMark(item: NavigationEntity) {
  const state = entityActivityState(item);
  const attention = item.attention;
  // 摘要为每个计入总数的独立会话提供明细，主会话入口只显示自己的未读。
  const chatUnread =
    attention?.chats?.reduce(
      (total, chat) => total + (chat.unreadMessageCount ?? 0),
      0,
    ) ?? 0;
  return conversationAttentionMark({
    requestKinds:
      attention?.requestKinds ??
      attention?.chats?.flatMap((chat) => chat.requestKinds ?? []),
    waiting: attention
      ? Boolean(attention.waitingWorkCount)
      : state === "waiting",
    running: attention
      ? Boolean(attention.runningChatCount || attention.mainConversationPending)
      : state === "running",
    failed: attention?.lastStatus === "failed",
    unread: (attention?.unreadMessageCount ?? 0) - chatUnread,
  });
}

function entityActivityState(item: NavigationEntity) {
  return item.kind === "waker"
    ? (item.waker.activityState ?? "idle")
    : (item.team.activityState ?? "idle");
}

function EntityRow({
  canReorder,
  collapsed,
  dropGroupEdge,
  dropPlacement,
  item,
  onHoverEnd,
  onHoverStart,
  onSelectPartner,
  onSelectTeam,
  onShowcaseWaker,
  registerItemRef,
  selected,
}: {
  canReorder: boolean;
  collapsed: boolean;
  dropGroupEdge?: "middle" | "end";
  dropPlacement?: NavigationDropPlacement;
  item: NavigationEntity;
  onHoverEnd?: () => void;
  onHoverStart?: () => void;
  onSelectPartner: (partnerId: string) => void;
  onSelectTeam: (channelId: string) => void;
  onShowcaseWaker?: (waker: Waker, origin: HTMLElement) => void;
  registerItemRef?: (key: string, node: HTMLButtonElement | null) => void;
  selected: boolean;
}) {
  const { t } = useTranslation("common");
  const {
    attributes,
    isDragging,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
  } = useSortable({ id: item.key });
  const label = entityLabel(
    item,
    item.kind === "waker"
      ? t("partnerNavigation.partner")
      : t("partnerNavigation.team"),
  );
  const mark = entityMark(item);
  const statusLabel =
    mark?.kind === "action"
      ? t(
          mark.request
            ? `partnerNavigation.request.${mark.request}`
            : "partnerNavigation.needsAction",
        )
      : mark?.kind === "working"
        ? t("partnerNavigation.working")
        : mark?.kind === "failed"
          ? t("partnerNavigation.recentFailed")
          : mark?.kind === "unread"
            ? t("partnerNavigation.unreadMessages", { count: mark.count })
            : undefined;
  const accessibleLabel = statusLabel ? `${label}，${statusLabel}` : label;
  const description =
    item.kind === "waker"
      ? item.waker.roleName
      : t("partnerNavigation.teamMemberCount", {
          count: item.team.members?.length ?? 0,
        });

  const handleRowClick = () => {
    if (item.kind === "waker") onSelectPartner(item.id);
    else onSelectTeam(item.id);
  };

  if (!collapsed) {
    return (
      <div
        ref={setNodeRef}
        data-avatar-hover-scope=""
        data-drop-group={dropGroupEdge}
        data-drop-placement={dropPlacement}
        style={{
          transform: CSS.Transform.toString(transform),
          transition,
        }}
        className={cn(
          "min-h-row-entity relative grid w-full min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center rounded-md",
          selected
            ? "bg-surface-panel text-content-primary [--team-avatar-outline:var(--surface-panel)]"
            : "hover:bg-surface-subtle [--team-avatar-outline:var(--canvas)] hover:[--team-avatar-outline:color(from_color-mix(in_srgb,var(--canvas),var(--surface-subtle))_srgb_r_g_b_/_1)]",
          isDragging && "z-10 opacity-35",
          dropGroupEdge && dropGroupHighlight,
          dropGroupEdge === "middle" && "before:-bottom-0.5",
          dropGroupEdge === "end" &&
            "before:bottom-0 before:rounded-b-md before:border-b",
          dropPlacement &&
            "after:bg-accent after:pointer-events-none after:absolute after:inset-x-2 after:z-10 after:h-0.5 after:rounded-full",
          dropPlacement === "before" && "after:top-0",
          dropPlacement === "after" && "after:bottom-0",
        )}
      >
        <span className="flex shrink-0 items-center pl-2">
          <EntityAvatar
            item={item}
            size="navigation"
            interactive={item.kind === "waker"}
            onOpenAction={handleRowClick}
            onDoubleInteract={
              item.kind === "waker"
                ? (origin) => onShowcaseWaker?.(item.waker, origin)
                : undefined
            }
          />
        </span>
        <button
          ref={setActivatorNodeRef}
          type="button"
          data-waker-nav-row={item.kind === "waker" ? "" : undefined}
          data-team-nav-row={item.kind === "team" ? "" : undefined}
          aria-current={selected ? "page" : undefined}
          aria-label={accessibleLabel}
          onClick={handleRowClick}
          className="focus-visible:ring-ring min-h-row-entity relative grid min-w-0 items-center rounded-md px-2 py-1 text-left outline-none focus-visible:ring-2"
          {...(canReorder ? attributes : undefined)}
          {...(canReorder ? listeners : undefined)}
        >
          <span className="min-w-0">
            <strong className="type-body block truncate font-medium">
              {label}
            </strong>
            {description ? (
              <span className="text-muted-foreground type-caption block truncate">
                {description}
              </span>
            ) : null}
          </span>
        </button>
      </div>
    );
  }

  const button = (
    <button
      ref={(node) => {
        setActivatorNodeRef(node);
        setNodeRef(node);
        registerItemRef?.(item.key, node);
      }}
      type="button"
      aria-current={selected ? "page" : undefined}
      aria-label={accessibleLabel}
      onBlur={onHoverEnd}
      onClick={() =>
        item.kind === "waker" ? onSelectPartner(item.id) : onSelectTeam(item.id)
      }
      onDoubleClick={(event) => {
        if (item.kind === "waker")
          onShowcaseWaker?.(item.waker, event.currentTarget);
      }}
      onFocus={onHoverStart}
      onMouseEnter={onHoverStart}
      onMouseLeave={onHoverEnd}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
      }}
      className={cn(
        "focus-visible:ring-ring size-row-navigation relative grid touch-none place-items-center rounded-sm outline-none focus-visible:ring-2",
        isDragging && "z-10 opacity-30",
      )}
      {...(canReorder ? attributes : undefined)}
      {...(canReorder ? listeners : undefined)}
    >
      <EntityAvatar item={item} />
    </button>
  );

  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent side="right">{accessibleLabel}</TooltipContent>
    </Tooltip>
  );
}

export function EntityNavigation({
  canReorder = true,
  collapsed,
  entityStatus = "ready",
  floating = false,
  items,
  mode,
  onManageEntities,
  onModeChange,
  onReorder,
  onRetryGroupImport,
  onSelectPartner,
  onSelectTeam,
  onShowcaseWaker,
  selectedKey,
  history,
  sidebarSearch,
  wakerGroups = [],
  renderWakerRow,
  renderTeamRow,
}: EntityNavigationProps) {
  const { t } = useTranslation("common");
  const [activeKey, setActiveKey] = useState("");
  const [dropTarget, setDropTarget] = useState<NavigationDropTarget | null>(
    null,
  );
  const keyboardDropTarget = useRef<NavigationDropTarget | null>(null);
  const [hoveredRailKey, setHoveredRailKey] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [switching, setSwitching] = useState(false);
  const switchTimer = useRef<number | undefined>(undefined);
  const itemRefs = useRef(new Map<string, HTMLButtonElement>());
  const indicatorRef = useRef<HTMLSpanElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const normalizedSearchQuery = (sidebarSearch ?? searchQuery)
    .trim()
    .toLocaleLowerCase("zh-CN");
  const visibleItems = normalizedSearchQuery
    ? items.filter((item) => {
        const searchable =
          item.kind === "waker"
            ? [item.waker.name, item.waker.roleName]
            : [item.team.channel?.name];
        return searchable.some((value) =>
          value?.toLocaleLowerCase("zh-CN").includes(normalizedSearchQuery),
        );
      })
    : items;
  const groupNames = new Map(
    wakerGroups.flatMap(({ name, wakerIds }) =>
      wakerIds.map((id) => [id, name] as const),
    ),
  );
  const groupedNavigation =
    !collapsed && mode === "waker" && wakerGroups.length > 0;
  const groups = (
    mode === "waker" ? ["", ...wakerGroups.map(({ name }) => name)] : [""]
  ).map((name) => ({
    name,
    items: visibleItems.filter(
      (item) =>
        (item.kind === "waker" ? (groupNames.get(item.id) ?? "") : "") === name,
    ),
  }));
  const navigationItems = collapsed
    ? items
    : groups.flatMap((group) => group.items);
  const activeItem = navigationItems.find((item) => item.key === activeKey);
  const itemKeys = navigationItems.map((item) => item.key);
  const groupAt = (index: number) => {
    const item = navigationItems[index];
    return item && (groupNames.get(item.id) ?? "");
  };
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: (event, args) => {
        const side = event.code;
        const over = args.context.over;
        if (
          groupedNavigation &&
          over &&
          (side === "ArrowLeft" || side === "ArrowRight") &&
          groupAt(itemKeys.indexOf(String(args.active))) !==
            groupAt(itemKeys.indexOf(String(over.id)))
        ) {
          event.preventDefault();
          const target: NavigationDropTarget = {
            targetId: String(over.id),
            placement: side === "ArrowLeft" ? "before" : "after",
            keyboard: true,
          };
          keyboardDropTarget.current = target;
          setDropTarget(target);
          return;
        }
        return sortableKeyboardCoordinates(event, args);
      },
    }),
  );
  const keyboardTargetItem = dropTarget?.keyboard
    ? navigationItems.find((item) => item.key === dropTarget.targetId)
    : undefined;
  const highlightedGroup = dropTarget
    ? groupAt(itemKeys.indexOf(dropTarget.targetId))
    : undefined;
  const sortWithinGroups: SortingStrategy = (args) => {
    const sourceGroup = groupAt(args.activeIndex);
    if (
      groupAt(args.overIndex) !== sourceGroup ||
      groupAt(args.index) !== sourceGroup
    ) {
      return null;
    }
    return verticalListSortingStrategy(args);
  };
  const entityName =
    mode === "waker"
      ? t("partnerNavigation.wakers")
      : t("partnerNavigation.teams");
  const feedbackStatus =
    entityStatus === "loading"
      ? "loading"
      : entityStatus === "error"
        ? "error"
        : items.length === 0
          ? "empty"
          : undefined;
  const feedbackLabel = feedbackStatus
    ? t(
        feedbackStatus === "loading"
          ? "partnerNavigation.loadingEntities"
          : feedbackStatus === "error"
            ? "partnerNavigation.entitiesUnavailable"
            : "partnerNavigation.noEntities",
        { name: entityName },
      )
    : "";
  const createLabel =
    mode === "waker"
      ? t("partnerNavigation.createWaker")
      : t("partnerNavigation.createTeam");
  const searchLabel =
    mode === "waker"
      ? t("partnerNavigation.searchWakers")
      : t("partnerNavigation.searchTeams");

  useEffect(
    () => () => {
      if (switchTimer.current) window.clearTimeout(switchTimer.current);
    },
    [],
  );
  const changeMode = (nextMode: PartnerNavigationPreferences["entityMode"]) => {
    if (nextMode === mode || switching) return;
    setSwitching(true);
    setSearchOpen(false);
    setSearchQuery("");
    onModeChange(nextMode);
    switchTimer.current = window.setTimeout(
      () => setSwitching(false),
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
        ? 0
        : modeSwitchDurationMs,
    );
  };
  const toggleMode = () => changeMode(mode === "waker" ? "team" : "waker");
  const openingSearchRef = useRef(false);
  const closeSearch = () => {
    setSearchOpen(false);
    setSearchQuery("");
  };
  const openSearch = () => {
    openingSearchRef.current = true;
    setSearchOpen(true);
  };
  const handleSearchBlur = () => {
    window.setTimeout(() => {
      if (openingSearchRef.current) return;
      closeSearch();
    }, 0);
  };
  useEffect(() => {
    if (!searchOpen) return;
    const frame = window.requestAnimationFrame(() => {
      openingSearchRef.current = false;
    });
    return () => window.cancelAnimationFrame(frame);
  }, [searchOpen]);
  const expandedCreateToolbar = (
    <div className="mt-3 flex items-center gap-2 pb-1">
      <button
        type="button"
        onClick={() => onManageEntities(mode)}
        aria-label={createLabel}
        className={cn(
          "border-border text-content-secondary hover:border-border-strong hover:bg-action-quiet-hover hover:text-content-primary focus-visible:ring-ring duration-quick ease-ui h-control-search-sm flex items-center justify-center rounded-sm border border-dashed transition-[background-color,border-color,color] outline-none focus-visible:ring-2",
          searchOpen
            ? "w-control-search-sm shrink-0"
            : "type-ui min-w-0 flex-1 gap-1.5 px-3",
        )}
      >
        <Plus className="size-icon-sm shrink-0" strokeWidth={1.5} />
        {searchOpen ? null : <span className="truncate">{createLabel}</span>}
      </button>
      {searchOpen ? (
        <SearchField
          aria-label={searchLabel}
          autoFocus
          containerClassName="min-w-0 flex-1"
          onChange={(event) => setSearchQuery(event.target.value)}
          onBlur={handleSearchBlur}
          onKeyDown={(event) => {
            if (event.key !== "Escape") return;
            closeSearch();
          }}
          placeholder={searchLabel}
          size="sm"
          value={searchQuery}
        />
      ) : (
        <button
          type="button"
          aria-label={searchLabel}
          onClick={openSearch}
          className="text-content-muted hover:bg-action-quiet-hover hover:text-content-primary focus-visible:ring-ring size-control-search-sm grid shrink-0 place-items-center rounded-sm outline-none focus-visible:ring-2"
        >
          <Search className="size-icon-sm" strokeWidth={1.5} />
        </button>
      )}
    </div>
  );
  const collapsedModeButton = (
    <Button
      type="button"
      disabled={switching}
      aria-label={
        mode === "waker"
          ? t("partnerNavigation.switchToTeams")
          : t("partnerNavigation.switchToWakers")
      }
      onClick={toggleMode}
      className="mt-1"
      shape="circle"
      size="icon"
      variant="ghost"
    >
      <ArrowLeftRight
        className={cn(
          "size-icon-md duration-standard transition-transform",
          switching && "rotate-180",
        )}
        strokeWidth={1.5}
      />
    </Button>
  );
  const syncIndicator = useCallback(
    (nextHoveredRailKey = hoveredRailKey) => {
      const indicator = indicatorRef.current;
      if (!indicator || !collapsed) return;
      if (activeKey) {
        indicator.style.opacity = "0";
        return;
      }
      const targetKey = nextHoveredRailKey || selectedKey;
      const item = itemRefs.current.get(targetKey);
      if (!item) {
        indicator.style.opacity = "0";
        return;
      }
      const expanded =
        targetKey === selectedKey &&
        (!nextHoveredRailKey || nextHoveredRailKey === selectedKey);
      const height = expanded
        ? partnerRailIndicatorHeight.selected
        : partnerRailIndicatorHeight.hover;
      const top = item.offsetTop + item.offsetHeight / 2 - height / 2;
      indicator.style.top = `${top}px`;
      indicator.style.height = `${height}px`;
      indicator.style.opacity = "1";
    },
    [activeKey, collapsed, hoveredRailKey, selectedKey],
  );
  const registerItemRef = useCallback(
    (key: string, node: HTMLButtonElement | null) => {
      if (node) itemRefs.current.set(key, node);
      else itemRefs.current.delete(key);
    },
    [],
  );
  useLayoutEffect(() => {
    syncIndicator();
  }, [items, navigationItems, syncIndicator]);
  useEffect(() => {
    if (!collapsed) return;
    const list = listRef.current;
    if (!list) return;
    const handleScroll = () => syncIndicator();
    list.addEventListener("scroll", handleScroll, { passive: true });
    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(handleScroll);
      observer.observe(list);
      for (const item of itemRefs.current.values()) observer.observe(item);
    }
    return () => {
      list.removeEventListener("scroll", handleScroll);
      observer?.disconnect();
    };
  }, [collapsed, navigationItems, syncIndicator]);
  const resolveDrop = ({ active, activatorEvent, over }: DragMoveEvent) => {
    if (!canReorder) return null;
    const sourceId = String(active.id);
    if (!over || sourceId === String(over.id)) return null;
    const targetId = String(over.id);
    const sourceIndex = itemKeys.indexOf(sourceId);
    const targetIndex = itemKeys.indexOf(targetId);
    if (sourceIndex < 0 || targetIndex < 0) return null;

    const crossGroup =
      groupedNavigation && groupAt(sourceIndex) !== groupAt(targetIndex);
    let placement: NavigationDropPlacement =
      sourceIndex < targetIndex ? "after" : "before";
    const keyboardTarget = keyboardDropTarget.current;
    if (crossGroup && keyboardTarget?.targetId === targetId)
      placement = keyboardTarget.placement;
    const translated = active.rect.current.translated;
    if (
      mode === "waker" &&
      (!groupedNavigation || crossGroup) &&
      translated &&
      activatorEvent instanceof MouseEvent
    ) {
      const center = translated.top + translated.height / 2;
      const targetCenter = over.rect.top + over.rect.height / 2;
      if (center !== targetCenter)
        placement = center < targetCenter ? "before" : "after";
    }
    return { sourceId, targetId, placement, crossGroup };
  };
  const previewDrop = (event: DragMoveEvent) => {
    if (keyboardDropTarget.current?.targetId !== event.over?.id)
      keyboardDropTarget.current = null;
    const drop = resolveDrop(event);
    const next = drop?.crossGroup
      ? {
          targetId: drop.targetId,
          placement: drop.placement,
          keyboard: event.activatorEvent instanceof KeyboardEvent,
        }
      : null;
    setDropTarget((current) =>
      current?.targetId === next?.targetId &&
      current?.placement === next?.placement &&
      current?.keyboard === next?.keyboard
        ? current
        : next,
    );
  };
  const clearDrag = () => {
    keyboardDropTarget.current = null;
    setActiveKey("");
    setDropTarget(null);
    syncIndicator();
  };
  const finishDrag = (event: DragEndEvent) => {
    const drop = resolveDrop(event);
    if (drop) onReorder(drop.sourceId, drop.targetId, drop.placement);
    clearDrag();
  };

  return (
    <TooltipProvider delayDuration={350}>
      <aside
        className={cn(
          "flex min-h-0 flex-col overflow-x-hidden",
          floating
            ? "max-h-[calc(var(--radix-popover-content-available-height)-0.5rem)] min-h-0 bg-transparent"
            : "h-full bg-transparent",
          collapsed
            ? floating
              ? "items-center"
              : "items-center px-2 pt-0 pb-2"
            : "min-w-0 px-3 py-2",
        )}
        data-collapsed={collapsed}
        data-floating={floating || undefined}
      >
        {collapsed ? (
          <div
            className={cn(
              "flex w-full shrink-0 flex-col items-center overflow-hidden px-1 pb-2",
              floating
                ? "max-h-full min-h-0 flex-1 shrink rounded-2xl bg-transparent"
                : "min-h-0 flex-1 bg-transparent",
            )}
          >
            {floating ? (
              collapsedModeButton
            ) : (
              <Tooltip>
                <TooltipTrigger asChild>{collapsedModeButton}</TooltipTrigger>
                <TooltipContent side="right">
                  {mode === "waker"
                    ? t("partnerNavigation.switchToTeams")
                    : t("partnerNavigation.switchToWakers")}
                </TooltipContent>
              </Tooltip>
            )}
            <span
              className="bg-border my-1 h-px w-6 shrink-0"
              aria-hidden="true"
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => onManageEntities(mode)}
                  aria-label={createLabel}
                  className="border-border text-muted-foreground hover:border-border-strong hover:bg-action-quiet-hover hover:text-content-primary focus-visible:ring-ring size-control-md duration-quick ease-ui grid shrink-0 place-items-center rounded-full border border-dashed transition-[background-color,border-color,color] outline-none focus-visible:ring-2"
                >
                  <Plus className="size-icon-md" strokeWidth={1.5} />
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">{createLabel}</TooltipContent>
            </Tooltip>
            <DndContext
              collisionDetection={closestCenter}
              modifiers={[restrictToVerticalAxis]}
              sensors={canReorder ? sensors : []}
              onDragCancel={clearDrag}
              onDragEnd={finishDrag}
              onDragStart={({ active }) => {
                setActiveKey(String(active.id));
                indicatorRef.current?.style.setProperty("opacity", "0");
              }}
            >
              <nav
                aria-label={t("partnerNavigation.partnerRail")}
                className={cn(
                  "group/partner-rail mt-1 flex w-full flex-col items-center",
                  "min-h-0 flex-1",
                )}
                data-motion="view"
                data-state={switching ? "open" : undefined}
                data-mode={mode}
              >
                <div
                  ref={listRef}
                  className={cn(
                    "scrollbar-hidden relative grid min-w-0 gap-1 overflow-x-hidden",
                    "min-h-0 flex-1 overflow-y-auto overscroll-contain",
                  )}
                >
                  <span
                    ref={indicatorRef}
                    aria-hidden="true"
                    className="bg-accent duration-ui ease-ui pointer-events-none absolute left-0 z-10 w-[3px] rounded-r-full opacity-0 transition-[top,height,opacity]"
                  />
                  <SortableContext
                    items={itemKeys}
                    strategy={verticalListSortingStrategy}
                  >
                    {feedbackStatus ? (
                      <EntityFeedback
                        collapsed
                        label={feedbackLabel}
                        status={feedbackStatus}
                      />
                    ) : null}
                    {navigationItems.map((item) => (
                      <EntityRow
                        key={item.key}
                        canReorder={canReorder}
                        collapsed
                        item={item}
                        onHoverEnd={() => {
                          setHoveredRailKey("");
                          syncIndicator("");
                        }}
                        onHoverStart={() => {
                          setHoveredRailKey(item.key);
                          syncIndicator(item.key);
                        }}
                        onSelectPartner={onSelectPartner}
                        onSelectTeam={onSelectTeam}
                        onShowcaseWaker={onShowcaseWaker}
                        registerItemRef={registerItemRef}
                        selected={item.key === selectedKey}
                      />
                    ))}
                  </SortableContext>
                </div>
              </nav>
              <DragOverlay>
                {activeItem ? (
                  <div className="bg-surface size-row-navigation grid place-items-center rounded-sm shadow-sm">
                    <EntityAvatar item={activeItem} />
                  </div>
                ) : null}
              </DragOverlay>
            </DndContext>
          </div>
        ) : (
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <SegmentedControl
              appearance="shell"
              ariaLabel={t("partnerNavigation.entityMode")}
              distribution="fill"
              onValueChange={changeMode}
              options={[
                { label: t("partnerNavigation.wakers"), value: "waker" },
                { label: t("partnerNavigation.teams"), value: "team" },
              ]}
              size="sm"
              value={mode}
            />
            {sidebarSearch === undefined ? expandedCreateToolbar : null}
            {onRetryGroupImport && mode === "waker" ? (
              <div className="flex items-center justify-between gap-2 px-2 py-1">
                <p role="alert" className="text-content-muted type-ui">
                  {t("partnerNavigation.wakerActions.groupImportError")}
                </p>
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  onClick={onRetryGroupImport}
                >
                  {t("retry")}
                </Button>
              </div>
            ) : null}
            <DndContext
              accessibility={
                groupedNavigation
                  ? {
                      screenReaderInstructions: {
                        draggable: t(
                          "partnerNavigation.groupedDragInstructions",
                        ),
                      },
                    }
                  : undefined
              }
              collisionDetection={closestCenter}
              modifiers={[restrictToVerticalAxis]}
              sensors={canReorder ? sensors : []}
              onDragCancel={clearDrag}
              onDragEnd={finishDrag}
              onDragMove={groupedNavigation ? previewDrop : undefined}
              onDragOver={groupedNavigation ? previewDrop : undefined}
              onDragStart={({ active }) => setActiveKey(String(active.id))}
            >
              {groupedNavigation ? (
                <span className="sr-only" role="status">
                  {keyboardTargetItem && dropTarget
                    ? t(
                        dropTarget.placement === "before"
                          ? "partnerNavigation.dropBefore"
                          : "partnerNavigation.dropAfter",
                        {
                          name: entityLabel(
                            keyboardTargetItem,
                            t("partnerNavigation.partner"),
                          ),
                        },
                      )
                    : null}
                </span>
              ) : null}
              <SortableContext
                items={itemKeys}
                strategy={
                  groupedNavigation
                    ? sortWithinGroups
                    : verticalListSortingStrategy
                }
              >
                <nav
                  aria-label={t("partnerNavigation.partnerRail")}
                  className={cn(
                    "scrollbar-hidden mt-1 min-h-0 min-w-0 flex-1 space-y-0.5 overflow-x-hidden overflow-y-auto",
                  )}
                  data-motion="view"
                  data-state={switching ? "open" : undefined}
                  data-mode={mode}
                >
                  {feedbackStatus ? (
                    <EntityFeedback
                      collapsed={false}
                      label={feedbackLabel}
                      status={feedbackStatus}
                    />
                  ) : null}
                  {!feedbackStatus && visibleItems.length === 0 ? (
                    <p className="text-content-muted type-ui px-2 py-4 text-center">
                      {t("partnerNavigation.noSearchResults")}
                    </p>
                  ) : null}
                  {groups.flatMap((group) => {
                    // Highlight across siblings so a moved row keeps its DOM identity.
                    const showGroupHighlight = group.name === highlightedGroup;
                    const rows = group.items.map((item, index) => {
                      const row = (
                        <EntityRow
                          key={item.key}
                          canReorder={canReorder}
                          collapsed={false}
                          dropGroupEdge={
                            showGroupHighlight
                              ? index === group.items.length - 1
                                ? "end"
                                : "middle"
                              : undefined
                          }
                          dropPlacement={
                            dropTarget?.targetId === item.key
                              ? dropTarget.placement
                              : undefined
                          }
                          item={item}
                          onSelectPartner={onSelectPartner}
                          onSelectTeam={onSelectTeam}
                          onShowcaseWaker={onShowcaseWaker}
                          selected={item.key === selectedKey}
                        />
                      );
                      if (item.kind === "waker" && renderWakerRow)
                        return renderWakerRow(item, row);
                      if (item.kind === "team" && renderTeamRow)
                        return renderTeamRow(item, row);
                      return row;
                    });
                    if (!rows.length || !groupedNavigation) return rows;
                    return [
                      <h3
                        key={`group:${group.name}`}
                        data-drop-group={
                          showGroupHighlight ? "start" : undefined
                        }
                        className={cn(
                          "text-muted-foreground type-caption px-3 pt-2 pb-1",
                          showGroupHighlight &&
                            "relative before:-bottom-0.5 before:rounded-t-md before:border-t",
                          showGroupHighlight && dropGroupHighlight,
                        )}
                      >
                        <span className="block truncate">
                          {group.name ||
                            t("partnerNavigation.wakerActions.ungrouped")}
                        </span>
                      </h3>,
                      ...rows,
                    ];
                  })}
                </nav>
              </SortableContext>
              <DragOverlay dropAnimation={groupedNavigation ? null : undefined}>
                {activeItem ? (
                  <div className="bg-surface flex min-w-40 items-center gap-2 rounded-md p-2 shadow-sm">
                    <EntityAvatar item={activeItem} size="navigation" />
                    <span className="type-body truncate">
                      {entityLabel(activeItem, t("partnerNavigation.partner"))}
                    </span>
                  </div>
                ) : null}
              </DragOverlay>
            </DndContext>
          </div>
        )}

        {history}
      </aside>
    </TooltipProvider>
  );
}

type FloatingEntityNavigationProps = Omit<
  EntityNavigationProps,
  "collapsed" | "floating" | "history"
> & {
  renderEntityChats?: (
    item: NavigationEntity,
    onBack: () => void,
    onClose: () => void,
  ) => ReactNode;
  selectedItem?: NavigationEntity;
};

export function FloatingEntityNavigation({
  items,
  mode,
  onManageEntities,
  renderEntityChats,
  onSelectPartner,
  onSelectTeam,
  onShowcaseWaker,
  selectedItem,
  ...props
}: FloatingEntityNavigationProps) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);
  const [conversationEntityKey, setChatEntityKey] = useState("");
  const conversationEntity = items.find(
    (item) => item.key === conversationEntityKey,
  );
  const selectedLabel = selectedItem
    ? entityLabel(
        selectedItem,
        selectedItem.kind === "waker"
          ? t("partnerNavigation.partner")
          : t("partnerNavigation.team"),
      )
    : "";
  const triggerLabel = selectedItem
    ? t("partnerNavigation.openEntitySwitcherWithCurrent", {
        name: selectedLabel,
      })
    : t("partnerNavigation.openEntitySwitcher");
  const launcherLabel =
    mode === "waker"
      ? t("partnerNavigation.selectWaker")
      : t("partnerNavigation.selectTeam");
  const selectedStatus = selectedItem
    ? entityActivityState(selectedItem)
    : "idle";
  const closePopover = () => {
    setChatEntityKey("");
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) setChatEntityKey("");
      }}
    >
      <PopoverTrigger asChild>
        <Button
          aria-label={triggerLabel}
          className="group/launcher hover:w-control-launcher focus-visible:w-control-launcher relative transition-[width,background-color,color,box-shadow,opacity]"
          data-activity-state={selectedStatus}
          shape="corner"
          size="icon-lg"
          type="button"
          variant="launcher"
        >
          <span className="size-control-lg relative grid shrink-0 place-items-center">
            <UsersRound className="size-icon-lg" strokeWidth={1.5} />
            {selectedItem ? (
              <ConversationAttentionMark
                mark={entityMark(selectedItem)}
                className="pointer-events-none absolute top-3 right-3 translate-x-1/2 -translate-y-1/2"
                variant="avatar"
              />
            ) : null}
          </span>
          <span
            aria-hidden="true"
            className="type-ui -translate-x-1 pr-4 whitespace-nowrap opacity-0 transition-[opacity,transform] group-hover/launcher:translate-x-0 group-hover/launcher:opacity-100 group-focus-visible/launcher:translate-x-0 group-focus-visible/launcher:opacity-100"
            data-launcher-label
          >
            {launcherLabel}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        aria-label={t(
          conversationEntity
            ? "partnerNavigation.conversationHistory"
            : "partnerNavigation.entitySwitcher",
        )}
        collisionPadding={16}
        side="top"
        sideOffset={8}
        variant="navigation"
      >
        {conversationEntity && renderEntityChats ? (
          renderEntityChats(
            conversationEntity,
            () => setChatEntityKey(""),
            closePopover,
          )
        ) : (
          <EntityNavigation
            {...props}
            collapsed
            floating
            items={items}
            mode={mode}
            onManageEntities={(currentMode) => {
              setOpen(false);
              onManageEntities(currentMode);
            }}
            onSelectPartner={(partnerId) => {
              const item = items.find(
                (candidate) =>
                  candidate.kind === "waker" && candidate.id === partnerId,
              );
              if (item && renderEntityChats) {
                setChatEntityKey(item.key);
                return;
              }
              setOpen(false);
              onSelectPartner(partnerId);
            }}
            onSelectTeam={(channelId) => {
              const item = items.find(
                (candidate) =>
                  candidate.kind === "team" && candidate.id === channelId,
              );
              if (item && renderEntityChats) {
                setChatEntityKey(item.key);
                return;
              }
              setOpen(false);
              onSelectTeam(channelId);
            }}
            onShowcaseWaker={(waker, origin) => {
              onShowcaseWaker?.(waker, origin);
              setOpen(false);
            }}
          />
        )}
      </PopoverContent>
    </Popover>
  );
}
