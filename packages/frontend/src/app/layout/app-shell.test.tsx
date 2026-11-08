import type { GlobalEventObserver } from "@/shared/api/global-events";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render as renderView,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect, useState, type ReactNode } from "react";
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

import { TooltipProvider } from "@/shared/ui/tooltip";
import type { ReactElement } from "react";
const render = (ui: ReactElement) =>
  renderView(ui, { wrapper: TooltipProvider });

import { AppShell } from "./app-shell";
import { getChatOptions } from "@/modules/chat";
import type * as PluginQueries from "@/modules/plugin/api/plugin-queries";
import { getWakerQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { ApiError } from "@/shared/api/errors";
import type { AppModuleId } from "./app-navigation-model";
import { TeamThreadPage } from "../pages/team-thread-page";
import {
  AppRouteScene as ProductionRouteScene,
  type AppRouteSceneProps,
} from "../scenes/app-route-scene";

function getContent(options: { name: string }) {
  return screen.getByRole("tab", options);
}
function findContent(options: { name: string }) {
  return screen.findByRole("tab", options);
}
function queryContent(options: { name: string }) {
  return screen.queryByRole("tab", options);
}
function allContents(options?: { name: string }) {
  return screen.getAllByRole("tab", options);
}
function queryAllContents() {
  return screen.queryAllByRole("tab");
}
function closeContent(label: string) {
  return screen.getByRole("button", { name: `关闭 ${label}` });
}

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

vi.mock("@/modules/project/api/project-queries", () => ({
  getProjectOptions: () => ({
    queryKey: ["project", "desk"],
    queryFn: async () => ({
      id: "desk",
      name: "Research Desk",
      available: true,
      sources: [],
    }),
  }),
  listProjectsOptions: () => ({
    queryKey: ["project-list"],
    queryFn: async () => ({
      resources: [{ id: "desk", name: "Research Desk", available: true }],
      total: 1,
    }),
  }),
  listProjectsQueryKey: () => ["project-list"],
  deleteProjectMutation: () => ({ mutationFn: async () => ({}) }),
}));
vi.mock("@/modules/project/components/project-form-dialog", () => ({
  ProjectFormDialog: function ProjectFormDialogStateProbe() {
    const location = useLocation();
    return (
      <output data-testid="project-scene-navigation-state">
        {JSON.stringify(location.state)}
      </output>
    );
  },
}));
vi.mock("@/modules/project/components/project-delete-dialog", () => ({
  ProjectDeleteDialog: () => null,
}));
vi.mock("@/modules/plugin/api/plugin-queries", async (importOriginal) => ({
  ...(await importOriginal<typeof PluginQueries>()),
  getPluginSkillOptions: () => ({
    queryKey: ["state-plugin-skill"],
    queryFn: async () => ({
      skill: { key: "guide", name: "操作指南" },
      content: "# 使用指南",
      resources: [],
      version: { id: "pinned" },
    }),
  }),
  getPluginOptions: () => ({
    queryKey: ["state-plugin"],
    queryFn: async () => ({
      plugin: { id: "p", name: "办公" },
      versions: [{ id: "pinned", package: { manifest: { name: "office" } } }],
    }),
  }),
}));

const storage = new Map<string, string>();

const latestChatFixture = vi.hoisted(() => ({
  historyBlocked: false,
  missingThread: false,
  missingChat: false,
  latestChatId: undefined as string | undefined,
  details: new Map<
    string,
    Promise<{ chat: { id: string; wakerId: string; title: string } }>
  >(),
  detailRequests: new Map<string, number>(),
  threadTitles: new Map<string, string>(),
  resolvedEntryOwner: undefined as string | undefined,
}));

vi.mock("@/modules/chat", () => ({
  listChatsOptions: ({
    query,
  }: {
    query: { wakerId: string; limit: number };
  }) => ({
    queryKey: ["chats", "latest", query.wakerId],
    queryFn: async () => {
      if (query.limit !== 1) throw new Error("最近会话只请求一条");
      return {
        resources: [
          {
            id: latestChatFixture.latestChatId ?? `${query.wakerId}-latest`,
            title: "最新会话",
            wakerId: query.wakerId,
          },
        ],
      };
    },
  }),
  chatListInfiniteOptions: (wakerId: string) => ({
    getNextPageParam: () => undefined,
    initialPageParam: undefined,
    queryFn: async () => {
      if (latestChatFixture.historyBlocked) await new Promise(() => {});
      return {
        resources: [
          {
            id: `${wakerId}-latest`,
            title: `${wakerId} 最新会话`,
            wakerId,
          },
        ],
      };
    },
    queryKey: ["chats", "list", wakerId],
  }),
  getChatOptions: ({ path }: { path: { chatId: string } }) => ({
    queryKey: ["chat", path.chatId],
    queryFn: async () => {
      latestChatFixture.detailRequests.set(
        path.chatId,
        (latestChatFixture.detailRequests.get(path.chatId) ?? 0) + 1,
      );
      const delayed = latestChatFixture.details.get(path.chatId);
      if (delayed) return delayed;
      return {
        chat: latestChatFixture.missingChat
          ? undefined
          : {
              id: path.chatId,
              title: `会话 ${path.chatId}`,
              wakerId:
                path.chatId.includes("waker-2") || path.chatId === "bare-b"
                  ? "waker-2"
                  : "waker-1",
            },
      };
    },
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

vi.mock("@/modules/waker", async () => ({
  WakerDetailLayout: (
    await import("@/modules/waker/components/waker-detail-layout")
  ).WakerDetailLayout,
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
      channel:
        path.channelId === "gone"
          ? undefined
          : {
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
      thread: latestChatFixture.missingThread
        ? undefined
        : {
            id: path.threadId,
            title: latestChatFixture.threadTitles.get(
              `${path.channelId}:${path.threadId}`,
            ),
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
  const { AppModuleRail } = await import("./app-module-rail");
  return {
    PartnerNavigationLayout: ({
      children,
      notificationBell,
      renderTitlebar,
      sidebarToolbar,
      preferences,
      settingsOpen,
      settingsSidebar,
      onManageEntities,
      onOpenPartnerDetail,
      onNavigateModule,
      moduleSidebar,
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
      renderTitlebar?: (navigationToggle: ReactNode) => ReactNode;
      sidebarToolbar?: ReactNode;
      preferences: { globalSidebarCollapsed: boolean };
      settingsOpen: boolean;
      settingsSidebar?: ReactNode;
      onManageEntities: (mode: "team" | "waker") => void;
      onOpenPartnerDetail: (partnerId: string) => void;
      onNavigateModule: (id: AppModuleId) => void;
      moduleSidebar?: ReactNode;
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
        <div data-sidebar-history-slot>
          {sidebarToolbar}
          {notificationBell}
        </div>
        <SceneChromeHost>{renderTitlebar?.(null)}</SceneChromeHost>
        <AppModuleRail
          activeModule={settingsOpen ? "settings" : "partners"}
          onNavigate={onNavigateModule}
        />
        <output data-testid="sidebar-collapsed">
          {String(preferences.globalSidebarCollapsed)}
        </output>
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
                onClick={() => onSelectPartner(waker.id)}
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
          </nav>
          {settingsSidebar}
          {moduleSidebar}
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
  const scenes = useAppSceneWorkspace();
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const [fileOpen, setFileOpen] = useState(false);
  const [selectedFile, setSelectedFile] = useState(false);
  const fixtureChannelId = teamThread?.[1];
  const fixtureChatId = /^\/chats\/([^/]+)$/.exec(location.pathname)?.[1];
  const detailQuery = useQuery({
    ...getChatOptions({ path: { chatId: fixtureChatId ?? "pending" } }),
    enabled: Boolean(fixtureChatId),
  });
  const fixtureOwner =
    detailQuery.data?.chat?.wakerId ??
    new URLSearchParams(location.search).get("wakerId") ??
    (!fixtureChatId ? latestChatFixture.resolvedEntryOwner : undefined);
  useEffect(() => {
    if (
      !/^\/(?:chats\/[^/]+|chat)?$/.test(location.pathname) &&
      !fixtureChannelId
    )
      return;
    scenes.syncWorkspace({
      chatOwner: fixtureChannelId
        ? { kind: "team", id: fixtureChannelId }
        : fixtureOwner
          ? { kind: "waker", id: fixtureOwner }
          : undefined,
      ownerTabId: scenes.routeSceneId,
      ownerHref: `${location.pathname}${location.search}`,
      primaryWorkspaceTabId: "primary",
      activeWorkspaceTabId: selectedFile ? "test-file" : "primary",
      onClose: () => {
        setFileOpen(false);
        setSelectedFile(false);
      },
      onSelect: (id) => setSelectedFile(id === "test-file"),
      tabs: [
        { id: "primary", label: "对话" },
        ...(fileOpen
          ? [{ id: "test-file", label: "同会话文件", closeable: true }]
          : []),
      ],
    });
  }, [
    fileOpen,
    selectedFile,
    fixtureOwner,
    location.pathname,
    location.search,
    scenes,
    fixtureChannelId,
  ]);
  const activeTab = workspace.snapshot.tabs.find(
    (tab) => tab.id === workspace.snapshot.activeTabId,
  );
  return (
    <div>
      {`${location.pathname}${location.search}`}
      <button
        type="button"
        onClick={() => {
          setFileOpen(true);
          setSelectedFile(true);
        }}
      >
        打开同会话文件
      </button>
      {fileOpen && !teamThread ? (
        <button
          role="tab"
          aria-selected={selectedFile}
          onClick={() =>
            scenes.dispatch({
              type: "select",
              tabId: `workspace:${scenes.routeSceneId}:test-file`,
            })
          }
        >
          同会话文件
        </button>
      ) : null}
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
  providedQueryClient?: QueryClient,
) {
  const queryClient =
    providedQueryClient ??
    new QueryClient({
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
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
  }

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="*" element={<AppShell routeScene={TestRouteScene} />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function StatefulProductionScene(props: AppRouteSceneProps) {
  if (
    props.location.startsWith("/resources?") ||
    props.location === "/resources"
  )
    return <TestRouteScene {...props} />;
  return <ProductionRouteScene {...props} />;
}
function SceneStateControls() {
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <>
      <output data-testid="state-route">
        {location.pathname}
        {location.search}
      </output>
      <button
        onClick={() =>
          navigate("/resources/plugins/p/skills/guide?version=pinned", {
            state: {
              resourceBack:
                "/resources?category=skills&search=desk&source=plugin",
            },
          })
        }
      >
        打开资源技能详情
      </button>
      <button onClick={() => navigate(-1)}>浏览器返回</button>
    </>
  );
}
function renderStatefulProductionShell() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/wakers/waker-1/projects?search=desk"]}>
        <SceneStateControls />
        <AppShell routeScene={StatefulProductionScene} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function renderLifecycleShell() {
  latestChatFixture.latestChatId = "chat-a";
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
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("AppShell 伙伴导航", () => {
  beforeEach(() => {
    vi.useRealTimers();
    storage.clear();
    latestChatFixture.historyBlocked = false;
    latestChatFixture.missingThread = false;
    latestChatFixture.missingChat = false;
    latestChatFixture.latestChatId = undefined;
    latestChatFixture.details.clear();
    latestChatFixture.detailRequests.clear();
    latestChatFixture.threadTitles.clear();
    latestChatFixture.resolvedEntryOwner = undefined;
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
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴一" }),
    ).toHaveAttribute("data-activity-state", "running");
    expect(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "团队一" },
      ),
    ).toHaveAttribute("data-activity-state", "running");

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
      expect(
        within(
          screen.getByRole("navigation", { name: "伙伴头像栏" }),
        ).getByRole("button", { name: "伙伴一" }),
      ).toHaveAttribute("data-activity-state", "idle");
      expect(
        within(
          screen.getByRole("navigation", { name: "伙伴头像栏" }),
        ).getByRole("button", { name: "团队一" }),
      ).toHaveAttribute("data-activity-state", "idle");
    });
  });

  it("伙伴活动变化只更新伙伴摘要，保留团队列表的缓存", async () => {
    renderShell();
    expect(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴一" }),
    ).toHaveAttribute("data-activity-state", "running");
    expect(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "团队一" },
      ),
    ).toHaveAttribute("data-activity-state", "running");
    railActivityFixture.waker = "idle";
    railActivityFixture.team = "idle";
    act(() => {
      for (const observer of globalListeners.get("entities") ?? [])
        observer.onEvent({
          protocolVersion: 1,
          epoch: "epoch",
          connectionId: "connection",
          deliverySequence: 1,
          topic: "entities",
          type: "entities.activity.changed",
          resource: { kind: "waker", id: "waker-1" },
          eventId: "activity",
          payload: {},
        });
    });
    await waitFor(() =>
      expect(
        within(
          screen.getByRole("navigation", { name: "伙伴头像栏" }),
        ).getByRole("button", { name: "伙伴一" }),
      ).toHaveAttribute("data-activity-state", "idle"),
    );
    expect(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "团队一" },
      ),
    ).toHaveAttribute("data-activity-state", "running");
  });

  it("伙伴详情从列表行菜单打开，不再占用右上角入口", async () => {
    const user = userEvent.setup();
    renderShell("/chat?wakerId=waker-1");

    expect(
      screen.queryByRole("button", { name: "打开伙伴一详情" }),
    ).not.toBeInTheDocument();

    await user.click(await screen.findByRole("button", { name: "管理伙伴一" }));

    expect(screen.getByText("/wakers/waker-1")).toBeInTheDocument();
    expect(activeRouteScene()).toHaveAttribute(
      "data-route-scene-id",
      "waker:waker-1:resources",
    );
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
    ["/chats/waker-2-chat?wakerId=waker-2", "/chat", "waker-2"],
    ["/teams/team-1/threads/thread-1", "/teams/team-1", undefined],
  ])(
    "聊天加号复用未发送的新会话，离开后仍复用：%s",
    async (entry, pathname, wakerId) => {
      const user = userEvent.setup();
      renderShell(entry);
      const savedTab = screen.getByRole("tab", { selected: true });

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
      expect(nextDraft.searchParams.get("draftId")).toBe(
        firstDraft.searchParams.get("draftId"),
      );
      await user.click(savedTab);
      await user.click(screen.getByRole("button", { name: "新建会话" }));
      expect(
        within(activeRouteScene()).getByText(new RegExp(`^${pathname}\\?`)),
      ).toHaveTextContent(firstDraft.searchParams.get("draftId") ?? "missing");
    },
  );

  it.each(["模块入口", "伙伴列表"])(
    "空白草稿经%s切回最近会话后仍保留",
    async (entry) => {
      const user = userEvent.setup();
      renderShell("/chats/waker-2-chat?wakerId=waker-2");
      await user.click(screen.getByRole("button", { name: "新建会话" }));
      const original =
        activeRouteScene().textContent?.match(/draftId=([^&]+)/)?.[1];
      expect(original).toBeTruthy();
      if (entry === "模块入口") {
        await user.click(
          within(
            screen.getByRole("navigation", { name: "模块导航" }),
          ).getByRole("button", { name: "伙伴" }),
        );
      } else {
        await user.click(
          within(
            screen.getByRole("navigation", { name: "伙伴头像栏" }),
          ).getByRole("button", { name: "伙伴二" }),
        );
      }
      await waitFor(() =>
        expect(activeRouteScene()).not.toHaveTextContent(original ?? "missing"),
      );
      await user.click(screen.getByRole("button", { name: "新建会话" }));
      expect(activeRouteScene()).toHaveTextContent(original ?? "missing");
      expect(screen.getAllByRole("tab", { name: "新会话" })).toHaveLength(1);
    },
  );

  it("空白草稿跨模块返回伙伴后仍复用同一个会话", async () => {
    const user = userEvent.setup();
    renderShell("/chats/waker-2-chat?wakerId=waker-2");
    await user.click(screen.getByRole("button", { name: "新建会话" }));
    const original =
      activeRouteScene().textContent?.match(/draftId=([^&]+)/)?.[1];
    expect(original).toBeTruthy();
    await user.click(
      within(activeRouteScene()).getByRole("button", { name: "前往记忆" }),
    );
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴二" },
      ),
    );
    await waitFor(() =>
      expect(activeRouteScene()).toHaveTextContent("/chats/waker-2-latest"),
    );
    await user.click(screen.getByRole("button", { name: "新建会话" }));
    expect(activeRouteScene()).toHaveTextContent(original ?? "missing");
    expect(screen.getAllByRole("tab", { name: "新会话" })).toHaveLength(1);
  });

  it("资源 Scene 不显示聊天的新会话加号", () => {
    renderShell("/wakers/waker-1/memory");
    expect(screen.queryByRole("button", { name: "新建会话" })).toBeNull();
  });

  it("历史列表请求未完成时，单击伙伴独立查询最近一条会话", async () => {
    latestChatFixture.historyBlocked = true;
    const user = userEvent.setup();
    renderShell("/wakers/waker-1/projects/project-a");
    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴二" }),
    );
    expect(
      await screen.findByText("/chats/waker-2-latest?wakerId=waker-2"),
    ).toBeVisible();
  });

  it("最近会话 URL 未变化时，单击伙伴仍切回会话页签", async () => {
    const user = userEvent.setup();
    renderShell("/chats/waker-1-latest?wakerId=waker-1");
    await user.click(
      await screen.findByRole("button", { name: "打开同会话文件" }),
    );
    expect(getContent({ name: "同会话文件" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    await waitFor(() =>
      expect(getContent({ name: "会话 waker-1-latest" })).toHaveAttribute(
        "aria-selected",
        "true",
      ),
    );
    expect(getContent({ name: "同会话文件" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
  });

  it("点击左侧伙伴时只打开该伙伴的最新会话 Scene", async () => {
    const user = userEvent.setup();
    renderShell("/wakers/waker-1/projects/project-a");

    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴二" }),
    );

    expect(
      await screen.findByText("/chats/waker-2-latest?wakerId=waker-2"),
    ).toBeInTheDocument();
    expect(queryContent({ name: "新会话" })).toBeNull();
    expect(await findContent({ name: "会话 waker-2-latest" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("从临时会话点击伙伴时保留草稿并打开最新会话", async () => {
    const user = userEvent.setup();
    renderShell("/chat?draftId=draft-a&wakerId=waker-1");

    expect(await findContent({ name: "新会话" })).toBeVisible();
    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴一" }),
    );

    expect(
      await screen.findByText("/chats/waker-1-latest?wakerId=waker-1"),
    ).toBeVisible();
    expect(queryContent({ name: "新会话" })).toBeVisible();
    expect(allContents()).toHaveLength(2);
    expect(getContent({ name: "会话 waker-1-latest" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
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

    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "团队一" }),
    );
    expect(
      await screen.findByText("/teams/team-1/threads/team-1-latest"),
    ).toBeInTheDocument();
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "团队二" },
      ),
    );

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
      const teamButton = await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "团队一" });

      act(() => teamButton.click());
      expect(teamHistoryRender).not.toHaveBeenCalled();

      expect(await screen.findByText("团队历史内容")).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "任务" }),
      ).not.toBeInTheDocument();
      expect(getContent({ name: "团队会话" })).toBeVisible();
    },
  );

  it("团队内容就绪前切回伙伴，不再挂载已经离开的团队", async () => {
    renderShell("/chat?wakerId=waker-1", true);
    const partnerButton = await within(
      screen.getByRole("navigation", { name: "伙伴头像栏" }),
    ).findByRole("button", { name: "伙伴二" });

    act(() => {
      within(screen.getByRole("navigation", { name: "伙伴头像栏" }))
        .getByRole("button", { name: "团队一" })
        .click();
      fireEvent.click(partnerButton);
    });

    expect(teamHistoryRender).not.toHaveBeenCalled();
    expect(screen.queryByText("团队历史内容")).not.toBeInTheDocument();
    expect(
      await screen.findByText("/chats/waker-2-latest?wakerId=waker-2"),
    ).toBeVisible();
  });

  it("伙伴管理页面不展示添加内容入口", async () => {
    renderShell("/wakers/waker-1");

    expect(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴一" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "添加内容" }),
    ).not.toBeInTheDocument();
  });

  it("旧 IM 深链打开设置内详情，退出模块后可恢复已关闭详情的设置Scene", async () => {
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
    expect(screen.getAllByRole("region", { name: "设置 Scene" })).toHaveLength(
      1,
    );
    await user.click(screen.getByRole("button", { name: "关闭连接" }));
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    await waitFor(() =>
      expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull(),
    );
    expect(await screen.findByText("/wakers", { exact: true })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(screen.getAllByRole("region", { name: "设置 Scene" })).toHaveLength(
      1,
    );
    expect(screen.queryByRole("dialog", { name: "IM 连接详情" })).toBeNull();
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
    expect(screen.getAllByRole("region", { name: "设置 Scene" })).toHaveLength(
      1,
    );
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
    expect(screen.queryByRole("tab", { name: "会话 chat-a" })).toBeNull();
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    await waitFor(() =>
      expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull(),
    );

    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(
      screen.getByRole("region", { name: "设置 Scene" }),
    ).toBeInTheDocument();

    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    await waitFor(() =>
      expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull(),
    );
    expect(activeRouteScene()).toHaveTextContent(
      "/chats/waker-1-latest?wakerId=waker-1",
    );
  });

  it("从 IM 深链切离后，设置入口恢复已有分类且不恢复临时详情", async () => {
    const user = userEvent.setup();
    renderShell("/im/connection-one?chat=binding-one");
    await user.click(await screen.findByRole("button", { name: "关闭连接" }));
    await user.click(screen.getByRole("button", { name: "管理伙伴一" }));
    await waitFor(() =>
      expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull(),
    );

    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(
      await screen.findByRole("heading", { name: "IM 平台列表" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "IM 集成" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getAllByRole("region", { name: "设置 Scene" })).toHaveLength(
      1,
    );
  });

  it.each([false, true])(
    "设置与聊天保留唯一通知入口，旧侧栏收起偏好被忽略（旧值=%s）",
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
        screen.getAllByRole("button", { name: "通知，打开任务看板" }),
      ).toHaveLength(1);
      expect(screen.getByTestId("sidebar-collapsed")).toHaveTextContent(
        "false",
      );
      await user.click(
        within(
          screen.getByRole("navigation", { name: "伙伴头像栏" }),
        ).getByRole("button", { name: "伙伴一" }),
      );
      await waitFor(() =>
        expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull(),
      );
      const bell = await screen.findByRole("button", {
        name: "通知，打开任务看板",
      });
      await user.click(bell);
      await waitFor(() =>
        expect(activeRouteScene()).toHaveAttribute(
          "data-route-scene-id",
          "work-board",
        ),
      );
      expect(activeRouteScene()).toHaveTextContent("/work-board");
      await waitFor(() =>
        expect(screen.queryByRole("region", { name: "设置 Scene" })).toBeNull(),
      );
      const activeBell = screen.getByRole("button", {
        name: "通知，打开任务看板",
      });
      expect(activeBell).toHaveAttribute("aria-current", "page");
      const historySlot = activeBell.closest("[data-sidebar-history-slot]");
      expect(historySlot).not.toBeNull();
      const historyButtons = historySlot?.querySelectorAll("button");
      expect(historyButtons?.[1]).toHaveAccessibleName("前进");
      expect(historyButtons?.[2]).toBe(activeBell);
      await user.click(activeBell);
      expect(
        document.querySelectorAll('[data-route-scene-id="work-board"]'),
      ).toHaveLength(1);
    },
  );

  it("不同会话使用真实标题并在顶部 SceneBar 中累积", async () => {
    const user = userEvent.setup();
    renderShell();

    expect(await findContent({ name: "会话 chat-a" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    await user.click(screen.getByRole("button", { name: "前往会话 B" }));

    expect(getContent({ name: "会话 chat-a" })).toBeVisible();
    expect(getContent({ name: "会话 chat-b" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    await user.click(getContent({ name: "会话 chat-a" }));
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

    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    expect(await screen.findByText("保留中的 Chat Scene")).toBeVisible();
    expect(sceneLifecycle).toEqual({ mounts: 1, unmounts: 0 });
  });

  it("回到伙伴后关闭owner卸载原内容，回退到仍挂载的资源 Scene", async () => {
    const user = userEvent.setup();
    renderLifecycleShell();
    await screen.findByText("保留中的 Chat Scene");
    await user.click(screen.getByRole("button", { name: "打开资源 Scene" }));
    expect(sceneLifecycle).toEqual({ mounts: 1, unmounts: 0 });
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    await findContent({ name: "会话 chat-a" });
    await user.click(closeContent("会话 chat-a"));
    expect(sceneLifecycle).toEqual({ mounts: 1, unmounts: 1 });
    expect(queryContent({ name: "会话 chat-a" })).toBeNull();
    expect(screen.getByText("资源 Scene")).toBeVisible();
  });

  it("工作流资源列表沿用资源库 Scene 并显示分类侧栏", async () => {
    renderShell("/resources?category=workflows");
    expect(document.querySelector("[data-titlebar-breadcrumbs]")).toBeNull();
    expect(screen.getByRole("button", { name: "工作流" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.queryByRole("tablist", { name: "场景标签" })).toBeNull();
    expect(activeRouteScene()).toHaveTextContent(
      "/resources?category=workflows",
    );
  });

  it.each([
    "/workflows/new?draftId=d1",
    "/workflows/flow-1",
    "/workflows/flow-1/runs/run-1",
  ])("工作流创作与运行 %s 隐藏整个资源分类侧栏", (entry) => {
    renderShell(entry);
    expect(screen.queryByRole("heading", { name: "资源库" })).toBeNull();
    expect(screen.queryByRole("button", { name: "技能" })).toBeNull();
    expect(screen.queryByRole("button", { name: "工作流" })).toBeNull();
  });

  it("关闭最后一个 Scene 后进入无标签欢迎页", async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(
      await screen.findByRole("button", { name: "关闭 会话 chat-a" }),
    );

    expect(queryAllContents()).toHaveLength(0);
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
    await user.click(closeContent("会话 chat-b"));
    await user.click(closeContent("会话 chat-a"));

    expect(queryAllContents()).toHaveLength(0);
    expect(screen.getByRole("region", { name: "欢迎页" })).toBeVisible();
    expect(screen.queryByText(/\/chats\/chat-[ab]/)).toBeNull();
  });

  it("同一伙伴的资源页面复用一个 Scene", async () => {
    const user = userEvent.setup();
    renderShell("/wakers/waker-1/memory");

    expect(activeRouteScene()).toHaveAttribute(
      "data-route-scene-id",
      "waker:waker-1:resources",
    );

    await user.click(screen.getByRole("button", { name: "前往技能" }));

    expect(activeRouteScene()).toHaveTextContent("/wakers/waker-1/skills");
    expect(
      document.querySelectorAll(
        '[data-route-scene-id="waker:waker-1:resources"]',
      ),
    ).toHaveLength(1);
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
    expect(activeRouteScene()).toHaveTextContent("/wakers/waker-1/memory");
    expect(screen.queryByText("项目管理员")).not.toBeInTheDocument();
  });

  it("把全局通知挂载在右上角", () => {
    renderShell();

    expect(screen.getByLabelText("通知容器")).toHaveAttribute(
      "data-position",
      "top-right",
    );
  });
  it("伙伴模块入口没有已验证聊天时返回真实伙伴列表", async () => {
    const user = userEvent.setup();
    renderShell("/usage");
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    expect(await screen.findByText("/wakers")).toBeVisible();
  });
  it("伙伴模块入口恢复已验证且团队仍存在的最近Thread", async () => {
    storage.set(
      "ally.partner-navigation",
      JSON.stringify({
        currentRailKey: "team:team-1",
        lastTeamThreads: { "team-1": "remembered" },
      }),
    );
    const user = userEvent.setup();
    renderShell("/usage");
    await waitFor(() =>
      expect(
        within(
          screen.getByRole("navigation", { name: "伙伴头像栏" }),
        ).getByRole("button", { name: "团队一" }),
      ).toBeVisible(),
    );
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    expect(
      await screen.findByText("/teams/team-1/threads/remembered"),
    ).toBeVisible();
  });

  it.each(["/usage", "/teams/team-31/threads/recent-thread"])(
    "最近团队不在首批列表时仍恢复有效Thread：%s",
    async (entry) => {
      storage.set(
        "ally.partner-navigation",
        JSON.stringify({
          currentRailKey: "team:team-31",
          lastTeamThreads: { "team-31": "recent-thread" },
        }),
      );
      const user = userEvent.setup();
      renderShell(entry);
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "团队一" });
      await user.click(screen.getByRole("button", { name: "用量统计" }));
      await user.click(screen.getByRole("button", { name: "伙伴" }));
      await waitFor(() =>
        expect(
          screen.getByText("/teams/team-31/threads/recent-thread"),
        ).toBeVisible(),
      );
    },
  );

  it("从团队Thread深链切到其他模块后，伙伴入口恢复该团队", async () => {
    const user = userEvent.setup();
    renderShell("/teams/team-1/threads/direct-thread");
    await within(
      screen.getByRole("navigation", { name: "伙伴头像栏" }),
    ).findByRole("button", { name: "团队一" });
    await user.click(screen.getByRole("button", { name: "用量统计" }));
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    expect(
      await screen.findByText("/teams/team-1/threads/direct-thread"),
    ).toBeVisible();
  });
  it("直接进入伙伴聊天后最近目标从团队更新到伙伴", async () => {
    storage.set(
      "ally.partner-navigation",
      JSON.stringify({
        currentRailKey: "team:team-1",
        lastTeamThreads: { "team-1": "old-thread" },
      }),
    );
    const user = userEvent.setup();
    renderShell("/chats/current-chat?wakerId=waker-1");
    await findContent({ name: "会话 current-chat" });
    await user.click(screen.getByRole("button", { name: "用量统计" }));
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    expect(
      await screen.findByText("/chats/current-chat?wakerId=waker-1"),
    ).toBeVisible();
  });

  it.each(["deleted-team", "deleted-thread"])(
    "伙伴入口拒绝已删除的最近团队目标：%s",
    async (kind) => {
      const teamId = kind === "deleted-team" ? "gone" : "team-1";
      latestChatFixture.missingThread = kind === "deleted-thread";
      storage.set(
        "ally.partner-navigation",
        JSON.stringify({
          currentRailKey: `team:${teamId}`,
          lastTeamThreads: { [teamId]: "gone-thread" },
        }),
      );
      const user = userEvent.setup();
      renderShell("/usage");
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴一" });
      await user.click(screen.getByRole("button", { name: "伙伴" }));
      expect(await screen.findByText("/wakers")).toBeVisible();
    },
  );
  it.each([false, true])(
    "伙伴入口仅恢复经原Query验证的聊天（已删除=%s）",
    async (missingChat) => {
      latestChatFixture.missingChat = missingChat;
      storage.set(
        "ally.chat.last-active",
        JSON.stringify({ chatId: "remembered-chat", wakerId: "waker-1" }),
      );
      const user = userEvent.setup();
      renderShell("/usage");
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴一" });
      await user.click(screen.getByRole("button", { name: "伙伴" }));
      expect(
        await screen.findByText(
          missingChat ? "/wakers" : "/chats/remembered-chat?wakerId=waker-1",
        ),
      ).toBeVisible();
    },
  );

  it("裸Chat深链在owner未知时不猜伙伴，Query迟到后只展示真实伙伴聊天", async () => {
    let resolveDetail!: (value: {
      chat: { id: string; wakerId: string; title: string };
    }) => void;
    latestChatFixture.details.set(
      "bare-b",
      new Promise((resolve) => {
        resolveDetail = resolve;
      }),
    );
    renderShell("/chats/bare-b");
    expect(screen.queryByRole("button", { name: "聊天列表" })).toBeNull();
    await act(async () =>
      resolveDetail({
        chat: { id: "bare-b", wakerId: "waker-2", title: "深链真实标题" },
      }),
    );
    expect(await findContent({ name: "深链真实标题" })).toBeVisible();
    expect(latestChatFixture.detailRequests.get("bare-b")).toBe(1);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "新建会话" }));
    expect(activeRouteScene()).toHaveTextContent("wakerId=waker-2");
  });

  it("当前伙伴关闭其他不影响其他身份和设置", async () => {
    const user = userEvent.setup();
    renderShell();
    await findContent({ name: "会话 chat-a" });
    await user.click(screen.getByRole("button", { name: "前往会话 B" }));
    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴二" }),
    );
    await findContent({ name: "会话 waker-2-latest" });
    await user.click(screen.getByRole("button", { name: "设置" }));
    await user.click(screen.getByRole("button", { name: "IM 集成" }));
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    await findContent({ name: "会话 waker-1-latest" });
    fireEvent.contextMenu(getContent({ name: "会话 chat-a" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "关闭其他页签" }),
    );
    expect(queryContent({ name: "会话 chat-b" })).toBeNull();
    expect(
      document.querySelector('[data-route-scene-id="chat:waker-2-latest"]'),
    ).not.toBeNull();
    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(screen.getByRole("button", { name: "IM 集成" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("团队Thread和成员保持平铺Tab，当前Thread的成员不混到另一团队", async () => {
    renderShell("/teams/team-1/threads/thread-one");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "打开同会话文件" }));
    expect(getContent({ name: "同会话文件" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "聊天列表" })).toBeNull();
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "团队二" },
      ),
    );
    await waitFor(() =>
      expect(activeRouteScene()).toHaveTextContent(
        "/teams/team-2/threads/team-2-latest",
      ),
    );
    expect(screen.queryByRole("tab", { name: "同会话文件" })).toBeNull();
    expect(screen.queryByRole("button", { name: "聊天列表" })).toBeNull();
  });

  it("后台Chat Query迟到标题仍进入其所属伙伴的Tab且不新增请求", async () => {
    let resolveDetail!: (value: {
      chat: { id: string; wakerId: string; title: string };
    }) => void;
    latestChatFixture.details.set(
      "slow",
      new Promise((resolve) => {
        resolveDetail = resolve;
      }),
    );
    renderShell("/chats/slow?wakerId=waker-1");
    const user = userEvent.setup();
    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴二" }),
    );
    await findContent({ name: "会话 waker-2-latest" });
    await act(async () =>
      resolveDetail({
        chat: { id: "slow", wakerId: "waker-1", title: "后台迟到的真实标题" },
      }),
    );
    await user.click(
      within(screen.getByRole("navigation", { name: "伙伴头像栏" })).getByRole(
        "button",
        { name: "伙伴一" },
      ),
    );
    expect(await findContent({ name: "后台迟到的真实标题" })).toBeVisible();
    expect(latestChatFixture.detailRequests.get("slow")).toBe(1);
  });

  it("实体权威404跳过历史，原Chat404仍可回伙伴并打开其最新聊天", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderShell("/chats/chat-a?wakerId=waker-1", false, client);
    await findContent({ name: "会话 chat-a" });
    const user = userEvent.setup();
    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴二" }),
    );
    await findContent({ name: "会话 waker-2-latest" });
    expect(screen.getByRole("button", { name: "后退" })).toBeEnabled();
    await act(async () => {
      await client
        .fetchQuery({
          queryKey: getWakerQueryKey({ path: { wakerId: "waker-1" } }),
          queryFn: async () => {
            throw new ApiError("伙伴已删除", "NOT_FOUND", 404);
          },
          retry: false,
        })
        .catch(() => {});
    });
    expect(screen.getByRole("button", { name: "后退" })).toBeDisabled();
    act(() =>
      client.setQueryData(getWakerQueryKey({ path: { wakerId: "waker-1" } }), {
        waker: { id: "waker-1" },
      }),
    );
    await act(async () => {
      await client
        .fetchQuery({
          queryKey: getChatOptions({ path: { chatId: "chat-a" } }).queryKey,
          queryFn: async () => {
            throw new ApiError("聊天已删除", "NOT_FOUND", 404);
          },
          retry: false,
        })
        .catch(() => {});
    });
    expect(screen.getByRole("button", { name: "后退" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "后退" }));
    expect(await findContent({ name: "会话 waker-1-latest" })).toBeVisible();
  });

  it("历史中已解析的伙伴不因实体分页列表未包含而失效", async () => {
    latestChatFixture.details.set(
      "out-of-page",
      Promise.resolve({
        chat: {
          id: "out-of-page",
          wakerId: "waker-outside",
          title: "分页外伙伴的聊天",
        },
      }),
    );
    renderShell("/chats/out-of-page");
    await findContent({ name: "分页外伙伴的聊天" });
    const user = userEvent.setup();
    await user.click(
      await within(
        screen.getByRole("navigation", { name: "伙伴头像栏" }),
      ).findByRole("button", { name: "伙伴二" }),
    );
    await findContent({ name: "会话 waker-2-latest" });
    expect(screen.getByRole("button", { name: "后退" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "后退" }));
    await waitFor(() =>
      expect(activeRouteScene()).toHaveTextContent("wakerId=waker-outside"),
    );
  });

  it("裸Chat Query解析后记住真实伙伴，切模块后伙伴入口恢复同一聊天", async () => {
    renderShell("/chats/bare-b");
    await findContent({ name: "会话 bare-b" });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "用量统计" }));
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    await waitFor(() =>
      expect(activeRouteScene()).toHaveTextContent(
        "/chats/bare-b?wakerId=waker-2",
      ),
    );
  });

  it("团队Thread使用既有Query中的真实标题而非通用类型标签", async () => {
    latestChatFixture.threadTitles.set(
      "team-1:titled-thread",
      "已落库的团队议题",
    );
    renderShell("/teams/team-1/threads/titled-thread");
    expect(await findContent({ name: "已落库的团队议题" })).toBeVisible();
  });

  it("AppShell到真实Routes保留项目卡片导航state与原列表搜索", async () => {
    renderStatefulProductionShell();
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("link", { name: "查看项目 Research Desk" }),
    );
    expect(
      await screen.findByRole("heading", { name: "Research Desk" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "项目" })).toHaveAttribute(
      "href",
      "/wakers/waker-1/projects?search=desk",
    );
    await user.click(screen.getByRole("link", { name: "项目" }));
    expect(screen.getByTestId("state-route")).toHaveTextContent(
      "/wakers/waker-1/projects?search=desk",
    );
  });

  it("真实Routes为前后台Scene保留各自返回state，浏览器回退恢复A", async () => {
    renderStatefulProductionShell();
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("link", { name: "查看项目 Research Desk" }),
    );
    await screen.findByRole("heading", { name: "Research Desk" });
    await user.click(screen.getByRole("button", { name: "打开资源技能详情" }));
    expect(
      await screen.findByRole("heading", { name: "操作指南" }),
    ).toBeVisible();
    const skills = screen.getByRole("link", { name: "插件" });
    expect(skills).toHaveAttribute(
      "href",
      "/resources?category=skills&search=desk&source=plugin",
    );
    expect(
      screen.getByTestId("project-scene-navigation-state"),
    ).toHaveTextContent(
      '{"projectBack":"/wakers/waker-1/projects?search=desk"}',
    );
    await user.click(screen.getByRole("button", { name: "浏览器返回" }));
    expect(
      await screen.findByRole("heading", { name: "Research Desk" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "项目" })).toHaveAttribute(
      "href",
      "/wakers/waker-1/projects?search=desk",
    );
  });

  it("根欢迎从页面贡献的实际owner显示操作，加号复用唯一欢迎草稿", async () => {
    latestChatFixture.resolvedEntryOwner = "waker-2";
    renderShell("/");
    expect(await screen.findByRole("tab", { name: "新会话" })).toBeVisible();
    await waitFor(() =>
      expect(
        within(
          screen.getByRole("navigation", { name: "伙伴头像栏" }),
        ).getByRole("button", { name: "伙伴二" }),
      ).toHaveAttribute("aria-current", "page"),
    );
    expect(screen.getByRole("button", { name: "工作区" })).toBeVisible();
    const welcome = activeRouteScene();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "新建会话" }));
    expect(activeRouteScene()).toBe(welcome);
    expect(screen.getAllByRole("tab", { name: "新会话" })).toHaveLength(1);
  });

  it("工作区开关由聊天顶栏最右actions承载，开关工作区后保持同一入口", async () => {
    renderShell();
    const user = userEvent.setup();
    const toggle = await screen.findByRole("button", { name: "工作区" });
    expect(
      toggle.closest("[data-scene-chrome-accessory-wrapper]"),
    ).not.toBeNull();
    expect(toggle.closest("[data-scene-pane-actions]")).toBeNull();
    await user.click(toggle);
    expect(
      within(activeRouteScene()).getByTestId("right-panel-state"),
    ).toHaveTextContent("workspace");
    expect(screen.getByRole("button", { name: "工作区" })).toBe(toggle);
    await user.click(toggle);
    expect(
      within(activeRouteScene()).getByTestId("right-panel-state"),
    ).toHaveTextContent("closed");
  });
});
