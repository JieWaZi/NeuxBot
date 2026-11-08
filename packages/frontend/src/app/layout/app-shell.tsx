import {
  useInfiniteQuery,
  useQuery,
  useQueries,
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
import {
  useLocation,
  useNavigate,
  useNavigationType,
  useSearchParams,
} from "react-router-dom";

import type { ChatRightPanel } from "@/modules/chat";
import { listChatsOptions, getChatOptions } from "@/modules/chat";
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
import { ApiError } from "@/shared/api/errors";
import {
  getWakerQueryKey,
  getChannelQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { nativeModelCatalogQueryKey } from "@/shared/api/runtime-queries";
import { listRuntimesQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  getChannelOptions,
  getChannelThreadOptions,
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
import {
  AppTitlebar,
  pageOwnsSceneHeader,
  AppNavigationHistoryControls,
  useAppNavigationHistory,
} from "./app-titlebar";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import {
  chatOwnerForScene,
  chatScenesForOwner,
} from "./entity-navigation-history";
import { Toaster } from "@/shared/ui/sonner";
import {
  useSceneWorkspace,
  WorkspaceProvider,
} from "@/shared/workspace/workspace-context";
import {
  type AppModuleId,
  resolveAppModuleHref,
  resolveAppNavigation,
} from "./app-navigation-model";
import { ResourcesContextSidebar } from "./resources-context-sidebar";
import {
  changeResourceCategory,
  resourceCategoryFromParams,
  resourceLibraryHref,
} from "../pages/resources-navigation";
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
  const navigationType = useNavigationType();
  const navigate = useNavigate();
  useEffect(() => {
    const href = legacyIMSettingsHref(location.pathname, location.search);
    if (href) navigate(href, { replace: true });
  }, [location.pathname, location.search, navigate]);
  return (
    <NotificationNavigationProvider resolveHref={notificationHref}>
      <WorkspaceProvider>
        <AppSceneWorkspaceProvider
          navigationType={navigationType}
          location={{
            pathname: location.pathname,
            search: location.search,
            key: location.key,
            state: location.state,
          }}
          navigate={(href, state) => navigate(href, { flushSync: true, state })}
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
  const navigation = resolveAppNavigation(location);
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
    queryKeys: [listWakersOptions().queryKey],
    eventFilter: (frame) =>
      frame.resource.kind === "global" || frame.resource.kind === "waker",
  });
  useQueryEvents({
    topic: "entities",
    resource: { kind: "global" },
    queryKeys: [teamChannelsInfiniteOptions().queryKey],
    eventFilter: (frame) =>
      frame.resource.kind === "global" || frame.resource.kind === "channel",
  });
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "global" },
    queryKeys: [listRuntimesQueryKey(), nativeModelCatalogQueryKey()],
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
  const hasPartnerContext = navigation.moduleId === "partners";
  const routePartnerId = location.pathname.startsWith("/wakers/")
    ? location.pathname.split("/")[2]
    : searchParams.get("wakerId")?.trim();
  const currentChatHint = chatLastActiveFromLocation(location);
  const currentChatId = /^\/chats\/([^/]+)$/.exec(location.pathname)?.[1];
  const currentChatQuery = useQuery({
    ...getChatOptions({
      path: {
        chatId: currentChatId ?? "pending",
      },
    }),
    enabled: Boolean(currentChatId),
  });
  const resolvedChat = currentChatQuery.data?.chat;
  const currentChat =
    currentChatId && resolvedChat?.wakerId
      ? { chatId: currentChatId, wakerId: resolvedChat.wakerId }
      : currentChatHint;
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
    enabled:
      Boolean(lastActiveChat?.chatId) &&
      lastActiveChat?.chatId !== currentChatId,
  });
  const validatedChat = lastActiveValidationQuery.data?.chat;
  const validatedLastActive =
    lastActiveChat &&
    lastActiveValidationQuery.isSuccess &&
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
  const activeRouteOwner = appScenes.snapshot.tabs.find(
    (tab) => tab.id === activeRouteSceneId,
  )?.chatOwner;
  const selectedPartnerId =
    currentChatQuery.data?.chat?.wakerId ||
    (activeRouteOwner?.kind === "waker" ? activeRouteOwner.id : undefined) ||
    routePartnerId ||
    currentChat?.wakerId ||
    (isChat && currentChatId ? "" : storedPartnerId || wakers[0]?.id || "");
  useEffect(() => {
    writePartnerNavigationPreferences(preferences);
  }, [preferences]);

  useLayoutEffect(() => {
    if (!currentChat?.chatId || !currentChat.wakerId) {
      return;
    }
    rememberChatLastActive(currentChat.wakerId, currentChat.chatId);
    // 深链与历史切换同样更新既有最近身份偏好，列表模式仍由用户独立切换。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPreferences((current) =>
      current.currentRailKey === `waker:${currentChat.wakerId}`
        ? current
        : {
            ...current,
            currentPartnerId: currentChat.wakerId,
            currentRailKey: `waker:${currentChat.wakerId}`,
          },
    );
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
    setPreferences((current) => {
      const next = rememberTeamThread(current, teamChannelId, teamThreadId);
      return next.currentRailKey === `team:${teamChannelId}`
        ? next
        : { ...next, currentRailKey: `team:${teamChannelId}` };
    });
  }, [teamChannelId, teamThreadId]);

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
      .fetchQuery({
        ...listChatsOptions({ query: { wakerId: partnerId, limit: 1 } }),
        staleTime: 0,
      })
      .then((data) => {
        if (entitySelectionRequestRef.current !== request) return;
        const latestChatId = data.resources?.[0]?.id;
        if (
          !latestChatId &&
          appScenes.openNewChat?.({ kind: "waker", id: partnerId })
        )
          return;
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
        if (
          !latestThreadId &&
          appScenes.openNewChat?.({ kind: "team", id: channelId })
        )
          return;
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
        if (
          !rememberedThreadId &&
          appScenes.openNewChat?.({ kind: "team", id: channelId })
        )
          return;
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
      usage: t("usage:title"),
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
    const href = resolveAppModuleHref("work-board");
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
  const openSettings = useCallback(() => {
    if (settingsOpen) return;
    const href = resolveAppModuleHref("settings", {
      settingsHref: `/settings/${settingsSection}`,
    });
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
    settingsOpen,
    settingsSection,
    location.pathname,
    location.search,
    t,
  ]);
  const rememberedTeamId = preferences.currentRailKey.startsWith("team:")
    ? preferences.currentRailKey.slice(5)
    : "";
  const rememberedTeamThreadId = preferences.lastTeamThreads[rememberedTeamId];
  const lastTeamDetailQuery = useQuery({
    ...getChannelOptions({
      path: { channelId: rememberedTeamId || "pending" },
    }),
    enabled: Boolean(rememberedTeamId && rememberedTeamThreadId),
  });
  // 分页列表只能证明已读实体；最近目标的存在性由同一团队详情 Query 验证。
  const rememberedTeamExists =
    lastTeamDetailQuery.isSuccess &&
    lastTeamDetailQuery.data?.channel?.id === rememberedTeamId;
  const lastTeamValidationQuery = useQuery({
    ...getChannelThreadOptions({
      path: {
        channelId: rememberedTeamId || "pending",
        threadId: rememberedTeamThreadId || "pending",
      },
    }),
    enabled: Boolean(rememberedTeamExists && rememberedTeamThreadId),
  });
  const validatedPartnerHref = rememberedTeamId
    ? rememberedTeamExists &&
      rememberedTeamThreadId &&
      lastTeamValidationQuery.isSuccess &&
      lastTeamValidationQuery.data?.thread?.id === rememberedTeamThreadId
      ? `/teams/${rememberedTeamId}/threads/${rememberedTeamThreadId}`
      : undefined
    : validatedLastActive &&
        availablePartnerIds.has(validatedLastActive.wakerId)
      ? chatHref(validatedLastActive.chatId, validatedLastActive.wakerId)
      : undefined;
  const navigateModule = (moduleId: AppModuleId) => {
    if (moduleId === "settings") {
      openSettings();
      return;
    }
    const href = resolveAppModuleHref(moduleId, {
      partnerHref: validatedPartnerHref,
    });
    navigate(href, { flushSync: true });
    const [pathname = "/", query] = href.split("?");
    appScenes.dispatch({
      type: "open",
      tab: routeSceneForLocation(
        { pathname, search: query ? `?${query}` : "" },
        routeSceneLabels,
      ),
    });
  };
  const closeSettings = () => {
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
      navigate("/settings/general", { flushSync: true });
    };
    window.addEventListener("ally:open-general-settings", openGeneral);
    return () =>
      window.removeEventListener("ally:open-general-settings", openGeneral);
  }, [navigate]);
  const chatRouteTabs = appScenes.snapshot.tabs.flatMap((tab) => {
    const match =
      tab.kind === "route" ? /^\/chats\/([^/?]+)/.exec(tab.href ?? "") : null;
    return match?.[1] ? [{ tab, chatId: match[1] }] : [];
  });
  // 只订阅页面已拥有的 Query Cache；后台标题与身份迟到不会新增请求。
  const cachedChats = useQueries({
    queries: chatRouteTabs.map(({ chatId }) => ({
      ...getChatOptions({ path: { chatId } }),
      enabled: false,
    })),
  });
  const teamThreadTabs = appScenes.snapshot.tabs.flatMap((tab) => {
    const match =
      tab.kind === "route"
        ? /^\/teams\/([^/]+)\/threads\/([^/?]+)/.exec(tab.href ?? "")
        : null;
    return match?.[1] && match[2]
      ? [{ tab, channelId: match[1], threadId: match[2] }]
      : [];
  });
  const cachedThreads = useQueries({
    queries: teamThreadTabs.map(({ channelId, threadId }) => ({
      ...getChannelThreadOptions({ path: { channelId, threadId } }),
      enabled: false,
    })),
  });
  const presentedTabs = appScenes.snapshot.tabs.map((tab) => {
    const index = chatRouteTabs.findIndex((item) => item.tab.id === tab.id);
    const chat = index >= 0 ? cachedChats[index]?.data?.chat : undefined;
    const threadIndex = teamThreadTabs.findIndex(
      (item) => item.tab.id === tab.id,
    );
    const thread =
      threadIndex >= 0 ? cachedThreads[threadIndex]?.data?.thread : undefined;
    return chat
      ? {
          ...tab,
          label: chat.title?.trim() || tab.label,
          chatOwner: chat.wakerId
            ? { kind: "waker" as const, id: chat.wakerId }
            : tab.chatOwner,
        }
      : thread?.title?.trim()
        ? { ...tab, label: thread.title.trim() }
        : tab;
  });
  const activeRouteScene = presentedTabs.find(
    (tab) => tab.id === activeRouteSceneId,
  );
  const activeChatOwner =
    isChat && currentChatQuery.data?.chat?.wakerId
      ? { kind: "waker" as const, id: currentChatQuery.data.chat.wakerId }
      : chatOwnerForScene(activeRouteScene);
  const ownerTabs = chatScenesForOwner(presentedTabs, activeChatOwner);
  const memberTabs =
    activeChatOwner?.kind === "team"
      ? appScenes.snapshot.tabs.filter(
          (tab) =>
            tab.kind === "workspace" && tab.ownerTabId === activeRouteSceneId,
        )
      : [];
  const pageBack =
    settingsOpen && searchParams.has("connection")
      ? () => {
          const next = new URLSearchParams(searchParams);
          next.delete("connection");
          next.delete("binding");
          navigate(`${location.pathname}${next.size ? `?${next}` : ""}`, {
            flushSync: true,
          });
        }
      : undefined;
  const cache = queryClient.getQueryCache();
  const subscribeCache = useCallback(
    (listener: () => void) => cache.subscribe(listener),
    [cache],
  );
  const missingQueries = useSyncExternalStore(subscribeCache, () =>
    cache
      .getAll()
      .filter(
        (query) =>
          query.state.error instanceof ApiError &&
          query.state.error.status === 404,
      )
      .map((query) => query.queryHash)
      .join("\n"),
  );
  const navigationHistory = useAppNavigationHistory({
    owner: isChatSurface ? activeChatOwner : undefined,
    // 历史记录已访问实体；分页缺席或其某个Chat删除不能证明实体失效。
    available: (owner) =>
      !cache
        .findAll({
          queryKey:
            owner.kind === "waker"
              ? getWakerQueryKey({ path: { wakerId: owner.id } })
              : getChannelQueryKey({ path: { channelId: owner.id } }),
        })
        .some((query) => missingQueries.split("\n").includes(query.queryHash)),
    onSelect: (owner) =>
      owner.kind === "waker" ? selectPartner(owner.id) : selectTeam(owner.id),
    onPageBack: pageBack,
  });
  const newOwnerChat = () => {
    if (appScenes.openNewChat?.(activeChatOwner)) return;
    if (!activeChatOwner) return;
    setChatRightPanel(undefined);
    navigate(
      activeChatOwner.kind === "team"
        ? `/teams/${activeChatOwner.id}?draftId=${crypto.randomUUID()}`
        : chatDraftHref(activeChatOwner.id),
      { flushSync: true },
    );
  };
  const sceneActions = (
    <>
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
          size="icon"
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
          sidebarToolbar={
            <AppNavigationHistoryControls {...navigationHistory} />
          }
          renderTitlebar={() =>
            isChatSurface && hasActiveScene ? (
              <SceneTabStrip
                activeTabId={appScenes.snapshot.activeTabId ?? ""}
                ariaLabel={t("chat:workspace.content")}
                tabs={[
                  ...(ownerTabs.length
                    ? ownerTabs
                    : activeRouteScene
                      ? [activeRouteScene]
                      : []),
                  ...memberTabs,
                ].map((tab) => ({
                  ...tab,
                  separatorBefore: tab.id === memberTabs[0]?.id,
                  label: tab.customLabel ?? tab.label,
                }))}
                onSelect={(tabId) =>
                  appScenes.dispatch({ type: "select", tabId })
                }
                onClose={(tabId) =>
                  appScenes.dispatch({ type: "close", tabId })
                }
                onRename={(tabId, label) =>
                  appScenes.dispatch({ type: "rename-tab", tabId, label })
                }
                onCloseOthers={(tabId) =>
                  appScenes.dispatch({
                    type: "close-others",
                    tabId,
                    candidateTabIds: (memberTabs.some((tab) => tab.id === tabId)
                      ? memberTabs
                      : ownerTabs
                    ).map((tab) => tab.id),
                  })
                }
                trailing={
                  <Button
                    aria-label={t("common:partnerNavigation.newChat")}
                    onClick={newOwnerChat}
                    disabled={!activeChatOwner}
                    size="icon"
                    title={t("common:partnerNavigation.newChat")}
                    type="button"
                    variant="ghost"
                  >
                    <Plus />
                  </Button>
                }
              />
            ) : !hasActiveScene ||
              pageOwnsSceneHeader(location.pathname) ? null : (
              <AppTitlebar
                breadcrumbs={
                  <span aria-current="page">
                    {isChatSurface
                      ? routeSceneLabels.chat
                      : routeSceneForLocation(
                          {
                            pathname: location.pathname,
                            search: location.search,
                          },
                          routeSceneLabels,
                        ).label}
                  </span>
                }
              />
            )
          }
          notificationBell={
            <NotificationBell
              active={workBoardActive}
              onOpen={openWorkBoard}
              connected={notificationsConnected}
            />
          }
          activeChatId={currentChat?.chatId}
          contextSidebarHidden={!hasActiveScene || !isWakerDetail}
          entityStatus={entityStatus}
          onManageEntities={(mode) =>
            navigate(mode === "team" ? "/wakers?tab=teams" : "/wakers")
          }
          onOpenPartnerDetail={openPartnerDetail}
          onNavigateModule={navigateModule}
          moduleSidebar={
            navigation.sidebarKind === "resources" ? (
              <ResourcesContextSidebar
                active={hasActiveScene && !settingsOpen}
                currentPath={location.pathname}
                category={resourceCategoryFromParams(
                  location.pathname === "/resources"
                    ? searchParams
                    : new URLSearchParams(navigation.parentHref.split("?")[1]),
                )}
                onCategoryChange={(category) => {
                  const next = changeResourceCategory(
                    new URLSearchParams(
                      resourceLibraryHref(location).split("?")[1],
                    ),
                    category,
                  );
                  navigate(`/resources?${next.toString()}`, {
                    flushSync: true,
                  });
                }}
                onPluginSelect={(href) =>
                  navigate(href, {
                    state: { resourceBack: resourceLibraryHref(location) },
                    flushSync: true,
                  })
                }
              />
            ) : undefined
          }
          onPreferencesChange={setPreferences}
          onSelectPartner={selectPartner}
          onSelectTeam={selectTeam}
          preferences={preferences}
          sceneContentFlush={
            (isChatSurface ||
              /^\/(?:knowledge|resources|usage|work-board|workflows|im|automatic-tasks)(?:\/|$)/.test(
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
                        locationState={tab.navigationState}
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
                  showSceneChrome
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
