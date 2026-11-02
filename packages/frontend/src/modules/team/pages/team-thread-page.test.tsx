import type * as TeamAPI from "../api/channel-queries";
import type * as GeneratedSDK from "@/shared/api/generated/sdk.gen";
import type * as ErrorNotices from "@/shared/api/error-notices";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelThreadMember as ChannelThreadMember } from "@/shared/api/generated/types.gen";

import { AppSceneWorkspaceContextProvider } from "@/shared/workspace/app-scene-workspace";
import { TeamThreadPage, type TeamThreadChatContext } from "./team-thread-page";

const teamThreadFixture = vi.hoisted(() => ({
  createThread: vi.fn(async () => ({
    thread: {
      id: "created-thread",
      leadChatId: "created-lead-chat",
      leadWakerId: "lead-1",
    },
    members: [{ chatId: "created-lead-chat", wakerId: "lead-1", isLead: true }],
    tasks: [],
  })),
  channelEventHandler: undefined as
    | ((event: {
        createdAt?: number;
        id?: string;
        payload?: unknown;
        threadId?: string;
        type?: string;
      }) => void)
    | undefined,
  channelLeadWakerId: "lead-1",
  channelMembers: [
    {
      runtimeId: "codex",
      waker: { id: "lead-1", name: "负责人" },
    },
    {
      runtimeId: "codex",
      waker: { id: "member-2", name: "执行成员" },
    },
  ],
  leadActiveExecutionId: "",
  leadProcessing: false,
  memberActivities: [] as Array<{
    chatId: string;
    executionId?: string;
    state: string;
    wakerId: string;
  }>,
  pendingInteractions: [] as Array<{
    chatId: string;
    id: string;
    type: string;
    wakerId: string;
  }>,
  loadTimeline: vi.fn(async (cursor?: string) => {
    void cursor;
    return {
      hasMore: false,
      resources: [
        {
          chatId: "member-chat",
          message: { id: "earlier-result", role: "assistant" },
          wakerId: "member-2",
        },
      ],
    };
  }),
  openMember: vi.fn(),
  reviewTask: vi.fn(async () => ({})),
  stopTask: vi.fn(async () => ({})),
  stopWork: vi.fn(async () => ({})),
  errorNotice: vi.fn(),
  threadLeadWakerId: "lead-1",
  threadCancelingAt: undefined as number | undefined,
  threadMembers: [
    {
      chatId: "lead-chat",
      isLead: true,
      position: 0,
      runtimeId: "codex",
      wakerId: "lead-1",
    },
    {
      chatId: "member-chat",
      position: 1,
      runtimeId: "codex",
      wakerId: "member-2",
    },
  ] as ChannelThreadMember[],
  timelineHasMore: false,
  leadChatLatestSequence: 7,
  timelineHeadReads: vi.fn(),
  timelineNextCursor: undefined as string | undefined,
  timeline: [
    {
      chatId: "member-chat",
      message: { id: "member-result", role: "assistant" },
      wakerId: "member-2",
    },
  ],
  tasks: [
    {
      assigneeWakerId: "member-2",
      description: "检查实现并提交结果",
      errorMessage: undefined as string | undefined,
      id: "task-1",
      status: "in_review",
      subject: "审核团队实现",
    },
  ] as Array<{
    assigneeWakerId: string;
    changedPaths?: string[];
    description?: string;
    errorMessage?: string;
    id: string;
    result?: string;
    status: string;
    subject: string;
  }>,
  updateMemberConfigs: vi.fn(async () => ({})),
  threadQueryDelay: 0,
  threadQueryError: undefined as unknown,
}));

vi.mock("@/shared/api/events", () => ({
  subscribeChannel: (
    _channelId: string,
    _threadId: string,
    options: { onEvent: typeof teamThreadFixture.channelEventHandler },
  ) => {
    teamThreadFixture.channelEventHandler = options.onEvent;
    return () => {
      teamThreadFixture.channelEventHandler = undefined;
    };
  },
}));

vi.mock("@/shared/api/error-notices", async (importOriginal) => ({
  ...(await importOriginal<typeof ErrorNotices>()),
  reportErrorNotice: teamThreadFixture.errorNotice,
}));

vi.mock("@/shared/api/generated/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<typeof GeneratedSDK>()),
  listChannelTimeline: async ({ query }: { query?: { cursor?: string } }) => {
    if (query?.cursor)
      return { data: await teamThreadFixture.loadTimeline(query.cursor) };
    teamThreadFixture.timelineHeadReads();
    return {
      data: {
        leadChatLatestSequence: teamThreadFixture.leadChatLatestSequence,
        hasMore: teamThreadFixture.timelineHasMore,
        nextCursor: teamThreadFixture.timelineNextCursor,
        resources: teamThreadFixture.timeline,
      },
    };
  },
}));

vi.mock("../api/channel-queries", async (importOriginal) => ({
  ...(await importOriginal<typeof TeamAPI>()),
  createChannelThreadMutation: () => ({
    mutationFn: teamThreadFixture.createThread,
  }),
  getChannelOptions: ({ path }: { path: { channelId: string } }) => ({
    queryFn: async () => ({
      channel: {
        id: path.channelId,
        leadWakerId: teamThreadFixture.channelLeadWakerId,
        name: "研发团队",
      },
      members: teamThreadFixture.channelMembers,
    }),
    queryKey: ["channel", path.channelId],
  }),
  getChannelThreadOptions: ({
    path,
  }: {
    path: { channelId: string; threadId: string };
  }) => ({
    queryFn: async () => {
      if (teamThreadFixture.threadQueryDelay > 0) {
        await new Promise((resolve) => {
          setTimeout(resolve, teamThreadFixture.threadQueryDelay);
        });
      }
      if (teamThreadFixture.threadQueryError) {
        throw teamThreadFixture.threadQueryError;
      }
      return {
        members: teamThreadFixture.threadMembers,
        leadActiveExecutionId: teamThreadFixture.leadActiveExecutionId,
        leadProcessing: teamThreadFixture.leadProcessing,
        memberActivities: teamThreadFixture.memberActivities,
        pendingInteractions: teamThreadFixture.pendingInteractions,
        tasks: teamThreadFixture.tasks,
        thread: {
          cancelingAt: teamThreadFixture.threadCancelingAt,
          id: path.threadId,
          leadChatId: "lead-chat",
          leadWakerId: teamThreadFixture.threadLeadWakerId,
          maxParallel: 3,
          title: "团队对话",
        },
      };
    },
    queryKey: ["channel-thread", path.channelId, path.threadId],
  }),
  getChannelThreadQueryKey: ({
    path,
  }: {
    path: { channelId: string; threadId: string };
  }) => ["channel-thread", path.channelId, path.threadId],
  listChannelThreadsQueryKey: ({ path }: { path: { channelId: string } }) => [
    "channel-threads",
    path.channelId,
  ],
  reviewChannelTaskMutation: () => ({
    mutationFn: teamThreadFixture.reviewTask,
  }),
  sendChannelMessageMutation: () => ({ mutationFn: vi.fn() }),
  stopChannelTaskMutation: () => ({
    mutationFn: teamThreadFixture.stopTask,
  }),
  stopChannelWorkMutation: () => ({ mutationFn: teamThreadFixture.stopWork }),
  updateChannelThreadMemberConfigsMutation: () => ({
    mutationFn: teamThreadFixture.updateMemberConfigs,
  }),
}));

function renderTeamChat({
  chatId,
  teamContext,
}: {
  chatId?: string;
  teamContext: TeamThreadChatContext;
}) {
  return (
    <div>
      <span data-testid="channel-detail-loading">
        {teamContext.channelDetailLoading ? "loading" : "ready"}
      </span>
      <span data-testid="thread-detail-loading">
        {teamContext.threadDetailLoading ? "loading" : "ready"}
      </span>
      <span data-testid="selected-chat">{chatId}</span>
      <span data-testid="lead-member">{teamContext.leadWakerId}</span>
      <span data-testid="lead-execution">
        {teamContext.leadActiveExecutionId}
      </span>
      <span data-testid="member-count">{teamContext.members.length}</span>
      <span data-testid="team-members">
        {teamContext.members
          .map((member) => `${member.id}:${member.isLead}`)
          .join(",")}
      </span>
      <span data-testid="team-member-details">
        {teamContext.members
          .map((member) => `${member.name}:${member.avatar ?? ""}`)
          .join(",")}
      </span>
      <span data-testid="team-member-activities">
        {teamContext.members
          .map(
            (member) =>
              `${member.id}:${member.activeExecutionId ?? ""}:${member.waitingApproval ?? false}:${member.waitingElicitation ?? false}`,
          )
          .join(",")}
      </span>
      <span data-testid="work-state">{teamContext.workState}</span>
      <span data-testid="work-phase">{teamContext.workProgress.phase}</span>
      <span data-testid="is-stopping">
        {teamContext.isStopping ? "stopping" : "ready"}
      </span>
      <span data-testid="timeline-message">
        {teamContext.timeline[0]?.message.id}:{teamContext.timeline[0]?.wakerId}
      </span>
      <span data-testid="timeline-messages">
        {teamContext.timeline.map((item) => item.message.id).join(",")}
      </span>
      {teamContext.hasEarlierTimelineMessages ? (
        <button
          type="button"
          disabled={teamContext.isLoadingEarlierTimelineMessages}
          onClick={() => void teamContext.onLoadEarlierTimelineMessages()}
        >
          加载更早团队消息
        </button>
      ) : null}
      <button
        type="button"
        onClick={() => void teamContext.messageSource.reconcile()}
      >
        恢复团队消息快照
      </button>
      <button
        type="button"
        disabled={teamContext.isStopping}
        onClick={() => void teamContext.onStopWork()}
      >
        停止团队工作
      </button>
      <button
        type="button"
        onClick={() =>
          void teamContext.onUpdateMemberConfigs([
            {
              model: "gpt-5",
              permissionLevel: "auto",
              reasoning: "medium",
              runtimeId: "codex",
              wakerId: teamContext.members[0]?.id ?? "",
            },
          ])
        }
      >
        更新成员配置
      </button>
      {teamContext.renderTaskBoard()}
    </div>
  );
}

function LocationOutput() {
  const location = useLocation();
  return (
    <output aria-label="location">{`${location.pathname}${location.search}`}</output>
  );
}

function renderPage(
  initialEntry: string,
  options?: {
    onThreadMissing?: (channelId: string, threadId: string) => void;
    seedChannel?: boolean;
  },
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  let teamContext: TeamThreadChatContext | undefined;
  if (options?.seedChannel) {
    queryClient.setQueryData(["channel", "channel-1"], {
      channel: {
        id: "channel-1",
        leadWakerId: teamThreadFixture.channelLeadWakerId,
        name: "研发团队",
      },
      members: teamThreadFixture.channelMembers,
    });
  }
  const view = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route
            path="teams/:channelId/threads/:threadId"
            element={
              <TeamThreadPage
                onThreadMissing={options?.onThreadMissing}
                renderChat={(props) => {
                  teamContext = props.teamContext;
                  return renderTeamChat(props);
                }}
              />
            }
          />
          <Route path="*" element={<LocationOutput />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return {
    ...view,
    queryClient,
    get teamContext() {
      return teamContext;
    },
  };
}

describe("TeamThreadPage", () => {
  it("首发创建结果在导航前进入详情缓存，新路由不再丢失负责人 Chat 身份", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    let context: TeamThreadChatContext | undefined;
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/teams/team-1?draftId=draft-1"]}>
          <Routes>
            <Route
              path="/teams/:channelId"
              element={
                <TeamThreadPage
                  renderChat={(props) => {
                    context = props.teamContext;
                    return renderTeamChat(props);
                  }}
                />
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await waitFor(() => expect(context?.channelDetailLoading).toBe(false));
    await act(async () => {
      await context?.onCreateThread({
        text: "启动任务",
        clientRequestId: "test-create",
      });
    });
    expect(teamThreadFixture.createThread).toHaveBeenCalled();
    expect(
      queryClient.getQueryData(["channel-thread", "team-1", "created-thread"]),
    ).toMatchObject({
      thread: { id: "created-thread", leadChatId: "created-lead-chat" },
      members: [{ chatId: "created-lead-chat", wakerId: "lead-1" }],
    });
  });
  beforeEach(() => {
    teamThreadFixture.channelLeadWakerId = "lead-1";
    teamThreadFixture.channelMembers = [
      {
        runtimeId: "codex",
        waker: { id: "lead-1", name: "负责人" },
      },
      {
        runtimeId: "codex",
        waker: { id: "member-2", name: "执行成员" },
      },
    ];
    teamThreadFixture.leadActiveExecutionId = "";
    teamThreadFixture.leadProcessing = false;
    teamThreadFixture.memberActivities = [];
    teamThreadFixture.pendingInteractions = [];
    teamThreadFixture.loadTimeline.mockClear();
    teamThreadFixture.openMember.mockClear();
    teamThreadFixture.reviewTask.mockClear();
    teamThreadFixture.stopTask.mockClear();
    teamThreadFixture.stopWork.mockClear();
    teamThreadFixture.errorNotice.mockClear();
    teamThreadFixture.channelEventHandler = undefined;
    teamThreadFixture.threadLeadWakerId = "lead-1";
    teamThreadFixture.threadCancelingAt = undefined;
    teamThreadFixture.threadMembers = [
      {
        chatId: "lead-chat",
        isLead: true,
        position: 0,
        runtimeId: "codex",
        wakerId: "lead-1",
      },
      {
        chatId: "member-chat",
        position: 1,
        runtimeId: "codex",
        wakerId: "member-2",
      },
    ];
    teamThreadFixture.timelineHasMore = false;
    teamThreadFixture.leadChatLatestSequence = 7;
    teamThreadFixture.timelineHeadReads.mockClear();
    teamThreadFixture.timelineNextCursor = undefined;
    teamThreadFixture.timeline = [
      {
        chatId: "member-chat",
        message: { id: "member-result", role: "assistant" },
        wakerId: "member-2",
      },
    ];
    teamThreadFixture.updateMemberConfigs.mockClear();
    teamThreadFixture.threadQueryDelay = 0;
    teamThreadFixture.threadQueryError = undefined;
    teamThreadFixture.tasks = [
      {
        assigneeWakerId: "member-2",
        description: "检查实现并提交结果",
        errorMessage: undefined,
        id: "task-1",
        status: "in_review",
        subject: "审核团队实现",
      },
    ];
  });

  it("团队消息源使用时间线游标，元数据刷新不重复读取消息", async () => {
    const page = renderPage("/teams/channel-1/threads/thread-1");
    await waitFor(() =>
      expect(page.teamContext?.messageSource.snapshot.ready).toBe(true),
    );
    expect(page.teamContext?.messageSource.snapshot.latestSequence).toBe(7);
    expect(page.teamContext?.messageSource.snapshot.messages).toEqual([]);
    expect(
      page.teamContext?.messageSource.commit([
        {
          sequence: 8,
          type: "message.created",
          message: {
            id: "durable-main",
            role: "assistant",
            executionId: "execution",
            status: "completed",
          },
        },
      ]),
    ).toBe(8);
    await waitFor(() =>
      expect(page.teamContext?.messageSource.snapshot.messages[0]?.id).toBe(
        "durable-main",
      ),
    );
    expect(teamThreadFixture.timelineHeadReads).toHaveBeenCalledTimes(1);
    await act(async () => {
      await page.queryClient.invalidateQueries({
        queryKey: ["channel-thread", "channel-1", "thread-1"],
      });
    });
    expect(teamThreadFixture.timelineHeadReads).toHaveBeenCalledTimes(1);
    teamThreadFixture.leadChatLatestSequence = 9;
    let reconciled: number | undefined;
    await act(async () => {
      reconciled = await page.teamContext?.messageSource.reconcile();
    });
    expect(reconciled).toBe(9);
    await waitFor(() =>
      expect(page.teamContext?.messageSource.snapshot.latestSequence).toBe(9),
    );
    expect(teamThreadFixture.timelineHeadReads).toHaveBeenCalledTimes(2);
    expect(
      page.queryClient.getQueryData(["chats", "messages", "lead-chat"]),
    ).toBeUndefined();
  });

  it("使用负责人成员 Chat 渲染团队主会话", async () => {
    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("selected-chat")).toHaveTextContent(
        "lead-chat",
      ),
    );
    expect(screen.getByTestId("lead-member")).toHaveTextContent("lead-1");
    expect(screen.getByTestId("member-count")).toHaveTextContent("2");
    expect(screen.getByTestId("work-state")).toHaveTextContent("review");
    expect(screen.getByTestId("timeline-message")).toHaveTextContent(
      "member-result:member-2",
    );
    expect(screen.getByRole("region", { name: "看板" })).toBeVisible();
    expect(screen.queryByText("团队执行")).not.toBeInTheDocument();
    expect(screen.queryByText("运行概览")).not.toBeInTheDocument();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(screen.queryByText(/并发任务/)).not.toBeInTheDocument();
    const todo = screen.getByRole("region", { name: "看板" });
    expect(within(todo).getByText("审核团队实现")).toBeVisible();
    expect(within(todo).getByText(/执行成员/)).toBeVisible();
    expect(
      within(todo).getByRole("heading", { name: "待人工验收" }),
    ).toBeVisible();
    expect(screen.getByText("检查实现并提交结果")).toBeVisible();
    expect(screen.getByRole("button", { name: "通过" })).toBeVisible();
    expect(screen.getByRole("button", { name: "驳回" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "停止当前工作" }),
    ).not.toBeInTheDocument();
  });

  it("已有会话使用 Thread 冻结的负责人和成员，不随团队当前配置漂移", async () => {
    teamThreadFixture.channelLeadWakerId = "current-lead";
    teamThreadFixture.channelMembers = [
      {
        runtimeId: "codex",
        waker: { id: "lead-1", name: "原负责人" },
      },
      {
        runtimeId: "codex",
        waker: { id: "member-2", name: "执行成员" },
      },
      {
        runtimeId: "codex",
        waker: { id: "current-lead", name: "新负责人" },
      },
    ];
    teamThreadFixture.threadLeadWakerId = "lead-1";

    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("lead-member")).toHaveTextContent("lead-1"),
    );
    expect(screen.getByTestId("team-members")).toHaveTextContent(
      "lead-1:true,member-2:false",
    );
    expect(screen.getByTestId("team-members")).not.toHaveTextContent(
      "current-lead",
    );
    expect(
      screen.queryByRole("button", { name: "查看 新负责人 的执行过程" }),
    ).not.toBeInTheDocument();
  });

  it("伙伴从当前团队删除后仍使用 Thread 冻结的名称和头像", async () => {
    teamThreadFixture.channelMembers = [];
    teamThreadFixture.threadMembers = [
      {
        avatar: "data:image/png;base64,lead",
        chatId: "lead-chat",
        displayName: "历史负责人",
        isLead: true,
        position: 0,
        runtimeId: "codex",
        wakerId: "lead-1",
      },
      {
        avatar: "data:image/png;base64,member",
        chatId: "member-chat",
        displayName: "历史前端伙伴",
        position: 1,
        runtimeId: "codex",
        wakerId: "member-2",
      },
    ];

    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("team-member-details")).toHaveTextContent(
        "历史前端伙伴:data:image/png;base64,member",
      ),
    );
    expect(screen.getByTestId("team-members")).toHaveTextContent(
      "lead-1:true,member-2:false",
    );
  });

  it("向聊天页提供成员名称、头像和对应 Chat", async () => {
    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("team-member-details")).toHaveTextContent(
        "负责人:,执行成员:",
      ),
    );
    expect(screen.getByTestId("team-members")).toHaveTextContent(
      "lead-1:true,member-2:false",
    );
  });

  it("使用团队快照中的成员执行与待处理交互状态", async () => {
    teamThreadFixture.memberActivities = [
      {
        chatId: "member-chat",
        executionId: "member-execution",
        state: "requires_action",
        wakerId: "member-2",
      },
    ];
    teamThreadFixture.pendingInteractions = [
      {
        chatId: "member-chat",
        id: "approval-1",
        type: "approval",
        wakerId: "member-2",
      },
    ];

    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("team-member-activities")).toHaveTextContent(
        "member-2:member-execution:true:false",
      ),
    );
  });

  it("驳回使用必填反馈对话框，不调用浏览器 prompt", async () => {
    renderPage("/teams/channel-1/threads/thread-1");

    expect(await screen.findByText("审核团队实现")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "驳回" }));

    const confirm = screen.getByRole("button", { name: "确认驳回" });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByLabelText("驳回反馈"), {
      target: { value: "请补充边界测试" },
    });
    fireEvent.click(confirm);

    await waitFor(() =>
      expect(teamThreadFixture.reviewTask).toHaveBeenCalledWith(
        expect.objectContaining({
          body: { approved: false, feedback: "请补充边界测试" },
        }),
        expect.anything(),
      ),
    );
  });

  it("人审操作失败时在任务详情附近展示错误", async () => {
    teamThreadFixture.reviewTask.mockRejectedValueOnce(new Error("failed"));
    renderPage("/teams/channel-1/threads/thread-1");

    expect(await screen.findByText("审核团队实现")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "通过" }));

    const error = await screen.findByText("操作未完成，请稍后重试。");
    expect(error).toHaveAttribute("role", "alert");
  });

  it("负责人正在处理成员结果时保持团队执行中", async () => {
    teamThreadFixture.leadProcessing = true;
    teamThreadFixture.tasks = [];

    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("work-state")).toHaveTextContent("running"),
    );
    expect(screen.getByTestId("work-phase")).toHaveTextContent("summarizing");
    expect(
      screen.queryByRole("button", { name: "停止当前工作" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("运行概览")).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "看板" })).toBeVisible();
    expect(screen.getByText("负责人尚未创建正式任务。")).toBeVisible();
    expect(screen.queryByRole("heading", { name: "待执行" })).toBeNull();
  });

  it("停止意图存在时展示收敛状态并禁用发送侧和任务操作", async () => {
    teamThreadFixture.threadCancelingAt = 1_700_000_000_000;

    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("work-phase")).toHaveTextContent("stopping"),
    );
    expect(screen.getByTestId("is-stopping")).toHaveTextContent("stopping");
    expect(screen.getByRole("button", { name: "停止团队工作" })).toBeDisabled();
    expect(screen.getByRole("region", { name: "看板" })).toBeVisible();
    expect(screen.getByRole("button", { name: "通过" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "驳回" })).toBeDisabled();
  });

  it("恢复耗尽事件只提示稳定文案并按事件标识去重", async () => {
    renderPage("/teams/channel-1/threads/thread-1");
    await waitFor(() =>
      expect(teamThreadFixture.channelEventHandler).toBeTypeOf("function"),
    );

    const recoveryEvent = {
      createdAt: 1,
      id: "event-recovery-failed",
      payload: { reason: "raw internal database error" },
      threadId: "thread-1",
      type: "task.recovery_failed",
    };
    act(() => {
      teamThreadFixture.channelEventHandler?.(recoveryEvent);
      teamThreadFixture.channelEventHandler?.(recoveryEvent);
      teamThreadFixture.channelEventHandler?.({
        createdAt: 2,
        id: "event-leader-exhausted",
        payload: { retryExhausted: true, reason: "raw runtime error" },
        threadId: "thread-1",
        type: "team.leader.processing",
      });
    });

    expect(teamThreadFixture.errorNotice).toHaveBeenCalledTimes(2);
    expect(teamThreadFixture.errorNotice).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: "team:thread-1",
        message:
          "一项团队任务未能自动恢复。请查看成员会话，调整后让负责人重试。",
      }),
    );
    expect(teamThreadFixture.errorNotice).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: "team:thread-1",
        message: "负责人未能继续汇总结果。请检查成员会话后发送消息继续。",
      }),
    );
    expect(teamThreadFixture.errorNotice).not.toHaveBeenCalledWith(
      expect.objectContaining({ message: expect.stringContaining("raw") }),
    );
  });

  it("历史任务结束后处理新的用户消息时不沿用汇总状态", async () => {
    teamThreadFixture.leadActiveExecutionId = "user-message-execution";
    teamThreadFixture.leadProcessing = false;
    teamThreadFixture.tasks = [
      {
        assigneeWakerId: "member-2",
        description: "上一轮已经结束的任务",
        errorMessage: undefined,
        id: "task-completed",
        status: "completed",
        subject: "上一轮任务",
      },
    ];

    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("work-state")).toHaveTextContent("running"),
    );
    expect(screen.getByTestId("work-phase")).toHaveTextContent("idle");
  });

  it("按状态分组展示看板任务卡和计数", async () => {
    teamThreadFixture.tasks = [
      {
        assigneeWakerId: "member-2",
        description: "正在实现",
        id: "task-running",
        status: "in_progress",
        subject: "实现功能",
      },
      {
        assigneeWakerId: "member-2",
        description: "等待资源",
        id: "task-pending",
        status: "pending",
        subject: "补充测试",
      },
      {
        assigneeWakerId: "member-2",
        description: "完成结果详情",
        id: "task-completed",
        status: "completed",
        subject: "梳理需求",
      },
      {
        assigneeWakerId: "member-2",
        changedPaths: ["projects/share/failed.ts"],
        description: "验证失败路径",
        errorMessage: "构建失败",
        id: "task-failed",
        result: "失败结果不应展示",
        status: "failed",
        subject: "验证构建",
      },
      {
        assigneeWakerId: "member-2",
        description: "已取消",
        id: "task-cancelled",
        status: "cancelled",
        subject: "废弃探索",
      },
    ];

    renderPage("/teams/channel-1/threads/thread-1");

    const todo = await screen.findByRole("region", { name: "看板" });
    await waitFor(() =>
      expect(screen.getByTestId("work-phase")).toHaveTextContent("executing"),
    );

    for (const name of ["进行中", "待执行"]) {
      const heading = within(todo).getByRole("heading", { name });
      const section = heading.closest("section");
      expect(section).not.toBeNull();
      if (!section) throw new Error("任务分组缺少 section");
      expect(within(section).getByText("1")).toBeVisible();
    }
    const reviewHeading = within(todo).getByRole("heading", {
      name: "待人工验收",
    });
    const reviewSection = reviewHeading.closest("section");
    expect(reviewSection).not.toBeNull();
    if (!reviewSection) throw new Error("待人工验收分组缺少 section");
    expect(within(reviewSection).getByText("0")).toBeVisible();
    expect(within(todo).getByText("实现功能")).toBeVisible();
    expect(within(todo).getByText("补充测试")).toBeVisible();
    for (const name of ["已完成", "失败", "已取消"]) {
      const heading = within(todo).getByRole("heading", { name });
      const section = heading.closest("section");
      expect(section).not.toBeNull();
      if (!section) throw new Error("任务分组缺少 section");
      expect(within(section).getByText("1")).toBeVisible();
    }
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(screen.queryByText(/并发任务/)).not.toBeInTheDocument();
    expect(within(todo).getByText("梳理需求")).toBeVisible();
    expect(within(todo).getAllByText(/执行成员/)).toHaveLength(5);
    expect(within(todo).getByText("完成结果详情")).toBeVisible();
    expect(within(todo).getByText("验证构建")).toBeVisible();
    expect(within(todo).getByText("验证失败路径")).toBeVisible();
    expect(within(todo).queryByText("构建失败")).not.toBeInTheDocument();
    expect(
      within(todo).queryByText("projects/share/failed.ts"),
    ).not.toBeInTheDocument();
    expect(
      within(todo).queryByText("失败结果不应展示"),
    ).not.toBeInTheDocument();
  });

  it("按不透明游标加载并前置更早的团队聚合消息", async () => {
    teamThreadFixture.timelineHasMore = true;
    teamThreadFixture.timelineNextCursor = "opaque-earlier-page";

    renderPage("/teams/channel-1/threads/thread-1");

    fireEvent.click(
      await screen.findByRole("button", { name: "加载更早团队消息" }),
    );

    await waitFor(() =>
      expect(teamThreadFixture.loadTimeline).toHaveBeenCalled(),
    );
    expect(teamThreadFixture.loadTimeline).toHaveBeenCalledWith(
      "opaque-earlier-page",
    );
    expect(screen.getByTestId("timeline-messages")).toHaveTextContent(
      "earlier-result,member-result",
    );
    expect(
      screen.queryByRole("button", { name: "加载更早团队消息" }),
    ).not.toBeInTheDocument();
  });

  it("最新快照移除临时投影后不从已加载历史中恢复它", async () => {
    teamThreadFixture.timelineHasMore = true;
    teamThreadFixture.timelineNextCursor = "opaque-earlier-page";
    teamThreadFixture.timeline = [
      {
        chatId: "lead-chat",
        message: { id: "channel-task-input:temporary", role: "user" },
        wakerId: "",
      },
    ];
    renderPage("/teams/channel-1/threads/thread-1");

    fireEvent.click(
      await screen.findByRole("button", { name: "加载更早团队消息" }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("timeline-messages")).toHaveTextContent(
        "earlier-result,channel-task-input:temporary",
      ),
    );

    teamThreadFixture.timelineHasMore = false;
    teamThreadFixture.timelineNextCursor = undefined;
    teamThreadFixture.timeline = [
      {
        chatId: "lead-chat",
        message: { id: "real-user-message", role: "user" },
        wakerId: "",
      },
    ];
    fireEvent.click(screen.getByRole("button", { name: "恢复团队消息快照" }));

    await waitFor(() =>
      expect(screen.getByTestId("timeline-messages")).toHaveTextContent(
        "earlier-result,real-user-message",
      ),
    );
    expect(screen.getByTestId("timeline-messages")).not.toHaveTextContent(
      "channel-task-input:temporary",
    );
  });

  it("把负责人协调执行标识交给团队主 Chat 恢复实时状态", async () => {
    teamThreadFixture.leadActiveExecutionId = "lead-execution-1";
    teamThreadFixture.tasks = [];

    renderPage("/teams/channel-1/threads/thread-1");

    await waitFor(() =>
      expect(screen.getByTestId("lead-execution")).toHaveTextContent(
        "lead-execution-1",
      ),
    );
    expect(screen.getByTestId("work-state")).toHaveTextContent("running");
  });

  it("channel 已缓存时 thread 加载在 shell 内渐进完成，不整页阻塞", async () => {
    teamThreadFixture.threadQueryDelay = 300;

    renderPage("/teams/channel-1/threads/thread-1", { seedChannel: true });

    expect(screen.queryByText("正在载入团队对话")).not.toBeInTheDocument();
    expect(
      await screen.findByTestId("thread-detail-loading"),
    ).toHaveTextContent("loading");
    expect(screen.getByTestId("member-count")).toHaveTextContent("2");
    const loadingTaskBoard = screen.getByRole("region", {
      name: "看板",
    });
    for (const name of [
      "待执行",
      "进行中",
      "待人工验收",
      "已完成",
      "失败",
      "已取消",
    ]) {
      expect(
        within(loadingTaskBoard).getByRole("heading", { name }),
      ).toBeVisible();
    }

    await waitFor(() =>
      expect(screen.getByTestId("thread-detail-loading")).toHaveTextContent(
        "ready",
      ),
    );
    await waitFor(() =>
      expect(screen.getByTestId("selected-chat")).toHaveTextContent(
        "lead-chat",
      ),
    );
  });

  it("channel 冷启动也在 shell 内 loading，不整页阻塞", async () => {
    renderPage("/teams/channel-1/threads/thread-1");

    expect(screen.queryByText("正在载入团队对话")).not.toBeInTheDocument();
    expect(
      await screen.findByTestId("channel-detail-loading"),
    ).toHaveTextContent("loading");
    await waitFor(() =>
      expect(screen.getByTestId("channel-detail-loading")).toHaveTextContent(
        "ready",
      ),
    );
  });

  it("保存的会话已失效时清理偏好并回退到唯一团队草稿", async () => {
    const onThreadMissing = vi.fn();
    teamThreadFixture.threadQueryError = new ApiError(
      "会话不存在",
      "ChannelThreadNotFound",
      404,
    );

    renderPage("/teams/channel-1/threads/deleted-thread", {
      onThreadMissing,
    });

    await waitFor(() =>
      expect(onThreadMissing).toHaveBeenCalledWith(
        "channel-1",
        "deleted-thread",
      ),
    );
    expect(await screen.findByLabelText("location")).toHaveTextContent(
      /\/teams\/channel-1\?draftId=.+/,
    );
  });
  it("隐藏Scene释放团队消费且不补读，恢复只读取最新窗口并保留已翻旧页", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
    });
    let currentContext: TeamThreadChatContext | undefined;
    const surface = (active: boolean) => (
      <QueryClientProvider client={queryClient}>
        <AppSceneWorkspaceContextProvider
          value={{
            routeSceneId: "team",
            snapshot: {
              activeTabId: active ? "team" : "other",
              tabs: [
                {
                  id: "team",
                  label: "团队",
                  kind: "route",
                  lastUsed: 0,
                  mountKey: "team",
                },
              ],
            },
            dispatch: () => {},
            syncWorkspace: () => {},
          }}
        >
          <MemoryRouter initialEntries={["/teams/channel-1/threads/thread-1"]}>
            <Routes>
              <Route
                path="/teams/:channelId/threads/:threadId"
                element={
                  <TeamThreadPage
                    renderChat={(props) => {
                      currentContext = props.teamContext;
                      return renderTeamChat(props);
                    }}
                  />
                }
              />
            </Routes>
          </MemoryRouter>
        </AppSceneWorkspaceContextProvider>
      </QueryClientProvider>
    );
    teamThreadFixture.timelineHasMore = true;
    teamThreadFixture.timelineNextCursor = "old-window";
    const view = render(surface(true));
    await waitFor(() =>
      expect(currentContext?.messageSource.snapshot.ready).toBe(true),
    );
    await act(async () => {
      await currentContext?.onLoadEarlierTimelineMessages();
    });
    expect(teamThreadFixture.timelineHeadReads).toHaveBeenCalledTimes(1);
    expect(teamThreadFixture.loadTimeline).toHaveBeenCalledTimes(1);
    expect(teamThreadFixture.channelEventHandler).toBeDefined();
    view.rerender(surface(false));
    await waitFor(() =>
      expect(teamThreadFixture.channelEventHandler).toBeUndefined(),
    );
    expect(teamThreadFixture.timelineHeadReads).toHaveBeenCalledTimes(1);
    teamThreadFixture.timelineHasMore = false;
    teamThreadFixture.timelineNextCursor = undefined;
    view.rerender(surface(true));
    await waitFor(() =>
      expect(teamThreadFixture.timelineHeadReads).toHaveBeenCalledTimes(2),
    );
    expect(teamThreadFixture.loadTimeline).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("timeline-messages")).toHaveTextContent(
      "earlier-result,member-result",
    );
    view.unmount();
    queryClient.clear();
  });
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
