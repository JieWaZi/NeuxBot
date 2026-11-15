import { Activity, StrictMode, useEffect } from "react";
import { createPortal, flushSync } from "react-dom";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
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
  useSceneChromeAccessoryWidth,
  useSceneChromeAccessoryWidthSetter,
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
  it("顶栏挂入宿主后再聚焦并测量操作区，暂停恢复后仍取得实际宽度", () => {
    const rect = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function (this: HTMLElement) {
        return new DOMRect(0, 0, this.isConnected ? 80 : 0, 38);
      });
    const view = (active: boolean) => (
      <SceneChromePortalProvider>
        <SceneChromeHost />
        <Activity mode={active ? "visible" : "hidden"}>
          <SceneChromePortal>
            <SceneChromeLayout actions={<button>操作</button>}>
              <input aria-label="顶栏输入" autoFocus />
            </SceneChromeLayout>
          </SceneChromePortal>
        </Activity>
      </SceneChromePortalProvider>
    );
    try {
      const { rerender } = render(view(true));
      expect(screen.getByRole("textbox", { name: "顶栏输入" })).toHaveFocus();
      const expectActionWidth = () =>
        expect(
          document.querySelector("[data-scene-chrome-accessory-wrapper]"),
        ).toHaveStyle({ "--scene-chrome-actions-width": "80px" });
      expectActionWidth();
      rerender(view(false));
      rerender(view(true));
      expectActionWidth();
    } finally {
      rect.mockRestore();
    }
  });

  it("严格模式下暂停 Scene 同步移出顶栏，恢复时保留同一节点与输入", () => {
    const view = (active: boolean) => (
      <StrictMode>
        <SceneChromePortalProvider>
          <SceneChromeHost />
          <Activity mode={active ? "visible" : "hidden"}>
            <main>
              <SceneChromePortal>
                <SceneChromeLayout>
                  <input aria-label="顶栏草稿" defaultValue="" />
                </SceneChromeLayout>
              </SceneChromePortal>
            </main>
          </Activity>
        </SceneChromePortalProvider>
      </StrictMode>
    );
    const { rerender } = render(view(true));
    const input = screen.getByRole("textbox", { name: "顶栏草稿" });
    fireEvent.change(input, { target: { value: "保留输入" } });
    for (let index = 0; index < 3; index += 1) {
      act(() => {
        flushSync(() => rerender(view(false)));
        expect(input).not.toBeInTheDocument();
      });
      rerender(view(true));
      expect(screen.getByRole("textbox", { name: "顶栏草稿" })).toBe(input);
      expect(input).toHaveValue("保留输入");
      expect(
        document.querySelectorAll(
          "[data-scene-chrome-host] [data-scene-tab-strip]",
        ),
      ).toHaveLength(1);
    }
  });

  it("相同操作区的新 JSX 不重建布局观察器，尺寸变化由既有观察器测量", () => {
    const observe = vi.fn();
    const disconnect = vi.fn();
    const callbacks: ResizeObserverCallback[] = [];
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(callback: ResizeObserverCallback) {
          callbacks.push(callback);
        }
        observe = observe;
        disconnect = disconnect;
      },
    );
    const rect = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockReturnValue({ width: 80 } as DOMRect);
    try {
      const view = render(
        <SceneChromeLayout actions={<button>操作</button>}>
          标签
        </SceneChromeLayout>,
      );
      expect(observe).toHaveBeenCalledTimes(1);
      rect.mockClear();
      view.rerender(
        <SceneChromeLayout actions={<button>操作</button>}>
          标签
        </SceneChromeLayout>,
      );
      expect(observe).toHaveBeenCalledTimes(1);
      expect(disconnect).not.toHaveBeenCalled();
      expect(rect).not.toHaveBeenCalled();
      rect.mockReturnValue({ width: 120 } as DOMRect);
      act(() => callbacks[0]?.([], {} as ResizeObserver));
      expect(
        document.querySelector("[data-scene-chrome-accessory-wrapper]"),
      ).toHaveStyle({ "--scene-chrome-actions-width": "120px" });
      expect(observe).toHaveBeenCalledTimes(1);
      view.unmount();
      expect(disconnect).toHaveBeenCalledTimes(1);
    } finally {
      rect.mockRestore();
      vi.unstubAllGlobals();
    }
  });

  it("重复登记同一工作台宽度不广播顶栏 Context", () => {
    const renders = vi.fn();
    function Probe() {
      const width = useSceneChromeAccessoryWidth();
      renders(width);
      return <output>{width}</output>;
    }
    function Writer() {
      const setWidth = useSceneChromeAccessoryWidthSetter();
      useEffect(() => {
        setWidth?.("owner", 320);
      }, [setWidth]);
      return <button onClick={() => setWidth?.("owner", 320)}>重复宽度</button>;
    }
    render(
      <SceneChromePortalProvider>
        <Probe />
        <Writer />
      </SceneChromePortalProvider>,
    );
    renders.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "重复宽度" }));
    expect(renders).not.toHaveBeenCalled();
  });

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
  { name: "设置／通用", retained: false, suspended: false, labels: ["通用"] },
  {
    name: "设置／智能体详情",
    retained: false,
    suspended: false,
    labels: ["智能体", "Claude"],
  },
  {
    name: "设置／集成连接",
    retained: false,
    suspended: false,
    labels: ["集成", "飞书", "工作群"],
  },
  {
    name: "工作流详情",
    retained: true,
    suspended: false,
    labels: ["工作流", "日报"],
  },
  {
    name: "工作流运行",
    retained: true,
    suspended: false,
    labels: ["工作流", "日报", "运行"],
  },
  { name: "资源库", retained: true, suspended: true, labels: ["资源库"] },
  {
    name: "伙伴详情",
    retained: true,
    suspended: true,
    labels: ["伙伴", "助手"],
  },
  {
    name: "知识库详情",
    retained: true,
    suspended: true,
    labels: ["知识库", "开发资料"],
  },
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
            <Activity
              mode={page.suspended && phase === "chat" ? "hidden" : "visible"}
            >
              <main hidden={phase === "chat"}>
                <SceneBreadcrumbs
                  ariaLabel="来源页面路径"
                  items={page.labels.map((label, index) => ({
                    href:
                      index < page.labels.length - 1
                        ? `/source/${index}`
                        : undefined,
                    label: lateUpdate ? `${label}（后台更新）` : label,
                  }))}
                />
              </main>
            </Activity>
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
      act(() => {
        flushSync(() =>
          view.rerender(<BreadcrumbView page={page} phase="chat" />),
        );
        expect(
          screen.queryByRole("navigation", { name: "来源页面路径" }),
        ).toBeNull();
      });
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
