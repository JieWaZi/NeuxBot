import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  Package,
  BookOpen,
  ArrowLeftRight,
  ChevronUp,
  CircleAlert,
  Clock3,
  MessageCircle,
  Workflow,
  LoaderCircle,
  Plus,
  Search,
  Settings,
  UsersRound,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import { TeamAvatarStack } from "@/modules/team";
import type { Waker } from "@/modules/waker";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as ChannelDetail } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { Avatar } from "@/shared/ui/avatar";
import { wakerCompanionActivity } from "@/shared/companion/activity";
import { Button } from "@/shared/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import { NavigationItem } from "@/shared/ui/navigation-panel";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

import {
  EntityActivityBadges,
  EntityActivitySummary,
  entityActivityCounts,
  entityActivitySummaryLabel,
} from "./entity-activity-summary";
import {
  readNavigationActionsExpanded,
  writeNavigationActionsExpanded,
  type PartnerNavigationPreferences,
} from "./partner-navigation-preferences";

export type NavigationEntity =
  | { id: string; key: string; kind: "team"; team: ChannelDetail }
  | { id: string; key: string; kind: "waker"; waker: Waker };

type EntityNavigationProps = {
  onOpenIM?: () => void;
  onOpenAutomaticTasks?: () => void;
  onOpenWorkflows?: () => void;
  onOpenResources?: () => void;
  onOpenKnowledge?: () => void;
  collapsed: boolean;
  entityStatus?: "error" | "loading" | "ready";
  floating?: boolean;
  items: NavigationEntity[];
  mode: PartnerNavigationPreferences["entityMode"];
  onManageEntities: (mode: PartnerNavigationPreferences["entityMode"]) => void;
  onModeChange: (mode: PartnerNavigationPreferences["entityMode"]) => void;
  onNewChat: (item: NavigationEntity) => void;
  onOpenSettings: () => void;
  onReorder: (sourceId: string, targetId: string) => void;
  onSelectPartner: (partnerId: string) => void;
  onSelectTeam: (channelId: string) => void;
  onShowcaseWaker?: (waker: Waker, origin: HTMLElement) => void;
  selectedKey: string;
  showSettings?: boolean;
};

const modeSwitchDurationMs = 220;
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

function TeamEntityAvatar({ team }: { team: ChannelDetail }) {
  const members = team.members ?? [];
  if (!members.some((member) => member.waker?.id)) {
    return (
      <span
        className="bg-muted text-muted-foreground size-avatar-sm grid place-items-center overflow-hidden rounded-sm"
        aria-hidden="true"
      >
        <UsersRound className="size-icon-md" strokeWidth={1.5} />
      </span>
    );
  }
  return <TeamAvatarStack members={members} />;
}

function EntityAvatar({
  item,
  interactive = false,
  onDoubleInteract,
}: {
  item: NavigationEntity;
  interactive?: boolean;
  onDoubleInteract?: (trigger: HTMLElement) => void;
}) {
  const { t } = useTranslation("common");
  return item.kind === "waker" ? (
    <Avatar
      interactive={interactive}
      name={item.waker.name ?? t("partnerNavigation.partner")}
      onDoubleInteract={onDoubleInteract}
      shape="square"
      src={item.waker.avatar}
      activity={wakerCompanionActivity(item.waker.activityState)}
      size="sm"
    />
  ) : (
    <TeamEntityAvatar team={item.team} />
  );
}

function entityLabel(item: NavigationEntity, fallback: string) {
  return item.kind === "waker"
    ? item.waker.name || fallback
    : item.team.channel?.name || fallback;
}

function entityActivityState(item: NavigationEntity) {
  return item.kind === "waker"
    ? (item.waker.activityState ?? "idle")
    : (item.team.activityState ?? "idle");
}

function EntityRow({
  collapsed,
  item,
  onHoverEnd,
  onHoverStart,
  onNewChat,
  onSelectPartner,
  onSelectTeam,
  onShowcaseWaker,
  registerItemRef,
  selected,
}: {
  collapsed: boolean;
  item: NavigationEntity;
  onHoverEnd?: () => void;
  onHoverStart?: () => void;
  onNewChat: (item: NavigationEntity) => void;
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
  const activityCounts = entityActivityCounts(item);
  const activitySummaryLabelText = entityActivitySummaryLabel(
    activityCounts,
    t,
  );
  const activitySummaryParts = activitySummaryLabelText
    ? activitySummaryLabelText.split(" · ")
    : [];
  const accessibleLabel = [label, ...activitySummaryParts].join("，");
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
        style={{
          transform: CSS.Transform.toString(transform),
          transition,
        }}
        className={cn(
          "group/entity hover:bg-surface-subtle min-h-row-navigation relative grid w-full min-w-0 items-center rounded-md",
          "grid-cols-[auto_minmax(0,1fr)_auto]",
          selected && "bg-selection-surface text-content-primary",
          isDragging && "z-10 opacity-35",
        )}
      >
        <span className="flex shrink-0 items-center pl-2">
          <EntityAvatar
            item={item}
            interactive={item.kind === "waker"}
            onDoubleInteract={
              item.kind === "waker"
                ? (origin) => onShowcaseWaker?.(item.waker, origin)
                : undefined
            }
          />
        </span>
        <button
          type="button"
          aria-current={selected ? "page" : undefined}
          aria-label={accessibleLabel}
          onClick={handleRowClick}
          className="focus-visible:ring-ring min-h-row-navigation relative grid min-w-0 items-center rounded-md px-2 py-1 text-left outline-none focus-visible:ring-2"
          {...attributes}
          {...listeners}
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
        <div className="flex items-center gap-0.5 pr-1">
          <EntityActivitySummary counts={activityCounts} />
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                size="icon"
                variant="ghost"
                aria-label={t("partnerNavigation.newChat")}
                className="opacity-0 group-focus-within/entity:opacity-100 group-hover/entity:opacity-100 hover:bg-transparent"
                onClick={(event) => {
                  event.stopPropagation();
                  onNewChat(item);
                }}
                onPointerDown={(event) => event.stopPropagation()}
              >
                <Plus className="size-icon-sm" strokeWidth={1.5} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("partnerNavigation.newChat")}</TooltipContent>
          </Tooltip>
        </div>
      </div>
    );
  }

  const button = (
    <button
      ref={(node) => {
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
      {...attributes}
      {...listeners}
    >
      <EntityAvatar item={item} />
      <EntityActivityBadges
        className="pointer-events-none absolute top-0.5 right-0.5"
        counts={activityCounts}
      />
    </button>
  );

  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent side="right">
        {activitySummaryParts.length > 0
          ? `${label} · ${activitySummaryParts.join(" · ")}`
          : label}
      </TooltipContent>
    </Tooltip>
  );
}

export function EntityNavigation({
  onOpenIM,
  onOpenAutomaticTasks,
  onOpenWorkflows,
  onOpenResources,
  onOpenKnowledge,
  collapsed,
  entityStatus = "ready",
  floating = false,
  items,
  mode,
  onManageEntities,
  onModeChange,
  onNewChat,
  onOpenSettings,
  onReorder,
  onSelectPartner,
  onSelectTeam,
  onShowcaseWaker,
  selectedKey,
  showSettings = true,
}: EntityNavigationProps) {
  const { t } = useTranslation("common");
  const [activeKey, setActiveKey] = useState("");
  const [hoveredRailKey, setHoveredRailKey] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [moreOpen, setMoreOpen] = useState(readNavigationActionsExpanded);
  const changeMoreOpen = (open: boolean) => {
    setMoreOpen(open);
    writeNavigationActionsExpanded(open);
  };
  const [switching, setSwitching] = useState(false);
  const switchTimer = useRef<number | undefined>(undefined);
  const itemRefs = useRef(new Map<string, HTMLButtonElement>());
  const indicatorRef = useRef<HTMLSpanElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );
  const normalizedSearchQuery = searchQuery.trim().toLocaleLowerCase("zh-CN");
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
  const navigationItems = collapsed ? items : visibleItems;
  const activeItem = navigationItems.find((item) => item.key === activeKey);
  const itemKeys = navigationItems.map((item) => item.key);
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
  const finishDrag = ({ active, over }: DragEndEvent) => {
    const sourceId = String(active.id);
    const targetId = over ? String(over.id) : "";
    if (targetId && targetId !== sourceId) {
      onReorder(sourceId, targetId);
    }
    setActiveKey("");
    syncIndicator();
  };

  return (
    <TooltipProvider delayDuration={350}>
      <aside
        className={cn(
          "flex min-h-0 flex-col overflow-x-hidden",
          floating
            ? "max-h-[calc(var(--radix-popover-content-available-height)-0.5rem)] min-h-0 bg-transparent"
            : "bg-surface-chrome h-full",
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
                : "bg-surface-scene rounded-full",
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
              sensors={sensors}
              onDragCancel={() => {
                setActiveKey("");
                syncIndicator();
              }}
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
                  floating ? "min-h-0 flex-1" : "shrink-0",
                )}
                data-motion="view"
                data-state={switching ? "open" : undefined}
                data-mode={mode}
              >
                <div
                  ref={listRef}
                  className={cn(
                    "scrollbar-hidden relative grid min-w-0 gap-1 overflow-x-hidden",
                    floating
                      ? "min-h-0 flex-1 overflow-y-auto overscroll-contain"
                      : "overflow-y-visible",
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
                        onNewChat={onNewChat}
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
            {expandedCreateToolbar}
            <DndContext
              collisionDetection={closestCenter}
              modifiers={[restrictToVerticalAxis]}
              sensors={sensors}
              onDragCancel={() => setActiveKey("")}
              onDragEnd={finishDrag}
              onDragStart={({ active }) => setActiveKey(String(active.id))}
            >
              <SortableContext
                items={itemKeys}
                strategy={verticalListSortingStrategy}
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
                  {visibleItems.map((item) => (
                    <EntityRow
                      key={item.key}
                      collapsed={false}
                      item={item}
                      onNewChat={onNewChat}
                      onSelectPartner={onSelectPartner}
                      onSelectTeam={onSelectTeam}
                      onShowcaseWaker={onShowcaseWaker}
                      selected={item.key === selectedKey}
                    />
                  ))}
                </nav>
              </SortableContext>
              <DragOverlay>
                {activeItem ? (
                  <div className="bg-surface flex min-w-40 items-center gap-2 rounded-md p-2 shadow-sm">
                    <EntityAvatar item={activeItem} />
                    <span className="type-body truncate">
                      {entityLabel(activeItem, t("partnerNavigation.partner"))}
                    </span>
                  </div>
                ) : null}
              </DragOverlay>
            </DndContext>
          </div>
        )}

        {showSettings ? (
          <Collapsible open={moreOpen} onOpenChange={changeMoreOpen} asChild>
            <div
              className={cn(
                "border-border-subtle w-full shrink-0 border-t pt-2",
                collapsed ? "mt-auto grid justify-items-center gap-1" : "mt-2",
              )}
            >
              {onOpenResources ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    {collapsed ? (
                      <Button
                        size="icon"
                        variant="choice"
                        aria-label={t("plugin:title")}
                        onClick={onOpenResources}
                      >
                        <Package />
                      </Button>
                    ) : (
                      <NavigationItem
                        density="navigation"
                        icon={<Package strokeWidth={1.5} />}
                        label={t("plugin:title")}
                        onClick={onOpenResources}
                      />
                    )}
                  </TooltipTrigger>
                  {collapsed ? (
                    <TooltipContent side="right">
                      {t("plugin:title")}
                    </TooltipContent>
                  ) : null}
                </Tooltip>
              ) : null}
              {onOpenKnowledge ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    {collapsed ? (
                      <Button
                        size="icon"
                        variant="choice"
                        aria-label={t("knowledge:knowledge")}
                        onClick={onOpenKnowledge}
                      >
                        <BookOpen />
                      </Button>
                    ) : (
                      <NavigationItem
                        density="navigation"
                        icon={<BookOpen strokeWidth={1.5} />}
                        label={t("knowledge:knowledge")}
                        onClick={onOpenKnowledge}
                      />
                    )}
                  </TooltipTrigger>
                  {collapsed ? (
                    <TooltipContent side="right">
                      {t("knowledge:knowledge")}
                    </TooltipContent>
                  ) : null}
                </Tooltip>
              ) : null}
              {onOpenAutomaticTasks ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    {collapsed ? (
                      <Button
                        size="icon"
                        variant="choice"
                        aria-label={t("automaticTask:title")}
                        onClick={onOpenAutomaticTasks}
                      >
                        <Clock3 />
                      </Button>
                    ) : (
                      <NavigationItem
                        density="navigation"
                        icon={<Clock3 strokeWidth={1.5} />}
                        label={t("automaticTask:title")}
                        onClick={onOpenAutomaticTasks}
                      />
                    )}
                  </TooltipTrigger>
                  {collapsed ? (
                    <TooltipContent side="right">
                      {t("automaticTask:title")}
                    </TooltipContent>
                  ) : null}
                </Tooltip>
              ) : null}
              <CollapsibleContent inert={!moreOpen} aria-hidden={!moreOpen}>
                <div
                  className={cn(collapsed && "grid justify-items-center gap-1")}
                >
                  {[
                    {
                      label: t("workflow:title"),
                      icon: Workflow,
                      action: onOpenWorkflows,
                    },
                    {
                      label: t("im:title"),
                      icon: MessageCircle,
                      action: onOpenIM,
                    },
                  ]
                    .filter((item) => item.action)
                    .map(({ label, icon: Icon, action }) => (
                      <Tooltip key={label}>
                        <TooltipTrigger asChild>
                          {collapsed ? (
                            <Button
                              size="icon"
                              variant="choice"
                              aria-label={label}
                              onClick={action}
                            >
                              <Icon />
                            </Button>
                          ) : (
                            <NavigationItem
                              density="navigation"
                              icon={<Icon strokeWidth={1.5} />}
                              label={label}
                              onClick={action}
                            />
                          )}
                        </TooltipTrigger>
                        {collapsed ? (
                          <TooltipContent side="right">{label}</TooltipContent>
                        ) : null}
                      </Tooltip>
                    ))}
                </div>
              </CollapsibleContent>
              <div
                className={cn(
                  "flex items-center",
                  collapsed && "flex-col gap-1",
                )}
              >
                <Tooltip>
                  <TooltipTrigger asChild>
                    {collapsed ? (
                      <Button
                        size="icon"
                        variant="choice"
                        aria-label={t("partnerNavigation.settings")}
                        onClick={onOpenSettings}
                      >
                        <Settings />
                      </Button>
                    ) : (
                      <NavigationItem
                        density="navigation"
                        icon={<Settings strokeWidth={1.5} />}
                        label={t("partnerNavigation.settings")}
                        onClick={onOpenSettings}
                      />
                    )}
                  </TooltipTrigger>
                  {collapsed ? (
                    <TooltipContent side="right">
                      {t("partnerNavigation.settings")}
                    </TooltipContent>
                  ) : null}
                </Tooltip>
                {onOpenWorkflows || onOpenIM ? (
                  <CollapsibleTrigger asChild>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="shrink-0"
                      aria-label={t(
                        moreOpen
                          ? "partnerNavigation.collapseNavigationActions"
                          : "partnerNavigation.expandNavigationActions",
                      )}
                    >
                      <ChevronUp
                        aria-hidden="true"
                        className={cn(
                          "duration-standard ease-ui transition-transform motion-reduce:transition-none",
                          moreOpen && "rotate-180",
                        )}
                        strokeWidth={1.5}
                      />
                    </Button>
                  </CollapsibleTrigger>
                ) : null}
              </div>
            </div>
          </Collapsible>
        ) : null}
      </aside>
    </TooltipProvider>
  );
}

type FloatingEntityNavigationProps = Omit<
  EntityNavigationProps,
  "collapsed" | "floating" | "onOpenSettings" | "showSettings"
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
  onNewChat,
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
  const selectedActivityCounts = selectedItem
    ? entityActivityCounts(selectedItem)
    : { runningCount: 0, waitingCount: 0 };
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
          <span className="size-control-lg grid shrink-0 place-items-center">
            <UsersRound className="size-icon-lg" strokeWidth={1.5} />
          </span>
          <span
            aria-hidden="true"
            className="type-ui -translate-x-1 pr-4 whitespace-nowrap opacity-0 transition-[opacity,transform] group-hover/launcher:translate-x-0 group-hover/launcher:opacity-100 group-focus-visible/launcher:translate-x-0 group-focus-visible/launcher:opacity-100"
            data-launcher-label
          >
            {launcherLabel}
          </span>
          {selectedItem ? (
            <EntityActivityBadges
              className="absolute top-1 right-1"
              counts={selectedActivityCounts}
            />
          ) : null}
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
            onNewChat={(item) => {
              closePopover();
              onNewChat(item);
            }}
            onOpenSettings={() => undefined}
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
            showSettings={false}
          />
        )}
      </PopoverContent>
    </Popover>
  );
}
