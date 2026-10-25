import {
  QueryClient,
  QueryClientProvider,
  type QueryKey,
} from "@tanstack/react-query";
import { act, render, renderHook, screen } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import "@/app/workspace-i18n";
import { client } from "@/shared/api/generated/client.gen";
import { initializeApi } from "@/shared/api/runtime";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import {
  getChannelQueryKey,
  getChannelThreadQueryKey,
  getChatQueryKey,
  getGlobalAutomaticTaskQueryKey,
  getProjectQueryKey,
  getWakerConnectorQueryKey,
  getWakerQueryKey,
  getWakerSkillQueryKey,
  getWorkflowQueryKey,
  getWorkflowRunQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { useAppSceneWorkspace } from "@/shared/workspace/app-scene-workspace";
import { AppSceneWorkspaceProvider } from "./app-scene-workspace-provider";

vi.mock("@/shared/platform", () => ({
  getCoreConnection: async () => ({
    baseUrl: "http://localhost:19876",
    token: "test-token",
  }),
}));

beforeAll(async () => {
  await initializeApi();
});
afterEach(() => {
  const { result, unmount } = renderHook(useErrorNotices);
  act(() => result.current.forEach((notice) => dismissErrorNotice(notice.id)));
  unmount();
  vi.restoreAllMocks();
});

// 生成键和生产请求使用同一连接配置，避免用初始化前的空地址构造夹具。
client.setConfig({ baseUrl: "http://localhost:19876" });

function SceneTabs() {
  const { snapshot, dispatch } = useAppSceneWorkspace();
  const notices = useErrorNotices();
  return (
    <>
      <SceneTabStrip
        activeTabId={snapshot.activeTabId || ""}
        ariaLabel="场景标签"
        tabs={snapshot.tabs}
        onSelect={(tabId) => dispatch({ type: "select", tabId })}
        onClose={(tabId) => dispatch({ type: "close", tabId })}
      />
      <output aria-label="错误通知数量">{notices.length}</output>
    </>
  );
}

function renderScene(href: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const navigate = vi.fn();
  const content = (nextHref: string) => {
    const [pathname = "", search = ""] = nextHref.split("?");
    return (
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <AppSceneWorkspaceProvider
            location={{ pathname, search: search ? `?${search}` : "" }}
            navigate={navigate}
          >
            <SceneTabs />
          </AppSceneWorkspaceProvider>
        </TooltipProvider>
      </QueryClientProvider>
    );
  };
  const view = render(content(href));
  return {
    queryClient,
    rerender: (nextHref: string) => view.rerender(content(nextHref)),
  };
}

async function fetchRecord(
  queryClient: QueryClient,
  queryKey: QueryKey,
  url: string,
  status = 404,
) {
  client.setConfig({
    fetch: vi.fn(
      async () =>
        new Response(
          JSON.stringify(
            status === 200 ? {} : { key: "ReadFailed", message: "记录不存在" },
          ),
          { status, headers: { "Content-Type": "application/json" } },
        ),
    ),
  });
  await act(async () => {
    const result = queryClient.fetchQuery({
      queryKey,
      queryFn: () => client.get({ url, throwOnError: true }),
    });
    if (status === 200) await result;
    else await expect(result).rejects.toMatchObject({ status });
  });
}

describe("页面主记录不存在时的 Scene 标签", () => {
  it("资源库直接打开、切换分类和详情时始终保留资源库标题", () => {
    const { rerender } = renderScene("/resources");
    const tab = screen.getByRole("tab", { name: "资源库" });
    for (const href of [
      "/resources?category=skills",
      "/resources?category=workflows",
      "/resources/plugins/catalog/office",
      "/resources/plugins/p/skills/guide?version=v1",
      "/resources?view=mine",
    ]) {
      rerender(href);
      expect(screen.getByRole("tab", { name: "资源库" })).toBe(tab);
      expect(
        screen.queryByRole("tab", { name: "页面" }),
      ).not.toBeInTheDocument();
      expect(screen.getAllByRole("tab")).toHaveLength(1);
    }
  });
  it("任务看板筛选后保留标签标题与同一 Scene", () => {
    const { rerender } = renderScene("/work-board");
    const tab = screen.getByRole("tab", { name: "任务看板" });
    for (const href of [
      "/work-board?source=automatic",
      "/work-board?source=automatic&states=failed&page=2",
      "/work-board",
    ]) {
      rerender(href);
      expect(screen.getByRole("tab", { name: "任务看板" })).toBe(tab);
      expect(screen.queryByRole("tab", { name: "页面" })).toBeNull();
      expect(screen.getAllByRole("tab")).toHaveLength(1);
    }
  });

  it.each([
    ["/im", "设置"],
    ["/automatic-tasks", "自动任务"],
  ])("%s 在列表、不同详情与返回列表之间复用同一标签", (listHref, label) => {
    const { rerender } = renderScene(listHref);
    const tab = screen.getByRole("tab", { name: label });
    for (const href of [
      `${listHref}/first`,
      "/chats/c1",
      `${listHref}/second?historyPage=2`,
      listHref,
    ]) {
      rerender(href);
      expect(screen.getByRole("tab", { name: label })).toBe(tab);
    }
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    expect(tab).toHaveAttribute("aria-selected", "true");
  });

  it("自动任务详情失效后返回列表清除标签错误状态", async () => {
    const { queryClient, rerender } = renderScene("/automatic-tasks/a1");
    await fetchRecord(
      queryClient,
      getGlobalAutomaticTaskQueryKey({ path: { taskId: "a1" } }),
      "/api/v1/automatic-tasks/a1",
    );
    const tab = screen.getByRole("tab", { name: "自动任务" });
    expect(tab).toHaveAttribute("aria-description");
    rerender("/automatic-tasks");
    expect(screen.getByRole("tab", { name: "自动任务" })).not.toHaveAttribute(
      "aria-description",
    );
    expect(screen.getAllByRole("tab")).toHaveLength(1);
  });

  it.each([
    [
      "/chats/c1",
      getChatQueryKey({ path: { chatId: "c1" } }),
      "/api/v1/chats/c1",
    ],
    [
      "/wakers/w1",
      getWakerQueryKey({ path: { wakerId: "w1" } }),
      "/api/v1/wakers/w1",
    ],
    [
      "/wakers/w1/projects/p1",
      getProjectQueryKey({ path: { wakerId: "w1", projectId: "p1" } }),
      "/api/v1/wakers/w1/projects/p1",
    ],
    [
      "/wakers/w1/skills/s1",
      getWakerSkillQueryKey({ path: { wakerId: "w1", skillId: "s1" } }),
      "/api/v1/wakers/w1/skills/s1",
    ],
    [
      "/wakers/w1/connectors/m1",
      getWakerConnectorQueryKey({ path: { wakerId: "w1", connectorId: "m1" } }),
      "/api/v1/wakers/w1/connectors/m1",
    ],
    [
      "/teams/t1",
      getChannelQueryKey({ path: { channelId: "t1" } }),
      "/api/v1/channels/t1",
    ],
    [
      "/teams/t1/threads/th1",
      getChannelThreadQueryKey({ path: { channelId: "t1", threadId: "th1" } }),
      "/api/v1/channels/t1/threads/th1",
    ],
    [
      "/automatic-tasks/a1",
      getGlobalAutomaticTaskQueryKey({ path: { taskId: "a1" } }),
      "/api/v1/automatic-tasks/a1",
    ],
    [
      "/workflows/f1?revision=r1",
      getWorkflowQueryKey({ path: { id: "f1" }, query: { revisionId: "r1" } }),
      "/api/v1/workflows/f1",
    ],
    [
      "/workflows/f1/runs/r1",
      getWorkflowRunQueryKey({ path: { id: "f1", runId: "r1" } }),
      "/api/v1/workflows/f1/runs/r1",
    ],
  ])(
    "%s 的主记录删除后显示失效标签且不通知，恢复成功后清除状态",
    async (href, queryKey, url) => {
      const { queryClient } = renderScene(href);
      act(() => queryClient.setQueryData(queryKey, {}));
      await fetchRecord(queryClient, queryKey, url);
      const tab = screen.getByRole("tab");
      expect(tab).toHaveAccessibleDescription("数据可能已被删除，请关闭该页面");
      expect(tab.parentElement).toHaveClass("bg-destructive/10");
      expect(screen.getByLabelText("错误通知数量")).toHaveTextContent("0");
      await fetchRecord(queryClient, queryKey, url, 200);
      expect(screen.getByRole("tab")).not.toHaveAttribute("aria-description");
    },
  );

  it("切换到其他 Scene 仍保留失效标记，同一伙伴内改为列表页则恢复正常", async () => {
    const { queryClient, rerender } = renderScene("/wakers/w1/projects/p1");
    await fetchRecord(
      queryClient,
      getProjectQueryKey({ path: { wakerId: "w1", projectId: "p1" } }),
      "/api/v1/wakers/w1/projects/p1",
    );
    rerender("/chats/c1");
    expect(
      screen.getByRole("tab", { name: "详情" }),
    ).toHaveAccessibleDescription("数据可能已被删除，请关闭该页面");
    expect(screen.getByRole("tab", { name: "会话" })).not.toHaveAttribute(
      "aria-description",
    );
    rerender("/wakers/w1/projects");
    expect(screen.getByRole("tab", { name: "详情" })).not.toHaveAttribute(
      "aria-description",
    );
  });

  it("关联项目的 404 不使自动任务页面失效", async () => {
    const { queryClient } = renderScene("/automatic-tasks/a1");
    await fetchRecord(
      queryClient,
      getProjectQueryKey({ path: { wakerId: "w1", projectId: "p1" } }),
      "/api/v1/wakers/w1/projects/p1",
    );
    expect(screen.getByRole("tab")).not.toHaveAttribute("aria-description");
  });
});
