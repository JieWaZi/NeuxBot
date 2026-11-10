import {
  ChatHistoryPopover,
  ChatHistoryPanelSurface,
  ChatHistoryNavigationRow,
  ChatForkMenuItem,
} from "@/modules/chat";
import { Folder, LoaderCircle, MessageSquareText } from "lucide-react";
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
import { navigateRouteImmediately } from "./immediate-route-navigation";
import { AppModuleRail } from "./app-module-rail";
import { resolveAppNavigation, type AppModuleId } from "./app-navigation-model";
import { ChatGroupHeader } from "@/modules/chat";
import { TeamChatTree } from "./team-context-sidebar";
import { EntityNavigation, type NavigationEntity } from "./entity-navigation";
import { useOptionalAppSceneWorkspace } from "@/shared/workspace/app-scene-workspace";
import type { ChatSceneOwner } from "@/shared/workspace/app-scene-model";
import { chatDraftHref, chatHref } from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import {
  clampContextSidebarWidth,
  pinnedAppModules,
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
  renderTitlebar?: (navigationToggle: ReactNode) => ReactNode;
  sidebarToolbar?: ReactNode;
  contextSidebarHidden?: boolean;
  entityStatus?: "error" | "loading" | "ready";
  onManageEntities: (mode: PartnerNavigationPreferences["entityMode"]) => void;
  onOpenPartnerDetail: (partnerId: string) => void;
  onNavigateModule: (moduleId: AppModuleId) => void;
  moduleSidebar?: ReactNode;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  onSelectPartner: (partnerId: string) => void;
  onSelectTeam: (channelId: string) => void;
  preferences: PartnerNavigationPreferences;
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

const PartnerContextSidebar = lazy(() =>
  import("./partner-context-sidebar").then((module) => ({
    default: module.PartnerContextSidebar,
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

function chatGroupOf(
  chat: Chat,
  labels: { temporaryDirectory: string; unnamedProject: string },
): Omit<ChatGroup, "chats"> {
  const projectId = chat.projectId?.trim();
  const projectName = chat.projectName?.trim();
  if (projectId) {
    return {
      key: `project:${projectId}`,
      label: projectName || labels.unnamedProject,
      type: "project",
    };
  }
  return {
    key: "folder:temporary",
    label: labels.temporaryDirectory,
    type: "folder",
  };
}

function groupChats(
  chats: readonly Chat[],
  labels: { temporaryDirectory: string; unnamedProject: string },
): ChatGroup[] {
  const groups = new Map<string, ChatGroup>();
  for (const chat of chats) {
    if (!chat.id) continue;
    const group = chatGroupOf(chat, labels);
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
  renderTitlebar,
  sidebarToolbar,
  contextSidebarHidden = false,
  entityStatus = "ready",
  onManageEntities,
  onOpenPartnerDetail,
  onNavigateModule,
  moduleSidebar,
  onPreferencesChange,
  onSelectPartner,
  onSelectTeam,
  preferences,
  sceneContentFlush = false,
  selectedPartnerId = "",
  selectedRailKey,
  settingsSidebar,
  settingsOpen,
  teamContext,
  wakers,
}: PartnerNavigationLayoutProps) {
  const navigate = useNavigate();
  const { t } = useTranslation("common");
  const appScenes = useOptionalAppSceneWorkspace();
  const openNewOwnerChat = (owner: ChatSceneOwner) => {
    if (appScenes?.openNewChat?.(owner)) return;
    navigate(
      owner.kind === "waker"
        ? chatDraftHref(owner.id)
        : `/teams/${owner.id}?draftId=${crypto.randomUUID()}`,
      { flushSync: true },
    );
  };
  const openNewEntityChat = (item: NavigationEntity) =>
    openNewOwnerChat({
      kind: item.kind === "waker" ? "waker" : "team",
      id: item.id,
    });
  const location = useLocation();
  const workflowChatOpen = /^\/workflows\/[^/]+(?:\/runs\/[^/]+)?$/.test(
    location.pathname,
  );
  const navigation = resolveAppNavigation(location);
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
  const [contextSidebarWidth, setContextSidebarWidth] = useState(() =>
    clampContextSidebarWidth(preferences.contextSidebarWidth),
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
  const settingsLayout = settingsOpen ? settingsSidebar : undefined;
  const partnerContext = !settingsOpen && navigation.sidebarKind === "partners";
  const sidebar = settingsLayout ?? moduleSidebar;
  const pageSidebar = Boolean(sidebar);
  const hasSidebar = pageSidebar || partnerContext;
  const sidebarWidth = pageSidebar
    ? Math.max(contextSidebarWidth, 220)
    : contextSidebarWidth;
  const resizeSidebar = (value: number, commit: boolean) => {
    const width = clampContextSidebarWidth(value);
    setContextSidebarWidth(width);
    if (commit)
      onPreferencesChange((current) =>
        current.contextSidebarWidth === width
          ? current
          : { ...current, contextSidebarWidth: width },
      );
  };
  const beginContextResize = (event: ReactMouseEvent<HTMLDivElement>) => {
    event.preventDefault();
    resizeCleanup.current();
    const startX = event.clientX;
    const startWidth = contextSidebarWidth;
    resizeCleanup.current = trackHorizontalDrag({
      initialX: startX,
      initialValue: startWidth,
      valueAt: (x) => clampContextSidebarWidth(startWidth + x - startX),
      onChange: setContextSidebarWidth,
      onCommit: (width) => resizeSidebar(width, true),
      onFinish: () => {
        resizeCleanup.current = () => undefined;
      },
    });
  };
  const bodyContextSidebarHidden = contextSidebarHidden || !partnerContext;
  const showContextSidebar =
    !bodyContextSidebarHidden && Boolean(contextSidebar);
  const showPartnerSidebarNavigation =
    !bodyContextSidebarHidden && !contextSidebar && Boolean(selectedPartnerId);
  const navigationInset = platform().chrome?.navigationInset ?? 8;
  const nativeDarwin =
    platform().kind === "desktop" &&
    platform().chrome?.nativeWindowControls === "darwin";

  return (
    <SceneChromePortalProvider>
      {/* 工作台框架只负责布局，Scene 保活由原所有者负责。 */}
      <div
        className="app-window-shell bg-surface-chrome grid h-full min-h-0 min-w-0 grid-cols-[var(--shell-rail-width)_minmax(0,1fr)] overflow-hidden"
        data-workflow-canvas-shell={
          (workflowChatOpen && !settingsOpen) || undefined
        }
      >
        <AppModuleRail
          activeModule={settingsOpen ? "settings" : navigation.moduleId}
          onNavigate={onNavigateModule}
          pinnedModules={pinnedAppModules(preferences.railPins)}
          onPinChange={(id, pinned) =>
            onPreferencesChange((current) => ({
              ...current,
              railPins: { ...current.railPins, [id]: pinned },
            }))
          }
        />
        <div
          data-app-workspace-frame
          data-page-sidebar={pageSidebar || undefined}
          data-sidebar-body-hidden={!hasSidebar || undefined}
          className={cn(
            "app-workspace-frame relative isolate grid min-h-0 min-w-0 grid-cols-[var(--shell-sidebar-active-width)_1px_minmax(0,1fr)] grid-rows-[minmax(0,1fr)] overflow-hidden",
          )}
          style={
            {
              "--shell-context-width": `${contextSidebarWidth}px`,
              "--shell-sidebar-active-width": `${sidebarWidth}px`,
              "--shell-navigation-leading-inset": `${navigationInset}px`,
              "--shell-sidebar-header-inset": nativeDarwin ? "24px" : "12px",
              "--shell-sidebar-header-height": "38px",
            } as CSSProperties
          }
        >
          <div
            className="app-context-sidebar col-start-1 row-start-1 grid min-h-0 min-w-0 grid-rows-[var(--shell-sidebar-header-height)_minmax(0,1fr)]"
            data-app-context-sidebar
          >
            <div
              className="app-sidebar-topbar flex min-w-0 items-center justify-end"
              data-app-sidebar-toolbar
              data-desktop-drag-region
            >
              <div
                data-sidebar-history-slot
                className="flex shrink-0 items-center"
                data-desktop-no-drag-region
              >
                {sidebarToolbar}
                {notificationBell}
              </div>
            </div>
            <div
              hidden={
                !partnerContext || pageSidebar || showPartnerSidebarNavigation
              }
              aria-hidden={
                !partnerContext ||
                pageSidebar ||
                showPartnerSidebarNavigation ||
                undefined
              }
              inert={
                !partnerContext || pageSidebar || showPartnerSidebarNavigation
              }
              data-sidebar-entity-body
              className="min-h-0 min-w-0 overflow-x-hidden overflow-y-auto overscroll-contain"
            >
              <EntityNavigation
                collapsed={false}
                entityStatus={entityStatus}
                mode={preferences.entityMode}
                onManageEntities={onManageEntities}
                onNewChat={openNewEntityChat}
                onModeChange={(entityMode) =>
                  onPreferencesChange((current) => ({
                    ...current,
                    entityMode,
                  }))
                }
                onReorder={updateOrder}
                onSelectPartner={onSelectPartner}
                onSelectTeam={onSelectTeam}
                onShowcaseWaker={(waker, origin) =>
                  setShowcase(captureShowcase(waker, origin))
                }
                selectedKey={selectedRailKey}
                items={visibleRailItems}
              />
            </div>
            {sidebar || showPartnerSidebarNavigation ? (
              <div
                className="absolute inset-x-0 top-(--shell-titlebar-height) bottom-0 overflow-x-hidden overflow-y-auto overscroll-contain"
                data-sidebar-page-nav
              >
                {sidebar ?? (
                  <Suspense fallback={null}>
                    <PartnerContextSidebar partnerId={selectedPartnerId} />
                  </Suspense>
                )}
              </div>
            ) : null}
          </div>
          {partnerContext && !pageSidebar ? (
            <div
              className="relative col-start-2 row-start-1 mt-(--shell-titlebar-height) min-h-0"
              onDoubleClick={() => resizeSidebar(280, true)}
            >
              <VerticalResizeHandle
                appearance="hover"
                className="h-full"
                ariaLabel={t("appNavigation.resizeSidebar")}
                ariaValueMin={contextSidebarWidthRange.min}
                ariaValueMax={contextSidebarWidthRange.max}
                ariaValueNow={contextSidebarWidth}
                onMouseDown={beginContextResize}
                onKeyDown={(event) => {
                  let next: number | undefined;
                  if (event.key === "Home") {
                    event.preventDefault();
                    resizeSidebar(contextSidebarWidthRange.min, true);
                    return;
                  }
                  if (event.key === "End") next = 400;
                  else if (event.key === "ArrowRight")
                    next = contextSidebarWidth + 12;
                  else if (event.key === "ArrowLeft")
                    next = contextSidebarWidth - 12;
                  if (next === undefined) return;
                  event.preventDefault();
                  resizeSidebar(next, true);
                }}
              />
            </div>
          ) : null}
          <section
            aria-label="场景工作区"
            data-content-spans-sidebar={!hasSidebar || undefined}
            className={cn(
              "relative row-start-1 grid min-h-0 min-w-0 grid-rows-[var(--shell-titlebar-height)_minmax(0,1fr)] overflow-hidden",
              hasSidebar ? "col-start-3" : "col-span-3 col-start-1",
            )}
          >
            <div
              className="app-content-header min-w-0"
              data-sidebar-offset={!hasSidebar || undefined}
            >
              <SceneChromeHost>{renderTitlebar?.(null)}</SceneChromeHost>
            </div>
            <div
              className={cn(
                "grid h-full min-h-0 min-w-0",
                sceneContentFlush || showPartnerSidebarNavigation
                  ? "pl-0"
                  : "pl-4",
                showContextSidebar
                  ? "grid-cols-[var(--shell-context-width)_minmax(0,1fr)]"
                  : "grid-cols-[minmax(0,1fr)]",
              )}
            >
              {showContextSidebar ? contextSidebar : null}
              <div className="app-scene-body-clip h-full min-h-0 min-w-0">
                {children}
              </div>
            </div>
            {partnerContext && !workflowChatOpen && selectedRailItem ? (
              <div className="absolute right-0 bottom-0 z-40">
                <CornerLauncher>
                  <ChatHistoryLauncher
                    activeChatId={activeChatId}
                    item={selectedRailItem}
                    onPreferencesChange={onPreferencesChange}
                    preferences={preferences}
                    teamContext={teamContext}
                  />
                </CornerLauncher>
              </div>
            ) : null}
          </section>
        </div>
        <WakerShowcaseDialog
          onOpenDetail={onOpenPartnerDetail}
          onStartChat={(wakerId) =>
            openNewOwnerChat({ kind: "waker", id: wakerId })
          }
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
  const appScenes = useOptionalAppSceneWorkspace();
  const openNewChat = () => {
    onClose();
    if (
      appScenes?.openNewChat?.({
        kind: item.kind === "waker" ? "waker" : "team",
        id: item.id,
      })
    )
      return;
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
  const appScenes = useOptionalAppSceneWorkspace();
  const queryClient = useQueryClient();
  const chatsQuery = useInfiniteQuery({
    ...chatListInfiniteOptions(selectedPartnerId || "pending"),
    enabled: Boolean(selectedPartnerId),
  });
  const chats = useMemo(
    () => chatsQuery.data?.pages.flatMap((page) => page.resources ?? []) ?? [],
    [chatsQuery.data],
  );
  const groups = useMemo(
    () =>
      groupChats(chats, {
        temporaryDirectory: t("conversationGroups.temporaryDirectory"),
        unnamedProject: t("conversationGroups.unnamedProject"),
      }),
    [chats, t],
  );
  const collapsedGroups = new Set(
    preferences.collapsedGroups[selectedPartnerId] ?? [],
  );
  const [taskAction, setTaskAction] = useState<{
    chatId: string;
    mode: "delete" | "rename";
    title: string;
  }>();
  const activeChat = chats.find((item) => item.id === activeChatId);
  const activeGroupKey = activeChat
    ? chatGroupOf(activeChat, {
        temporaryDirectory: t("conversationGroups.temporaryDirectory"),
        unnamedProject: t("conversationGroups.unnamedProject"),
      }).key
    : "";
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
              if (
                appScenes?.openNewChat?.({
                  kind: "waker",
                  id: selectedPartnerId,
                })
              )
                return;
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
