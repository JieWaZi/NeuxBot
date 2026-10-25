import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState, type ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type * as ChatModule from "@/modules/chat";
import type { Waker } from "@/modules/waker";
import { FloatingChat } from "@/modules/chat/components/floating-chat";
import { SettingsContextSidebar } from "@/modules/settings/components/settings-context-sidebar";
import "@/shared/i18n";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import { PartnerNavigationLayout } from "./partner-navigation-layout";
import type { PartnerNavigationPreferences } from "./partner-navigation-preferences";

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
                projectId: "project-1",
                projectName: "项目甲",
                title: "项目会话",
                updatedAt: Date.now(),
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
  teamContext,
}: {
  activeChatId?: string;
  contextSidebarHidden?: boolean;
  floatingChat?: boolean;
  selectedPartnerId?: string;
  sceneContentFlush?: boolean;
  settingsOpen?: boolean;
  settingsSidebar?: ReactNode;
  teamContext?: { channelId: string; threadId?: string };
}) {
  const [preferences, setPreferences] = useState(initialPreferences);
  const [sceneNavigationVisible, setSceneNavigationVisible] =
    useState(settingsOpen);
  const [fullView, setFullView] = useState(floatingChat);
  return (
    <PartnerNavigationLayout
      activeChatId={activeChatId}
      channels={[]}
      contextSidebarHidden={contextSidebarHidden}
      onManageEntities={() => undefined}
      onOpenPartnerDetail={() => undefined}
      onOpenAutomaticTasks={() => undefined}
      onOpenSettings={() => undefined}
      onPreferencesChange={setPreferences}
      onSceneNavigationVisibleChange={setSceneNavigationVisible}
      onSelectPartner={() => undefined}
      onSelectTeam={() => undefined}
      preferences={preferences}
      sceneNavigationVisible={sceneNavigationVisible}
      sceneContentFlush={sceneContentFlush}
      selectedPartnerId={selectedPartnerId}
      selectedRailKey={`waker:${selectedPartnerId}`}
      settingsOpen={settingsOpen}
      settingsSidebar={settingsSidebar}
      teamContext={teamContext}
      wakers={wakers}
    >
      <main>
        主内容
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
            teamContext={teamContext}
          />
        </WorkspaceProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("PartnerNavigationLayout", () => {
  it("侧栏加号每次打开所选伙伴的独立新会话草稿", async () => {
    const user = userEvent.setup();
    renderLayout();
    await user.click(screen.getByRole("button", { name: "展开全局侧栏" }));
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

  it("完整视图沿用右下角唯一入口，展开和收起全局导航都不叠加按钮", async () => {
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
    await user.click(screen.getByRole("button", { name: "展开全局侧栏" }));
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
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
    expect(
      screen.getByRole("button", { name: "打开身份切换器" }),
    ).toBeVisible();
    expect(
      screen.getByRole("region", { name: "场景工作区" }).children[1],
    ).toHaveClass("grid-cols-[minmax(0,1fr)]");
  });

  it("收起左栏时先选择伙伴再查看会话，展开后改用会话入口", async () => {
    const user = userEvent.setup();
    renderLayout();

    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
    const floatingTrigger = screen.getByRole("button", {
      name: "打开身份切换器，当前：伙伴 6",
    });
    expect(floatingTrigger).toHaveAttribute("data-activity-state", "running");
    expect(floatingTrigger.parentElement).toHaveClass("right-0", "bottom-0");
    expect(floatingTrigger.querySelector(".lucide-users-round")).not.toBeNull();
    expect(floatingTrigger.querySelector("img")).toBeNull();
    expect(within(floatingTrigger).getByText("选择伙伴")).toHaveClass(
      "opacity-0",
    );

    await user.click(floatingTrigger);

    const rail = await screen.findByRole("navigation", { name: "伙伴" });
    expect(within(rail).getByRole("button", { name: "伙伴 1" })).toBeVisible();
    expect(
      within(rail).getByRole("button", { name: "伙伴 5，1 个会话需要处理" }),
    ).toBeVisible();
    expect(
      await within(rail).findByRole("button", {
        name: "伙伴 6，4 个会话正在运行",
      }),
    ).toHaveAttribute("aria-current", "page");
    expect(rail.querySelector(".overflow-y-auto")).not.toBeNull();
    expect(
      within(rail).queryByRole("button", { name: "展开全部伙伴" }),
    ).toBeNull();

    await user.click(
      within(rail).getByRole("button", {
        name: "伙伴 6，4 个会话正在运行",
      }),
    );
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
    expect(
      within(screen.getByLabelText("历史会话")).queryByText("伙伴 6"),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "返回身份列表" })).toBeVisible();
    expect(
      within(screen.getByLabelText("历史会话")).getByRole("button", {
        name: "新建会话",
      }).textContent,
    ).toBe("");
    expect(await screen.findByText("项目会话")).toBeVisible();

    const expandButton = screen.getByRole("button", {
      name: "展开全局侧栏",
    });
    expect(expandButton.closest("[data-desktop-drag-region]")).toBeNull();

    await user.click(expandButton);

    expect(
      screen.queryByRole("button", {
        name: "打开身份切换器，当前：伙伴 6",
      }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "查看历史会话" })).toBeVisible();
    expect(
      within(screen.getByRole("navigation", { name: "伙伴" })).getByRole(
        "button",
        { name: "伙伴 5，1 个会话需要处理" },
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("navigation", { name: "当前伙伴功能" }),
    ).toBeVisible();
  });

  it("场景顶栏、详情 Dock 和主内容共用一个场景容器", async () => {
    const user = userEvent.setup();
    renderLayout();

    await user.click(screen.getByRole("button", { name: "展开全局侧栏" }));
    const navigationToolbar = screen.getByRole("toolbar", {
      name: "导航控制",
    });
    expect(
      within(navigationToolbar).getByRole("button", { name: "返回" }),
    ).toBeDisabled();
    expect(
      within(navigationToolbar).getByRole("button", { name: "前进" }),
    ).toBeDisabled();

    const sceneSurface = screen.getByRole("region", { name: "场景工作区" });

    expect(sceneSurface).toContainElement(screen.getByText("主内容"));
    expect(sceneSurface).toContainElement(
      await screen.findByRole("navigation", { name: "当前伙伴功能" }),
    );
    expect(
      screen.queryByRole("separator", { name: "调整上下文侧栏宽度" }),
    ).toBeNull();
    expect(sceneSurface).toHaveClass("rounded-l-3xl", "overflow-hidden");
    expect(sceneSurface.parentElement).toHaveStyle({
      "--shell-navigation-leading-inset": "8px",
    });
  });

  it("聊天场景的顶栏与正文共用左边界", () => {
    renderLayout({ sceneContentFlush: true });
    const sceneSurface = screen.getByRole("region", { name: "场景工作区" });
    expect(sceneSurface.children[1]).toHaveClass("pl-0");
    expect(sceneSurface.children[1]).not.toHaveClass("pl-4");
  });

  it("设置 Scene 的前进后退只切换左栏导航，不改变 Scene 内容", async () => {
    const user = userEvent.setup();
    renderLayout({
      settingsOpen: true,
      settingsSidebar: (
        <SettingsContextSidebar
          activeSection="general"
          onSelect={() => undefined}
        />
      ),
    });

    const settingsMenu = screen.getByRole("navigation", { name: "设置菜单" });
    expect(settingsMenu).toBeVisible();
    expect(settingsMenu.closest("aside")).not.toHaveClass("border-r");
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
    expect(
      screen.queryByRole("separator", { name: "调整上下文侧栏宽度" }),
    ).toBeNull();
    const toolbar = screen.getByRole("toolbar", { name: "导航控制" });
    expect(toolbar).toHaveTextContent("设置");
    expect(toolbar).not.toHaveClass("border-b");

    const backButton = within(toolbar).getByRole("button", { name: "返回" });
    const forwardButton = within(toolbar).getByRole("button", {
      name: "前进",
    });
    expect(backButton).toBeEnabled();
    expect(forwardButton).toBeDisabled();

    await user.click(backButton);

    expect(screen.queryByRole("navigation", { name: "设置菜单" })).toBeNull();
    expect(screen.getByRole("navigation", { name: "伙伴" })).toBeVisible();
    expect(screen.getByText("主内容")).toBeVisible();
    expect(toolbar).not.toHaveTextContent("设置");
    expect(backButton).toBeDisabled();
    expect(forwardButton).toBeEnabled();

    await user.click(forwardButton);

    expect(screen.getByRole("navigation", { name: "设置菜单" })).toBeVisible();
    expect(screen.queryByRole("navigation", { name: "伙伴" })).toBeNull();
    expect(toolbar).toHaveTextContent("设置");
    expect(backButton).toBeEnabled();
    expect(forwardButton).toBeDisabled();
  });

  it("详情 Dock 支持键盘切换四个功能，并保持唯一选中入口", async () => {
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

  it("伙伴菜单不包含会话，并从右下角浮层按来源查看会话", async () => {
    const user = userEvent.setup();
    renderLayout();

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
    await user.click(screen.getByRole("button", { name: "展开全局侧栏" }));
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
    expect(
      screen.getByRole("button", { name: "伙伴 6，4 个会话正在运行" }),
    ).not.toHaveAttribute("aria-expanded");
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
    expect(screen.getByRole("button", { name: "临时目录" })).toBeVisible();
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

    await user.click(screen.getByRole("button", { name: "展开全局侧栏" }));
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
