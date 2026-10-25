import { TooltipProvider } from "@/shared/ui/tooltip";
import { configurePlatform, platform } from "@/shared/platform/provider";
import type { ApplicationUpdateState } from "@neuxbot/platform-contracts/application-update";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect, type ReactNode } from "react";
import {
  MemoryRouter,
  Outlet,
  createMemoryRouter,
  Route,
  RouterProvider,
  Routes,
  useLocation,
  useNavigate,
  useOutletContext,
} from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { useAppSceneWorkspace } from "@/shared/workspace/app-scene-workspace";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";

import { AppShell } from "./app-shell";
import { TeamThreadPage } from "../pages/team-thread-page";
import type { AppRouteSceneProps } from "../scenes/app-route-scene";

const teamHistoryRender = vi.hoisted(() => vi.fn());
const railActivityFixture = vi.hoisted(() => ({
  team: "running" as "idle" | "running" | "waiting",
  waker: "running" as "idle" | "running" | "waiting",
}));
const sceneLifecycle = vi.hoisted(() => ({
  mounts: 0,
  unmounts: 0,
}));

vi.mock("@/modules/team/pages/team-thread-page", () => ({
  TeamThreadPage: ({ renderChat }: { renderChat: () => ReactNode }) => {
    teamHistoryRender();
    return renderChat();
  },
}));

const globalListeners = vi.hoisted(
  () => new Map<string, Set<GlobalEventObserver>>(),
);
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: (
    topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    const listeners = globalListeners.get(topic) ?? new Set();
    listeners.add(observer);
    globalListeners.set(topic, listeners);
    return () => listeners.delete(observer);
  },
}));
vi.mock("@/modules/notification", () => ({
  NotificationNavigationProvider: ({ children }: { children: ReactNode }) =>
    children,
  NotificationPresenter: () => null,
  NotificationBell: ({
    active,
    onOpen,
  }: {
    active?: boolean;
    onOpen: () => void;
  }) => (
    <button
      aria-label="通知，打开任务看板"
      aria-current={active ? "page" : undefined}
      onClick={onOpen}
    />
  ),
}));
vi.mock("@/modules/chat/pages/team-chat-page", () => ({
  TeamChatPageContent: () => <div>团队历史内容</div>,
}));

vi.mock("@/modules/im", () => ({
  IMPage: () => <h1>IM 平台列表</h1>,
  IMConnectionDetailDialog: ({
    connectionId,
    initialBindingId,
    onClose,
  }: {
    connectionId: string;
    initialBindingId?: string;
    onClose: () => void;
  }) => (
    <div role="dialog" aria-label="IM 连接详情">
      <p>
        {connectionId}/{initialBindingId}
      </p>
      <button onClick={onClose}>关闭连接</button>
    </div>
  ),
}));

const storage = new Map<string, string>();

vi.mock("@/modules/chat", () => ({
  chatListInfiniteOptions: (wakerId: string) => ({
    getNextPageParam: () => undefined,
    initialPageParam: undefined,
    queryFn: async () => ({
      resources: [
        {
          id: `${wakerId}-latest`,
          title: `${wakerId} 最新会话`,
          wakerId,
        },
      ],
    }),
    queryKey: ["chats", "list", wakerId],
  }),
  getChatOptions: ({ path }: { path: { chatId: string } }) => ({
    queryKey: ["chat", path.chatId],
    queryFn: async () => ({
      chat: {
        id: path.chatId,
        title: `会话 ${path.chatId}`,
        wakerId: "waker-1",
      },
    }),
  }),
}));

vi.mock("@/shared/ui/sonner", () => ({
  Toaster: ({ position }: { position?: string }) => (
    <div aria-label="通知容器" data-position={position} />
  ),
}));

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ mode: "system", setMode: vi.fn() }),
}));

vi.mock("@/modules/waker", () => ({
  listWakersOptions: () => ({
    queryKey: ["wakers"],
    queryFn: async () => ({
      resources: [
        {
          activityState: railActivityFixture.waker,
          id: "waker-1",
          name: "伙伴一",
          roleName: "项目管理员",
        },
        { id: "waker-2", name: "伙伴二" },
      ],
    }),
  }),
}));

vi.mock("@/modules/team/api/channel-queries", () => ({
  getChannelOptions: ({ path }: { path: { channelId: string } }) => ({
    queryKey: ["channel", path.channelId],
    queryFn: async () => ({
      channel: {
        id: path.channelId,
        leadWakerId: "waker-1",
        name: "团队一",
      },
      members: [
        { waker: { id: "waker-1", name: "伙伴一" } },
        { waker: { id: "waker-2", name: "伙伴二" } },
      ],
    }),
  }),
  getChannelThreadOptions: ({
    path,
  }: {
    path: { channelId: string; threadId: string };
  }) => ({
    queryKey: ["channel-thread", path.channelId, path.threadId],
    queryFn: async () => ({
      members: [
        {
          chatId: "lead-chat",
          displayName: "伙伴一",
          isLead: true,
          position: 0,
          wakerId: "waker-1",
        },
        {
          chatId: "member-chat",
          displayName: "伙伴二",
          position: 1,
          wakerId: "waker-2",
        },
      ],
      thread: {
        id: path.threadId,
        leadChatId: "lead-chat",
        leadWakerId: "waker-1",
      },
    }),
  }),
  teamChannelsInfiniteOptions: () => ({
    getNextPageParam: () => undefined,
    initialPageParam: 1,
    queryFn: async () => ({
      resources: [
        {
          activityState: railActivityFixture.team,
          channel: { id: "team-1", name: "团队一" },
          members: [],
        },
        {
          activityState: "idle",
          channel: { id: "team-2", name: "团队二" },
          members: [],
        },
      ],
    }),
    queryKey: ["channels"],
  }),
  teamThreadsInfiniteOptions: (channelId: string) => ({
    getNextPageParam: () => undefined,
    initialPageParam: 1,
    queryFn: async () => ({
      resources: [
        {
          id: `${channelId}-latest`,
          title: `${channelId} 最新会话`,
        },
      ],
    }),
    queryKey: ["channel-threads", channelId],
  }),
}));

vi.mock("./partner-navigation-layout", async () => {
  const { SceneChromeHost, SceneChromePortalProvider } =
    await import("@/shared/ui/scene-chrome-portal");
  return {
    PartnerNavigationLayout: ({
      children,
      notificationBell,
      preferences,
      settingsOpen,
      settingsSidebar,
      onManageEntities,
      onOpenPartnerDetail,
      onOpenSettings,
      onSelectPartner,
      onSelectTeam,
      selectedPartnerId,
      channels,
      wakers,
    }: {
      channels: Array<{
        activityState?: "idle" | "running" | "waiting";
        channel?: { id?: string; name?: string };
      }>;
      children: ReactNode;
      notificationBell?: ReactNode;
      preferences: { globalSidebarCollapsed: boolean };
      settingsOpen: boolean;
      settingsSidebar?: ReactNode;
      onManageEntities: (mode: "team" | "waker") => void;
      onOpenPartnerDetail: (partnerId: string) => void;
      onOpenSettings: () => void;
      onSelectPartner: (partnerId: string) => void;
      onSelectTeam: (channelId: string) => void;
      selectedPartnerId: string;
      wakers: Array<{
        activityState?: "idle" | "running" | "waiting";
        id: string;
        name: string;
      }>;
    }) => (
      <SceneChromePortalProvider>
        <SceneChromeHost />
        {preferences.globalSidebarCollapsed && !settingsOpen
          ? null
          : notificationBell}
        <div>
          <nav aria-label="伙伴头像栏">
            {wakers.map((waker) => (
              <button
                key={waker.id}
                type="button"
                aria-current={
                  waker.id === selectedPartnerId ? "page" : undefined
                }
                data-activity-state={waker.activityState ?? "idle"}
                onDoubleClick={() => onSelectPartner(waker.id)}
              >
                {waker.name}
              </button>
            ))}
            {wakers.map((waker) => (
              <button
                key={`manage:${waker.id}`}
                type="button"
                onClick={() => onOpenPartnerDetail(waker.id)}
              >
                管理{waker.name}
              </button>
            ))}
            {channels.map((channel) => (
              <button
                key={channel.channel?.id}
                type="button"
                data-activity-state={channel.activityState ?? "idle"}
                onClick={() => onSelectTeam(channel.channel?.id ?? "")}
              >
                {channel.channel?.name}
              </button>
            ))}
            <button type="button" onClick={() => onManageEntities("waker")}>
              伙伴管理
            </button>
            <button type="button" onClick={onOpenSettings}>
              设置
            </button>
          </nav>
          {settingsSidebar}
          {children}
        </div>
      </SceneChromePortalProvider>
    ),
  };
});

function LocationView() {
  const location = useLocation();
  const navigate = useNavigate();
  const outlet = useOutletContext<{
    chatRightPanel?: string;
    setChatRightPanel: (panel?: "workspace") => void;
  }>();
  const teamThread = /^\/teams\/([^/]+)\/threads\/([^/]+)$/.exec(
    location.pathname,
  );
  const workspaceScopeKey = teamThread
    ? `team:${teamThread[1]}:${teamThread[2]}`
    : "app-shell-test";
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const activeTab = workspace.snapshot.tabs.find(
    (tab) => tab.id === workspace.snapshot.activeTabId,
  );
  return (
    <div>
      {`${location.pathname}${location.search}`}
      <span data-testid="right-panel-state">
        {outlet.chatRightPanel ?? "closed"}
      </span>
      <span data-testid="active-workspace-tab">
        {activeTab?.kind ?? "chat"}
      </span>
      <button type="button" onClick={() => navigate("/wakers/waker-1/memory")}>
        前往记忆
      </button>
      <button type="button" onClick={() => navigate("/wakers/waker-1/skills")}>
        前往技能
      </button>
      <button
        type="button"
        onClick={() => navigate("/chats/chat-b?wakerId=waker-1")}
      >
        前往会话 B
      </button>
    </div>
  );
}

function ChatLifecycleView() {
  const navigate = useNavigate();
  const { routeSceneId } = useAppSceneWorkspace();
  useEffect(() => {
    sceneLifecycle.mounts += 1;
    return () => {
      sceneLifecycle.unmounts += 1;
    };
  }, []);
  return (
    <div>
      <span>保留中的 Chat Scene</span>
      <span data-testid="route-scene-scope">{routeSceneId}</span>
      <button type="button" onClick={() => navigate("/wakers/waker-1/memory")}>
        打开资源 Scene
      </button>
    </div>
  );
}

function ResourceLifecycleView() {
  const { routeSceneId } = useAppSceneWorkspace();
  return (
    <div>
      资源 Scene
      <span data-testid="route-scene-scope">{routeSceneId}</span>
    </div>
  );
}

function activeRouteScene() {
  const scene = document.querySelector<HTMLElement>(
    '[data-route-scene-active="true"]',
  );
  if (!scene) throw new Error("未找到激活的 Route Scene");
  return scene;
}

function TestRouteScene({ location, outletContext }: AppRouteSceneProps) {
  return (
    <Routes location={location}>
      <Route element={<Outlet context={outletContext} />}>
        <Route path="*" element={<LocationView />} />
      </Route>
    </Routes>
  );
}

function TestTeamRouteScene({ location, outletContext }: AppRouteSceneProps) {
  return (
    <Routes location={location}>
      <Route element={<Outlet context={outletContext} />}>
        <Route path="teams/:channelId" element={<TeamThreadPage />}>
          <Route index element={<></>} />
          <Route path="threads/:threadId" element={<></>} />
        </Route>
        <Route path="*" element={<LocationView />} />
      </Route>
    </Routes>
  );
}

function TestLifecycleRouteScene({
  location,
  outletContext,
}: AppRouteSceneProps) {
  return (
    <Routes location={location}>
      <Route element={<Outlet context={outletContext} />}>
        <Route path="chats/:chatId" element={<ChatLifecycleView />} />
        <Route
          path="wakers/:wakerId/memory"
          element={<ResourceLifecycleView />}
        />
      </Route>
    </Routes>
  );
}

function renderShell(
  initialEntry = "/chats/chat-a?wakerId=waker-1",
  withTeamPage = false,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  if (withTeamPage) {
    const router = createMemoryRouter(
      [
        {
          path: "*",
          element: <AppShell routeScene={TestTeamRouteScene} />,
        },
      ],
      { initialEntries: [initialEntry] },
    );
    return render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>,
    );
  }

  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <MemoryRouter initialEntries={[initialEntry]}>
          <Routes>
            <Route
              path="*"
              element={<AppShell routeScene={TestRouteScene} />}
            />
          </Routes>
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

function renderLifecycleShell() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createMemoryRouter(
    [
      {
        path: "*",
        element: <AppShell routeScene={TestLifecycleRouteScene} />,
      },
    ],
    { initialEntries: ["/chats/chat-a?wakerId=waker-1"] },
  );
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("AppShell 伙伴导航", () => {
  beforeEach(() => {
    vi.useRealTimers();
    storage.clear();
    teamHistoryRender.mockClear();
    railActivityFixture.team = "running";
    railActivityFixture.waker = "running";
    sceneLifecycle.mounts = 0;
    sceneLifecycle.unmounts = 0;
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: {
        getItem: (key: string) => storage.get(key) ?? null,
        removeItem: (key: string) => storage.delete(key),
        setItem: (key: string, value: string) => storage.set(key, value),
      },
    });
  });

  it("伙伴和团队活动状态由统一资源事件刷新", async () => {
    renderShell();

    expect(
      await screen.findByRole("button", { name: "伙伴一" }),
    ).toHaveAttribute("data-activity-state", "running");
    expect(screen.getByRole("button", { name: "团队一" })).toHaveAttribute(
      "data-activity-state",
      "running",
    );

    railActivityFixture.team = "idle";
    railActivityFixture.waker = "idle";
    act(() => {
      for (const observer of globalListeners.get("entities") ?? [])
        observer.onEvent({
          protocolVersion: 1,
          epoch: "epoch",
          connectionId: "connection",
          deliverySequence: 1,
          topic: "entities",
          type: "entities.changed",
          resource: { kind: "global" },
          eventId: "event",
          payload: {},
        });
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "伙伴一" })).toHaveAttribute(
        "data-activity-state",
        "idle",
      );
      expect(screen.getByRole("button", { name: "团队一" })).toHaveAttribute(
        "data-activity-state",
        "idle",
      );
    });
  });

  it("伙伴详情从列表行菜单打开，不再占用右上角入口", async () => {
    const user = userEvent.setup();
    renderShell("/chat?wakerId=waker-1");

    expect(
      screen.queryByRole("button", { name: "打开伙伴一详情" }),
    ).not.toBeInTheDocument();

    await user.click(await screen.findByRole("button", { name: "管理伙伴一" }));

    expect(screen.getByText("/wakers/waker-1")).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "详情" })).toBeVisible();
  });

  it("团队顶栏不再单独显示成员入口", async () => {
    renderShell("/teams/team-1/threads/thread-1");

    expect(screen.queryByRole("button", { name: /打开伙伴.*详情/ })).toBeNull();
    expect(await screen.findByRole("button", { name: "工作区" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "查看团队成员" })).toBeNull();
  });

  it("聊天页顶栏不再显示旧添加内容入口", async () => {
    renderShell();

    expect(await screen.findByRole("button", { name: "工作区" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "添加内容" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "任务列表" }),
    ).not.toBeInTheDocument();
  });

  it.each([
    ["/chats/chat-a?wakerId=waker-2", "/chat", "waker-2"],
    ["/teams/team-1/threads/thread-1", "/teams/team-1", undefined],
  ])(
    "聊天 Tab 后的加号为当前身份打开独立新会话：%s",
    async (entry, pathname, wakerId) => {
      const user = userEvent.setup();
      renderShell(entry);

      const trailing = document.querySelector<HTMLElement>(
        "[data-scene-tab-trailing]",
      );
      expect(trailing).not.toBeNull();
      if (!trailing) throw new Error("未找到聊天 Tab 的新会话入口");
      await user.click(
        within(trailing).getByRole("button", { name: "新建会话" }),
      );

      const firstDraft = new URL(
        within(activeRouteScene()).getByText(new RegExp(`^${pathname}\\?`))
          .firstChild?.textContent ?? "",
        "https://ally.test",
      );
      expect(firstDraft.pathname).toBe(pathname);
      expect(firstDraft.searchParams.get("wakerId")).toBe(wakerId ?? null);
      expect(firstDraft.searchParams.get("draftId")).toBeTruthy();
      expect(screen.getByText(entry)).not.toBeVisible();

      await user.click(screen.getByRole("button", { name: "新建会话" }));
      const nextDraft = new URL(
        within(activeRouteScene()).getByText(new RegExp(`^${pathname}\\?`))
          .firstChild?.textContent ?? "",
        "https://ally.test",
      );
      expect(nextDraft.searchParams.get("draftId")).not.toBe(
        firstDraft.searchParams.get("draftId"),
      );
    },
  );

  it("资源 Scene 不显示聊天的新会话加号", () => {
    renderShell("/wakers/waker-1/memory");
    expect(screen.queryByRole("button", { name: "新建会话" })).toBeNull();
  });

  it("点击左侧伙伴时只打开该伙伴的最新会话 Scene", async () => {
    const user = userEvent.setup();
    renderShell("/wakers/waker-1/projects/project-a");

    await user.dblClick(await screen.findByRole("button", { name: "伙伴二" }));

    expect(
      await screen.findByText("/chats/waker-2-latest?wakerId=waker-2"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "新会话" })).toBeNull();
    expect(
      await screen.findByRole("tab", { name: "会话 waker-2-latest" }),
    ).toHaveAttribute("aria-selected", "true");
  });

  it("从临时会话点击伙伴时用最新会话替换临时 Scene", async () => {
    const user = userEvent.setup();
    renderShell("/chat?draftId=draft-a&wakerId=waker-1");

    expect(await screen.findByRole("tab", { name: "新会话" })).toBeVisible();
    await user.dblClick(await screen.findByRole("button", { name: "伙伴一" }));

    expect(
      await screen.findByText("/chats/waker-1-latest?wakerId=waker-1"),
    ).toBeVisible();
    expect(screen.queryByRole("tab", { name: "新会话" })).toBeNull();
    expect(screen.getAllByRole("tab")).toHaveLength(1);
    expect(
      screen.getByRole("tab", { name: "会话 waker-1-latest" }),
    ).toHaveAttribute("aria-selected", "true");
  });

  it("团队主区只保留会话和工作区入口", async () => {
    const user = userEvent.setup();
    renderShell("/teams/team-1/threads/thread-1");

    expect(await screen.findByRole("button", { name: "工作区" })).toBeVisible();
    expect(screen.getByRole("button", { name: "工作区" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "任务" }),
    ).not.toBeInTheDocument();
    expect(
      within(activeRouteScene()).getByTestId("right-panel-state"),
    ).toHaveTextContent("closed");

    await user.click(screen.getByRole("button", { name: "工作区" }));
    expect(screen.getByTestId("right-panel-state")).toHaveTextContent(
      "workspace",
    );
    expect(screen.getByTestId("active-workspace-tab")).toHaveTextContent(
      "chat",
    );
    expect(screen.queryByRole("button", { name: "查看团队成员" })).toBeNull();
    expect(screen.queryByRole("button", { name: "添加内容" })).toBeNull();
  });

  it("点击团队打开最新会话，快速切换不会被先前团队带回", async () => {
    storage.set(
      "ally.partner-navigation",
      JSON.stringify({
        currentPartnerId: "waker-1",
        currentRailKey: "waker:waker-1",
        lastTeamThreads: {
          "team-1": "thread-remembered-1",
          "team-2": "thread-remembered-2",
        },
      }),
    );
    const user = userEvent.setup();
    renderShell();

    await user.click(await screen.findByRole("button", { name: "团队一" }));
    expect(
      await screen.findByText("/teams/team-1/threads/team-1-latest"),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "团队二" }));

    expect(
      await screen.findByText("/teams/team-2/threads/team-2-latest"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("/teams/team-1/threads/team-1-latest"),
    ).not.toBeVisible();
  });

  it.each([undefined, "thread-remembered"])(
    "点击团队解析最新会话后进入历史内容：%s",
    async (threadId) => {
      storage.set(
        "ally.partner-navigation",
        JSON.stringify({ lastTeamThreads: { "team-1": threadId } }),
      );
      renderShell("/chat?wakerId=waker-1", true);
      const teamButton = await screen.findByRole("button", { name: "团队一" });

      act(() => teamButton.click());
      expect(teamHistoryRender).not.toHaveBeenCalled();

      expect(await screen.findByText("团队历史内容")).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "任务" }),
      ).not.toBeInTheDocument();
      expect(screen.getByRole("tab", { name: "团队会话" })).toBeVisible();
    },
  );

  it("团队内容就绪前切回伙伴，不再挂载已经离开的团队", async () => {
    renderShell("/chat?wakerId=waker-1", true);
    const partnerButton = await screen.findByRole("button", { name: "伙伴二" });

    act(() => {
      screen.getByRole("button", { name: "团队一" }).click();
      fireEvent.dblClick(partnerButton);
    });

    expect(teamHistoryRender).not.toHaveBeenCalled();
    expect(screen.queryByText("团队历史内容")).not.toBeInTheDocument();
    expect(
      await screen.findByText("/chats/waker-2-latest?wakerId=waker-2"),
    ).toBeVisible();
  });

  it("伙伴管理页面不展示添加内容入口", async () => {
    renderShell("/wakers/waker-1");

    expect(await screen.findByRole("button", { name: "伙伴一" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "添加内容" }),
    ).not.toBeInTheDocument();
  });

  it("旧 IM 深链打开设置内详情，关闭唯一设置后回到首页", async () => {
    const user = userEvent.setup();
    renderShell("/im/connection-one?chat=binding-one");
    expect(
      await screen.findByRole("heading", { name: "IM 平台列表" }),
    ).toBeVisible();
    expect(
      screen.getByRole("dialog", { name: "IM 连接详情" }),
    ).toHaveTextContent("connection-one/binding-one");
    expect(screen.getByRole("button", { name: "IM 集成" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getAllByRole("tab")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "关闭连接" }));
    await user.click(screen.getByRole("button", { name: "关闭 设置" }));
    expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull();
    expect(await screen.findByText("/", { exact: true })).toBeVisible();
  });

  it("发布集成中切换 IM 分类后不会重开已关闭的深链详情", async () => {
    const user = userEvent.setup();
    renderShell("/im/connection-one?chat=binding-one");
    await screen.findByRole("dialog", { name: "IM 连接详情" });
    await user.click(screen.getByRole("button", { name: "关闭连接" }));
    await user.click(screen.getByRole("button", { name: "通用" }));
    expect(screen.queryByRole("heading", { name: "IM 平台列表" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "IM 集成" }));
    expect(screen.getByRole("heading", { name: "IM 平台列表" })).toBeVisible();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getAllByRole("tab")).toHaveLength(1);
  });

  it("设置入口打开唯一 Scene，切到会话后保留设置 Scene", async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(await screen.findByRole("button", { name: "设置" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "设置 Scene" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("/chats/chat-a?wakerId=waker-1"),
    ).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "会话 chat-a" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
    await user.click(screen.getByRole("tab", { name: "会话 chat-a" }));
    expect(screen.getByRole("tab", { name: "设置" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
    expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull();

    await user.click(screen.getByRole("tab", { name: "设置" }));
    expect(
      screen.getByRole("region", { name: "设置 Scene" }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "关闭 设置" }));

    expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull();
    expect(
      screen.getByText("/chats/chat-a?wakerId=waker-1"),
    ).toBeInTheDocument();
  });

  it("从 IM 深链切离后，设置入口恢复已有分类且不恢复临时详情", async () => {
    const user = userEvent.setup();
    renderShell("/im/connection-one?chat=binding-one");
    await user.click(await screen.findByRole("button", { name: "关闭连接" }));
    await user.click(screen.getByRole("button", { name: "管理伙伴一" }));
    expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(
      await screen.findByRole("heading", { name: "IM 平台列表" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "IM 集成" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getAllByRole("tab", { name: "设置" })).toHaveLength(1);
  });

  it.each([false, true])(
    "设置隐藏铃铛，返回会话后恢复唯一看板入口（侧栏收起=%s）",
    async (globalSidebarCollapsed) => {
      const user = userEvent.setup();
      storage.set(
        "ally.partner-navigation",
        JSON.stringify({ currentPartnerId: "waker-1", globalSidebarCollapsed }),
      );
      renderShell();
      await user.click(await screen.findByRole("button", { name: "设置" }));
      expect(screen.getByRole("region", { name: "设置 Scene" })).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "通知，打开任务看板" }),
      ).toBeNull();
      await user.click(screen.getByRole("tab", { name: "会话 chat-a" }));
      expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull();
      const bell = await screen.findByRole("button", {
        name: "通知，打开任务看板",
      });
      await user.click(bell);
      expect(
        await screen.findByRole("tab", { name: "任务看板" }),
      ).toHaveAttribute("aria-selected", "true");
      expect(activeRouteScene()).toHaveTextContent("/work-board");
      expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull();
      const activeBell = screen.getByRole("button", {
        name: "通知，打开任务看板",
      });
      expect(activeBell).toHaveAttribute("aria-current", "page");
      await user.click(activeBell);
      expect(screen.getAllByRole("tab", { name: "任务看板" })).toHaveLength(1);
    },
  );

  it("不同会话使用真实标题并在顶部 SceneBar 中累积", async () => {
    const user = userEvent.setup();
    renderShell();

    expect(
      await screen.findByRole("tab", { name: "会话 chat-a" }),
    ).toHaveAttribute("aria-selected", "true");

    await user.click(screen.getByRole("button", { name: "前往会话 B" }));

    expect(screen.getByRole("tab", { name: "会话 chat-a" })).toBeVisible();
    expect(screen.getByRole("tab", { name: "会话 chat-b" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    await user.click(screen.getByRole("tab", { name: "会话 chat-a" }));
    expect(screen.getByText("/chats/chat-a?wakerId=waker-1")).toBeVisible();
  });

  it("切换到资源 Scene 后保持 Chat Scene 挂载", async () => {
    const user = userEvent.setup();
    renderLifecycleShell();

    expect(await screen.findByText("保留中的 Chat Scene")).toBeVisible();
    expect(sceneLifecycle).toEqual({ mounts: 1, unmounts: 0 });

    await user.click(screen.getByRole("button", { name: "打开资源 Scene" }));

    expect(await screen.findByText("资源 Scene")).toBeVisible();
    expect(sceneLifecycle).toEqual({ mounts: 1, unmounts: 0 });
    expect(
      screen
        .getAllByTestId("route-scene-scope")
        .map((node) => node.textContent),
    ).toEqual(["chat:chat-a", "waker:waker-1:resources"]);

    await user.click(screen.getByRole("tab", { name: "会话 chat-a" }));
    expect(await screen.findByText("保留中的 Chat Scene")).toBeVisible();
    expect(sceneLifecycle).toEqual({ mounts: 1, unmounts: 0 });
  });

  it("关闭最后一个 Scene 后进入无标签欢迎页", async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(
      await screen.findByRole("button", { name: "关闭 会话 chat-a" }),
    );

    expect(screen.queryAllByRole("tab")).toHaveLength(0);
    expect(screen.getByRole("region", { name: "欢迎页" })).toBeVisible();
    expect(
      screen.queryByText("/chats/chat-a?wakerId=waker-1"),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "添加内容" })).toBeNull();
    expect(screen.queryByRole("button", { name: "工作区" })).toBeNull();
  });

  it("依次关闭多个 Scene 后不再渲染最后关闭的页面", async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(await screen.findByRole("button", { name: "前往会话 B" }));
    await user.click(screen.getByRole("button", { name: "关闭 会话 chat-b" }));
    await user.click(screen.getByRole("button", { name: "关闭 会话 chat-a" }));

    expect(screen.queryAllByRole("tab")).toHaveLength(0);
    expect(screen.getByRole("region", { name: "欢迎页" })).toBeVisible();
    expect(screen.queryByText(/\/chats\/chat-[ab]/)).toBeNull();
  });

  it("同一伙伴的资源页面复用一个 Scene", async () => {
    const user = userEvent.setup();
    renderShell("/wakers/waker-1/memory");

    expect(await screen.findByRole("tab", { name: "详情" })).toBeVisible();

    await user.click(screen.getByRole("button", { name: "前往技能" }));

    expect(screen.getByRole("tab", { name: "详情" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getAllByRole("tab", { name: "详情" })).toHaveLength(1);
  });

  it("主内容标题栏不重复展示伙伴及菜单信息", async () => {
    storage.set(
      "ally.partner-navigation",
      JSON.stringify({
        currentPartnerId: "waker-1",
        sidebarCollapsed: true,
      }),
    );
    renderShell("/wakers/waker-1/memory");

    expect(await screen.findAllByText("伙伴一")).toHaveLength(1);
    expect(screen.getByRole("tab", { name: "详情" })).toBeVisible();
    expect(screen.queryByText("项目管理员")).not.toBeInTheDocument();
  });

  it("把全局通知挂载在右上角", () => {
    renderShell();

    expect(screen.getByLabelText("通知容器")).toHaveAttribute(
      "data-position",
      "top-right",
    );
  });
  it.each([false, true])(
    "更新操作进入关于软件，不恢复旧设置分类（曾打开设置=%s）",
    async (openedSettings) => {
      const state: ApplicationUpdateState = {
        revision: 1,
        currentVersion: "0.1.0",
        platform: "darwin",
        arch: "arm64",
        status: "available",
        downloadedBytes: 0,
        release: {
          version: "0.2.0",
          url: "https://github.com/JieWaZi/NeuxBot/releases/tag/v0.2.0",
          notes: "更新",
          publishedAt: "2026-10-25T00:00:00Z",
          downloadSize: 100,
        },
      };
      configurePlatform({
        ...platform(),
        applicationUpdate: {
          application_update_state: async () => state,
          check_application_update: async () => state,
          download_application_update: async () => state,
          open_application_installer: async () => state,
          onApplicationUpdate: () => () => undefined,
        },
      });
      renderShell();
      if (openedSettings) {
        await userEvent
          .setup()
          .click(await screen.findByRole("button", { name: "设置" }));
        await userEvent
          .setup()
          .click(screen.getByRole("tab", { name: "会话 chat-a" }));
      }
      const actions = await screen.findAllByRole("button", {
        name: "下载新版本 v0.2.0",
      });
      const action = actions[0];
      if (!action) throw new Error("未找到更新入口");
      await userEvent.setup().click(action);
      expect(
        await screen.findByRole("heading", { name: "新版本 v0.2.0" }),
      ).toBeInTheDocument();
    },
  );
});
