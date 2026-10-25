import {
  ChatHistoryPopover,
  ChatHistoryPanelSurface,
  ChatHistoryNavigationRow,
  ChatForkMenuItem,
} from "@/modules/chat";
import {
  ArrowLeft,
  ArrowRight,
  Folder,
  LoaderCircle,
  MessageSquareText,
  PanelLeft,
} from "lucide-react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type Dispatch,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  type SetStateAction,
  type UIEvent,
} from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router-dom";

import {
  ChatTaskDialog,
  chatListInfiniteOptions,
  chatMessagesInfiniteOptions,
  getChatOptions,
} from "@/modules/chat";
import { WakerShowcaseDialog, type Waker } from "@/modules/waker";
import { trackHorizontalDrag } from "@/shared/lib/horizontal-drag";
import type {
  GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as ChannelDetail,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsChat as Chat,
} from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { platform } from "@/shared/platform/provider";
import { DetailDockLayout } from "@/shared/ui/detail-dock-layout";
import { navigateRouteImmediately } from "./immediate-route-navigation";
import { ChatGroupHeader } from "@/modules/chat";
import { TeamChatTree } from "./team-context-sidebar";
import {
  EntityNavigation,
  FloatingEntityNavigation,
  type NavigationEntity,
} from "./entity-navigation";
import { chatDraftHref, chatHref } from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import {
  clampContextSidebarWidth,
  contextSidebarWidthRange,
  moveItem,
  reconcilePartnerOrder,
  type PartnerNavigationPreferences,
} from "./partner-navigation-preferences";
import { VerticalResizeHandle } from "@/shared/ui/vertical-resize-handle";
import {
  SceneChromeHost,
  SceneChromePortalProvider,
} from "@/shared/ui/scene-chrome-portal";
import {
  FloatingChatLauncherProvider,
  useFloatingChatLauncher,
} from "@/shared/workspace/floating-chat-launcher";

type PartnerNavigationLayoutProps = {
  activeChatId?: string;
  channels: ChannelDetail[];
  children: ReactNode;
  contextSidebar?: ReactNode;
  notificationBell?: ReactNode;
  contextSidebarHidden?: boolean;
  entityStatus?: "error" | "loading" | "ready";
  onManageEntities: (mode: PartnerNavigationPreferences["entityMode"]) => void;
  onOpenPartnerDetail: (partnerId: string) => void;
  onOpenAutomaticTasks: () => void;
  onOpenWorkflows?: () => void;
  onOpenResources?: () => void;
  onOpenKnowledge?: () => void;
  onOpenSettings: () => void;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  onSceneNavigationVisibleChange: (visible: boolean) => void;
  onSelectPartner: (partnerId: string) => void;
  onSelectTeam: (channelId: string) => void;
  preferences: PartnerNavigationPreferences;
  sceneNavigationVisible: boolean;
  sceneContentFlush?: boolean;
  selectedPartnerId?: string;
  selectedRailKey: string;
  settingsSidebar?: ReactNode;
  settingsOpen: boolean;
  teamContext?: {
    channelId: string;
    threadId?: string;
  };
  wakers: Waker[];
};

type ChatGroup = {
  chats: Chat[];
  key: string;
  label: string;
  type: "folder" | "project";
};

const PartnerDetailDock = lazy(() =>
  import("./partner-detail-dock").then((module) => ({
    default: module.PartnerDetailDock,
  })),
);

function partnerSectionHref(partnerId: string, section: string) {
  const root = `/wakers/${partnerId}`;
  return section === "overview" ? root : `${root}/${section}`;
}

function waitingInteraction(value: unknown) {
  if (!value || typeof value !== "object") return false;
  return (
    ("waitingApproval" in value && value.waitingApproval === true) ||
    ("waitingElicitation" in value && value.waitingElicitation === true)
  );
}

function chatGroupOf(chat: Chat): Omit<ChatGroup, "chats"> {
  const projectId = chat.projectId?.trim();
  const projectName = chat.projectName?.trim();
  if (projectId) {
    return {
      key: `project:${projectId}`,
      label: projectName || "未命名项目",
      type: "project",
    };
  }
  const directory = chat.additionalWorkingDirectories?.[0]?.trim();
  const label =
    projectName ||
    directory?.split(/[\\/]/).filter(Boolean).at(-1) ||
    "临时文件夹";
  return {
    key: `folder:${directory || label}`,
    label,
    type: "folder",
  };
}

function groupChats(chats: readonly Chat[]): ChatGroup[] {
  const groups = new Map<string, ChatGroup>();
  for (const chat of chats) {
    if (!chat.id) continue;
    const group = chatGroupOf(chat);
    const current = groups.get(group.key);
    if (current) current.chats.push(chat);
    else groups.set(group.key, { ...group, chats: [chat] });
  }
  for (const group of groups.values()) {
    group.chats.sort(
      (left, right) => (right.updatedAt ?? 0) - (left.updatedAt ?? 0),
    );
  }
  return [...groups.values()].sort((left, right) => {
    if (left.type !== right.type) return left.type === "project" ? -1 : 1;
    return left.label.localeCompare(right.label, "zh-CN");
  });
}

function captureShowcase(waker: Waker, origin: HTMLElement) {
  const rect = origin.getBoundingClientRect();
  return {
    origin,
    rect: {
      height: rect.height,
      left: rect.left,
      top: rect.top,
      width: rect.width,
    },
    waker,
  };
}

export function PartnerNavigationLayout(props: PartnerNavigationLayoutProps) {
  return (
    <FloatingChatLauncherProvider>
      <PartnerNavigationLayoutContent {...props} />
    </FloatingChatLauncherProvider>
  );
}

function PartnerNavigationLayoutContent({
  activeChatId,
  channels,
  children,
  contextSidebar,
  notificationBell,
  contextSidebarHidden = false,
  entityStatus = "ready",
  onManageEntities,
  onOpenPartnerDetail,
  onOpenAutomaticTasks,
  onOpenWorkflows,
  onOpenResources,
  onOpenKnowledge,
  onOpenSettings,
  onPreferencesChange,
  onSceneNavigationVisibleChange,
  onSelectPartner,
  onSelectTeam,
  preferences,
  sceneNavigationVisible,
  sceneContentFlush = false,
  selectedPartnerId = "",
  selectedRailKey,
  settingsSidebar,
  settingsOpen,
  teamContext,
  wakers,
}: PartnerNavigationLayoutProps) {
  const navigate = useNavigate();
  const openNewEntityChat = (item: NavigationEntity) => {
    navigate(
      item.kind === "waker"
        ? chatDraftHref(item.id)
        : `/teams/${item.id}?draftId=${crypto.randomUUID()}`,
      { flushSync: true },
    );
  };
  const location = useLocation();
  const workflowChatOpen = /^\/workflows\/[^/]+(?:\/runs\/[^/]+)?$/.test(
    location.pathname,
  );
  const automaticTasksOpen =
    location.pathname.startsWith("/im") ||
    location.pathname.startsWith("/automatic-tasks") ||
    location.pathname.startsWith("/workflows");
  const availableRailKeys = useMemo(
    () => [
      ...wakers.flatMap((waker) => (waker.id ? [`waker:${waker.id}`] : [])),
      ...channels.flatMap((detail) =>
        detail.channel?.id ? [`team:${detail.channel.id}`] : [],
      ),
    ],
    [channels, wakers],
  );
  const orderedRailKeys = reconcilePartnerOrder(
    availableRailKeys,
    preferences.railOrder,
  );
  useEffect(() => {
    if (availableRailKeys.length === 0) return;
    onPreferencesChange((current) => {
      const nextOrder = reconcilePartnerOrder(
        availableRailKeys,
        current.railOrder,
      );
      if (
        nextOrder.length === current.railOrder.length &&
        nextOrder.every((id, index) => id === current.railOrder[index])
      ) {
        return current;
      }
      return { ...current, railOrder: nextOrder };
    });
  }, [availableRailKeys, onPreferencesChange]);
  const navigationEntityByKey = new Map<string, NavigationEntity>();
  for (const waker of wakers) {
    if (!waker.id) continue;
    navigationEntityByKey.set(`waker:${waker.id}`, {
      id: waker.id,
      key: `waker:${waker.id}`,
      kind: "waker",
      waker,
    });
  }
  for (const team of channels) {
    const channelId = team.channel?.id;
    if (!channelId) continue;
    navigationEntityByKey.set(`team:${channelId}`, {
      id: channelId,
      key: `team:${channelId}`,
      kind: "team",
      team,
    });
  }
  const modeRailKeys = orderedRailKeys.filter((key) =>
    key.startsWith(`${preferences.entityMode}:`),
  );
  const visibleRailItems = modeRailKeys.flatMap<NavigationEntity>((key) => {
    const item = navigationEntityByKey.get(key);
    return item ? [item] : [];
  });
  const selectedRailItem = navigationEntityByKey.get(selectedRailKey);
  const resizeCleanup = useRef<() => void>(() => undefined);
  const [contextSidebarWidth, setContextSidebarWidth] = useState(
    preferences.contextSidebarWidth,
  );
  const [showcase, setShowcase] = useState<{
    origin: HTMLElement;
    rect: { height: number; left: number; top: number; width: number };
    waker: Waker;
  } | null>(null);
  useEffect(() => () => resizeCleanup.current(), []);
  const updateOrder = (sourceId: string, targetId: string) => {
    onPreferencesChange((current) => ({
      ...current,
      railOrder: moveItem(orderedRailKeys, sourceId, targetId),
    }));
  };
  const beginContextResize = (event: ReactMouseEvent<HTMLDivElement>) => {
    event.preventDefault();
    resizeCleanup.current();
    const startX = event.clientX;
    const startWidth = contextSidebarWidth;
    resizeCleanup.current = trackHorizontalDrag({
      initialX: startX,
      initialValue: startWidth,
      valueAt: (clientX) =>
        clampContextSidebarWidth(startWidth + clientX - startX),
      onChange: setContextSidebarWidth,
      onCommit: (width) =>
        onPreferencesChange((current) =>
          current.contextSidebarWidth === width
            ? current
            : { ...current, contextSidebarWidth: width },
        ),
      onFinish: () => {
        resizeCleanup.current = () => undefined;
      },
    });
  };
  const toggleCollapsed = () =>
    onPreferencesChange((current) => ({
      ...current,
      globalSidebarCollapsed: !current.globalSidebarCollapsed,
    }));
  const settingsLayout = settingsOpen ? settingsSidebar : undefined;
  const showSceneNavigation = Boolean(settingsLayout && sceneNavigationVisible);
  const bodyContextSidebarHidden =
    contextSidebarHidden || Boolean(settingsLayout) || automaticTasksOpen;
  const showContextSidebar =
    !bodyContextSidebarHidden && Boolean(contextSidebar);
  const showDetailDock =
    !bodyContextSidebarHidden && !contextSidebar && Boolean(selectedPartnerId);
  const navigationInset = platform().chrome?.navigationInset ?? 8;
  const navigationCollapsed =
    !settingsLayout && preferences.globalSidebarCollapsed;

  return (
    <SceneChromePortalProvider>
      <div
        className="bg-surface-chrome duration-layout ease-ui relative isolate grid h-full min-h-0 min-w-0 grid-cols-[var(--shell-global-active-width)_minmax(0,1fr)] overflow-hidden transition-[grid-template-columns]"
        style={
          {
            "--shell-context-width": `${contextSidebarWidth}px`,
            "--shell-navigation-leading-inset": `${navigationInset}px`,
            "--shell-titlebar-navigation-width": `${navigationInset + 60}px`,
            "--shell-global-active-width": settingsLayout
              ? "var(--shell-global-sidebar-width)"
              : preferences.globalSidebarCollapsed
                ? "var(--shell-global-sidebar-collapsed-width)"
                : "var(--shell-global-sidebar-width)",
          } as CSSProperties
        }
      >
        {navigationCollapsed ? (
          <div
            className="absolute top-0 left-(--shell-navigation-leading-inset) z-30 flex h-(--shell-titlebar-height) items-center"
            data-desktop-no-drag-region
          >
            <NavigationToggleButton collapsed onClick={toggleCollapsed} />
          </div>
        ) : null}
        <div className="grid min-h-0 min-w-0 grid-rows-[var(--shell-titlebar-height)_minmax(0,1fr)] overflow-hidden">
          <NavigationToolbar
            notificationBell={notificationBell}
            canGoBack={showSceneNavigation}
            canGoForward={Boolean(settingsLayout) && !showSceneNavigation}
            collapsed={navigationCollapsed}
            onBack={() => onSceneNavigationVisibleChange(false)}
            onForward={() => onSceneNavigationVisibleChange(true)}
            onToggleCollapsed={toggleCollapsed}
            title={showSceneNavigation ? "设置" : undefined}
          />
          {showSceneNavigation ? (
            settingsLayout
          ) : navigationCollapsed ? null : (
            <EntityNavigation
              collapsed={false}
              entityStatus={entityStatus}
              mode={preferences.entityMode}
              onManageEntities={onManageEntities}
              onNewChat={openNewEntityChat}
              onModeChange={(entityMode) =>
                onPreferencesChange((current) => ({ ...current, entityMode }))
              }
              onOpenSettings={onOpenSettings}
              onOpenIM={() => navigate("/im")}
              onOpenAutomaticTasks={onOpenAutomaticTasks}
              onOpenWorkflows={onOpenWorkflows}
              onOpenResources={onOpenResources}
              onOpenKnowledge={onOpenKnowledge}
              onReorder={updateOrder}
              onSelectPartner={onSelectPartner}
              onSelectTeam={onSelectTeam}
              onShowcaseWaker={(waker, origin) =>
                setShowcase(captureShowcase(waker, origin))
              }
              selectedKey={selectedRailKey}
              items={visibleRailItems}
            />
          )}
        </div>
        <section
          aria-label="场景工作区"
          className={cn(
            "bg-surface-scene relative grid min-h-0 min-w-0 grid-rows-[var(--shell-titlebar-height)_minmax(0,1fr)] overflow-hidden rounded-l-3xl",
            sceneContentFlush &&
              !navigationCollapsed &&
              "border-border-subtle border-l",
          )}
        >
          <div className="min-w-0">
            <SceneChromeHost navigationCollapsed={navigationCollapsed} />
          </div>
          <div
            className={cn(
              "duration-layout ease-ui grid min-h-0 min-w-0 transition-[grid-template-columns]",
              sceneContentFlush || showDetailDock ? "pl-0" : "pl-4",
              showContextSidebar
                ? "grid-cols-[var(--shell-context-width)_1px_minmax(0,1fr)]"
                : "grid-cols-[minmax(0,1fr)]",
            )}
          >
            {showContextSidebar ? contextSidebar : null}
            {showContextSidebar ? (
              <VerticalResizeHandle
                ariaLabel="调整上下文侧栏宽度"
                ariaValueMax={contextSidebarWidthRange.max}
                ariaValueMin={contextSidebarWidthRange.min}
                ariaValueNow={contextSidebarWidth}
                onKeyDown={(event) => {
                  const direction =
                    event.key === "ArrowLeft"
                      ? -1
                      : event.key === "ArrowRight"
                        ? 1
                        : 0;
                  if (!direction) return;
                  event.preventDefault();
                  const width = clampContextSidebarWidth(
                    contextSidebarWidth + direction * 8,
                  );
                  setContextSidebarWidth(width);
                  onPreferencesChange((current) => ({
                    ...current,
                    contextSidebarWidth: width,
                  }));
                }}
                onMouseDown={beginContextResize}
              />
            ) : null}
            <DetailDockLayout
              navigation={
                showDetailDock ? (
                  <Suspense fallback={null}>
                    <PartnerDetailDock partnerId={selectedPartnerId} />
                  </Suspense>
                ) : undefined
              }
            >
              {children}
            </DetailDockLayout>
          </div>
          {!settingsLayout ? (
            <div className="absolute right-0 bottom-0 z-40">
              <CornerLauncher>
                {workflowChatOpen ? null : navigationCollapsed ? (
                  <FloatingEntityNavigation
                    entityStatus={entityStatus}
                    items={visibleRailItems}
                    mode={preferences.entityMode}
                    onManageEntities={onManageEntities}
                    onModeChange={(entityMode) =>
                      onPreferencesChange((current) => ({
                        ...current,
                        entityMode,
                      }))
                    }
                    onNewChat={openNewEntityChat}
                    onReorder={updateOrder}
                    onSelectPartner={onSelectPartner}
                    onSelectTeam={onSelectTeam}
                    onShowcaseWaker={(waker, origin) =>
                      setShowcase(captureShowcase(waker, origin))
                    }
                    renderEntityChats={(item, onBack, onClose) => (
                      <ChatHistoryPanel
                        activeChatId={activeChatId}
                        item={item}
                        onBack={onBack}
                        onClose={onClose}
                        onPreferencesChange={onPreferencesChange}
                        preferences={preferences}
                        teamContext={teamContext}
                      />
                    )}
                    selectedItem={selectedRailItem}
                    selectedKey={selectedRailKey}
                  />
                ) : selectedRailItem ? (
                  <ChatHistoryLauncher
                    activeChatId={activeChatId}
                    item={selectedRailItem}
                    onPreferencesChange={onPreferencesChange}
                    preferences={preferences}
                    teamContext={teamContext}
                  />
                ) : null}
              </CornerLauncher>
            </div>
          ) : null}
        </section>
        <WakerShowcaseDialog
          onOpenDetail={onOpenPartnerDetail}
          onStartChat={(wakerId) => navigate(chatDraftHref(wakerId))}
          open={Boolean(showcase)}
          origin={showcase?.origin}
          originRect={showcase?.rect}
          waker={showcase?.waker ?? null}
          onOpenChange={(open) => {
            if (!open) setShowcase(null);
          }}
        />
      </div>
    </SceneChromePortalProvider>
  );
}

function CornerLauncher({ children }: { children: ReactNode }) {
  const floatingChat = useFloatingChatLauncher();
  if (!floatingChat) return children;

  return (
    <Button
      aria-label={floatingChat.ariaLabel}
      className="group/launcher relative transition-[width,background-color,color,box-shadow,opacity] hover:w-40 focus-visible:w-40"
      data-activity-state={floatingChat.state}
      data-floating-chat-shell-launcher
      onClick={floatingChat.onActivate}
      shape="corner"
      size="icon-lg"
      title={floatingChat.ariaLabel}
      type="button"
      variant="launcher"
    >
      <span className="size-control-lg grid shrink-0 place-items-center">
        {floatingChat.state === "running" ? (
          <LoaderCircle
            className="size-icon-lg motion-safe:animate-spin"
            strokeWidth={1.5}
          />
        ) : (
          <MessageSquareText className="size-icon-lg" strokeWidth={1.5} />
        )}
      </span>
      <span
        aria-hidden="true"
        className="type-ui min-w-0 -translate-x-1 truncate pr-4 opacity-0 transition-[opacity,transform] group-hover/launcher:translate-x-0 group-hover/launcher:opacity-100 group-focus-visible/launcher:translate-x-0 group-focus-visible/launcher:opacity-100"
      >
        {floatingChat.label}
      </span>
      {floatingChat.state === "waiting" ? (
        <span
          className="bg-warning absolute top-1 right-1 size-2 rounded-full"
          aria-hidden="true"
        />
      ) : null}
    </Button>
  );
}

function ChatHistoryLauncher({
  activeChatId,
  item,
  onPreferencesChange,
  preferences,
  teamContext,
}: {
  activeChatId?: string;
  item: NavigationEntity;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  preferences: PartnerNavigationPreferences;
  teamContext?: { channelId: string; threadId?: string };
}) {
  return (
    <ChatHistoryPopover>
      {(close) => (
        <ChatHistoryPanel
          activeChatId={activeChatId}
          item={item}
          onClose={close}
          onPreferencesChange={onPreferencesChange}
          preferences={preferences}
          teamContext={teamContext}
        />
      )}
    </ChatHistoryPopover>
  );
}

function ChatHistoryPanel({
  activeChatId,
  item,
  onBack,
  onClose,
  onPreferencesChange,
  preferences,
  teamContext,
}: {
  activeChatId?: string;
  item: NavigationEntity;
  onBack?: () => void;
  onClose: () => void;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  preferences: PartnerNavigationPreferences;
  teamContext?: { channelId: string; threadId?: string };
}) {
  const navigate = useNavigate();
  const openNewChat = () => {
    onClose();
    navigate(
      item.kind === "waker"
        ? chatDraftHref(item.id)
        : `/teams/${item.id}?draftId=${crypto.randomUUID()}`,
      { flushSync: true },
    );
  };

  return (
    <ChatHistoryPanelSurface onBack={onBack} onNew={openNewChat}>
      {item.kind === "waker" ? (
        <PartnerChatTree
          activeChatId={activeChatId}
          onChatOpened={onClose}
          onPreferencesChange={onPreferencesChange}
          preferences={preferences}
          selectedPartnerId={item.id}
        />
      ) : (
        <TeamChatTree
          channelId={item.id}
          onPreferencesChange={onPreferencesChange}
          onThreadOpened={onClose}
          preferences={preferences}
          threadId={
            teamContext?.channelId === item.id
              ? teamContext.threadId
              : undefined
          }
        />
      )}
    </ChatHistoryPanelSurface>
  );
}

function NavigationToolbar({
  notificationBell,
  canGoBack,
  canGoForward,
  collapsed,
  onBack,
  onForward,
  onToggleCollapsed,
  title,
}: {
  notificationBell?: ReactNode;
  canGoBack: boolean;
  canGoForward: boolean;
  collapsed: boolean;
  onBack: () => void;
  onForward: () => void;
  onToggleCollapsed: () => void;
  title?: string;
}) {
  return (
    <div
      className={cn(
        "bg-surface-chrome relative flex min-w-0 items-center overflow-visible",
        collapsed
          ? "justify-center"
          : "gap-0.5 pr-2 pl-(--shell-navigation-leading-inset)",
      )}
      data-desktop-drag-region
      role="toolbar"
      aria-label="导航控制"
    >
      {collapsed ? null : (
        <NavigationToggleButton collapsed={false} onClick={onToggleCollapsed} />
      )}
      {collapsed ? null : (
        <>
          <Button
            type="button"
            onClick={onBack}
            aria-label="返回"
            size="icon-sm"
            variant="ghost"
            disabled={!canGoBack}
          >
            <ArrowLeft className="size-icon-sm" strokeWidth={1.5} />
          </Button>
          <Button
            type="button"
            onClick={onForward}
            aria-label="前进"
            size="icon-sm"
            variant="ghost"
            disabled={!canGoForward}
          >
            <ArrowRight className="size-icon-sm" strokeWidth={1.5} />
          </Button>
          {notificationBell}
          {title ? (
            <span className="text-content-secondary type-ui ml-1 min-w-0 truncate">
              {title}
            </span>
          ) : null}
        </>
      )}
    </div>
  );
}

function NavigationToggleButton({
  collapsed,
  onClick,
}: {
  collapsed: boolean;
  onClick: () => void;
}) {
  const { t } = useTranslation("common");

  return (
    <Button
      type="button"
      onClick={onClick}
      data-desktop-no-drag-region
      aria-label={
        collapsed
          ? t("partnerNavigation.expandGlobalSidebar")
          : t("partnerNavigation.collapseGlobalSidebar")
      }
      size="icon-sm"
      variant="ghost"
    >
      <PanelLeft className="size-icon-sm" strokeWidth={1.5} />
    </Button>
  );
}

function PartnerChatTree({
  activeChatId,
  onChatOpened,
  onPreferencesChange,
  preferences,
  selectedPartnerId,
}: {
  activeChatId?: string;
  onChatOpened?: () => void;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  preferences: PartnerNavigationPreferences;
  selectedPartnerId: string;
}) {
  const { t } = useTranslation("common");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const chatsQuery = useInfiniteQuery({
    ...chatListInfiniteOptions(selectedPartnerId || "pending"),
    enabled: Boolean(selectedPartnerId),
  });
  const chats = useMemo(
    () => chatsQuery.data?.pages.flatMap((page) => page.resources ?? []) ?? [],
    [chatsQuery.data],
  );
  const groups = useMemo(() => groupChats(chats), [chats]);
  const collapsedGroups = new Set(
    preferences.collapsedGroups[selectedPartnerId] ?? [],
  );
  const [taskAction, setTaskAction] = useState<{
    chatId: string;
    mode: "delete" | "rename";
    title: string;
  }>();
  const activeChat = chats.find((item) => item.id === activeChatId);
  const activeGroupKey = activeChat ? chatGroupOf(activeChat).key : "";
  const autoExpanded = useRef("");
  useEffect(() => {
    if (!activeChatId || !activeGroupKey) return;
    const marker = `${selectedPartnerId}:${activeChatId}:${activeGroupKey}`;
    if (autoExpanded.current === marker) return;
    autoExpanded.current = marker;
    onPreferencesChange((current) => {
      const next = (current.collapsedGroups[selectedPartnerId] ?? []).filter(
        (key) => key !== activeGroupKey,
      );
      return {
        ...current,
        collapsedGroups: {
          ...current.collapsedGroups,
          [selectedPartnerId]: next,
        },
      };
    });
  }, [activeChatId, activeGroupKey, onPreferencesChange, selectedPartnerId]);

  const toggleGroup = (groupKey: string) => {
    onPreferencesChange((current) => {
      const currentKeys = new Set(
        current.collapsedGroups[selectedPartnerId] ?? [],
      );
      if (currentKeys.has(groupKey)) currentKeys.delete(groupKey);
      else currentKeys.add(groupKey);
      return {
        ...current,
        collapsedGroups: {
          ...current.collapsedGroups,
          [selectedPartnerId]: [...currentKeys],
        },
      };
    });
  };
  const prefetchChat = (chatId: string) => {
    if (!chatId) return;
    void queryClient.prefetchQuery(getChatOptions({ path: { chatId } }));
    void queryClient.prefetchInfiniteQuery(chatMessagesInfiniteOptions(chatId));
  };
  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const element = event.currentTarget;
    if (
      chatsQuery.hasNextPage &&
      !chatsQuery.isFetchingNextPage &&
      element.scrollHeight - element.scrollTop - element.clientHeight < 120
    ) {
      void chatsQuery.fetchNextPage();
    }
  };
  return (
    <div className="min-w-0 pb-1" data-chat-tree="partner">
      <div
        className="scrollbar-hidden max-h-72 min-h-0 overflow-x-hidden overflow-y-auto pr-1"
        onScroll={handleScroll}
      >
        {selectedPartnerId ? (
          <>
            {chatsQuery.isPending ? (
              <div
                aria-busy="true"
                className="text-muted-foreground type-caption flex items-center gap-2 px-2 py-4"
                role="status"
              >
                <LoaderCircle className="size-icon-sm animate-spin" />
                {t("partnerNavigation.loadingChats")}
              </div>
            ) : chatsQuery.isError ? (
              <div className="px-2 py-4">
                <p className="text-muted-foreground type-caption">
                  {t("partnerNavigation.chatsUnavailable")}
                </p>
                <Button
                  className="mt-2"
                  size="sm"
                  variant="ghost"
                  onClick={() => void chatsQuery.refetch()}
                >
                  {t("retry")}
                </Button>
              </div>
            ) : groups.length === 0 ? (
              <p className="text-muted-foreground type-caption px-2 py-4">
                {t("partnerNavigation.noChats")}
              </p>
            ) : (
              groups.map((group, index) => (
                <div className="mt-0.5" key={group.key}>
                  <ChatGroupHeader
                    action={
                      index === 0
                        ? {
                            current: false,
                            href: partnerSectionHref(
                              selectedPartnerId,
                              "chats",
                            ),
                            label: t("partnerNavigation.sections.chats"),
                          }
                        : undefined
                    }
                    collapsed={collapsedGroups.has(group.key)}
                    icon={Folder}
                    label={group.label}
                    onActionOpened={onChatOpened}
                    onToggle={() => toggleGroup(group.key)}
                  />
                  {!collapsedGroups.has(group.key) ? (
                    <div className="grid gap-0.5 pl-5">
                      {group.chats.map((chat) => (
                        <ChatNavigationRow
                          key={chat.id}
                          active={chat.id === activeChatId}
                          chat={chat}
                          onAction={(mode) =>
                            setTaskAction({
                              chatId: chat.id ?? "",
                              mode,
                              title: chat.title ?? "",
                            })
                          }
                          onPrefetch={prefetchChat}
                          onOpened={onChatOpened}
                        />
                      ))}
                    </div>
                  ) : null}
                </div>
              ))
            )}
            {chatsQuery.isFetchingNextPage ? (
              <div
                aria-busy="true"
                className="text-muted-foreground type-caption flex items-center justify-center gap-2 py-3"
                role="status"
              >
                <LoaderCircle className="size-icon-sm animate-spin" />
                {t("partnerNavigation.loadingMore")}
              </div>
            ) : null}
          </>
        ) : null}
      </div>
      {taskAction ? (
        <ChatTaskDialog
          key={`${taskAction.mode}:${taskAction.chatId}`}
          chatId={taskAction.chatId}
          mode={taskAction.mode}
          open
          title={taskAction.title}
          onOpenChange={(open) => {
            if (!open) setTaskAction(undefined);
          }}
          onDeleted={(chatId) => {
            if (chatId === activeChatId) {
              navigate(chatDraftHref(selectedPartnerId), {
                replace: true,
              });
            }
          }}
        />
      ) : null}
    </div>
  );
}

type ChatNavigationRowProps = {
  active: boolean;
  chat: Chat;
  onAction: (mode: "delete" | "rename") => void;
  onPrefetch: (chatId: string) => void;
  onOpened?: () => void;
};

function ChatNavigationRow({
  active,
  chat,
  onAction,
  onPrefetch,
  onOpened,
}: ChatNavigationRowProps) {
  const { t } = useTranslation("common");
  const navigate = useNavigate();
  const chatId = chat.id ?? "";
  const href = chatHref(chatId, chat.wakerId ?? "");
  const status = waitingInteraction(chat)
    ? "waiting"
    : chat.activeExecutionId
      ? "running"
      : "idle";
  return (
    <ChatHistoryNavigationRow
      active={active}
      title={chat.title || t("partnerNavigation.untitledChat")}
      status={status}
      href={href}
      onOpen={(event) => {
        onOpened?.();
        navigateRouteImmediately(event, navigate, href);
      }}
      onPrefetch={() => onPrefetch(chatId)}
      onAction={onAction}
      extraMenu={(menuOpen) => (
        <ChatForkMenuItem
          chatId={chatId}
          wakerId={chat.wakerId}
          enabled={menuOpen}
          busy={status !== "idle"}
        />
      )}
    />
  );
}
