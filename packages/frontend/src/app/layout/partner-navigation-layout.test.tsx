import type { DragEndEvent } from "@dnd-kit/core";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AssistantRuntime } from "@assistant-ui/react";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  memo,
  useEffect,
  useId,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type * as ChatModule from "@/modules/chat";
import type * as WakerModule from "@/modules/waker";
import type { Waker } from "@/modules/waker";
import { FloatingChat } from "@/modules/chat/components/floating-chat";
import { ChatWorkspaceSurface } from "@/modules/chat/components/chat-workspace-surface";
import type { ChatRightPanel } from "@/modules/chat/components/chat-right-panel-host";
import { conversationKeyOf } from "@/modules/chat/model/conversation-identity";
import { WorkbenchProvider } from "@/modules/chat/workbench/workbench-store";
import { SettingsContextSidebar } from "@/modules/settings/components/settings-context-sidebar";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { client as apiClient } from "@/shared/api/generated/client.gen";
import { i18n } from "@/shared/i18n";
import { configurePlatform, platform } from "@/shared/platform/provider";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  useSceneWorkspace,
  WorkspaceProvider,
} from "@/shared/workspace/workspace-context";
import { ResourcesContextSidebar } from "./resources-context-sidebar";
import { PartnerNavigationLayout } from "./partner-navigation-layout";
import type { PartnerNavigationPreferences } from "./partner-navigation-preferences";

const history = vi.hoisted(() => ({
  executionId: undefined as string | undefined,
  waitingApproval: false,
  waitingElicitation: false,
  nextPage: false,
  observers: new Set<GlobalEventObserver>(),
}));
const duplicateWaker = vi.hoisted(() => vi.fn());
const removeWaker = vi.hoisted(() => vi.fn(async () => ({})));
const navigationGroups = vi.hoisted(() => ({
  groups: [] as { name: string; wakerIds: string[] }[],
  ungrouped: [] as string[],
  finishDrag: undefined as ((event: DragEndEvent) => void) | undefined,
  queryKey: ["waker-navigation-groups"],
  move: vi.fn(),
  place: vi.fn(),
  import: vi.fn(),
}));

beforeEach(() => {
  removeWaker.mockClear();
  navigationGroups.groups = [];
  navigationGroups.ungrouped = [];
  navigationGroups.finishDrag = undefined;
  navigationGroups.place
    .mockReset()
    .mockImplementation(
      async ({
        path,
        body,
      }: {
        path: { wakerId: string };
        body: { targetWakerId: string; placement: string };
      }) => {
        const listFor = (id: string) =>
          navigationGroups.groups.find((group) => group.wakerIds.includes(id))
            ?.wakerIds ?? navigationGroups.ungrouped;
        const source = listFor(path.wakerId);
        const target = listFor(body.targetWakerId);
        source.splice(source.indexOf(path.wakerId), 1);
        target.splice(
          target.indexOf(body.targetWakerId) +
            Number(body.placement === "after"),
          0,
          path.wakerId,
        );
      },
    );
  navigationGroups.move
    .mockReset()
    .mockImplementation(
      async ({
        path,
        body,
      }: {
        path: { wakerId: string };
        body: { name: string };
      }) => {
        for (const group of navigationGroups.groups)
          group.wakerIds = group.wakerIds.filter((id) => id !== path.wakerId);
        navigationGroups.ungrouped = navigationGroups.ungrouped.filter(
          (id) => id !== path.wakerId,
        );
        if (!body.name) {
          navigationGroups.ungrouped.push(path.wakerId);
          return;
        }
        const group = navigationGroups.groups.find(
          ({ name }) => name === body.name,
        );
        if (group) group.wakerIds.push(path.wakerId);
        else
          navigationGroups.groups.push({
            name: body.name,
            wakerIds: [path.wakerId],
          });
      },
    );
  navigationGroups.import
    .mockReset()
    .mockImplementation(
      async ({
        body,
      }: {
        body: { groups: typeof navigationGroups.groups; wakerOrder: string[] };
      }) => {
        if (navigationGroups.groups.length) return;
        navigationGroups.groups = structuredClone(body.groups);
        navigationGroups.ungrouped = body.wakerOrder.filter(
          (id) => !body.groups.some((group) => group.wakerIds.includes(id)),
        );
      },
    );
});

vi.mock("@/modules/chat/api/work-ticket-activity", () => ({
  useWorkTicketActivity: () => ({ data: [] }),
}));

vi.mock(import("@/modules/waker/api/waker-queries"), async (original) => ({
  ...(await original()),
  deleteWakerMutation: () => ({ mutationFn: removeWaker }),
}));

vi.mock(import("@dnd-kit/core"), async (importOriginal) => {
  const core = await importOriginal();
  return {
    ...core,
    DndContext: memo(function CaptureSidebarDrop(
      props: ComponentProps<typeof core.DndContext>,
    ) {
      const { onDragStart, onDragCancel, onDragEnd, sensors } = props;
      useEffect(() => {
        if (!onDragStart || !onDragCancel || !sensors?.length) return;
        navigationGroups.finishDrag = onDragEnd;
        return () => {
          if (navigationGroups.finishDrag === onDragEnd)
            navigationGroups.finishDrag = undefined;
        };
      }, [onDragStart, onDragCancel, onDragEnd, sensors]);
      return <core.DndContext {...props} />;
    }),
  };
});

vi.mock("@/modules/chat/components/chat-thread", () => ({
  ChatThread: () => null,
}));

vi.mock("@/modules/waker", async (importOriginal) => ({
  ...(await importOriginal<typeof WakerModule>()),
  duplicateWakerMutation: () => ({ mutationFn: duplicateWaker }),
  listWakerNavigationGroupsQueryKey: () => navigationGroups.queryKey,
  listWakerNavigationGroupsOptions: () => ({
    queryKey: navigationGroups.queryKey,
    queryFn: async () => ({
      groups: structuredClone(navigationGroups.groups),
      ungroupedWakerIds: [...navigationGroups.ungrouped],
    }),
  }),
  moveWakerToNavigationGroupMutation: () => ({
    mutationFn: navigationGroups.move,
  }),
  placeWakerInNavigationMutation: () => ({
    mutationFn: navigationGroups.place,
  }),
  importWakerNavigationGroupsMutation: () => ({
    mutationFn: navigationGroups.import,
  }),
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
  chatListInfiniteOptions: (wakerId: string, query?: string) => ({
    getNextPageParam: (page: { nextCursor?: string }) => page.nextCursor,
    initialPageParam: "",
    queryFn: async ({ pageParam }: { pageParam: string }) => ({
      nextCursor: !pageParam && history.nextPage ? "older" : undefined,
      resources:
        query === "服务端深处的会话" && wakerId === "waker-6"
          ? [{ id: "chat-remote", title: query, wakerId }]
          : query === "项目会话" && wakerId === "waker-6"
            ? [{ id: "chat-project", title: query, wakerId }]
            : query
              ? []
              : pageParam === "older"
                ? [{ id: "chat-older", title: "更早的会话", wakerId }]
                : wakerId === "waker-6"
                  ? [
                      {
                        id: "chat-project",
                        activeExecutionId: history.executionId,
                        waitingApproval: history.waitingApproval,
                        waitingElicitation: history.waitingElicitation,
                        lastRuntimeId: "codex",
                        lastModel: "gpt-5.6",
                        projectId: "project-1",
                        projectName: "项目甲",
                        title: "项目会话",
                        updatedAt: Date.now(),
                        wakerId,
                      },
                      {
                        additionalWorkingDirectories: [
                          "/managed/chat-a/workspace",
                        ],
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
    queryKey: ["chats", wakerId, query],
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
  deleteChannelMutation: () => ({ mutationFn: vi.fn() }),
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
  teamThreadsInfiniteOptions: (channelId: string, query?: string) => ({
    queryFn: async () => ({
      resources: [
        {
          id: "thread-project",
          leadRuntimeId: "claude",
          leadModel: "claude-sonnet",
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
      ].filter((thread) => !query || thread.title.includes(query)),
    }),
    queryKey: ["channel-threads", channelId, query],
    initialPageParam: 1,
    getNextPageParam: () => undefined,
  }),
  teamQueryKeys: {
    channels: () => ["channels"],
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
  globalSidebarCollapsed: false,
  lastTeamThreads: {},
  railExpanded: false,
  railOrder: [],
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

function LayoutChatWorkbench() {
  const scopeKey = useId();
  const workspace = useSceneWorkspace(scopeKey);
  const [panel, setPanel] = useState<ChatRightPanel>();
  return (
    <ChatWorkspaceSurface
      activeRightPanel={panel}
      workspace={workspace}
      onAddCodeReference={() => {}}
      onDismissRightPanel={() => setPanel(undefined)}
      onOpenRightPanel={() => setPanel("workspace")}
      adapter={{
        identity: {
          scopeKey,
          key: conversationKeyOf(scopeKey),
          workspaceDirectory: "",
        },
        snapshot: {
          isRunning: false,
          pendingApprovalCount: 0,
          pendingElicitationCount: 0,
          streamState: "closed",
        },
        thread: {
          runtime: {} as AssistantRuntime,
          messages: [],
          wakerName: "伙伴 6",
          emptyState: null,
          sendFailure: { kind: "none" },
          hasEarlierMessages: false,
          isHistoryLoading: false,
          isLoadingEarlier: false,
          onLoadEarlier: async () => {},
        },
      }}
      showSummary={false}
    />
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
  channels,
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
  channels?: Parameters<typeof PartnerNavigationLayout>[0]["channels"];
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
      channels={channels ?? []}
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
      selectedRailKey={
        teamContext
          ? `team:${teamContext.channelId}`
          : `waker:${selectedPartnerId}`
      }
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
        {!fullView &&
        !activeSettings &&
        (teamContext ||
          location.pathname === "/chat" ||
          location.pathname.startsWith("/chats/")) ? (
          <LayoutChatWorkbench />
        ) : null}
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
  reactStrictMode = false,
  sceneContentFlush,
  selectedPartnerId,
  settingsOpen,
  settingsSidebar,
  renderTitlebar,
  navigationPreferences,
  sidebarToolbar,
  notificationBell,
  teamContext,
  channels,
}: {
  activeChatId?: string;
  contextSidebarHidden?: boolean;
  floatingChat?: boolean;
  initialEntry?: string;
  reactStrictMode?: boolean;
  sceneContentFlush?: boolean;
  selectedPartnerId?: string;
  settingsOpen?: boolean;
  settingsSidebar?: ReactNode;
  renderTitlebar?: (navigationToggle: ReactNode) => ReactNode;
  navigationPreferences?: Partial<PartnerNavigationPreferences>;
  sidebarToolbar?: ReactNode;
  notificationBell?: ReactNode;
  teamContext?: { channelId: string; threadId?: string };
  channels?: Parameters<typeof PartnerNavigationLayout>[0]["channels"];
} = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <WorkspaceProvider>
          <WorkbenchProvider>
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
              channels={channels}
            />
          </WorkbenchProvider>
        </WorkspaceProvider>
      </MemoryRouter>
    </QueryClientProvider>,
    { wrapper: TooltipProvider, reactStrictMode },
  );
}

describe("PartnerNavigationLayout", () => {
  it.each([
    ["waker-6", "/wakers"],
    ["waker-2", "/chat/main/waker/waker-6"],
  ])(
    "主会话打开时从侧栏删除 %s，按当前路由归属恢复导航",
    async (deletedId, destination) => {
      renderLayout({ initialEntry: "/chat/main/waker/waker-6" });
      const user = userEvent.setup();
      const name = deletedId === "waker-6" ? "伙伴 6" : "伙伴 2";
      fireEvent.contextMenu(
        await screen.findByRole("button", { name: new RegExp("^" + name) }),
      );
      await user.click(screen.getByRole("menuitem", { name: "删除伙伴" }));
      const dialog = screen.getByRole("dialog", { name: "删除伙伴" });
      await user.type(within(dialog).getByRole("textbox"), name);
      await user.click(
        within(dialog).getByRole("button", { name: "删除伙伴" }),
      );
      expect(removeWaker).toHaveBeenCalledWith(
        expect.objectContaining({ path: { wakerId: deletedId } }),
        expect.anything(),
      );
      await waitFor(() => expect(dialog).not.toBeInTheDocument());
      await waitFor(() =>
        expect(screen.getByTestId("current-route")).toHaveTextContent(
          destination,
        ),
      );
    },
  );

  it.each([true, false])(
    "伙伴右键将最新合格发言标为未读（主会话有公开发言：%s）",
    async (publicMessage) => {
      const base = "http://workbench-reading.test";
      const main = `${base}/api/v1/main-conversations/waker/waker-1`;
      const latest = `${base}/api/v1/chats/recent-chat`;
      const chats = vi.fn();
      const writes = vi.fn();
      const state = {
        complete: true,
        holdUnread: false,
        throughSequence: 10,
        unreadGeneration: 7,
      };
      const server = setupServer(
        http.get(main + "/reading", () =>
          HttpResponse.json({
            ...state,
            conversationId: "public-conversation",
            latestReadableMessageID: publicMessage
              ? "public-message"
              : undefined,
          }),
        ),
        http.post(main + "/unread", async ({ request }) => {
          writes("main", await request.json());
          return HttpResponse.json({});
        }),
        http.get(`${base}/api/v1/chats`, () => {
          chats();
          return HttpResponse.json({ resources: [{ id: "recent-chat" }] });
        }),
        http.get(latest + "/reading", () =>
          HttpResponse.json({
            ...state,
            chatId: "recent-chat",
            latestReadableMessageID: "chat-message",
          }),
        ),
        http.post(latest + "/unread", async ({ request }) => {
          writes("chat", await request.json());
          return HttpResponse.json({});
        }),
      );
      const config = apiClient.getConfig();
      apiClient.setConfig({ baseUrl: base });
      server.listen({ onUnhandledRequest: "bypass" });
      try {
        renderLayout();
        fireEvent.contextMenu(screen.getByRole("button", { name: "伙伴 1" }));
        await userEvent.click(
          screen.getByRole("menuitem", { name: "标为未读" }),
        );
        await waitFor(() =>
          expect(writes).toHaveBeenCalledExactlyOnceWith(
            publicMessage ? "main" : "chat",
            {
              fromMessageID: publicMessage ? "public-message" : "chat-message",
              observedUnreadGeneration: 7,
            },
          ),
        );
        expect(chats).toHaveBeenCalledTimes(publicMessage ? 0 : 1);
      } finally {
        server.close();
        apiClient.setConfig(config);
      }
    },
  );

  it("伙伴右键菜单可新建分组、移入已有分组、编辑和打开删除确认", async () => {
    navigationGroups.groups = [{ name: "已有分组", wakerIds: ["waker-2"] }];
    navigationGroups.ungrouped = [
      "waker-1",
      "waker-3",
      "waker-4",
      "waker-5",
      "waker-6",
    ];
    renderLayout();
    await screen.findByRole("heading", { name: "已有分组" });
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
    const heading = await screen.findByRole("heading", { name: "研究" });
    expect(heading.nextElementSibling).toContainElement(row);

    fireEvent.contextMenu(row);
    await openMove();
    await user.click(screen.getByRole("menuitem", { name: "已有分组" }));
    await waitFor(() => expect(heading).not.toBeInTheDocument());
    const existing = screen.getByRole("heading", { name: "已有分组" });
    expect(existing.nextElementSibling).toContainElement(
      screen.getByRole("button", { name: "伙伴 2" }),
    );
    expect(existing.nextElementSibling?.nextElementSibling).toContainElement(
      row,
    );

    fireEvent.contextMenu(row);
    await openMove();
    await user.click(screen.getByRole("menuitem", { name: "未分组" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "已有分组" })
          .previousElementSibling,
      ).toContainElement(row),
    );
    expect(
      navigationGroups.move.mock.calls.map(([request]) => request.body.name),
    ).toEqual(["研究", "已有分组", ""]);

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

  it("按 Core 顺序显示同组排序和跨组拖放的落点，重新挂载后保留", async () => {
    navigationGroups.groups = [
      { name: "研究", wakerIds: ["waker-1", "waker-2"] },
      { name: "开发", wakerIds: ["waker-3", "waker-4"] },
    ];
    navigationGroups.ungrouped = ["waker-5", "waker-6"];
    const mounted = renderLayout();
    await screen.findByRole("heading", { name: "研究" });
    const orderedPartners = () =>
      screen
        .getAllByRole("button", { name: /^伙伴 [1-6]/ })
        .map(
          (element) =>
            element.getAttribute("aria-label")?.match(/^伙伴 [1-6]/)?.[0],
        );
    const finishDrag = (source: number, target: number, offset?: number) => {
      const activatorEvent =
        offset === undefined
          ? new KeyboardEvent("keydown")
          : new MouseEvent("mousedown");
      act(() =>
        navigationGroups.finishDrag?.({
          activatorEvent,
          active: {
            id: `waker:waker-${source}`,
            rect: {
              current: {
                translated:
                  offset === undefined ? null : { top: offset, height: 20 },
              },
            },
          },
          over: { id: `waker:waker-${target}`, rect: { top: 0, height: 20 } },
        } as unknown as DragEndEvent),
      );
    };

    expect(navigationGroups.finishDrag).toBeTypeOf("function");
    finishDrag(2, 1);
    await waitFor(() =>
      expect(navigationGroups.place).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { wakerId: "waker-2" },
          body: { targetWakerId: "waker-1", placement: "before" },
        }),
        expect.anything(),
      ),
    );
    await waitFor(() =>
      expect(orderedPartners()).toEqual([
        "伙伴 5",
        "伙伴 6",
        "伙伴 2",
        "伙伴 1",
        "伙伴 3",
        "伙伴 4",
      ]),
    );
    await waitFor(() =>
      expect(navigationGroups.finishDrag).toBeTypeOf("function"),
    );
    // 拖向下方的目标组，但落点在第一行前半部分，实际插在第一行之前。
    finishDrag(1, 3, -4);
    const expected = [
      "伙伴 5",
      "伙伴 6",
      "伙伴 2",
      "伙伴 1",
      "伙伴 3",
      "伙伴 4",
    ];
    const development = () =>
      screen.getByRole("heading", { name: "开发" }).nextElementSibling;
    await waitFor(() =>
      expect(development()).toContainElement(
        screen.getByRole("button", { name: "伙伴 1" }),
      ),
    );
    expect(navigationGroups.place).toHaveBeenLastCalledWith(
      expect.objectContaining({
        path: { wakerId: "waker-1" },
        body: { targetWakerId: "waker-3", placement: "before" },
      }),
      expect.anything(),
    );
    expect(orderedPartners()).toEqual(expected);
    await waitFor(() =>
      expect(navigationGroups.finishDrag).toBeTypeOf("function"),
    );
    mounted.unmount();
    renderLayout();
    await screen.findByRole("heading", { name: "开发" });
    expect(development()).toContainElement(
      screen.getByRole("button", { name: "伙伴 1" }),
    );
    expect(orderedPartners()).toEqual(expected);
  });

  it("StrictMode 下旧分组导入只执行一次，成功后恢复移动和复制", async () => {
    const groups = [{ name: "已有分组", wakerIds: ["waker-2"] }];
    renderLayout({
      reactStrictMode: true,
      navigationPreferences: {
        wakerGroups: groups,
        railOrder: ["waker:waker-5", "waker:waker-1"],
      },
    });
    expect(
      await screen.findByRole("heading", { name: "已有分组" }),
    ).toBeVisible();
    expect(navigationGroups.import).toHaveBeenCalledWith(
      expect.objectContaining({
        body: { groups, wakerOrder: ["waker-5", "waker-1"] },
      }),
      expect.anything(),
    );
    expect(navigationGroups.import).toHaveBeenCalledTimes(1);
    const savedPreferences = screen.getByTestId("navigation-preferences");
    await waitFor(() =>
      expect(savedPreferences).not.toHaveTextContent("wakerGroups"),
    );
    expect(savedPreferences).not.toHaveTextContent("waker:waker-5");
    fireEvent.contextMenu(screen.getByRole("button", { name: "伙伴 1" }));
    expect(
      screen.getByRole("menuitem", { name: "移动到" }),
    ).not.toHaveAttribute("data-disabled");
    expect(
      screen.getByRole("menuitem", { name: "复制伙伴" }),
    ).not.toHaveAttribute("data-disabled");
  });

  it("导入旧分组失败时可在当前页面重试并恢复分组操作", async () => {
    navigationGroups.import.mockRejectedValueOnce(new Error("import failed"));
    const groups = [{ name: "已有分组", wakerIds: ["waker-1"] }];
    renderLayout({ navigationPreferences: { wakerGroups: groups } });
    const retry = await screen.findByRole("button", { name: "重试" });
    expect(screen.getByTestId("navigation-preferences")).toHaveTextContent(
      "已有分组",
    );
    const user = userEvent.setup();
    fireEvent.contextMenu(screen.getByRole("button", { name: "伙伴 1" }));
    expect(screen.getByRole("menuitem", { name: "移动到" })).toHaveAttribute(
      "data-disabled",
    );
    expect(screen.getByRole("menuitem", { name: "复制伙伴" })).toHaveAttribute(
      "data-disabled",
    );
    await user.keyboard("{Escape}");
    await user.click(retry);
    expect(
      await screen.findByRole("heading", { name: "已有分组" }),
    ).toBeVisible();
    await waitFor(() =>
      expect(
        screen.getByTestId("navigation-preferences"),
      ).not.toHaveTextContent("wakerGroups"),
    );
    expect(navigationGroups.import).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
  });

  it("拖放后立即呈现落点，并在保存失败时恢复 Core 顺序和原行焦点", async () => {
    navigationGroups.groups = [
      { name: "研究", wakerIds: ["waker-1", "waker-2"] },
      { name: "开发", wakerIds: ["waker-3", "waker-4"] },
    ];
    let rejectPlacement: (reason: Error) => void = () => {};
    const request = new Promise<never>((_, reject) => {
      rejectPlacement = reject;
    });
    navigationGroups.place.mockReturnValueOnce(request);
    renderLayout();
    const sourceGroup = await screen.findByRole("heading", { name: "研究" });
    const targetGroup = screen.getByRole("heading", { name: "开发" });
    const source = screen.getByRole("button", { name: "伙伴 1" });
    const target = screen.getByRole("button", { name: "伙伴 3" });
    source.focus();

    act(() =>
      navigationGroups.finishDrag?.({
        activatorEvent: new MouseEvent("mousedown"),
        active: {
          id: "waker:waker-1",
          rect: { current: { translated: { top: -4, height: 20 } } },
        },
        over: { id: "waker:waker-3", rect: { top: 0, height: 20 } },
      } as unknown as DragEndEvent),
    );
    expect(targetGroup.nextElementSibling).toContainElement(source);
    expect(targetGroup.nextElementSibling?.nextElementSibling).toContainElement(
      target,
    );
    expect(source).toHaveFocus();
    expect(sourceGroup.nextElementSibling).toContainElement(
      screen.getByRole("button", { name: "伙伴 2" }),
    );
    expect(navigationGroups.finishDrag).toBeUndefined();
    expect(source).not.toHaveAttribute("aria-roledescription");
    await waitFor(() =>
      expect(navigationGroups.place).toHaveBeenCalledTimes(1),
    );
    await act(async () => rejectPlacement(new Error("write failed")));
    await waitFor(() =>
      expect(sourceGroup.nextElementSibling).toContainElement(source),
    );
    expect(targetGroup.nextElementSibling).toContainElement(target);
    expect(source).toHaveFocus();
    await waitFor(() =>
      expect(navigationGroups.finishDrag).toBeTypeOf("function"),
    );
    expect(source).toHaveAttribute("aria-roledescription", "sortable");
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

  it("左栏顶栏将历史箭头靠左，侧栏开关和通知靠右", () => {
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
    const toolbar = bell.closest("[data-app-sidebar-toolbar]") as HTMLElement;
    expect(toolbar).toHaveClass("justify-between");
    const back = screen.getByRole("button", { name: "后退" });
    const forward = screen.getByRole("button", { name: "前进" });
    const toggle = screen.getByRole("button", { name: "收起左侧栏" });
    expect(back.parentElement).toBe(toolbar.firstElementChild);
    expect(forward.parentElement).toBe(back.parentElement);
    expect(toggle.parentElement).toBe(toolbar.lastElementChild);
    expect(bell.parentElement).toBe(toggle.parentElement);
    expect(within(toolbar).getAllByRole("button")).toEqual([
      back,
      forward,
      toggle,
      bell,
    ]);
    expect(
      screen.getByRole("navigation", { name: "模块导航" }),
    ).not.toContainElement(bell);
  });

  it("模块栏位于整窗首列，展开的伙伴侧栏提供收起入口", () => {
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
    expect(screen.getByRole("button", { name: "收起左侧栏" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
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

  it("搜索能找到未加载到侧栏的历史会话", async () => {
    const user = userEvent.setup();
    renderLayout();
    expect(screen.queryByText("服务端深处的会话")).not.toBeInTheDocument();
    await user.type(
      screen.getByRole("searchbox", { name: "搜索会话" }),
      "服务端深处的会话",
    );
    expect(await screen.findByText("服务端深处的会话")).toBeVisible();
  });

  it("左侧切换会话列表不切换主内容，并保留会话搜索", async () => {
    const user = userEvent.setup();
    renderLayout();
    const main = screen.getByText("主内容");
    const route = screen.getByTestId("current-route").textContent;
    const search = screen.getByRole("searchbox", { name: "搜索会话" });
    await user.keyboard("{Control>}k{/Control}");
    expect(search).toHaveFocus();
    await user.click(screen.getByRole("radio", { name: "会话" }));
    expect(
      await within(screen.getByRole("region", { name: "会话" })).findByText(
        "项目会话",
      ),
    ).toBeVisible();
    expect(screen.getByText("主内容")).toBe(main);
    expect(screen.getByTestId("current-route")).toHaveTextContent(route ?? "");
    expect(screen.getByRole("searchbox", { name: "搜索会话" })).toBe(search);
    expect(screen.queryByRole("button", { name: "查看历史会话" })).toBeNull();
    await user.click(
      within(screen.getByRole("radiogroup", { name: "左侧栏视图" })).getByRole(
        "radio",
        { name: "伙伴" },
      ),
    );
    expect(screen.getByRole("navigation", { name: "伙伴" })).toBeVisible();
    await user.type(search, "项目会话");
    expect(
      await within(screen.getByRole("region", { name: "会话" })).findByText(
        "项目会话",
      ),
    ).toBeVisible();
    expect(screen.queryByText("临时会话")).toBeNull();
  });

  it("收起隐藏左栏并释放正文空间，键盘展开恢复宽度与原导航", async () => {
    const user = userEvent.setup();
    renderLayout({
      navigationPreferences: { contextSidebarWidth: 300 },
      sidebarToolbar: <button>实体后退</button>,
      notificationBell: <button>通知入口</button>,
    });
    const frame = document.querySelector("[data-app-workspace-frame]");
    const scene = screen.getByRole("region", { name: "场景工作区" });
    const main = screen.getByText("主内容");
    const rail = screen.getByRole("navigation", { name: "模块导航" });
    const entityList = screen.getByRole("radiogroup", { name: "身份列表" });
    const toggle = screen.getByRole("button", { name: "收起左侧栏" });
    const bell = screen.getByRole("button", { name: "通知入口" });
    const panelIcon = toggle.querySelector("svg");
    const sceneClass = scene.className;
    expect(frame).toHaveClass("duration-layout", "ease-ui");
    expect(toggle.nextElementSibling).toBe(bell);
    await user.click(toggle);
    expect(screen.getByRole("button", { name: "展开左侧栏" })).toBe(toggle);
    expect(toggle.querySelector("svg")).toBe(panelIcon);
    expect(toggle.nextElementSibling).toBe(bell);
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(toggle).toHaveFocus();
    expect(screen.getByRole("button", { name: "实体后退" })).toBeVisible();
    expect(bell).toBeVisible();
    expect(screen.queryByRole("radiogroup", { name: "身份列表" })).toBeNull();
    expect(entityList.closest("[inert]")).toHaveAttribute(
      "id",
      toggle.getAttribute("aria-controls"),
    );
    expect(scene).toHaveAttribute("data-content-spans-sidebar");
    expect(scene.className).toBe(sceneClass);
    expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "0px" });
    expect(
      screen.queryByRole("separator", { name: "调整模块侧栏宽度" }),
    ).toBeNull();
    expect(screen.getByText("主内容")).toBe(main);
    expect(screen.getByRole("navigation", { name: "模块导航" })).toBe(rail);
    await user.keyboard("{Enter}");
    expect(screen.getByRole("button", { name: "收起左侧栏" })).toBe(toggle);
    expect(entityList).toBeVisible();
    expect(scene).not.toHaveAttribute("data-content-spans-sidebar");
    expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "301px" });
    expect(
      screen.getByRole("separator", { name: "调整模块侧栏宽度" }),
    ).toHaveAttribute("aria-valuenow", "300");
    await user.click(screen.getByRole("searchbox", { name: "搜索会话" }));
    expect(screen.getByRole("searchbox", { name: "搜索会话" })).toHaveFocus();
  });

  it("跨模块保持收起状态，有侧栏页面可重新展开", async () => {
    const user = userEvent.setup();
    renderLayout({ navigationPreferences: { globalSidebarCollapsed: true } });
    expect(screen.queryByRole("radiogroup", { name: "身份列表" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "用量统计" }));
    expect(screen.queryByRole("button", { name: "展开左侧栏" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "资源库" }));
    expect(screen.queryByRole("button", { name: "技能" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(screen.queryByRole("navigation", { name: "设置菜单" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "展开左侧栏" }));
    expect(screen.getByRole("navigation", { name: "设置菜单" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "资源库" }));
    expect(screen.getByRole("button", { name: "技能" })).toBeVisible();
  });

  it("拖动到216px即停止，键盘Home/End仍调整正常宽度且不触发收起", () => {
    renderLayout({
      navigationPreferences: {
        contextSidebarWidth: 280,
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
    expect(handle).toHaveAttribute("aria-valuenow", "248");
    expect(screen.getByRole("button", { name: "收起左侧栏" })).toBeVisible();
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

  it.each([
    { mode: "waker", name: "伙伴 1", path: "/chat", wakerId: "waker-1" },
    { mode: "team", name: "研发团队", path: "/teams/team-1", wakerId: null },
  ] as const)(
    "$name 通过侧栏右键或键盘菜单打开该身份的新会话",
    async ({ mode, name, path, wakerId }) => {
      const user = userEvent.setup();
      renderLayout({
        channels: [{ channel: { id: "team-1", name: "研发团队" } }],
        navigationPreferences: { entityMode: mode },
      });
      const rail = screen.getByRole("navigation", { name: "伙伴" });
      const row = within(rail).getByRole("button", { name });
      const route = screen.getByTestId("current-route");
      expect(
        within(rail).queryAllByRole("button", { name: "新建会话" }),
      ).toHaveLength(0);

      fireEvent.contextMenu(row);
      expect(route).toHaveTextContent("/chat?wakerId=waker-6");
      await user.click(
        await screen.findByRole("menuitem", { name: "新建会话" }),
      );
      const firstRoute = new URL(route.textContent ?? "", "https://bot.local");
      expect(firstRoute.pathname).toBe(path);
      expect(firstRoute.searchParams.get("wakerId")).toBe(wakerId);
      expect(firstRoute.searchParams.get("draftId")).toBeTruthy();

      row.focus();
      await user.keyboard("{Shift>}{F10}{/Shift}");
      expect(
        await screen.findByRole("menuitem", { name: "新建会话" }),
      ).toBeVisible();
      await user.keyboard("{Home}{Enter}");
      const secondRoute = new URL(route.textContent ?? "", "https://bot.local");
      expect(secondRoute.pathname).toBe(path);
      expect(secondRoute.searchParams.get("wakerId")).toBe(wakerId);
      expect(secondRoute.searchParams.get("draftId")).toBeTruthy();
      expect(secondRoute.searchParams.get("draftId")).not.toBe(
        firstRoute.searchParams.get("draftId"),
      );
    },
  );

  it("完整视图沿用右下角唯一入口，不叠加伙伴history按钮", async () => {
    const user = userEvent.setup();
    renderLayout({ floatingChat: true });

    const launcher = await screen.findByRole("button", {
      name: "工作中，打开对话",
    });
    expect(launcher).toHaveAttribute("data-floating-chat-shell-launcher");
    expect(launcher).toHaveClass("size-control-hit-target");
    expect(launcher.firstElementChild).toHaveClass("h-control-corner-launcher");
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
    expect(screen.getByRole("radio", { name: "会话" })).toBeVisible();
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

  it("保留左侧会话行的 CLI 模型、状态与打开路由", async () => {
    const user = userEvent.setup();
    renderLayout();
    await user.click(screen.getByRole("radio", { name: "会话" }));
    const list = screen.getByRole("region", { name: "会话" });
    const chat = await within(list).findByRole("link", { name: "项目会话" });
    expect(chat).toHaveAccessibleDescription("gpt-5.6");
    expect(
      within(chat).getByTitle("gpt-5.6").previousElementSibling,
    ).toHaveAttribute("data-history-status");
    const oldChat = within(list).getByRole("link", { name: "临时会话" });
    expect(oldChat).not.toHaveAttribute("aria-description");
    await user.click(oldChat);
    expect(screen.getByTestId("current-route")).toHaveTextContent(
      "/chats/chat-folder",
    );
  });

  it("会话列表无需先滚动即可读取下一页", async () => {
    const user = userEvent.setup();
    history.nextPage = true;
    try {
      renderLayout();
      await user.click(screen.getByRole("radio", { name: "会话" }));
      const list = screen.getByRole("region", { name: "会话" });
      const loadMore = await within(list).findByRole("button", {
        name: "加载更多",
      });
      await user.click(loadMore);
      expect(
        await within(list).findByRole("link", { name: "更早的会话" }),
      ).toBeVisible();
    } finally {
      history.nextPage = false;
    }
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
      expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "217px" });
      const handle = screen.getByRole("separator", {
        name: "调整模块侧栏宽度",
      });
      fireEvent.mouseDown(handle, { clientX: 216 });
      expect(frame).toHaveClass("transition-none");
      fireEvent.mouseUp(document, { clientX: 300 });
      expect(frame).not.toHaveClass("transition-none");
      expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "301px" });
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
    expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "217px" });
    const handle = screen.getByRole("separator", { name: "调整模块侧栏宽度" });
    fireEvent.mouseDown(handle, { clientX: 216 });
    fireEvent.mouseUp(document, { clientX: 300 });
    await user.click(screen.getByRole("button", { name: "设置" }));
    expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "301px" });
    fireEvent.keyDown(
      screen.getByRole("separator", { name: "调整模块侧栏宽度" }),
      { key: "ArrowRight" },
    );
    await user.click(screen.getByRole("button", { name: "伙伴" }));
    expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "313px" });
    await user.click(screen.getByRole("button", { name: "用量统计" }));
    expect(
      screen.queryByRole("separator", { name: "调整模块侧栏宽度" }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "资源库" }));
    expect(frame).toHaveStyle({ "--shell-sidebar-body-width": "313px" });
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
    ["zh-CN", "默认", "会话"],
    ["en-US", "Default", "Conversations"],
  ])(
    "%s 将不同临时路径合并为一个可折叠分组",
    async (language, label, historyLabel) => {
      await i18n.changeLanguage(language);
      try {
        const user = userEvent.setup();
        renderLayout();
        await user.click(screen.getByRole("radio", { name: historyLabel }));
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
    },
  );

  it("伙伴详情菜单不包含工作台的会话入口", async () => {
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
    expect(screen.queryByRole("button", { name: "查看历史会话" })).toBeNull();
  });

  it("团队会话列表展示负责人最近使用的模型，并省略列表内新建入口", async () => {
    const user = userEvent.setup();
    renderLayout({
      initialEntry: "/teams/team-1/threads/thread-project",
      teamContext: { channelId: "team-1", threadId: "thread-project" },
      channels: [{ channel: { id: "team-1", name: "研发团队" } }],
      navigationPreferences: { entityMode: "team" },
    });
    await user.click(screen.getByRole("radio", { name: "会话" }));
    const list = screen.getByRole("region", { name: "会话" });
    const chat = await within(list).findByRole("link", {
      name: /团队项目会话/,
    });
    expect(chat).toHaveAccessibleDescription("claude-sonnet");
    expect(chat).toHaveAttribute("aria-current", "page");
    expect(
      within(chat).getByTitle("claude-sonnet").previousElementSibling,
    ).toHaveAttribute("data-history-status");
    expect(within(list).queryByRole("button", { name: "新建会话" })).toBeNull();
    const olderChat = within(list).getByRole("link", { name: /团队临时会话/ });
    expect(olderChat).not.toHaveAttribute("aria-description");
  });

  it("切换左栏时沿用两页轨道，离屏页不能交互", async () => {
    const user = userEvent.setup();
    renderLayout();
    const track = document.querySelector("[data-sidebar-page-track]");
    const partners = track?.querySelector('[data-sidebar-page="partners"]');
    const conversations = track?.querySelector(
      '[data-sidebar-page="conversations"]',
    );

    expect(track).toHaveAttribute("data-sidebar-page-track", "partners");
    expect(conversations).toHaveAttribute("inert");
    await user.click(screen.getByRole("radio", { name: "会话" }));

    expect(track).toHaveAttribute("data-sidebar-page-track", "conversations");
    expect(partners).toHaveAttribute("inert");
    expect(conversations).not.toHaveAttribute("inert");
    expect(screen.getByRole("region", { name: "会话" })).toBe(conversations);
  });

  it("会话列表标签按来源分组，列表顶部直接显示分组", async () => {
    const user = userEvent.setup();
    renderLayout();
    await user.click(screen.getByRole("radio", { name: "会话" }));
    const historyPanel = screen.getByRole("region", { name: "会话" });
    expect(historyPanel.querySelector("[data-chat-history-list]")).toHaveClass(
      "overflow-y-auto",
    );
    expect(
      within(historyPanel).queryByRole("button", { name: "新建会话" }),
    ).toBeNull();
    expect(
      within(historyPanel).getByRole("combobox", { name: "会话范围" }),
    ).toHaveTextContent("伙伴 6");
    expect(
      within(historyPanel).queryByRole("button", { name: "伙伴 6，工作中" }),
    ).toBeNull();
    expect(
      within(historyPanel).queryByRole("button", { name: "置顶" }),
    ).toBeNull();
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
    ).toHaveClass("pl-2");
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

    await user.click(screen.getByRole("radio", { name: "会话" }));

    const projectChatRows = await waitFor(() =>
      screen.findAllByRole("link", {
        name: /项目会话/,
      }),
    );
    const selected = projectChatRows.filter(
      (link) => link.getAttribute("aria-current") === "page",
    );
    expect(selected).toHaveLength(1);
    expect(selected[0]?.parentElement).toHaveClass("bg-action-quiet-pressed");
    for (const link of projectChatRows) {
      expect(link.parentElement).not.toHaveClass(
        "focus-within:bg-action-quiet-pressed",
      );
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
    await user.click(screen.getByRole("radio", { name: "会话" }));
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
    await user.click(screen.getByRole("radio", { name: "会话" }));
    const link = await screen.findByRole("link", { name: /项目会话/ });
    const dot = () => link.querySelector("[data-history-status]");
    expect(dot()).toHaveAttribute("data-history-status", "running");
    expect(dot()).toHaveClass("bg-success");
    expect(link.querySelector(".lucide-loader-circle")).toBeNull();
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

it("团队身份行通过侧栏菜单保留编辑和删除入口", async () => {
  renderLayout({
    channels: [{ channel: { id: "team-1", name: "研发团队" } }],
    navigationPreferences: { entityMode: "team" },
  });
  fireEvent.contextMenu(
    await screen.findByRole("button", { name: /^研发团队/ }),
  );
  expect(
    await screen.findByRole("menuitem", { name: "编辑团队" }),
  ).toBeVisible();
  expect(screen.getByRole("menuitem", { name: "删除团队" })).toBeVisible();
});
