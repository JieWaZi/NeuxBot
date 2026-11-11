import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { observeRenderedState } from "@/test/observe-rendered-state";

const teamPageMounts = vi.hoisted(() => ({ current: 0 }));
const workflowPageMounts = vi.hoisted(() => ({ current: 0 }));

vi.mock("@/modules/connector/pages/connector-add-page", () => ({
  ConnectorAddPage: () => <div>添加连接器页面已就绪</div>,
}));
vi.mock("@/modules/connector/pages/connector-detail-page", () => ({
  ConnectorDetailPage: () => <div>连接器详情已就绪</div>,
}));
vi.mock("@/modules/plugin/pages/plugin-connector-detail-page", async () => {
  const { useParams, useSearchParams } = await import("react-router-dom");
  return {
    PluginConnectorDetailPage: () => {
      const params = useParams();
      const [search] = useSearchParams();
      return (
        <div>
          插件连接器详情已就绪 {params.catalogKey || params.pluginId}{" "}
          {params.connectorKey} {search.get("sourceCommit")}{" "}
          {search.get("archiveSHA256")}
        </div>
      );
    },
  };
});
vi.mock("@/modules/skill/pages/skill-add-page", () => ({
  SkillAddPage: () => <div>添加技能页面已就绪</div>,
}));
vi.mock("@/modules/skill/pages/skill-detail-page", () => ({
  SkillDetailPage: () => <div>技能详情已就绪</div>,
}));
vi.mock("@/modules/memory/pages/waker-memory-page", () => ({
  WakerMemoryPage: () => <div>记忆页面已就绪</div>,
}));
vi.mock("./pages/resources-page", () => ({
  ResourcesPage: () => <div>资源库已就绪</div>,
}));
vi.mock("@/modules/workflow/pages/workflow-detail-page", async () => {
  const { useEffect } = await import("react");
  const { useParams } = await import("react-router-dom");
  return {
    WorkflowDetailPage: () => {
      const { runId } = useParams();
      useEffect(() => {
        workflowPageMounts.current += 1;
      }, []);
      return <div>{runId ? "工作流运行已就绪" : "工作流定义已就绪"}</div>;
    },
  };
});
vi.mock("@/modules/workflow/pages/workflow-run-page", () => ({
  WorkflowRunPage: () => <div>工作流运行已就绪</div>,
}));
vi.mock("./pages/team-thread-page", async () => {
  const { useEffect } = await import("react");
  return {
    TeamThreadPage: () => {
      useEffect(() => {
        teamPageMounts.current += 1;
      }, []);
      return <div>团队页面</div>;
    },
  };
});

import { AppRouteScene } from "./scenes/app-route-scene";

const outletContext = {
  chatRightPanel: undefined,
  onTeamThreadDeleted: vi.fn(),
  setChatRightPanel: vi.fn(),
};

function routeScene(location: string) {
  return (
    <MemoryRouter>
      <AppRouteScene location={location} outletContext={outletContext} />
    </MemoryRouter>
  );
}

describe("公共路由加载", () => {
  it("工作流切换运行与优化时保持页面挂载", async () => {
    workflowPageMounts.current = 0;
    const { rerender } = render(routeScene("/workflows/flow-1"));
    await screen.findByText("工作流定义已就绪");
    const initial = workflowPageMounts.current;
    rerender(routeScene("/workflows/flow-1/runs/run-1?result=1"));
    await screen.findByText("工作流运行已就绪");
    rerender(routeScene("/workflows/flow-1?optimizeRun=run-1"));
    await screen.findByText("工作流定义已就绪");
    expect(workflowPageMounts.current).toBe(initial);
  });
  it.each([
    ["/resources?category=workflows", "资源库已就绪"],
    ["/workflows/new", "工作流定义已就绪"],
    ["/workflows/workflow-1", "工作流定义已就绪"],
    ["/workflows/workflow-1/runs/run-1", "工作流运行已就绪"],
  ])("工作流入口 %s 首次渲染立即挂载页面", async (location, readyText) => {
    render(routeScene(location));

    expect(screen.getByText(readyText)).toBeVisible();
    expect(
      screen.queryByRole("status", { name: "正在载入页面" }),
    ).not.toBeInTheDocument();
  });

  it.each([
    "/resources/skills/new",
    "/wakers/waker-1/resources/skills/new",
    "/wakers/waker-1/skills/new",
  ])("技能添加路由 %s 立即挂载独立页", (location) => {
    render(routeScene(location));
    expect(screen.getByText("添加技能页面已就绪")).toBeVisible();
    expect(screen.queryByText("技能详情已就绪")).not.toBeInTheDocument();
  });

  it.each([
    "/resources/connectors/new",
    "/wakers/waker-1/resources/connectors/new",
    "/wakers/waker-1/connectors/new",
  ])("连接器添加路由 %s 立即挂载独立页", (location) => {
    render(routeScene(location));
    expect(screen.getByText("添加连接器页面已就绪")).toBeVisible();
    expect(screen.queryByText("连接器详情已就绪")).not.toBeInTheDocument();
  });

  it("目录插件连接器详情匹配固定来源参数", () => {
    render(
      routeScene(
        "/resources/plugins/catalog/github/connectors/tools?sourceCommit=commit-1&archiveSHA256=digest-1",
      ),
    );
    expect(screen.getByText(/插件连接器详情已就绪/)).toHaveTextContent(
      "github tools commit-1 digest-1",
    );
    expect(screen.queryByText("连接器详情已就绪")).not.toBeInTheDocument();
  });

  it("伙伴功能页冷加载不使用整页 Spinner，同模块内切换不再触发 Suspense", async () => {
    const { container, rerender } = render(
      routeScene("/wakers/waker-1/skills/skill-1"),
    );
    expect(container.querySelector("[data-route-loading-frame]")).toBeNull();
    expect(
      screen.queryByRole("status", { name: "正在载入页面" }),
    ).not.toBeInTheDocument();
    expect(await screen.findByText("技能详情已就绪")).toBeVisible();
    expect(container.querySelector("[data-route-loading-frame]")).toBeNull();

    rerender(routeScene("/wakers/waker-1/memory"));
    await screen.findByText("记忆页面已就绪");
    const states = observeRenderedState(container, () =>
      Boolean(screen.queryByRole("status", { name: "正在载入页面" })),
    );
    rerender(routeScene("/wakers/waker-1/skills/skill-1"));
    await waitFor(() =>
      expect(screen.getByText("技能详情已就绪")).toBeVisible(),
    );
    states.disconnect();
    expect(states.samples).not.toContain(true);
  });

  it("团队草稿提交为 Thread 时不重新挂载页面", async () => {
    teamPageMounts.current = 0;
    const { rerender } = render(routeScene("/teams/team-1?draftId=draft-a"));
    expect(await screen.findByText("团队页面")).toBeVisible();
    const mountsAfterDraft = teamPageMounts.current;

    rerender(routeScene("/teams/team-1/threads/thread-1"));

    expect(await screen.findByText("团队页面")).toBeVisible();
    expect(teamPageMounts.current).toBe(mountsAfterDraft);
    expect(
      screen.queryByRole("status", { name: /正在载入/ }),
    ).not.toBeInTheDocument();
  });
});
