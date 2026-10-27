import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { PanelRight, Plus } from "lucide-react";
import {
  type ComponentType,
  lazy,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";

import type { ChatRightPanel } from "@/modules/chat";
import { chatListInfiniteOptions, getChatOptions } from "@/modules/chat";
import {
  SettingsContextSidebar,
  SettingsScene,
  type SettingsSection,
} from "@/modules/settings";
import { listWakersOptions } from "@/modules/waker";
import {
  NotificationBell,
  NotificationPresenter,
  NotificationNavigationProvider,
} from "@/modules/notification";
import { notificationHref } from "../notifications/notification-navigation";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listRuntimesQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  teamChannelsInfiniteOptions,
  teamThreadsInfiniteOptions,
} from "@/modules/team";
import {
  AppSceneRouteScope,
  useAppSceneWorkspace,
} from "@/shared/workspace/app-scene-workspace";
import {
  chatDraftHref,
  chatEntryHref,
  chatHref,
  chatLastActiveFromLocation,
  getChatLastActiveSnapshot,
  rememberChatLastActive,
  subscribeChatNavigation,
} from "@/shared/lib/chat-navigation";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { SceneChromeActionsProvider } from "@/shared/ui/scene-chrome-actions";
import { SceneChromePortal } from "@/shared/ui/scene-chrome-portal";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import { Toaster } from "@/shared/ui/sonner";
import {
  useSceneWorkspace,
  WorkspaceProvider,
} from "@/shared/workspace/workspace-context";
import { PartnerNavigationLayout } from "./partner-navigation-layout";
import { TeamMemberMenu } from "./team-context-sidebar";
import { WelcomeScene } from "../scenes/welcome-scene";
import {
  AppRouteScene,
  type AppRouteSceneProps,
} from "../scenes/app-route-scene";
import {
  routeSceneForLocation,
  settingsSceneTab,
  type RouteSceneLabels,
} from "../scenes/app-route-scene-model";
import { AppSceneWorkspaceProvider } from "../scenes/app-scene-workspace-provider";
import {
  legacyIMSettingsHref,
  settingsSectionFromPath,
} from "../scenes/settings-route";
import {
  readPartnerNavigationPreferences,
  forgetTeamThread,
  rememberTeamThread,
  writePartnerNavigationPreferences,
} from "./partner-navigation-preferences";

const IMIntegrationSettingsContent = lazy(() =>
  import("../scenes/im-integration-settings-content").then((module) => ({
    default: module.IMIntegrationSettingsContent,
  })),
);

const titlebarActionClass =
  "border-transparent bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground";

export function AppShell({
  routeScene: RouteScene = AppRouteScene,
}: {
  routeScene?: ComponentType<AppRouteSceneProps>;
}) {
  const location = useLocation();
  const navigate = useNavigate();
  useEffect(() => {
    const href = legacyIMSettingsHref(location.pathname, location.search);
    if (href) navigate(href, { replace: true });
  }, [location.pathname, location.search, navigate]);
  return (
    <NotificationNavigationProvider resolveHref={notificationHref}>
      <WorkspaceProvider>
        <AppSceneWorkspaceProvider
          location={{ pathname: location.pathname, search: location.search }}
          navigate={(href) => navigate(href, { flushSync: true })}
        >
          <AppShellContent RouteScene={RouteScene} />
        </AppSceneWorkspaceProvider>
      </WorkspaceProvider>
    </NotificationNavigationProvider>
  );
}

function AppShellContent({
  RouteScene,
}: {
  RouteScene: ComponentType<AppRouteSceneProps>;
}) {
  const { t } = useTranslation(["team", "chat", "common", "automaticTask"]);
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const appScenes = useAppSceneWorkspace();
  const [searchParams] = useSearchParams();
  const [preferences, setPreferences] = useState(
    readPartnerNavigationPreferences,
  );
  const hasActiveScene = Boolean(appScenes.snapshot.activeTabId);
  const activeScene = appScenes.snapshot.tabs.find(
    (tab) => tab.id === appScenes.snapshot.activeTabId,
  );
  const activeRouteSceneId =
    activeScene?.kind === "route"
      ? activeScene.id
      : activeScene?.kind === "workspace"
        ? activeScene.ownerTabId
        : undefined;
  const settingsOpen = appScenes.snapshot.activeTabId === "settings";
  const entitySelectionRequestRef = useRef(0);
  const settingsPreviousCollapsedRef = useRef(false);
  const previousSettingsOpenRef = useRef(false);
  const [sceneNavigationVisible, setSceneNavigationVisible] = useState(() =>
    Boolean(settingsSectionFromPath(location.pathname)),
  );
  const [lastSettingsSection, setSettingsSection] =
    useState<SettingsSection>("general");
  const settingsTab = appScenes.snapshot.tabs.find(
    (tab) => tab.id === "settings",
  );
  const settingsSection =
    settingsSectionFromPath(location.pathname) ??
    settingsSectionFromPath(settingsTab?.href?.split("?")[0] || "") ??
    lastSettingsSection;
  const selectSettingsSection = (section: SettingsSection) => {
    setSettingsSection(section);
    navigate(`/settings/${section}`, { flushSync: true });
  };
  const [chatRightPanel, setChatRightPanel] = useState<
    ChatRightPanel | undefined
  >(undefined);
  const lastActiveChat = useSyncExternalStore(
    subscribeChatNavigation,
    getChatLastActiveSnapshot,
    getChatLastActiveSnapshot,
  );
  const wakersQuery = useQuery({
    ...listWakersOptions({ query: { pageNum: 1, pageSize: 100 } }),
  });
  const wakers = wakersQuery.data?.resources ?? [];
  const channelsQuery = useInfiniteQuery({
    ...teamChannelsInfiniteOptions(),
  });
  const channels =
    channelsQuery.data?.pages.flatMap((page) => page.resources ?? []) ?? [];
  const entityStatus =
    preferences.entityMode === "waker"
      ? wakersQuery.isPending
        ? "loading"
        : wakersQuery.isError
          ? "error"
          : "ready"
      : channelsQuery.isPending
        ? "loading"
        : channelsQuery.isError
          ? "error"
          : "ready";
  const [notificationsConnected, setNotificationsConnected] = useState(false);
  useQueryEvents({
    topic: "entities",
    resource: { kind: "global" },
    queryKeys: [
      listWakersOptions().queryKey,
      teamChannelsInfiniteOptions().queryKey,
    ],
  });
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "global" },
    queryKeys: [listRuntimesQueryKey()],
  });
  const isChat =
    location.pathname === "/" ||
    location.pathname === "/chat" ||
    location.pathname.startsWith("/chats/");
  const teamRoute = /^\/teams\/([^/]+)(?:\/threads\/([^/]+)|\/chats)?$/.exec(
    location.pathname,
  );
  const teamChannelId = teamRoute?.[1] ?? "";
  const teamThreadId = teamRoute?.[2];
  const isTeamContext = Boolean(teamChannelId);
  const isTeamChat = Boolean(
    teamChannelId && !location.pathname.endsWith("/chats"),
  );
  const teamChatScope = isTeamChat
    ? `${teamChannelId}:${teamThreadId ?? `draft:${searchParams.get("draftId") ?? "default"}`}`
    : "";
  const teamWorkspaceScopeKey = teamChatScope
    ? `team:${teamChatScope}`
    : "app-shell";
  const teamWorkspace = useSceneWorkspace(teamWorkspaceScopeKey);
  const defaultedTeamChat = useRef(teamChatScope);
  useEffect(() => {
    if (!teamChatScope) {
      defaultedTeamChat.current = "";
      return;
    }
    if (defaultedTeamChat.current === teamChatScope) return;
    defaultedTeamChat.current = teamChatScope;
    setChatRightPanel(undefined);
  }, [teamChatScope]);
  const isChatSurface = isChat || isTeamChat;
  const activeRightPanel = chatRightPanel;
  const isWakerDetail =
    /^\/wakers\/[^/]+(?:\/(?:connectors|memory|overview|projects|skills|resources|plugins)(?:\/.*)?)?$/.test(
      location.pathname,
    );
  const hasPartnerContext =
    isChatSurface || isTeamContext || location.pathname.startsWith("/wakers/");
  const routePartnerId = location.pathname.startsWith("/wakers/")
    ? location.pathname.split("/")[2]
    : searchParams.get("wakerId")?.trim();
  const currentChat = chatLastActiveFromLocation(location);
  const currentChatId = currentChat?.chatId;
  const currentChatQuery = useQuery({
    ...getChatOptions({
      path: {
        chatId: currentChatId ?? "pending",
      },
    }),
    enabled: Boolean(currentChatId),
  });
  const partnerWorkspaceScopeKey = isChat
    ? currentChatId
      ? `chat:${currentChatId}`
      : `draft:${searchParams.get("draftId") ?? "default"}`
    : "app-shell-partner";
  const partnerWorkspace = useSceneWorkspace(partnerWorkspaceScopeKey);
  const chatWorkspace = isTeamChat ? teamWorkspace : partnerWorkspace;
  const activeChatWorkspaceTab = isChatSurface
    ? chatWorkspace.snapshot.tabs.find(
        (tab) => tab.id === chatWorkspace.snapshot.activeTabId,
      )
    : undefined;
  const hasOpenSceneTabs = appScenes.snapshot.tabs.length > 0;
  const showChatChromeActions =
    hasOpenSceneTabs &&
    isChatSurface &&
    (!activeChatWorkspaceTab ||
      activeChatWorkspaceTab.kind === "chat" ||
      activeChatWorkspaceTab.kind === "member");
  const lastActiveValidationQuery = useQuery({
    ...getChatOptions({
      path: {
        chatId: lastActiveChat?.chatId ?? "pending",
      },
    }),
    enabled: Boolean(lastActiveChat?.chatId),
  });
  const validatedChat = lastActiveValidationQuery.data?.chat;
  const validatedLastActive =
    lastActiveChat &&
    validatedChat?.id === lastActiveChat.chatId &&
    validatedChat.wakerId === lastActiveChat.wakerId
      ? lastActiveChat
      : undefined;
  const availablePartnerIds = new Set(
    wakers.flatMap((waker) => (waker.id ? [waker.id] : [])),
  );
  const storedPartnerId = [
    preferences.currentPartnerId,
    validatedLastActive?.wakerId,
  ].find((partnerId) => partnerId && availablePartnerIds.has(partnerId));
  const selectedPartnerId =
    routePartnerId ||
    currentChat?.wakerId ||
    storedPartnerId ||
    wakers[0]?.id ||
    "";
  useEffect(() => {
    writePartnerNavigationPreferences(preferences);
  }, [preferences]);

  useLayoutEffect(() => {
    if (!currentChat?.chatId || !currentChat.wakerId) {
      return;
    }
    rememberChatLastActive(currentChat.wakerId, currentChat.chatId);
  }, [currentChat?.chatId, currentChat?.wakerId]);

  useLayoutEffect(() => {
    if (!currentChatId) return;
    const title = currentChatQuery.data?.chat?.title?.trim();
    if (!title) return;
    const tabId = `chat:${currentChatId}`;
    const tab = appScenes.snapshot.tabs.find((item) => item.id === tabId);
    if (!tab || tab.label === title) return;
    appScenes.dispatch({ type: "rename", tabId, label: title });
  }, [appScenes, currentChatId, currentChatQuery.data?.chat?.title]);

  useLayoutEffect(() => {
    if (!teamChannelId || !teamThreadId) return;
    // 路由是浏览器外部状态；进入已落库 Thread 后同步导航偏好一次。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPreferences((current) =>
      rememberTeamThread(current, teamChannelId, teamThreadId),
    );
  }, [teamChannelId, teamThreadId]);

  useLayoutEffect(() => {
    const wasOpen = previousSettingsOpenRef.current;
    previousSettingsOpenRef.current = settingsOpen;
    if (wasOpen === settingsOpen) return;
    setSceneNavigationVisible(settingsOpen);
    // Settings 激活时占用完整导航栏；切离后恢复进入前的折叠状态。
    setPreferences((current) => {
      if (settingsOpen) {
        settingsPreviousCollapsedRef.current = current.globalSidebarCollapsed;
      }
      return {
        ...current,
        globalSidebarCollapsed: settingsOpen
          ? false
          : settingsPreviousCollapsedRef.current,
      };
    });
  }, [settingsOpen]);

  const openPartnerDetail = (partnerId: string) => {
    navigate(`/wakers/${partnerId}`, { flushSync: true });
  };

  const selectPartner = (partnerId: string) => {
    const request = entitySelectionRequestRef.current + 1;
    entitySelectionRequestRef.current = request;
    setPreferences((current) => ({
      ...current,
      currentPartnerId: partnerId,
      currentRailKey: `waker:${partnerId}`,
      entityMode: "waker",
    }));
    setChatRightPanel(undefined);
    void queryClient
      .fetchInfiniteQuery(chatListInfiniteOptions(partnerId))
      .then((data) => {
        if (entitySelectionRequestRef.current !== request) return;
        const latestChatId = data.pages[0]?.resources?.[0]?.id;
        navigate(
          latestChatId
            ? chatHref(latestChatId, partnerId)
            : chatDraftHref(partnerId),
          { flushSync: true },
        );
      })
      .catch(() => {
        if (entitySelectionRequestRef.current !== request) return;
        navigate(chatEntryHref(partnerId), { flushSync: true });
      });
  };

  const selectTeam = (channelId: string) => {
    const request = entitySelectionRequestRef.current + 1;
    entitySelectionRequestRef.current = request;
    setPreferences((current) => ({
      ...current,
      currentRailKey: `team:${channelId}`,
      entityMode: "team",
    }));
    setChatRightPanel(undefined);
    void import("@/app/pages/team-thread-page");
    void queryClient
      .fetchInfiniteQuery(teamThreadsInfiniteOptions(channelId))
      .then((data) => {
        if (entitySelectionRequestRef.current !== request) return;
        const latestThreadId = data.pages[0]?.resources?.[0]?.id;
        navigate(
          latestThreadId
            ? `/teams/${channelId}/threads/${latestThreadId}`
            : `/teams/${channelId}?draftId=${crypto.randomUUID()}`,
          { flushSync: true },
        );
      })
      .catch(() => {
        if (entitySelectionRequestRef.current !== request) return;
        const rememberedThreadId = preferences.lastTeamThreads[channelId];
        navigate(
          rememberedThreadId
            ? `/teams/${channelId}/threads/${rememberedThreadId}`
            : `/teams/${channelId}?draftId=${crypto.randomUUID()}`,
          { flushSync: true },
        );
      });
  };

  const routeSceneLabels = useMemo<RouteSceneLabels>(
    () => ({
      resources: t("plugin:title"),
      knowledge: t("knowledge:knowledge"),
      workBoard: t("workBoard:title"),
      im: t("im:title"),
      automaticTask: t("automaticTask:title"),
      workflow: t("workflow:title"),
      chat: t("common:sceneTabs.chat"),
      detail: t("common:sceneTabs.detail"),
      newChat: t("common:sceneTabs.newChat"),
      page: t("common:sceneTabs.page"),
      partner: t("common:sceneTabs.partner"),
      settings: t("common:sceneTabs.settings"),
      team: t("common:sceneTabs.team"),
      teamChat: t("common:sceneTabs.teamChat"),
    }),
    [t],
  );
  const openWorkBoard = useCallback(() => {
    setSceneNavigationVisible(false);
    const href = "/work-board";
    navigate(href, { flushSync: true });
    appScenes.dispatch({
      type: "open",
      tab: routeSceneForLocation(
        { pathname: href, search: "" },
        routeSceneLabels,
      ),
    });
  }, [appScenes, navigate, routeSceneLabels]);
  const workBoardActive = location.pathname === "/work-board" && !settingsOpen;
  const bellInSceneChrome = preferences.globalSidebarCollapsed && !settingsOpen;
  const openAutomaticTasks = useCallback(() => {
    setSceneNavigationVisible(false);
    const href = "/automatic-tasks";
    navigate(href, { flushSync: true });
    appScenes.dispatch({
      type: "open",
      tab: routeSceneForLocation(
        { pathname: href, search: "" },
        routeSceneLabels,
      ),
    });
  }, [appScenes, navigate, routeSceneLabels]);
  const openSettings = useCallback(() => {
    setSceneNavigationVisible(true);
    if (settingsOpen) return;
    const href = `/settings/${settingsSection}`;
    settingsPreviousCollapsedRef.current = preferences.globalSidebarCollapsed;
    if (`${location.pathname}${location.search}` !== href) {
      navigate(href, { flushSync: true });
    } else {
      appScenes.dispatch({
        type: "open",
        tab: { ...settingsSceneTab(t("common:sceneTabs.settings")), href },
      });
    }
  }, [
    appScenes,
    navigate,
    preferences.globalSidebarCollapsed,
    settingsOpen,
    settingsSection,
    location.pathname,
    location.search,
    t,
  ]);
  const closeSettings = () => {
    setSceneNavigationVisible(false);
    setSettingsSection(settingsSection);
    appScenes.dispatch({ type: "close", tabId: "settings" });
    if (
      appScenes.snapshot.tabs.length === 1 &&
      settingsSectionFromPath(location.pathname)
    ) {
      navigate("/", { replace: true });
    }
  };
  useEffect(() => {
    const openGeneral = () => {
      setSettingsSection("general");
      setSceneNavigationVisible(true);
      navigate("/settings/general", { flushSync: true });
    };
    window.addEventListener("ally:open-general-settings", openGeneral);
    return () =>
      window.removeEventListener("ally:open-general-settings", openGeneral);
  }, [navigate]);
  const sceneActions = (
    <>
      {bellInSceneChrome ? (
        <NotificationBell
          active={workBoardActive}
          onOpen={openWorkBoard}
          connected={notificationsConnected}
        />
      ) : null}
      {hasActiveScene && !settingsOpen && showChatChromeActions ? (
        <Button
          aria-label={t("workspace")}
          onClick={() => {
            setChatRightPanel((current) =>
              current === "workspace" ? undefined : "workspace",
            );
          }}
          className={cn(
            titlebarActionClass,
            activeRightPanel === "workspace" && "text-foreground",
          )}
          size="icon-sm"
          title={t("workspace")}
          type="button"
          variant="ghost"
        >
          <PanelRight />
        </Button>
      ) : null}
    </>
  );

  return (
    <SceneChromeActionsProvider actions={sceneActions}>
      <NotificationPresenter onConnectionChange={setNotificationsConnected} />
      <div className="bg-canvas relative h-full overflow-hidden">
        <PartnerNavigationLayout
          notificationBell={
            settingsOpen || bellInSceneChrome ? undefined : (
              <NotificationBell
                active={workBoardActive}
                onOpen={openWorkBoard}
                connected={notificationsConnected}
              />
            )
          }
          activeChatId={currentChat?.chatId}
          contextSidebarHidden={!hasActiveScene || !isWakerDetail}
          entityStatus={entityStatus}
          onManageEntities={(mode) =>
            navigate(mode === "team" ? "/wakers?tab=teams" : "/wakers")
          }
          onOpenPartnerDetail={openPartnerDetail}
          onOpenAutomaticTasks={openAutomaticTasks}
          onOpenKnowledge={() => {
            setSceneNavigationVisible(false);
            const href = "/knowledge";
            navigate(href, { flushSync: true });
            appScenes.dispatch({
              type: "open",
              tab: routeSceneForLocation(
                { pathname: href, search: "" },
                routeSceneLabels,
              ),
            });
          }}
          onOpenResources={() => {
            setSceneNavigationVisible(false);
            const href = "/resources";
            navigate(href, { flushSync: true });
            appScenes.dispatch({
              type: "open",
              tab: routeSceneForLocation(
                { pathname: href, search: "" },
                routeSceneLabels,
              ),
            });
          }}
          onOpenSettings={openSettings}
          onPreferencesChange={setPreferences}
          onSceneNavigationVisibleChange={setSceneNavigationVisible}
          onSelectPartner={selectPartner}
          onSelectTeam={selectTeam}
          preferences={preferences}
          sceneNavigationVisible={sceneNavigationVisible}
          sceneContentFlush={
            (isChatSurface ||
              /^\/(?:knowledge|resources|work-board|workflows|im|automatic-tasks)(?:\/|$)/.test(
                location.pathname,
              )) &&
            !settingsOpen
          }
          channels={channels}
          selectedRailKey={
            isTeamContext
              ? `team:${teamChannelId}`
              : selectedPartnerId
                ? `waker:${selectedPartnerId}`
                : preferences.currentRailKey
          }
          selectedPartnerId={hasPartnerContext ? selectedPartnerId : undefined}
          settingsSidebar={
            settingsOpen ? (
              <SettingsContextSidebar
                activeSection={settingsSection}
                onSelect={selectSettingsSection}
              />
            ) : undefined
          }
          settingsOpen={settingsOpen}
          teamContext={
            isTeamContext
              ? {
                  channelId: teamChannelId,
                  threadId: teamThreadId,
                }
              : undefined
          }
          wakers={wakers}
        >
          <div className="relative h-full min-h-0 overflow-hidden">
            <SceneChromePortal>
              <SceneTabStrip
                activeTabId={appScenes.snapshot.activeTabId ?? ""}
                ariaLabel={t("common:partnerNavigation.sceneTabList")}
                onClose={(tabId) => {
                  if (tabId === "settings") {
                    closeSettings();
                    return;
                  }
                  appScenes.dispatch({ type: "close", tabId });
                }}
                onCloseOthers={(tabId) => {
                  setSceneNavigationVisible(tabId === "settings");
                  appScenes.dispatch({ type: "close-others", tabId });
                }}
                onSelect={(tabId) => {
                  if (tabId === "settings" && !settingsOpen) {
                    settingsPreviousCollapsedRef.current =
                      preferences.globalSidebarCollapsed;
                  }
                  setSceneNavigationVisible(tabId === "settings");
                  appScenes.dispatch({ type: "select", tabId });
                }}
                tabs={appScenes.snapshot.tabs}
                trailing={
                  (showChatChromeActions ||
                    /^\/workflows\/[^/]+(?:\/runs\/[^/]+)?$/.test(
                      location.pathname,
                    )) &&
                  !settingsOpen ? (
                    <Button
                      aria-label={t("common:partnerNavigation.newChat")}
                      onClick={() => {
                        if (appScenes.openNewChat?.()) return;
                        setChatRightPanel(undefined);
                        navigate(
                          isTeamChat
                            ? `/teams/${teamChannelId}?draftId=${crypto.randomUUID()}`
                            : chatDraftHref(selectedPartnerId),
                          { flushSync: true },
                        );
                      }}
                      size="icon-sm"
                      title={t("common:partnerNavigation.newChat")}
                      type="button"
                      variant="ghost"
                    >
                      <Plus />
                    </Button>
                  ) : undefined
                }
              />
            </SceneChromePortal>
            {appScenes.snapshot.tabs
              .filter((tab) => tab.kind === "route" && tab.href)
              .map((tab) => {
                const active = tab.id === activeRouteSceneId && !settingsOpen;
                return (
                  <div
                    key={tab.mountKey ?? tab.id}
                    aria-hidden={!active}
                    data-route-scene-active={active || undefined}
                    data-route-scene-id={tab.id}
                    hidden={!active}
                    className={cn(
                      "absolute inset-0 h-full min-h-0",
                      !active && "hidden",
                    )}
                    inert={!active}
                  >
                    <AppSceneRouteScope routeSceneId={tab.id}>
                      <RouteScene
                        location={tab.href ?? "/"}
                        outletContext={{
                          chatRightPanel: active ? activeRightPanel : undefined,
                          onTeamThreadDeleted: (
                            channelId: string,
                            threadId: string,
                          ) =>
                            setPreferences((current) =>
                              forgetTeamThread(current, channelId, threadId),
                            ),
                          setChatRightPanel,
                          renderTeamMembers: isTeamChat
                            ? (onMemberOpened: () => void) => (
                                <TeamMemberMenu
                                  channelId={teamChannelId}
                                  onMemberOpened={onMemberOpened}
                                  threadId={teamThreadId}
                                />
                              )
                            : undefined,
                        }}
                      />
                    </AppSceneRouteScope>
                  </div>
                );
              })}
            {!hasActiveScene ? <WelcomeScene /> : null}
            {settingsOpen ? (
              <div className="absolute inset-0 z-10">
                <SettingsScene
                  section={settingsSection}
                  imIntegrationContent={<IMIntegrationSettingsContent />}
                  onClose={closeSettings}
                  onSelectSection={selectSettingsSection}
                  showSceneChrome={false}
                />
              </div>
            ) : null}
          </div>
        </PartnerNavigationLayout>
        <Toaster position="top-right" />
      </div>
    </SceneChromeActionsProvider>
  );
}
