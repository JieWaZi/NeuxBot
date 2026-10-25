import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  MemoryRouter,
  Outlet,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";

import { TeamThreadListPage } from "./team-thread-list-page";

const fixture = vi.hoisted(() => ({
  listQueries: [] as Array<{
    pageNum: number;
    pageSize: number;
    query?: string;
    updatedAtEnd?: number;
    updatedAtStart?: number;
  }>,
  onDeleted: vi.fn(),
  remove: vi.fn(
    async (request: { path: { channelId: string; threadId: string } }) => {
      void request;
      return {};
    },
  ),
}));

vi.mock("../api/channel-queries", () => ({
  deleteChannelThreadMutation: () => ({
    mutationFn: (request: { path: { channelId: string; threadId: string } }) =>
      fixture.remove(request),
  }),
  getChannelOptions: ({ path }: { path: { channelId: string } }) => ({
    queryKey: ["channel", path.channelId],
    queryFn: async () => ({
      channel: { id: path.channelId, name: "研发团队" },
    }),
  }),
  getChannelThreadQueryKey: ({
    path,
  }: {
    path: { channelId: string; threadId: string };
  }) => ["channel-thread", path.channelId, path.threadId],
  listChannelThreadsOptions: ({
    path,
    query,
  }: {
    path: { channelId: string };
    query: {
      pageNum: number;
      pageSize: number;
      query?: string;
      updatedAtEnd?: number;
      updatedAtStart?: number;
    };
  }) => ({
    queryKey: ["channel-threads", path.channelId, query],
    queryFn: async () => {
      fixture.listQueries.push(query);
      const source =
        query.pageNum > 1
          ? [
              {
                additionalWorkingDirectories: undefined,
                activityState: "waiting" as const,
                id: "thread-second-page",
                projectName: "NeuxBot",
                title: "第二页会话",
                updatedAt: 1_788_512_300_000,
              },
            ]
          : [
              {
                activityState: "running" as const,
                id: "thread-frontend",
                projectName: "NeuxBot",
                title: "前端整改",
                updatedAt: 1_788_512_400_000,
              },
              {
                additionalWorkingDirectories: ["/tmp/backend"],
                activityState: "idle" as const,
                id: "thread-backend",
                title: "后端排查",
                updatedAt: 1_788_512_340_000,
              },
            ];
      const resources = source.filter(
        (thread) =>
          !query.query ||
          [
            thread.title,
            thread.projectName,
            thread.additionalWorkingDirectories?.[0],
          ].some((value) => value?.includes(query.query ?? "")),
      );
      return {
        hasMore: false,
        pageNum: query.pageNum,
        pageSize: query.pageSize,
        resources,
        totalSize: query.query ? resources.length : 11,
      };
    },
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

function Layout() {
  return <Outlet context={{ onTeamThreadDeleted: fixture.onDeleted }} />;
}

function LocationOutput() {
  const location = useLocation();
  return <output aria-label="location">{location.pathname}</output>;
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceProvider>
        <MemoryRouter initialEntries={["/teams/team-1/chats"]}>
          <Routes>
            <Route element={<Layout />}>
              <Route
                path="teams/:channelId/chats"
                element={<TeamThreadListPage />}
              />
              <Route
                path="teams/:channelId/threads/:threadId"
                element={<LocationOutput />}
              />
            </Route>
          </Routes>
        </MemoryRouter>
      </WorkspaceProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("TeamThreadListPage", () => {
  beforeEach(() => {
    fixture.onDeleted.mockClear();
    fixture.remove.mockClear();
    fixture.listQueries.length = 0;
  });

  it("展示团队对话任务表格并按当前查询过滤", async () => {
    renderPage();
    const user = userEvent.setup();

    expect(
      screen.getByRole("heading", { name: "对话任务" }),
    ).toBeInTheDocument();
    const table = await screen.findByRole("table", { name: "对话任务列表" });
    await screen.findByText("前端整改");
    expect(table).toHaveTextContent("前端整改");
    expect(table).toHaveTextContent("后端排查");
    expect(table).toHaveTextContent("NeuxBot");
    expect(table).toHaveTextContent("backend");

    await user.type(
      screen.getByRole("searchbox", { name: "查询名称" }),
      "前端",
    );
    await waitFor(() => expect(table).not.toHaveTextContent("后端排查"));
    expect(table).toHaveTextContent("前端整改");
    expect(fixture.listQueries.at(-1)).toMatchObject({ query: "前端" });
  });

  it("使用与伙伴对话任务一致的日期筛选和每页条数", async () => {
    renderPage();
    const user = userEvent.setup();
    await screen.findByRole("table", { name: "对话任务列表" });
    await screen.findByText("前端整改");

    await user.click(screen.getByRole("button", { name: "每页条数" }));
    await user.click(screen.getByRole("menuitemradio", { name: "30 条/页" }));
    await waitFor(() =>
      expect(fixture.listQueries.at(-1)).toMatchObject({
        pageNum: 1,
        pageSize: 30,
      }),
    );

    await user.click(screen.getByRole("button", { name: "选择最近活动时间" }));
    const dayButtons = within(screen.getByRole("grid")).getAllByRole("button");
    const startDate = dayButtons[10];
    const endDate = dayButtons[12];
    if (!startDate || !endDate) throw new Error("日历缺少可选择的日期");
    await user.click(startDate);
    await user.click(endDate);
    await waitFor(() =>
      expect(fixture.listQueries.at(-1)).toMatchObject({
        updatedAtEnd: expect.any(Number),
        updatedAtStart: expect.any(Number),
      }),
    );
  });

  it("使用自定义页码按钮翻页并可打开目标会话", async () => {
    renderPage();
    const user = userEvent.setup();

    await screen.findByText("前端整改");
    await user.click(screen.getByRole("button", { name: "下一页" }));
    expect(await screen.findByText("第二页会话")).toBeInTheDocument();
    expect(fixture.listQueries.at(-1)).toMatchObject({
      pageNum: 2,
    });
    expect(screen.getByRole("button", { name: "前往第 2 页" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    await user.click(screen.getByRole("link", { name: "第二页会话" }));
    expect(await screen.findByLabelText("location")).toHaveTextContent(
      "/teams/team-1/threads/thread-second-page",
    );
  });

  it("确认后删除会话并通知导航清理上次会话", async () => {
    renderPage();
    const user = userEvent.setup();

    await user.click(
      await screen.findByRole("button", { name: "前端整改的会话操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "删除会话" }));
    await user.click(screen.getByRole("button", { name: "确认删除" }));

    await waitFor(() =>
      expect(fixture.remove).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { channelId: "team-1", threadId: "thread-frontend" },
        }),
      ),
    );
    expect(fixture.onDeleted).toHaveBeenCalledWith("team-1", "thread-frontend");
  });
});
