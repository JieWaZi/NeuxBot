import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import type { PartnerNavigationPreferences } from "./partner-navigation-preferences";
import { TeamChatTree, TeamMemberMenu } from "./team-context-sidebar";
import {
  useSceneWorkspace,
  WorkspaceProvider,
} from "@/shared/workspace/workspace-context";

const fixture = vi.hoisted(() => ({
  remove: vi.fn(
    async (request: { path: { channelId: string; threadId: string } }) => {
      void request;
      return {};
    },
  ),
}));

vi.mock("@/modules/team/api/channel-queries", () => ({
  deleteChannelThreadMutation: () => ({
    mutationFn: (request: { path: { channelId: string; threadId: string } }) =>
      fixture.remove(request),
  }),
  getChannelOptions: () => ({
    queryKey: ["channel", "team-1"],
    queryFn: async () => ({
      channel: {
        id: "team-1",
        leadWakerId: "lead-1",
        name: "研发团队",
      },
      members: [
        { waker: { id: "lead-1", name: "负责人" } },
        { waker: { id: "member-2", name: "前端工程师" } },
      ],
    }),
  }),
  getChannelThreadOptions: () => ({
    queryKey: ["channel-thread", "thread-1"],
    queryFn: async () => ({
      memberActivities: [
        {
          chatId: "member-chat",
          executionId: "execution-1",
          state: "running",
          wakerId: "member-2",
        },
      ],
      members: [
        {
          chatId: "lead-chat",
          displayName: "负责人",
          isLead: true,
          position: 0,
          wakerId: "lead-1",
        },
        {
          chatId: "member-chat",
          displayName: "前端工程师",
          position: 1,
          wakerId: "member-2",
        },
      ],
      tasks: [
        {
          assigneeWakerId: "member-2",
          id: "task-1",
          status: "in_progress",
        },
      ],
      thread: {
        id: "thread-1",
        leadChatId: "lead-chat",
        leadWakerId: "lead-1",
        projectId: "project-1",
        projectName: "NeuxBot",
        title: "研发会话",
        updatedAt: Date.now(),
      },
    }),
  }),
  getChannelThreadQueryKey: ({
    path,
  }: {
    path: { channelId: string; threadId: string };
  }) => ["channel-thread", path.channelId, path.threadId],
  teamQueryKeys: {
    threads: (channelId: string) => ["channel-threads", channelId],
    timelinePages: (channelId: string, threadId: string) => [
      "channel-timeline",
      channelId,
      threadId,
    ],
  },
  teamThreadsInfiniteOptions: () => ({
    initialPageParam: 1,
    queryKey: ["channel-threads", "team-1", "infinite"],
    queryFn: async () => ({
      resources: [
        {
          id: "thread-1",
          projectId: "project-1",
          projectName: "NeuxBot",
          title: "研发会话",
          updatedAt: Date.now(),
        },
      ],
    }),
    getNextPageParam: () => undefined,
  }),
}));

const initialPreferences: PartnerNavigationPreferences = {
  collapsedGroups: {},
  contextSidebarWidth: 240,
  currentPartnerId: "",
  currentRailKey: "team:team-1",
  entityMode: "team",
  globalSidebarCollapsed: false,
  lastTeamThreads: {},
  railExpanded: false,
  railOrder: [],
};

function SidebarHarness({
  threadId,
  view = "members",
}: {
  threadId?: string | null;
  view?: "chats" | "members";
} = {}) {
  const location = useLocation();
  const [preferences, setPreferences] = useState(initialPreferences);
  const resolvedThreadId =
    threadId === null ? undefined : (threadId ?? "thread-1");
  return (
    <>
      {view === "chats" ? (
        <TeamChatTree
          channelId="team-1"
          onPreferencesChange={setPreferences}
          preferences={preferences}
          threadId={resolvedThreadId}
        />
      ) : (
        <TeamMemberMenu channelId="team-1" threadId={resolvedThreadId} />
      )}
      <output aria-label="location">{location.pathname}</output>
    </>
  );
}

function WorkspaceState({ scopeKey = "team:team-1:thread-1" }) {
  const workspace = useSceneWorkspace(scopeKey);
  const activeTab = workspace.snapshot.tabs.find(
    (tab) => tab.id === workspace.snapshot.activeTabId,
  );
  return (
    <output aria-label="workspace-state">
      {activeTab?.kind === "member" ? activeTab.wakerId : activeTab?.kind}
    </output>
  );
}

describe("团队导航菜单", () => {
  beforeEach(() => fixture.remove.mockClear());

  it("确认后永久删除当前团队会话并返回团队草稿", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(["channel-threads", "team-1", "infinite"], {
      pageParams: [1],
      pages: [
        {
          resources: [
            {
              id: "thread-1",
              projectId: "project-1",
              projectName: "NeuxBot",
              title: "研发会话",
              updatedAt: Date.now(),
            },
          ],
        },
      ],
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/teams/team-1/threads/thread-1"]}>
          <SidebarHarness view="chats" />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    const user = userEvent.setup();

    await user.click(
      await screen.findByRole(
        "button",
        { name: "会话“研发会话”操作" },
        { timeout: 5_000 },
      ),
    );
    await user.click(screen.getByRole("menuitem", { name: "删除会话" }));
    expect(screen.getByRole("dialog")).toHaveTextContent(
      "任务、成员消息和执行记录将被永久删除",
    );
    await user.click(screen.getByRole("button", { name: "确认删除" }));

    await waitFor(() =>
      expect(fixture.remove).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { channelId: "team-1", threadId: "thread-1" },
        }),
      ),
    );
    expect(screen.getByLabelText("location")).toHaveTextContent(
      "/teams/team-1",
    );
  });

  it("菜单只展示成员列表，并从侧栏打开成员 Tab", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceProvider>
          <MemoryRouter initialEntries={["/teams/team-1/threads/thread-1"]}>
            <SidebarHarness />
            <WorkspaceState />
          </MemoryRouter>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );
    const user = userEvent.setup();

    const memberNavigation = await screen.findByRole("navigation", {
      name: "团队成员会话",
    });
    expect(memberNavigation).not.toHaveClass("bg-surface", "border-b");
    expect(screen.queryByText("研发团队")).toBeNull();
    expect(screen.queryByRole("button", { name: "置顶" })).toBeNull();
    expect(screen.queryByRole("link", { name: /研发会话/ })).toBeNull();
    const memberButton = await screen.findByRole("button", {
      name: "打开 前端工程师 的会话，当前状态：执行中",
    });
    expect(memberButton).toBeVisible();
    expect(memberButton).toHaveClass("hover:bg-action-quiet-hover");

    await user.click(
      screen.getByRole("button", { name: /打开 前端工程师 的会话/ }),
    );
    expect(screen.getByLabelText("workspace-state")).toHaveTextContent(
      "member-2",
    );
  });

  it("新团队对话可在首次发送前打开成员草稿 Tab", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceProvider>
          <MemoryRouter initialEntries={["/teams/team-1?draftId=draft-1"]}>
            <SidebarHarness threadId={null} />
            <WorkspaceState scopeKey="team:team-1:draft:draft-1" />
          </MemoryRouter>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );
    const user = userEvent.setup();

    await user.click(
      await screen.findByRole("button", {
        name: /打开 前端工程师 的会话/,
      }),
    );

    expect(screen.getByLabelText("workspace-state")).toHaveTextContent(
      "member-2",
    );
  });
});
