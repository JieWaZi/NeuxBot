import "./partner-navigation-pages.css";

import { waitingConversations, workAttentionHref } from "@/modules/work-board";
import {
  ChatHistoryPanelSurface,
  ChatHistoryNavigationRow,
  ChatForkMenuItem,
} from "@/modules/chat";
import {
  Folder,
  LoaderCircle,
  MessageSquareText,
  PanelLeft,
  Plus,
} from "lucide-react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
  lazy,
  Suspense,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type Dispatch,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  type SetStateAction,
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
import type {
  ChatAttention,
  ExecutorAttention,
} from "@/modules/workspaceattention";
import { captureChatOpenIntent } from "@/shared/api/chat-reading";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { platform } from "@/shared/platform/provider";
import { navigateRouteImmediately } from "./immediate-route-navigation";
import { AppModuleRail } from "./app-module-rail";
import { resolveAppNavigation, type AppModuleId } from "./app-navigation-model";
import { ChatGroupHeader } from "@/modules/chat";
import { TeamChatTree } from "./team-context-sidebar";
import { SidebarWorkTree } from "./sidebar-work-tree";
import { EntityNavigation, type NavigationEntity } from "./entity-navigation";
import { useOptionalAppSceneWorkspace } from "@/shared/workspace/app-scene-workspace";
import type { ChatSceneOwner } from "@/shared/workspace/app-scene-model";
import { chatDraftHref, chatHref } from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import { SearchField } from "@/shared/ui/search-field";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { CornerLauncherButton } from "@/shared/ui/corner-launcher-button";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import {
  clampContextSidebarWidth,
  defaultContextSidebarWidth,
  pinnedAppModules,
  contextSidebarWidthRange,
  moveItem,
  reconcilePartnerOrder,
  type NavigationDropPlacement,
  type PartnerNavigationPreferences,
} from "./partner-navigation-preferences";
import { WakerNavigationMenu } from "./waker-navigation-menu";
import { useWakerNavigationGroups } from "./use-waker-navigation-groups";
import { mainConversationOwner } from "../scenes/main-conversation-route";
import { TeamNavigationMenu } from "./team-navigation-menu";
import { VerticalResizeHandle } from "@/shared/ui/vertical-resize-handle";
import {
  SceneChromeHost,
  SceneChromePortalProvider,
} from "@/shared/ui/scene-chrome-portal";
import {
  FloatingChatLauncherProvider,
  useFloatingChatLauncher,
} from "@/shared/workspace/floating-chat-launcher";

const PartnerContextSidebar = lazy(() =>
  import("./partner-context-sidebar").then((module) => ({
    default: module.PartnerContextSidebar,
  })),
);
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
  executorAttention?: ReadonlyMap<string, ExecutorAttention>;
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
  executorAttention,
  onManageEntities,
  onOpenPartnerDetail,
  onNavigateModule,
  moduleSidebar,
  onPreferencesChange,
  onSelectPartner,
  onSelectTeam,
  preferences,
  sceneContentFlush = false,
  selectedPartnerId,
  selectedRailKey,
  settingsSidebar,
  settingsOpen,
  teamContext,
  wakers,
}: PartnerNavigationLayoutProps) {
  const navigate = useNavigate();
  const { t } = useTranslation("common");
  const sidebarBodyId = useId();
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
  const location = useLocation();
  const workflowChatOpen = /^\/workflows\/[^/]+(?:\/runs\/[^/]+)?$/.test(
    location.pathname,
  );
  const navigation = resolveAppNavigation(location);
  const navigationGroups = useWakerNavigationGroups(
    preferences,
    onPreferencesChange,
  );
  const availableTeamKeys = useMemo(
    () =>
      channels.flatMap((detail) =>
        detail.channel?.id ? [`team:${detail.channel.id}`] : [],
      ),
    [channels],
  );
  const availableWakerKeys = wakers.flatMap((waker) =>
    waker.id ? [`waker:${waker.id}`] : [],
  );
  useEffect(() => {
    if (availableTeamKeys.length === 0) return;
    onPreferencesChange((current) => {
      const nextOrder = [
        ...current.railOrder.filter((key) => key.startsWith("waker:")),
        ...reconcilePartnerOrder(availableTeamKeys, current.railOrder),
      ];
      if (
        nextOrder.length === current.railOrder.length &&
        nextOrder.every((id, index) => id === current.railOrder[index])
      ) {
        return current;
      }
      return { ...current, railOrder: nextOrder };
    });
  }, [availableTeamKeys, onPreferencesChange]);
  const navigationEntityByKey = new Map<string, NavigationEntity>();
  for (const waker of wakers) {
    if (!waker.id) continue;
    navigationEntityByKey.set(`waker:${waker.id}`, {
      id: waker.id,
      key: `waker:${waker.id}`,
      kind: "waker",
      waker,
      attention: executorAttention?.get(waker.id),
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
      attention: executorAttention?.get(channelId),
    });
  }
  const modeRailKeys =
    preferences.entityMode === "waker"
      ? reconcilePartnerOrder(
          availableWakerKeys,
          navigationGroups.wakerOrder.map((id) => `waker:${id}`),
        )
      : reconcilePartnerOrder(availableTeamKeys, preferences.railOrder);
  const visibleRailItems = modeRailKeys.flatMap<NavigationEntity>((key) => {
    const item = navigationEntityByKey.get(key);
    return item ? [item] : [];
  });
  const selectedRailItem = navigationEntityByKey.get(selectedRailKey);
  const resizeCleanup = useRef<() => void>(() => undefined);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [contextSidebarWidth, setContextSidebarWidth] = useState(() =>
    clampContextSidebarWidth(preferences.contextSidebarWidth),
  );
  const [showcase, setShowcase] = useState<{
    origin: HTMLElement;
    rect: { height: number; left: number; top: number; width: number };
    waker: Waker;
  } | null>(null);
  useEffect(() => () => resizeCleanup.current(), []);
  const updateOrder = (
    sourceId: string,
    targetId: string,
    placement: NavigationDropPlacement,
  ) => {
    const source = navigationEntityByKey.get(sourceId);
    const target = navigationEntityByKey.get(targetId);
    if (source?.kind === "waker" && target?.kind === "waker") {
      void navigationGroups.place(source.id, target.id, placement);
      return;
    }
    onPreferencesChange((current) => ({
      ...current,
      railOrder: [
        ...current.railOrder.filter((key) => key.startsWith("waker:")),
        ...moveItem(
          reconcilePartnerOrder(availableTeamKeys, current.railOrder),
          sourceId,
          targetId,
          placement,
        ),
      ],
    }));
  };
  const settingsLayout = settingsOpen ? settingsSidebar : undefined;
  const partnerContext = !settingsOpen && navigation.sidebarKind === "partners";
  const chatHistoryItem =
    partnerContext && !workflowChatOpen ? selectedRailItem : undefined;
  const sidebar = settingsLayout ?? moduleSidebar;
  const pageSidebar = Boolean(sidebar);
  const hasSidebar = pageSidebar || partnerContext;
  const sidebarCollapsed = hasSidebar && preferences.globalSidebarCollapsed;
  const sidebarExpanded = hasSidebar && !sidebarCollapsed;
  const sidebarToggleLabel = t(
    sidebarCollapsed
      ? "appNavigation.expandSidebar"
      : "appNavigation.collapseSidebar",
  );
  const toggleSidebar = () => {
    resizeCleanup.current();
    setSidebarResizing(false);
    onPreferencesChange((current) => ({
      ...current,
      globalSidebarCollapsed: !current.globalSidebarCollapsed,
    }));
  };
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
    setSidebarResizing(true);
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
        setSidebarResizing(false);
      },
    });
  };
  const bodyContextSidebarHidden = contextSidebarHidden || !partnerContext;
  const showContextSidebar =
    !bodyContextSidebarHidden && Boolean(contextSidebar);
  const showPartnerSettingsNavigation =
    !bodyContextSidebarHidden &&
    !contextSidebar &&
    Boolean(selectedPartnerId) &&
    /^\/wakers\/[^/]+(?:\/|$)/.test(location.pathname);
  const [sidebarSearch, setSidebarSearch] = useState("");
  const searchQuery = sidebarSearch.trim();
  const [settledSearch, setSettledSearch] = useState("");
  const [searchBatch, setSearchBatch] = useState({ key: "", size: 6 });
  useEffect(() => {
    const timer = window.setTimeout(() => setSettledSearch(searchQuery), 180);
    return () => window.clearTimeout(timer);
  }, [searchQuery]);
  const sidebarSearchRef = useRef<HTMLDivElement>(null);
  const [listScope, setListScope] = useState<string>();
  const sidebarView = preferences.conversationSidebarView ?? "partners";
  const displayedSidebarView = sidebarSearch.trim()
    ? "conversations"
    : sidebarView;
  const allConversationItems = [...navigationEntityByKey.values()];
  const defaultScope = sidebarSearch.trim()
    ? "all"
    : (chatHistoryItem?.key ?? "all");
  const effectiveScope =
    listScope && navigationEntityByKey.has(listScope)
      ? listScope
      : listScope === "all"
        ? "all"
        : defaultScope;
  const conversationItems =
    effectiveScope === "all"
      ? allConversationItems
      : allConversationItems.filter((item) => item.key === effectiveScope);
  const searchBatchKey = JSON.stringify([effectiveScope, searchQuery]);
  const batchSize = searchBatchKey === searchBatch.key ? searchBatch.size : 6;
  const visibleConversationItems = searchQuery
    ? conversationItems.slice(0, batchSize)
    : conversationItems;
  const searchScopeKeys = new Set(
    visibleConversationItems.map((item) => item.key),
  );
  const searchPending = Boolean(searchQuery && searchQuery !== settledSearch);
  const sidebarEntityName = (key: string) => {
    const item = navigationEntityByKey.get(key);
    return item?.kind === "waker" ? item.waker.name : item?.team.channel?.name;
  };
  useEffect(() => {
    if (
      !sidebarExpanded ||
      !partnerContext ||
      pageSidebar ||
      showPartnerSettingsNavigation
    )
      return;
    const focusSearch = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        !(event.metaKey || event.ctrlKey) ||
        event.key.toLowerCase() !== "k"
      )
        return;
      const input = sidebarSearchRef.current?.querySelector("input");
      if (!input) return;
      event.preventDefault();
      input.focus();
      input.select();
    };
    window.addEventListener("keydown", focusSearch);
    return () => window.removeEventListener("keydown", focusSearch);
  }, [
    sidebarExpanded,
    partnerContext,
    pageSidebar,
    showPartnerSettingsNavigation,
  ]);
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
          className={cn(
            "app-workspace-frame duration-layout ease-ui relative isolate grid min-h-0 min-w-0 grid-cols-[var(--shell-sidebar-body-width)_minmax(0,1fr)] grid-rows-[minmax(0,1fr)] overflow-hidden motion-reduce:transition-none",
            hasSidebar && !sidebarResizing
              ? "transition-[--shell-sidebar-body-width]"
              : "transition-none",
          )}
          style={
            {
              "--shell-context-width": `${contextSidebarWidth}px`,
              "--shell-sidebar-body-width": sidebarExpanded
                ? `${contextSidebarWidth + 1}px`
                : "0px",
              "--shell-sidebar-toolbar-min-width": hasSidebar
                ? "var(--shell-sidebar-compact-width)"
                : "var(--shell-context-width)",
              "--shell-navigation-leading-inset": `${navigationInset}px`,
              "--shell-sidebar-header-inset": nativeDarwin ? "24px" : "12px",
              "--shell-sidebar-header-height": "38px",
            } as CSSProperties
          }
        >
          <div
            className="app-context-sidebar col-start-1 row-start-1 grid min-h-0 min-w-0 grid-cols-[minmax(0,1fr)] grid-rows-[var(--shell-sidebar-header-height)_minmax(0,1fr)]"
            data-app-context-sidebar
          >
            <div
              className="app-sidebar-topbar flex min-w-0 items-center justify-between"
              data-app-sidebar-toolbar
              data-desktop-drag-region
            >
              <div
                data-sidebar-history-slot
                className="flex shrink-0 items-center"
                data-desktop-no-drag-region
              >
                {sidebarToolbar}
              </div>
              <div
                className="flex shrink-0 items-center"
                data-desktop-no-drag-region
              >
                {hasSidebar ? (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        aria-controls={sidebarBodyId}
                        aria-expanded={sidebarExpanded}
                        aria-label={sidebarToggleLabel}
                        className="shrink-0"
                        data-desktop-no-drag-region
                        onClick={toggleSidebar}
                        size="icon"
                        type="button"
                        variant="ghost"
                      >
                        <PanelLeft aria-hidden="true" strokeWidth={1.5} />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{sidebarToggleLabel}</TooltipContent>
                  </Tooltip>
                ) : null}
                {notificationBell}
              </div>
            </div>
            <div
              className="app-sidebar-body relative min-h-0 min-w-0 overflow-hidden"
              aria-hidden={!sidebarExpanded || undefined}
              hidden={!hasSidebar}
              id={sidebarBodyId}
              inert={!sidebarExpanded}
            >
              <div
                hidden={
                  !partnerContext ||
                  pageSidebar ||
                  showPartnerSettingsNavigation
                }
                aria-hidden={
                  !partnerContext ||
                  pageSidebar ||
                  showPartnerSettingsNavigation ||
                  undefined
                }
                inert={
                  !partnerContext ||
                  pageSidebar ||
                  showPartnerSettingsNavigation
                }
                data-sidebar-entity-body
                className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden"
              >
                <div
                  className="flex shrink-0 items-center gap-1 px-3 py-2"
                  ref={sidebarSearchRef}
                >
                  <SearchField
                    containerClassName="min-w-0 flex-1"
                    aria-label={t("partnerNavigation.sidebarSearch")}
                    placeholder={t("partnerNavigation.sidebarSearch")}
                    size="sm"
                    value={sidebarSearch}
                    maxLength={120}
                    onChange={(event) => setSidebarSearch(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Escape" && sidebarSearch) {
                        event.stopPropagation();
                        setSidebarSearch("");
                      }
                    }}
                  />
                  {displayedSidebarView === "partners" ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t(
                        preferences.entityMode === "waker"
                          ? "partnerNavigation.createWaker"
                          : "partnerNavigation.createTeam",
                      )}
                      onClick={() => onManageEntities(preferences.entityMode)}
                    >
                      <Plus aria-hidden="true" strokeWidth={1.5} />
                    </Button>
                  ) : null}
                </div>
                <div className="min-h-0 flex-1 overflow-clip">
                  <div data-sidebar-page-track={displayedSidebarView}>
                    <div
                      className="min-h-0 overflow-hidden"
                      data-sidebar-page="partners"
                      aria-hidden={
                        displayedSidebarView !== "partners" || undefined
                      }
                      inert={displayedSidebarView !== "partners"}
                    >
                      <EntityNavigation
                        sidebarSearch=""
                        canReorder={
                          preferences.entityMode !== "waker" ||
                          navigationGroups.canMove
                        }
                        collapsed={false}
                        entityStatus={entityStatus}
                        mode={preferences.entityMode}
                        onManageEntities={onManageEntities}
                        onModeChange={(entityMode) =>
                          onPreferencesChange((current) => ({
                            ...current,
                            entityMode,
                          }))
                        }
                        onReorder={updateOrder}
                        onRetryGroupImport={navigationGroups.retryImport}
                        onSelectPartner={onSelectPartner}
                        onSelectTeam={onSelectTeam}
                        onShowcaseWaker={(waker, origin) =>
                          setShowcase(captureShowcase(waker, origin))
                        }
                        selectedKey={selectedRailKey}
                        items={visibleRailItems}
                        wakerGroups={navigationGroups.groups}
                        renderTeamRow={(item, row) => (
                          <div key={item.key}>
                            <TeamNavigationMenu
                              key={item.key}
                              detail={item.team}
                              onNewChat={() =>
                                openNewOwnerChat({ kind: "team", id: item.id })
                              }
                              onDeleted={() => {
                                onPreferencesChange((current) =>
                                  current.currentRailKey === item.key
                                    ? { ...current, currentRailKey: "" }
                                    : current,
                                );
                                if (
                                  location.pathname === `/teams/${item.id}` ||
                                  location.pathname.startsWith(
                                    `/teams/${item.id}/`,
                                  )
                                )
                                  navigate("/chat", { replace: true });
                              }}
                            >
                              {row}
                            </TeamNavigationMenu>
                            <SidebarWorkTree
                              kind={item.kind}
                              id={item.id}
                              sourceName={sidebarEntityName}
                            />
                          </div>
                        )}
                        renderWakerRow={(item, row) => (
                          <div key={item.key}>
                            <WakerNavigationMenu
                              key={item.key}
                              waker={item.waker}
                              wakerId={item.id}
                              groups={navigationGroups.groups}
                              canMove={navigationGroups.canMove}
                              canDuplicate={navigationGroups.migrationResolved}
                              onNewChat={() =>
                                openNewOwnerChat({ kind: "waker", id: item.id })
                              }
                              onMove={(name) =>
                                navigationGroups.move(item.id, name)
                              }
                              onDeleted={() => {
                                void navigationGroups.refresh();
                                const owner = mainConversationOwner(
                                  location.pathname,
                                );
                                if (
                                  owner
                                    ? owner.kind === "waker" &&
                                      owner.id === item.id
                                    : selectedRailKey === item.key &&
                                      (location.pathname === "/chat" ||
                                        activeChatId)
                                )
                                  navigate("/wakers", { replace: true });
                              }}
                            >
                              {row}
                            </WakerNavigationMenu>
                            <SidebarWorkTree
                              kind={item.kind}
                              id={item.id}
                              sourceName={sidebarEntityName}
                            />
                          </div>
                        )}
                      />
                    </div>
                    <div
                      data-sidebar-page="conversations"
                      aria-hidden={
                        displayedSidebarView !== "conversations" || undefined
                      }
                      inert={displayedSidebarView !== "conversations"}
                      role="region"
                      aria-label={t("partnerNavigation.sidebarConversations")}
                      className="scrollbar-hidden min-h-0 overflow-x-hidden overflow-y-auto px-3"
                    >
                      {displayedSidebarView === "conversations" &&
                      allConversationItems.length > 1 ? (
                        <div className="bg-canvas sticky top-0 z-10 pb-1">
                          <Select
                            value={effectiveScope}
                            onValueChange={setListScope}
                          >
                            <SelectTrigger
                              aria-label={t("partnerNavigation.sidebarScope")}
                            >
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="all">
                                {t("partnerNavigation.sidebarAll")}
                              </SelectItem>
                              {allConversationItems.map((item) => (
                                <SelectItem key={item.key} value={item.key}>
                                  {sidebarEntityName(item.key) || item.key}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                      ) : null}
                      {displayedSidebarView === "conversations" &&
                      searchPending ? (
                        <p
                          role="status"
                          className="type-caption text-muted-foreground px-2 py-4"
                        >
                          {t("partnerNavigation.loadingChats")}
                        </p>
                      ) : null}
                      {displayedSidebarView === "conversations" &&
                      !searchPending
                        ? visibleConversationItems.map((item) => (
                            <section
                              key={item.key}
                              aria-label={
                                item.kind === "waker"
                                  ? item.waker.name
                                  : item.team.channel?.name
                              }
                            >
                              {conversationItems.length > 1 ? (
                                <p className="type-caption text-muted-foreground px-2 pt-2">
                                  {item.kind === "waker"
                                    ? item.waker.name
                                    : item.team.channel?.name}
                                </p>
                              ) : null}
                              {sidebarSearch.trim() ? (
                                <SidebarWorkTree
                                  key={item.key}
                                  kind={item.kind}
                                  id={item.id}
                                  search={searchQuery}
                                  searchScopeKeys={searchScopeKeys}
                                  sourceName={sidebarEntityName}
                                />
                              ) : null}
                              <ChatHistoryPanel
                                activeChatId={activeChatId}
                                item={item}
                                onPreferencesChange={onPreferencesChange}
                                preferences={preferences}
                                teamContext={teamContext}
                                search={searchQuery}
                              />
                            </section>
                          ))
                        : null}
                      {displayedSidebarView === "conversations" &&
                      !searchPending &&
                      searchQuery &&
                      visibleConversationItems.length <
                        conversationItems.length ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() =>
                            setSearchBatch({
                              key: searchBatchKey,
                              size: batchSize + 6,
                            })
                          }
                        >
                          {t("partnerNavigation.searchMoreScopes")}
                        </Button>
                      ) : null}
                    </div>
                  </div>
                </div>
                <div className="shrink-0 px-3 pt-1 pb-2">
                  <SegmentedControl
                    appearance="shell"
                    ariaLabel={t("partnerNavigation.sidebarView")}
                    distribution="fill"
                    onValueChange={(value) => {
                      if (value !== "partners" && value !== "conversations")
                        return;
                      setSidebarSearch("");
                      onPreferencesChange((current) => ({
                        ...current,
                        conversationSidebarView: value,
                      }));
                    }}
                    options={[
                      {
                        label: t("partnerNavigation.sidebarPartners"),
                        value: "partners",
                      },
                      {
                        label: t("partnerNavigation.sidebarConversations"),
                        value: "conversations",
                      },
                    ]}
                    size="sm"
                    value={displayedSidebarView}
                  />
                </div>
              </div>
              {sidebar || showPartnerSettingsNavigation ? (
                <div
                  className="absolute inset-0 overflow-x-hidden overflow-y-auto overscroll-contain"
                  data-sidebar-page-nav
                >
                  {sidebar ??
                    (selectedPartnerId ? (
                      <Suspense fallback={null}>
                        <PartnerContextSidebar partnerId={selectedPartnerId} />
                      </Suspense>
                    ) : null)}
                </div>
              ) : null}
            </div>
            {sidebarExpanded ? (
              <div
                className="absolute end-0 top-(--shell-titlebar-height) bottom-0 z-10"
                onDoubleClick={() =>
                  resizeSidebar(defaultContextSidebarWidth, true)
                }
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
          </div>
          <section
            aria-label={t("sceneWorkspace")}
            data-content-spans-sidebar={!sidebarExpanded || undefined}
            className="relative col-start-2 row-start-1 grid min-h-0 min-w-0 grid-rows-[var(--shell-titlebar-height)_minmax(0,1fr)] overflow-hidden"
          >
            <div className="app-content-header min-w-0">
              <SceneChromeHost>{renderTitlebar?.(null)}</SceneChromeHost>
            </div>
            <div
              className={cn(
                "grid h-full min-h-0 min-w-0",
                sceneContentFlush || showPartnerSettingsNavigation
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
            {chatHistoryItem ? <CornerLauncher /> : null}
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

function CornerLauncher() {
  const floatingChat = useFloatingChatLauncher();
  if (!floatingChat) return null;

  return (
    <div className="absolute right-0 bottom-0 z-40">
      <CornerLauncherButton
        aria-label={floatingChat.ariaLabel}
        data-activity-state={floatingChat.state}
        data-floating-chat-shell-launcher
        icon={
          floatingChat.state === "running" ? (
            <LoaderCircle
              className="motion-safe:animate-spin"
              strokeWidth={1.5}
            />
          ) : (
            <MessageSquareText strokeWidth={1.5} />
          )
        }
        indicator={
          floatingChat.state === "waiting" ? (
            <span className="bg-warning absolute top-0.5 right-0.5 size-1 rounded-full" />
          ) : null
        }
        label={floatingChat.label}
        onClick={floatingChat.onActivate}
        title={floatingChat.ariaLabel}
        wide
      />
    </div>
  );
}

function ChatHistoryPanel({
  activeChatId,
  item,
  onPreferencesChange,
  preferences,
  teamContext,
  search,
}: {
  activeChatId?: string;
  item: NavigationEntity;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  preferences: PartnerNavigationPreferences;
  teamContext?: { channelId: string; threadId?: string };
  search?: string;
}) {
  return (
    <ChatHistoryPanelSurface>
      {item.kind === "waker" ? (
        <PartnerChatTree
          activeChatId={activeChatId}
          onPreferencesChange={onPreferencesChange}
          preferences={preferences}
          selectedPartnerId={item.id}
          search={search}
          attention={item.attention?.chats}
        />
      ) : (
        <TeamChatTree
          channelId={item.id}
          search={search}
          attention={item.attention?.chats}
          onPreferencesChange={onPreferencesChange}
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
  attention,
  onPreferencesChange,
  preferences,
  selectedPartnerId,
  search = "",
}: {
  activeChatId?: string;
  attention?: ChatAttention[];
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  preferences: PartnerNavigationPreferences;
  selectedPartnerId: string;
  search?: string;
}) {
  const { t } = useTranslation("common");
  const navigate = useNavigate();
  const appScenes = useOptionalAppSceneWorkspace();
  const queryClient = useQueryClient();
  const historyOptions = chatListInfiniteOptions(
    selectedPartnerId || "pending",
    search || undefined,
  );
  const chatsQuery = useInfiniteQuery({
    ...historyOptions,
    enabled: Boolean(selectedPartnerId),
  });
  useQueryEvents({
    topic: "entities",
    resource: { kind: "global" },
    queryKeys: [historyOptions.queryKey],
    enabled: Boolean(selectedPartnerId),
    eventTypes: ["entities.activity.changed"],
    eventFilter: ({ resource }) =>
      resource.kind === "global" ||
      (resource.kind === "waker" && resource.id === selectedPartnerId),
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
  return (
    <div
      className="flex min-h-0 min-w-0 flex-1 flex-col"
      data-chat-tree="partner"
    >
      <div className="scrollbar-hidden min-h-0 flex-1 overflow-x-hidden overflow-y-auto overscroll-contain pr-1 pb-1">
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
            ) : chatsQuery.isLoadingError ? (
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
                {t(
                  search
                    ? "partnerNavigation.noSearchResults"
                    : "partnerNavigation.noChats",
                )}
              </p>
            ) : (
              groups.map((group, index) => (
                <div className="mt-3 first:mt-0" key={group.key}>
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
                    onToggle={() => toggleGroup(group.key)}
                  />
                  {search.trim() || !collapsedGroups.has(group.key) ? (
                    <div className="mt-1 grid gap-0.5 pl-2">
                      {group.chats.map((chat) => (
                        <ChatNavigationRow
                          key={chat.id}
                          active={chat.id === activeChatId}
                          chat={chat}
                          attention={attention?.find(
                            (entry) => entry.work?.id === chat.id,
                          )}
                          onAction={(mode) =>
                            setTaskAction({
                              chatId: chat.id ?? "",
                              mode,
                              title: chat.title ?? "",
                            })
                          }
                          onPrefetch={prefetchChat}
                        />
                      ))}
                    </div>
                  ) : null}
                </div>
              ))
            )}
            <ResourceInfiniteScroll
              hasMore={chatsQuery.hasNextPage}
              pending={chatsQuery.isFetching}
              error={
                chatsQuery.isFetchNextPageError ? chatsQuery.error : undefined
              }
              onLoadMore={() => void chatsQuery.fetchNextPage()}
            />
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
  attention?: ChatAttention;
  chat: Chat;
  onAction: (mode: "delete" | "rename") => void;
  onPrefetch: (chatId: string) => void;
};

function ChatNavigationRow({
  active,
  attention,
  chat,
  onAction,
  onPrefetch,
}: ChatNavigationRowProps) {
  const { t } = useTranslation("common");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const chatId = chat.id ?? "";
  const questions = attention?.requests?.filter(({ kind }) => kind === "input");
  const pending = waitingConversations(
    questions?.length ? questions : attention?.requests,
  )[0];
  const requestKinds = attention?.requestKinds?.length
    ? attention.requestKinds
    : [
        ...(chat.waitingElicitation ? ["input"] : []),
        ...(chat.waitingApproval ? ["approval"] : []),
      ];
  const href = pending
    ? workAttentionHref(pending.request)
    : chatHref(chatId, chat.wakerId ?? "");
  const status = waitingInteraction(chat)
    ? "waiting"
    : chat.activeExecutionId
      ? "running"
      : "idle";
  return (
    <ChatHistoryNavigationRow
      active={active}
      title={chat.title || t("partnerNavigation.untitledChat")}
      runtimeId={chat.lastRuntimeId}
      modelId={chat.lastModel}
      protection={
        typeof chat.retentionProtected === "boolean" && chatId
          ? {
              chatId,
              wakerId: chat.wakerId ?? "",
              protected: chat.retentionProtected,
            }
          : undefined
      }
      status={status}
      requestKinds={requestKinds}
      unreadCount={attention?.unreadMessageCount}
      href={href}
      onOpen={(event) => {
        navigateRouteImmediately(event, navigate, href, () =>
          pending ? undefined : captureChatOpenIntent(queryClient, chatId),
        );
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
