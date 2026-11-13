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
import { useState, type ReactNode } from "react";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type * as ChatModule from "@/modules/chat";
import type * as WakerModule from "@/modules/waker";
import type { Waker } from "@/modules/waker";
import { FloatingChat } from "@/modules/chat/components/floating-chat";
import { SettingsContextSidebar } from "@/modules/settings/components/settings-context-sidebar";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { i18n } from "@/shared/i18n";
import { configurePlatform, platform } from "@/shared/platform/provider";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import { ResourcesContextSidebar } from "./resources-context-sidebar";
import { PartnerNavigationLayout } from "./partner-navigation-layout";
import type { PartnerNavigationPreferences } from "./partner-navigation-preferences";

const history = vi.hoisted(() => ({
  executionId: undefined as string | undefined,
  waitingApproval: false,
  waitingElicitation: false,
  observers: new Set<GlobalEventObserver>(),
}));
const duplicateWaker = vi.hoisted(() => vi.fn());

vi.mock("@/modules/waker", async (importOriginal) => ({
  ...(await importOriginal<typeof WakerModule>()),
  duplicateWakerMutation: () => ({ mutationFn: duplicateWaker }),
}));

vi.mock(import("@/shared/api/global-events"), async (importOriginal) => ({
  ...(await importOriginal()),
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    history.observers.add(observer);
    return () => history.observers.delete(observer);
  },
}));

vi.mock("@/modules/chat", async (importOriginal) => ({
  ...(await importOriginal<typeof ChatModule>()),
  ChatForkMenuItem: () => null,
  ChatTaskDialog: () => null,
  chatListInfiniteOptions: (wakerId: string) => ({
    getNextPageParam: () => undefined,
    initialPageParam: undefined,
    queryFn: async () => ({
      resources:
        wakerId === "waker-6"
          ? [
              {
                id: "chat-project",
                activeExecutionId: history.executionId,
                waitingApproval: history.waitingApproval,
                waitingElicitation: history.waitingElicitation,
                projectId: "project-1",
                projectName: "项目甲",
                title: "项目会话",
                updatedAt: Date.now(),
                wakerId,
              },
              {
                additionalWorkingDirectories: ["/managed/chat-a/workspace"],
                projectName: "workspace",
                id: "chat-temp-a",
                title: "临时会话甲",
                updatedAt: 2,
                wakerId,
              },
              {
                additionalWorkingDirectories: [
                  "C:\\managed\\chat-b\\workspace",
                ],
                id: "chat-temp-b",
                title: "临时会话乙",
                updatedAt: 1,
                wakerId,
              },
              {
                additionalWorkingDirectories: ["/tmp/临时目录"],
                id: "chat-folder",
                title: "临时会话",
                updatedAt: Date.now() - 60_000,
                wakerId,
              },
            ]
          : [],
    }),
    queryKey: ["chats", wakerId],
  }),
  chatMessagesInfiniteOptions: (chatId: string) => ({
    getNextPageParam: () => undefined,
    initialPageParam: undefined,
    queryFn: async () => ({ resources: [] }),
    queryKey: ["chat-messages", chatId],
  }),
  getChatOptions: ({ path }: { path: { chatId: string } }) => ({
    queryFn: async () => ({
      chat: {
        id: path.chatId,
        projectId: "project-1",
        projectName: "项目甲",
        title: "项目会话",
        wakerId: "waker-6",
      },
    }),
    queryKey: ["chat", path.chatId],
  }),
}));

vi.mock("@/modules/team/api/channel-queries", () => ({
  deleteChannelThreadMutation: () => ({ mutationFn: vi.fn() }),
  getChannelOptions: ({ path }: { path: { channelId: string } }) => ({
    queryFn: async () => ({
      channel: {
        id: path.channelId,
        leadWakerId: "waker-1",
        name: "研发团队",
      },
      members: [
        { waker: { id: "waker-1", name: "伙伴 1" } },
        { waker: { id: "waker-2", name: "伙伴 2" } },
      ],
    }),
    queryKey: ["channel", path.channelId],
  }),
  getChannelThreadOptions: ({
    path,
  }: {
    path: { channelId: string; threadId: string };
  }) => ({
    queryFn: async () => ({
      members: [
        { chatId: "lead-chat", isLead: true, wakerId: "waker-1" },
        { chatId: "member-chat", wakerId: "waker-2" },
      ],
      thread: {
        id: path.threadId,
        projectId: "project-1",
        projectName: "项目甲",
        title: "团队项目会话",
      },
    }),
    queryKey: ["channel-thread", path.channelId, path.threadId],
  }),
  getChannelThreadQueryKey: ({
    path,
  }: {
    path: { channelId: string; threadId: string };
  }) => ["channel-thread", path.channelId, path.threadId],
  teamThreadsInfiniteOptions: (channelId: string) => ({
    queryFn: async () => ({
      resources: [
        {
          id: "thread-project",
          projectId: "project-1",
          projectName: "项目甲",
          title: "团队项目会话",
          updatedAt: Date.now(),
        },
        {
          additionalWorkingDirectories: ["/tmp/团队临时目录"],
          id: "thread-folder",
          title: "团队临时会话",
          updatedAt: Date.now() - 60_000,
        },
      ],
    }),
    queryKey: ["channel-threads", channelId],
    initialPageParam: 1,
    getNextPageParam: () => undefined,
  }),
  teamQueryKeys: {
    threads: (channelId: string) => ["channel-threads", channelId],
    timelinePages: (channelId: string, threadId: string) => [
      "channel-timeline",
      channelId,
      threadId,
    ],
  },
}));

const wakers = Array.from({ length: 6 }, (_, index) => ({
  activityState:
    index === 4
      ? ("waiting" as const)
      : index === 5
        ? ("running" as const)
        : ("idle" as const),
  id: `waker-${index + 1}`,
  name: `伙伴 ${index + 1}`,
  roleName: `角色 ${index + 1}`,
  runningCount: index === 5 ? 4 : 0,
  waitingCount: index === 4 ? 1 : 0,
})) as Waker[];

const initialPreferences: PartnerNavigationPreferences = {
  collapsedGroups: {},
  contextSidebarWidth: 240,
  currentPartnerId: "waker-6",
  currentRailKey: "waker:waker-6",
  entityMode: "waker",
  globalSidebarCollapsed: true,
  lastTeamThreads: {},
  railExpanded: false,
  railOrder: wakers.map((waker) => `waker:${waker.id ?? ""}`),
};

function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="current-route">
      {location.pathname}
      {location.search}
    </output>
  );
}

function TestLayout({
  activeChatId,
  contextSidebarHidden,
  floatingChat = false,
  selectedPartnerId = "waker-6",
  sceneContentFlush = false,
  settingsOpen = false,
  settingsSidebar,
  renderTitlebar,
  navigationPreferences,
  sidebarToolbar,
  notificationBell,
  teamContext,
}: {
  activeChatId?: string;
  contextSidebarHidden?: boolean;
  floatingChat?: boolean;
  selectedPartnerId?: string;
  sceneContentFlush?: boolean;
  settingsOpen?: boolean;
  settingsSidebar?: ReactNode;
  renderTitlebar?: (navigationToggle: ReactNode) => ReactNode;
  navigationPreferences?: Partial<PartnerNavigationPreferences>;
  sidebarToolbar?: ReactNode;
  notificationBell?: ReactNode;
  teamContext?: { channelId: string; threadId?: string };
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const activeSettings =
    settingsOpen || location.pathname.startsWith("/settings");
  const [preferences, setPreferences] = useState({
    ...initialPreferences,
    ...navigationPreferences,
  });
  const [fullView, setFullView] = useState(floatingChat);
  return (
    <PartnerNavigationLayout
      renderTitlebar={renderTitlebar}
      sidebarToolbar={sidebarToolbar}
      notificationBell={notificationBell}
      activeChatId={activeChatId}
      channels={[]}
      contextSidebarHidden={
        contextSidebarHidden ?? !location.pathname.startsWith("/wakers/")
      }
      onManageEntities={() => undefined}
      onOpenPartnerDetail={() => undefined}
      onNavigateModule={(id) =>
        navigate(
          id === "settings"
            ? "/settings/general"
            : id === "resources"
              ? "/resources"
              : id === "usage"
                ? "/usage"
                : "/wakers",
        )
      }
      onPreferencesChange={setPreferences}
      onSelectPartner={() => undefined}
      onSelectTeam={() => undefined}
      preferences={preferences}
      sceneContentFlush={sceneContentFlush}
      selectedPartnerId={selectedPartnerId}
      selectedRailKey={`waker:${selectedPartnerId}`}
      settingsOpen={activeSettings}
      settingsSidebar={
        activeSettings
          ? (settingsSidebar ?? (
              <SettingsContextSidebar
                activeSection="general"
                onSelect={() => undefined}
              />
            ))
          : undefined
      }
      moduleSidebar={
        location.pathname === "/resources" ? (
          <ResourcesContextSidebar
            category="plugins"
            onCategoryChange={() => undefined}
          />
        ) : undefined
      }
      teamContext={teamContext}
      wakers={wakers}
    >
      <main>
        主内容
        <output data-testid="navigation-preferences">
          {JSON.stringify(preferences)}
        </output>
        <LocationProbe />
        {floatingChat ? (
          <>
            <button type="button" onClick={() => setFullView(false)}>
              退出完整视图
            </button>
            <FloatingChat
              chatId="chat-project"
              fullView={fullView}
              scopeKey="scope-project"
              snapshot={{
                isRunning: true,
                pendingApprovalCount: 0,
                pendingElicitationCount: 0,
                streamState: "open",
              }}
              title="项目会话"
            >
              <div>同一个聊天</div>
            </FloatingChat>
          </>
        ) : null}
      </main>
    </PartnerNavigationLayout>
  );
}

function renderLayout({
  activeChatId,
  contextSidebarHidden,
  floatingChat,
  initialEntry = "/chat?wakerId=waker-6",
  sceneContentFlush,
  selectedPartnerId,
  settingsOpen,
  settingsSidebar,
  renderTitlebar,
  navigationPreferences,
  sidebarToolbar,
  notificationBell,
  teamContext,
}: {
  activeChatId?: string;
  contextSidebarHidden?: boolean;
  floatingChat?: boolean;
  initialEntry?: string;
  sceneContentFlush?: boolean;
  selectedPartnerId?: string;
  settingsOpen?: boolean;
  settingsSidebar?: ReactNode;
  renderTitlebar?: (navigationToggle: ReactNode) => ReactNode;
  navigationPreferences?: Partial<PartnerNavigationPreferences>;
  sidebarToolbar?: ReactNode;
  notificationBell?: ReactNode;
  teamContext?: { channelId: string; threadId?: string };
} = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <WorkspaceProvider>
          <TestLayout
            activeChatId={activeChatId}
            contextSidebarHidden={contextSidebarHidden}
            floatingChat={floatingChat}
            sceneContentFlush={sceneContentFlush}
            selectedPartnerId={selectedPartnerId}
            settingsOpen={settingsOpen}
            settingsSidebar={settingsSidebar}
            renderTitlebar={renderTitlebar}
            navigationPreferences={navigationPreferences}
            sidebarToolbar={sidebarToolbar}
            notificationBell={notificationBell}
            teamContext={teamContext}
          />
        </WorkspaceProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("PartnerNavigationLayout", () => {
  it("伙伴右键菜单可新建分组、移入已有分组、编辑和打开删除确认", async () => {
    renderLayout({
      navigationPreferences: {
        wakerGroups: [{ name: "已有分组", wakerIds: ["waker-2"] }],
      },
    });
    const user = userEvent.setup();
    const row = screen.getByRole("button", { name: "伙伴 1" });
    row.focus();
    await user.keyboard("{Shift>}{F10}{/Shift}");
    expect(screen.getByRole("menuitem", { name: "标为未读" })).toBeVisible();
    expect(screen.getByRole("menuitem", { name: "复制伙伴" })).toBeVisible();
    const openMove = async () => {
      screen.getByRole("menuitem", { name: "移动到" }).focus();
      await user.keyboard("{ArrowRight}");
    };
    await openMove();
    await user.click(screen.getByRole("menuitem", { name: "新建分组" }));
    const groupDialog = screen.getByRole("dialog", { name: "新建分组" });
    await user.type(within(groupDialog).getByLabelText("分组名称"), "研究");
    await user.click(within(groupDialog).getByRole("button", { name: "保存" }));
    const heading = screen.getByRole("heading", { name: "研究" });
    expect(heading.nextElementSibling).toContainElement(row);

    fireEvent.contextMenu(row);
    await openMove();
    await user.click(screen.getByRole("menuitem", { name: "已有分组" }));
    expect(heading).not.toBeInTheDocument();
    const existing = screen.getByRole("heading", { name: "已有分组" });
    expect(existing.nextElementSibling).toContainElement(row);

    fireEvent.contextMenu(row);
    await openMove();
    await user.click(screen.getByRole("menuitem", { name: "未分组" }));
    expect(
      screen.getByRole("heading", { name: "未分组" }).nextElementSibling,
    ).toContainElement(row);

    fireEvent.contextMenu(row);
    await user.click(screen.getByRole("menuitem", { name: "编辑伙伴" }));
    expect(screen.getByRole("dialog", { name: "编辑伙伴" })).toBeVisible();
    await user.keyboard("{Escape}");
    fireEvent.contextMenu(row);
    await user.click(screen.getByRole("menuitem", { name: "删除伙伴" }));
    const deleteDialog = screen.getByRole("dialog", { name: "删除伙伴" });
    expect(
      within(deleteDialog).getByRole("button", { name: "删除伙伴" }),
    ).toBeDisabled();
  });

  it("复制通过原伙伴标识请求新伙伴", async () => {
    duplicateWaker.mockResolvedValueOnce({ wakerId: "new-partner" });
    renderLayout();
    fireEvent.contextMenu(screen.getByRole("button", { name: "伙伴 1" }));
    await userEvent
      .setup()
      .click(screen.getByRole("menuitem", { name: "复制伙伴" }));
    await waitFor(() =>
      expect(duplicateWaker).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { wakerId: "waker-1" },
          body: { requestId: expect.any(String) },
        }),
        expect.anything(),
      ),
    );
  });

  it("通知紧跟前进按钮，模块栏不重复显示通知", () => {
    renderLayout({
      sidebarToolbar: (
        <>
          <button>后退</button>
          <button>前进</button>
        </>
      ),
      notificationBell: <button>通知入口</button>,
    });
    const bell = screen.getByRole("button", { name: "通知入口" });
    const slot = bell.closest("[data-sidebar-history-slot]");
    expect(slot).not.toBeNull();
    expect(slot?.querySelectorAll("button")[1]).toHaveAccessibleName("前进");
    expect(slot?.lastElementChild).toBe(bell);
    expect(
      screen.getByRole("navigation", { name: "模块导航" }),
    ).not.toContainElement(bell);
  });

  it("Rail仍是整窗首列，正常伙伴列表始终显示且没有全局收起/展开按钮", () => {
    renderLayout({
      renderTitlebar: () => <header aria-label="内容顶栏">当前内容</header>,
      sidebarToolbar: <button>实体后退</button>,
    });
    const header = screen.getByRole("banner", { name: "内容顶栏" });
    const sidebar = document.querySelector("[data-app-context-sidebar]");
    const rail = screen.getByRole("navigation", { name: "模块导航" });
    expect(rail.parentElement?.firstElementChild).toBe(rail);
    expect(sidebar).not.toContainElement(header);
    expect(screen.getByRole("region", { name: "场景工作区" })).toContainElement(
      header,
    );
    expect(screen.getByRole("radiogroup", { name: "身份列表" })).toBeVisible();
    expect(screen.getByRole("button", { name: "实体后退" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /展开全局侧栏|收起全局侧栏/ }),
    ).toBeNull();
  });

  it.each([
    ["desktop", "darwin"],
    ["desktop", undefined],
    ["web", undefined],
  ] as const)(
    "%s宿主原生控件%s不改变38px区域顶行",
    (kind, nativeWindowControls) => {
      configurePlatform({
        ...platform(),
        kind,
        chrome: { navigationInset: 72, nativeWindowControls },
      });
      renderLayout();
      expect(document.querySelector("[data-app-workspace-frame]")).toHaveStyle({
        "--shell-sidebar-header-height": "38px",
      });
      expect(document.querySelector("[data-scene-chrome-host]")).toHaveClass(
        "h-(--shell-titlebar-height)",
      );
    },
  );

  it("所有模块保留左上toolbar，无sidebar正文时正文跨frame且原Entity保活", async () => {
    const user = userEvent.setup();
    renderLayout({
      navigationPreferences: { globalSidebarCollapsed: false },
      sidebarToolbar: <button>实体后退</button>,
    });
    const toolbarAction = screen.getByRole("button", { name: "实体后退" });
    const entity = document.querySelector("[data-collapsed=false]");
    const main = screen.getByText("主内容");
    await user.click(screen.getByRole("button", { name: "用量统计" }));
    expect(screen.getByRole("button", { name: "实体后退" })).toBe(
      toolbarAction,
    );
    expect(entity).toBeInTheDocument();
    expect(entity).not.toBeVisible();
    expect(entity?.closest("[inert]")).not.toBeNull();
    expect(screen.getByRole("region", { name: "场景工作区" })).toHaveAttribute(
      "data-content-spans-sidebar",
    );
    expect(screen.getByText("主内容")).toBe(main);
    await user.click(screen.getByRole("button", { name: "资源库" }));
    expect(screen.getByRole("button", { name: "实体后退" })).toBe(
      toolbarAction,
    );
    expect(entity).not.toBeVisible();
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    expect(document.querySelector("[data-collapsed=false]")).toBe(entity);
    expect(entity).toBeVisible();
  });

  it.each(["/resources", "/usage", "/work-board", "/automatic-tasks"])(
    "非伙伴模块%s隐藏伙伴右下history，原实体列表仍保活",
    (entry) => {
      renderLayout({ initialEntry: entry });
      expect(
        document.querySelector("[data-sidebar-entity-body]"),
      ).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "查看历史会话" })).toBeNull();
    },
  );

  it("右下只保留当前身份的历史入口，不重复弹实体导航", async () => {
    const user = userEvent.setup();
    renderLayout();
    expect(screen.getAllByRole("navigation", { name: "伙伴" })).toHaveLength(1);
    expect(screen.queryByRole("button", { name: /打开身份切换器/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "查看历史会话" }));
    expect(await screen.findByText("项目会话")).toBeVisible();
    expect(screen.getAllByRole("navigation", { name: "伙伴" })).toHaveLength(1);
  });

  it("忽略旧fold偏好与窄窗口自动收起，搜索仍从正常列表工具打开", async () => {
    const user = userEvent.setup();
    renderLayout({
      navigationPreferences: {
        contextSidebarWidth: 88,
        globalSidebarCollapsed: true,
      },
    });
    const frame = document.querySelector(
      "[data-app-workspace-frame]",
    ) as HTMLElement;
    expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "216px" });
    vi.spyOn(frame, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, 0, 300, 700),
    );
    fireEvent(window, new Event("resize"));
    expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "216px" });
    expect(screen.getByRole("radiogroup", { name: "身份列表" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /展开全局侧栏|收起全局侧栏/ }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "搜索伙伴" }));
    expect(screen.getByRole("searchbox", { name: "搜索伙伴" })).toHaveFocus();
  });

  it("拖动到216px即停止，键盘Home/End仍调整正常宽度且不触发收起", () => {
    renderLayout({
      navigationPreferences: {
        contextSidebarWidth: 280,
        globalSidebarCollapsed: true,
      },
    });
    const handle = screen.getByRole("separator", { name: "调整模块侧栏宽度" });
    expect(handle).toHaveAttribute("aria-valuemin", "216");
    fireEvent.mouseDown(handle, { clientX: 280 });
    fireEvent.mouseUp(document, { clientX: 88 });
    expect(handle).toHaveAttribute("aria-valuenow", "216");
    expect(screen.getByRole("radiogroup", { name: "身份列表" })).toBeVisible();
    fireEvent.keyDown(handle, { key: "End" });
    expect(handle).toHaveAttribute("aria-valuenow", "400");
    fireEvent.keyDown(handle, { key: "Home" });
    expect(handle).toHaveAttribute("aria-valuenow", "216");
    fireEvent.doubleClick(handle);
    expect(handle).toHaveAttribute("aria-valuenow", "280");
    expect(
      screen.queryByRole("button", { name: /展开全局侧栏|收起全局侧栏/ }),
    ).toBeNull();
  });

  it.each([
    ["/wakers/waker-6", "概览"],
    ["/wakers/waker-6/projects/project-1", "项目"],
    ["/wakers/waker-6/memory", "记忆"],
    ["/wakers/waker-6/resources?category=skills", "资源"],
    ["/wakers/waker-6/plugins/plugin-1/skills/skill-1", "资源"],
  ])(
    "详情及子路由%s左栏显示原伙伴功能导航，正文不再挂Dock",
    async (entry, label) => {
      renderLayout({ initialEntry: entry });
      const nav = await screen.findByRole("navigation", {
        name: "当前伙伴功能",
      });
      expect(
        document.querySelector("[data-app-context-sidebar]"),
      ).toContainElement(nav);
      expect(
        screen.getByRole("region", { name: "场景工作区" }),
      ).not.toContainElement(nav);
      expect(within(nav).getByRole("link", { name: label })).toHaveAttribute(
        "aria-current",
        "page",
      );
      expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
      expect(
        document.querySelector("[data-sidebar-entity-body]"),
      ).toBeInTheDocument();
      expect(
        document.querySelector("[data-slot=detail-dock-overlay]"),
      ).toBeNull();
    },
  );

  it("详情导航切换与返回伙伴列表保持原Entity正文实例", async () => {
    const user = userEvent.setup();
    renderLayout({ initialEntry: "/wakers/waker-6" });
    const entity = document.querySelector("[data-sidebar-entity-body]");
    await user.click(await screen.findByRole("link", { name: "项目" }));
    expect(document.querySelector("[data-sidebar-entity-body]")).toBe(entity);
    expect(entity).not.toBeVisible();
    await user.click(
      within(screen.getByRole("navigation", { name: "模块导航" })).getByRole(
        "button",
        { name: "伙伴" },
      ),
    );
    expect(document.querySelector("[data-sidebar-entity-body]")).toBe(entity);
    expect(entity).toBeVisible();
    expect(
      screen.queryByRole("navigation", { name: "当前伙伴功能" }),
    ).toBeNull();
  });

  it("伙伴聊天保留原伙伴团队列表，不切换详情导航或插入Dock", () => {
    renderLayout({ initialEntry: "/chat?wakerId=waker-6" });
    expect(screen.getByRole("navigation", { name: "伙伴" })).toBeVisible();
    expect(screen.getByRole("radiogroup", { name: "身份列表" })).toBeVisible();
    expect(
      screen.queryByRole("navigation", { name: "当前伙伴功能" }),
    ).toBeNull();
    expect(
      document.querySelector("[data-slot=detail-dock-overlay]"),
    ).toBeNull();
  });

  it("侧栏加号每次打开所选伙伴的独立新会话草稿", async () => {
    const user = userEvent.setup();
    renderLayout();
    const first = screen.getAllByRole("button", { name: "新建会话" })[0];
    if (!first) throw new Error("缺少新建会话按钮");
    await user.click(first);
    const firstRoute = new URL(
      screen.getByTestId("current-route").textContent ?? "",
      "https://ally.local",
    );
    expect(firstRoute.pathname).toBe("/chat");
    expect(firstRoute.searchParams.get("wakerId")).toBe("waker-1");
    expect(firstRoute.searchParams.get("draftId")).toBeTruthy();
    await user.click(first);
    const secondRoute = new URL(
      screen.getByTestId("current-route").textContent ?? "",
      "https://ally.local",
    );
    expect(secondRoute.searchParams.get("wakerId")).toBe("waker-1");
    expect(secondRoute.searchParams.get("draftId")).not.toBe(
      firstRoute.searchParams.get("draftId"),
    );
  });

  it("完整视图沿用右下角唯一入口，不叠加伙伴history按钮", async () => {
    const user = userEvent.setup();
    renderLayout({ floatingChat: true });

    const launcher = await screen.findByRole("button", {
      name: "工作中，打开对话",
    });
    expect(launcher).toHaveAttribute("data-floating-chat-shell-launcher");
    expect(
      screen.queryByRole("button", { name: "打开身份切换器，当前：伙伴 6" }),
    ).toBeNull();
    expect(
      document.querySelectorAll("[data-floating-chat-shell-launcher]"),
    ).toHaveLength(1);

    await user.click(launcher);
    expect(screen.getByRole("region", { name: "悬浮对话" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "查看历史会话" })).toBeNull();
    expect(
      document.querySelectorAll("[data-floating-chat-shell-launcher]"),
    ).toHaveLength(1);

    await user.click(screen.getByRole("button", { name: "退出完整视图" }));
    expect(
      screen.queryByRole("button", { name: "工作中，打开对话" }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "查看历史会话" })).toBeVisible();
  });

  it("伙伴管理页面隐藏伙伴菜单并让主内容占据单列", () => {
    renderLayout({
      contextSidebarHidden: true,
      initialEntry: "/wakers",
      selectedPartnerId: "",
    });

    expect(
      screen.queryByRole("navigation", { name: "当前伙伴功能" }),
    ).toBeNull();
    expect(screen.getByRole("navigation", { name: "伙伴" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "打开身份切换器" })).toBeNull();
    expect(
      screen.getByRole("region", { name: "场景工作区" }).children[1],
    ).toHaveClass("grid-cols-[minmax(0,1fr)]");
  });

  it("调整侧栏宽度后右下仍为同一伙伴历史，保留实体活动与选择", async () => {
    const user = userEvent.setup();
    renderLayout();
    const history = screen.getByRole("button", { name: "查看历史会话" });
    expect(history.parentElement).toHaveClass("right-0", "bottom-0");
    await user.click(history);
    expect(await screen.findByText("项目会话")).toBeVisible();
    expect(screen.queryByRole("button", { name: "返回身份列表" })).toBeNull();
    expect(
      within(screen.getByLabelText("历史会话")).getByRole("button", {
        name: "新建会话",
      }).textContent,
    ).toBe("");
    const rail = screen.getByRole("navigation", { name: "伙伴" });
    expect(
      within(rail).getByRole("button", { name: "伙伴 5，需要操作" }),
    ).toBeVisible();
    expect(
      within(rail).getByRole("button", { name: "伙伴 6，工作中" }),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("button", { name: "查看历史会话" })).toBe(history);
    expect(
      screen.queryByRole("navigation", { name: "当前伙伴功能" }),
    ).toBeNull();
  });

  it("详情导航位于左侧，场景顶栏和主体保持各自内容边界", async () => {
    renderLayout({ initialEntry: "/wakers/waker-6" });

    const sceneSurface = screen.getByRole("region", { name: "场景工作区" });

    expect(sceneSurface).toContainElement(screen.getByText("主内容"));
    const navigation = await screen.findByRole("navigation", {
      name: "当前伙伴功能",
    });
    expect(sceneSurface).not.toContainElement(navigation);
    expect(
      document.querySelector("[data-app-context-sidebar]"),
    ).toContainElement(navigation);
    expect(
      screen.queryByRole("separator", { name: "调整上下文侧栏宽度" }),
    ).toBeNull();
    expect(sceneSurface).toHaveClass("overflow-hidden");
    expect(document.querySelector(".isolate")).toHaveStyle({
      "--shell-navigation-leading-inset": "8px",
    });
  });

  it("聊天场景的顶栏与正文共用左边界", () => {
    renderLayout({ sceneContentFlush: true });
    const sceneSurface = screen.getByRole("region", { name: "场景工作区" });
    expect(sceneSurface.children[1]).toHaveClass("pl-0");
    expect(sceneSurface.children[1]).not.toHaveClass("pl-4");
  });

  it.each([
    "/workflows/new",
    "/workflows/definition-1",
    "/workflows/definition-1/runs/run-1",
  ])("工作流 %s 不装配空侧栏并选中资源库", (initialEntry) => {
    renderLayout({
      initialEntry,
      selectedPartnerId: "",
    });
    expect(
      document.querySelector("[data-app-context-sidebar]"),
    ).toBeInTheDocument();
    expect(
      document.querySelector("[data-sidebar-entity-body]"),
    ).not.toBeVisible();
    expect(screen.getByRole("region", { name: "场景工作区" })).toHaveAttribute(
      "data-content-spans-sidebar",
    );
    expect(
      screen.queryByRole("separator", { name: "调整模块侧栏宽度" }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "资源库" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.queryByRole("button", { name: "工作流" })).toBeNull();
    expect(screen.getByRole("navigation", { name: "模块导航" })).toBeVisible();
  });

  it("模块切换只更换模块侧栏，固定模块入口与内容保持挂载", async () => {
    const user = userEvent.setup();
    renderLayout();
    const rail = screen.getByRole("navigation", { name: "模块导航" });
    const main = screen.getByText("主内容");
    await user.click(within(rail).getByRole("button", { name: "资源库" }));
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
    expect(screen.getByRole("button", { name: "技能" })).toBeVisible();
    expect(screen.getByRole("navigation", { name: "模块导航" })).toBe(rail);
    await user.click(within(rail).getByRole("button", { name: "设置" }));
    expect(screen.getByRole("navigation", { name: "设置菜单" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "技能" })).toBeNull();
    expect(screen.getByText("主内容")).toBe(main);
  });

  it.each(["/settings/general", "/resources"])(
    "%s 与伙伴侧栏共用宽度范围并支持拖动",
    (entry) => {
      renderLayout({
        initialEntry: entry,
        navigationPreferences: { contextSidebarWidth: 216 },
      });
      const frame = document.querySelector("[data-app-workspace-frame]");
      expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "216px" });
      const handle = screen.getByRole("separator", {
        name: "调整模块侧栏宽度",
      });
      fireEvent.mouseDown(handle, { clientX: 216 });
      fireEvent.mouseUp(document, { clientX: 300 });
      expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "300px" });
      expect(
        JSON.parse(
          screen.getByTestId("navigation-preferences").textContent || "{}",
        ),
      ).toMatchObject({ contextSidebarWidth: 300 });
      fireEvent.keyDown(handle, { key: "Home" });
      expect(handle).toHaveAttribute("aria-valuenow", "216");
      fireEvent.keyDown(handle, { key: "End" });
      expect(handle).toHaveAttribute("aria-valuenow", "400");
    },
  );

  it("资源库、设置和伙伴之间切换始终沿用最近调整的全局侧栏宽度", async () => {
    const user = userEvent.setup();
    renderLayout({ navigationPreferences: { contextSidebarWidth: 216 } });
    const frame = document.querySelector("[data-app-workspace-frame]");
    await user.click(screen.getByRole("button", { name: "资源库" }));
    expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "216px" });
    const handle = screen.getByRole("separator", { name: "调整模块侧栏宽度" });
    fireEvent.mouseDown(handle, { clientX: 216 });
    fireEvent.mouseUp(document, { clientX: 300 });
    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "300px" });
    fireEvent.keyDown(
      screen.getByRole("separator", { name: "调整模块侧栏宽度" }),
      { key: "ArrowRight" },
    );
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "312px" });
    await user.click(screen.getByRole("button", { name: "用量统计" }));
    expect(
      screen.queryByRole("separator", { name: "调整模块侧栏宽度" }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "资源库" }));
    expect(frame).toHaveStyle({ "--shell-sidebar-active-width": "312px" });
  });

  it("详情侧栏支持键盘切换原四个功能，并保持唯一选中入口", async () => {
    const user = userEvent.setup();
    renderLayout({ initialEntry: "/wakers/waker-6" });

    const menu = await screen.findByRole("navigation", {
      name: "当前伙伴功能",
    });
    const overview = within(menu).getByRole("link", { name: "概览" });
    expect(overview).toHaveAttribute("aria-current", "page");

    for (const [label, href] of [
      ["项目", "/wakers/waker-6/projects"],
      ["记忆", "/wakers/waker-6/memory"],
      ["资源", "/wakers/waker-6/resources"],
      ["概览", "/wakers/waker-6"],
    ]) {
      const link = within(menu).getByRole("link", { name: label });
      expect(link).toHaveAttribute("href", href);
      link.focus();
      await user.keyboard("{Enter}");
      expect(link).toHaveAttribute("aria-current", "page");
      expect(
        within(menu)
          .getAllByRole("link")
          .filter((item) => item.getAttribute("aria-current") === "page"),
      ).toEqual([link]);
    }
    expect(screen.getByText("主内容")).toBeVisible();
  });

  it.each([
    ["zh-CN", "默认"],
    ["en-US", "Default"],
  ])("%s 将不同临时路径合并为一个可折叠分组", async (language, label) => {
    await i18n.changeLanguage(language);
    try {
      const user = userEvent.setup();
      renderLayout();
      await user.click(screen.getByRole("button", { name: "查看历史会话" }));
      const group = await screen.findByRole("button", { name: label });
      expect(screen.getAllByRole("button", { name: label })).toHaveLength(1);
      expect(screen.getByText("临时会话甲")).toBeVisible();
      expect(screen.getByText("临时会话乙")).toBeVisible();
      expect(screen.getByText("临时会话")).toBeVisible();
      await user.click(group);
      expect(screen.queryByText("临时会话甲")).toBeNull();
      expect(screen.queryByText("临时会话乙")).toBeNull();
      expect(screen.queryByText("临时会话")).toBeNull();
      expect(screen.getByText("项目会话")).toBeVisible();
    } finally {
      await i18n.changeLanguage("zh-CN");
    }
  });

  it("伙伴菜单不包含会话，并从右下角浮层按来源查看会话", async () => {
    const user = userEvent.setup();
    renderLayout({ initialEntry: "/wakers/waker-6" });

    const menu = await screen.findByRole("navigation", {
      name: "当前伙伴功能",
    });
    expect(
      within(menu)
        .getAllByRole("link")
        .map((item) => item.getAttribute("aria-label")),
    ).toEqual(["概览", "项目", "记忆", "资源"]);
    expect(within(menu).queryByText("伙伴 6")).toBeNull();
    expect(within(menu).queryByText("会话")).toBeNull();
    expect(screen.queryByText("项目会话")).toBeNull();
    await user.click(screen.getByRole("button", { name: "查看历史会话" }));
    const historyPanel = screen.getByLabelText("历史会话");
    expect(historyPanel).toBeVisible();
    expect(
      within(historyPanel).getByRole("heading", { name: "会话列表" }),
    ).toBeVisible();
    expect(historyPanel.querySelector("[data-chat-history-panel]")).toHaveClass(
      "h-80",
    );
    expect(historyPanel.querySelector("[data-chat-history-list]")).toHaveClass(
      "overflow-y-auto",
    );
    expect(
      within(historyPanel).getByRole("button", { name: "新建会话" }),
    ).toBeVisible();
    expect(
      within(historyPanel).getByRole("button", { name: "新建会话" })
        .textContent,
    ).toBe("");
    expect(
      within(screen.getByLabelText("历史会话")).queryByText("伙伴 6"),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "伙伴 6，工作中" })).toBeNull();
    expect(screen.queryByRole("button", { name: "置顶" })).toBeNull();
    const taskListLink = await screen.findByRole("link", { name: "对话任务" });
    expect(taskListLink).toHaveAttribute("href", "/wakers/waker-6/chats");
    expect(taskListLink.textContent).toBe("");
    expect(taskListLink.querySelector(".lucide-table-2")).not.toBeNull();
    expect(taskListLink).toHaveClass(
      "opacity-0",
      "group-hover/group:opacity-100",
    );
    const projectGroup = await screen.findByRole("button", { name: "项目甲" });
    expect(projectGroup).toBeVisible();
    expect(screen.getByRole("button", { name: "默认" })).toBeVisible();
    expect(screen.getAllByText("项目会话")).toHaveLength(1);
    expect(screen.getByText("临时会话")).toBeVisible();
    expect(
      screen.getByText("项目会话").parentElement?.parentElement?.parentElement,
    ).toHaveClass("pl-5");
    for (const menuButton of screen.getAllByRole("button", {
      name: "会话“项目会话”操作",
    })) {
      expect(menuButton).toHaveClass("hover:bg-transparent");
    }
    expect(projectGroup.querySelector(".lucide-chevron-down")).toHaveClass(
      "opacity-0",
    );

    await user.click(projectGroup);

    expect(projectGroup).toHaveAttribute("aria-expanded", "false");
    const collapsedChevron = projectGroup.querySelector(
      ".lucide-chevron-right",
    );
    expect(collapsedChevron).toHaveClass(
      "opacity-0",
      "group-hover/group:opacity-100",
      "group-focus-visible/toggle:opacity-100",
    );
    expect(collapsedChevron).not.toHaveClass(
      "group-focus-within/group:opacity-100",
    );
  });

  it("进入具体会话后只保留一个会话选中态", async () => {
    const user = userEvent.setup();
    renderLayout({
      activeChatId: "chat-project",
      contextSidebarHidden: true,
      initialEntry: "/chats/chat-project?wakerId=waker-6",
    });

    expect(
      screen.queryByRole("navigation", { name: "当前伙伴功能" }),
    ).toBeNull();

    await user.click(screen.getByRole("button", { name: "查看历史会话" }));

    const projectChatRows = await waitFor(() =>
      screen.findAllByRole("link", {
        name: /项目会话/,
      }),
    );
    expect(
      projectChatRows.filter((link) =>
        link.parentElement?.classList.contains("bg-muted"),
      ),
    ).toHaveLength(1);
    for (const link of projectChatRows) {
      expect(link.parentElement).not.toHaveClass("focus-within:bg-muted");
    }
  });
});

it.each([
  { waitingElicitation: false, label: "待审批", glyph: "lucide-shield-check" },
  {
    waitingElicitation: true,
    label: "待输入",
    glyph: "lucide-message-circle-question-mark",
  },
])("伙伴历史用图标区分$label", async ({ waitingElicitation, label, glyph }) => {
  history.waitingApproval = true;
  history.waitingElicitation = waitingElicitation;
  const user = userEvent.setup();
  const view = renderLayout();
  try {
    await user.click(screen.getByRole("button", { name: "查看历史会话" }));
    const link = await screen.findByRole("link", { name: /项目会话/ });
    expect(
      within(link).getByRole("img", { name: label }).querySelector("svg"),
    ).toHaveClass(glyph);
    expect(link.querySelector("[data-history-status]")).toBeNull();
  } finally {
    view.unmount();
    history.waitingApproval = false;
    history.waitingElicitation = false;
  }
});

it.each([
  {
    cause: "同一伙伴已提交的活动变化",
    type: "entities.activity.changed",
    resource: { kind: "waker", id: "waker-6" },
  },
  {
    cause: "连接就绪",
    type: "entities.ready",
    resource: { kind: "global" },
  },
])("伙伴历史在$cause后刷新，终态清除运行状态点", async ({ type, resource }) => {
  history.executionId = "execution";
  const user = userEvent.setup();
  const view = renderLayout();
  try {
    await user.click(screen.getByRole("button", { name: "查看历史会话" }));
    const link = await screen.findByRole("link", { name: /项目会话/ });
    const dot = () => link.querySelector("[data-history-status]");
    expect(dot()).toHaveAttribute("data-history-status", "running");
    expect(dot()).toHaveClass("bg-success");
    expect(link.querySelector("svg")).toBeNull();
    history.executionId = undefined;
    act(() => {
      for (const observer of history.observers)
        observer.onEvent({
          protocolVersion: 1,
          epoch: "core",
          connectionId: "connection",
          deliverySequence: 1,
          topic: "entities",
          type,
          resource,
          eventId: "updated",
          payload: { kind: "chat", id: "chat-project" },
        });
    });
    await waitFor(() =>
      expect(dot()).toHaveAttribute("data-history-status", "idle"),
    );
  } finally {
    view.unmount();
    history.executionId = undefined;
  }
});
