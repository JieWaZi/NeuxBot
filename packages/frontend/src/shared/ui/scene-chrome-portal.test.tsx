import { StrictMode } from "react";
import { createPortal } from "react-dom";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MemoryRouter } from "react-router-dom";

import { AppSceneWorkspaceContextProvider } from "@/shared/workspace/app-scene-workspace";
import { SceneBreadcrumbs } from "./scene-breadcrumbs";
import { SceneTabStrip } from "./scene-tab-strip";

import {
  SceneChromeHost,
  SceneChromeLayout,
  SceneChromePaneActionsPortal,
  SceneChromePortal,
  SceneChromePortalProvider,
  useSceneChromeAccessoryTarget,
} from "./scene-chrome-portal";

function WorkbenchHeader() {
  const target = useSceneChromeAccessoryTarget();
  const header = <button>工作台标签</button>;
  return target ? createPortal(header, target) : <main>{header}</main>;
}

function View({
  resources,
  chat = !resources,
}: {
  resources: boolean;
  chat?: boolean;
}) {
  return (
    <SceneChromePortalProvider>
      <SceneChromeHost>
        {chat ? (
          <SceneChromeLayout>
            <span>聊天</span>
          </SceneChromeLayout>
        ) : null}
      </SceneChromeHost>
      {resources ? (
        <SceneChromePortal>
          <SceneChromeLayout>
            <span>资源库</span>
          </SceneChromeLayout>
        </SceneChromePortal>
      ) : (
        <SceneChromePaneActionsPortal>
          <button>会话摘要</button>
        </SceneChromePaneActionsPortal>
      )}
      <WorkbenchHeader />
    </SceneChromePortalProvider>
  );
}

describe("Scene chrome handoff", () => {
  it.each(["会话摘要", "工作台标签"])(
    "从资源库返回聊天后，%s仍留在共享顶栏",
    (label) => {
      const view = render(<View resources />);
      // 路由先更新 Shell，随后 Scene 的激活状态才完成交接。
      view.rerender(<View resources chat />);
      view.rerender(<View resources={false} />);
      const host = document.querySelector<HTMLElement>(
        "[data-scene-chrome-host]",
      );
      if (!host) throw new Error("顶栏未挂载");
      expect(within(host).getByRole("button", { name: label })).toBeVisible();
      expect(screen.queryByRole("main")).not.toBeInTheDocument();
    },
  );

  it("严格模式下反复切换资源库和聊天不会丢失顶栏内容", () => {
    const view = render(
      <StrictMode>
        <View resources />
      </StrictMode>,
    );
    for (let index = 0; index < 3; index += 1) {
      view.rerender(
        <StrictMode>
          <View resources chat />
        </StrictMode>,
      );
      view.rerender(
        <StrictMode>
          <View resources={false} />
        </StrictMode>,
      );
      const host = document.querySelector<HTMLElement>(
        "[data-scene-chrome-host]",
      );
      if (!host) throw new Error("顶栏未挂载");
      expect(
        within(host).getByRole("button", { name: "会话摘要" }),
      ).toBeVisible();
      expect(
        within(host).getByRole("button", { name: "工作台标签" }),
      ).toBeVisible();
      view.rerender(
        <StrictMode>
          <View resources />
        </StrictMode>,
      );
    }
  });
});

const sourcePages = [
  { name: "设置／通用", retained: false, labels: ["通用"] },
  { name: "设置／智能体详情", retained: false, labels: ["智能体", "Claude"] },
  {
    name: "设置／集成连接",
    retained: false,
    labels: ["集成", "飞书", "工作群"],
  },
  { name: "工作流详情", retained: true, labels: ["工作流", "日报"] },
  { name: "工作流运行", retained: true, labels: ["工作流", "日报", "运行"] },
  { name: "资源库", retained: true, labels: ["资源库"] },
  { name: "伙伴详情", retained: true, labels: ["伙伴", "助手"] },
  { name: "知识库详情", retained: true, labels: ["知识库", "开发资料"] },
];

type SourcePage = (typeof sourcePages)[number];
type HandoffPhase = "page" | "handoff" | "chat";

function BreadcrumbView({
  page,
  phase,
  lateUpdate = false,
}: {
  page: SourcePage;
  phase: HandoffPhase;
  lateUpdate?: boolean;
}) {
  return (
    <MemoryRouter>
      <SceneChromePortalProvider>
        <SceneChromeHost>
          {phase !== "page" ? (
            <SceneTabStrip
              activeTabId="chat"
              ariaLabel="聊天标签"
              tabs={[{ id: "chat", label: "当前聊天" }]}
              onSelect={() => {}}
              onClose={() => {}}
            />
          ) : null}
        </SceneChromeHost>
        <AppSceneWorkspaceContextProvider
          value={{
            routeSceneId: "page",
            snapshot: {
              activeTabId: phase === "chat" ? "chat" : "page",
              tabs: [
                { id: "page", kind: "route", label: page.name, lastUsed: 1 },
                { id: "chat", kind: "route", label: "当前聊天", lastUsed: 2 },
              ],
            },
            dispatch: () => {},
            syncWorkspace: () => {},
          }}
        >
          {page.retained || phase !== "chat" ? (
            <SceneBreadcrumbs
              ariaLabel="来源页面路径"
              items={page.labels.map((label) => ({
                label: lateUpdate ? `${label}（后台更新）` : label,
              }))}
            />
          ) : null}
        </AppSceneWorkspaceContextProvider>
        {phase === "chat" ? (
          <SceneChromePaneActionsPortal>
            <button>会话摘要</button>
          </SceneChromePaneActionsPortal>
        ) : null}
        <WorkbenchHeader />
      </SceneChromePortalProvider>
    </MemoryRouter>
  );
}

describe("页面顶栏返回聊天", () => {
  it.each(sourcePages)(
    "$name返回聊天后，标签与摘要不被旧页面卸载或后台更新移出顶栏",
    (page) => {
      const view = render(<BreadcrumbView page={page} phase="page" />);
      expect(
        screen.getByRole("navigation", { name: "来源页面路径" }),
      ).toBeVisible();
      view.rerender(<BreadcrumbView page={page} phase="handoff" />);
      view.rerender(<BreadcrumbView page={page} phase="chat" />);
      view.rerender(<BreadcrumbView page={page} phase="chat" lateUpdate />);
      const host = document.querySelector<HTMLElement>(
        "[data-scene-chrome-host]",
      );
      if (!host) throw new Error("顶栏未挂载");
      expect(within(host).getByRole("tab", { name: "当前聊天" })).toBeVisible();
      expect(
        within(host).getByRole("button", { name: "会话摘要" }),
      ).toBeVisible();
      expect(
        within(host).getByRole("button", { name: "工作台标签" }),
      ).toBeVisible();
      expect(
        screen.queryByRole("navigation", { name: "来源页面路径" }),
      ).toBeNull();
      expect(screen.queryByRole("main")).not.toBeInTheDocument();
      expect(host.querySelectorAll("[data-scene-tab-strip]")).toHaveLength(1);
    },
  );
});
