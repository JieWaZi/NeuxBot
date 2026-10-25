import type { AssistantRuntime } from "@assistant-ui/react";
import {
  act,
  createEvent,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/shared/i18n";
import * as desktop from "@/shared/platform";
import {
  SceneChromeHost,
  SceneChromePortal,
  SceneChromePortalProvider,
} from "@/shared/ui/scene-chrome-portal";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import { AppSceneWorkspaceContextProvider } from "@/shared/workspace/app-scene-workspace";
import {
  useSceneWorkspace,
  WorkspaceProvider,
} from "@/shared/workspace/workspace-context";
import { WorkbenchProvider, useWorkbench } from "../workbench/workbench-store";
import { workbenchDocumentBuffers } from "../workbench/document-buffers";
import { conversationKeyOf } from "../model/conversation-identity";
import {
  ChatWorkspaceSurface,
  type ChatWorkspaceAdapter,
} from "./chat-workspace-surface";
import type { ChatRightPanel } from "./chat-right-panel-host";

vi.mock("./chat-thread", () => ({
  ChatThread: ({
    isHistoryLoading,
    wakerName,
  }: {
    isHistoryLoading?: boolean;
    wakerName: string;
  }) => (
    <section aria-busy={isHistoryLoading} data-testid="contract-chat-thread">
      {wakerName}
    </section>
  ),
}));

const lifecycle = vi.hoisted(() => ({
  browserMounts: 0,
  browserUnmounts: 0,
  terminalMounts: 0,
  terminalUnmounts: 0,
}));
vi.mock("./chat-browser-view", async () => {
  const { useEffect } = await import("react");
  return {
    ChatBrowserView: ({
      active,
      onOpenTool,
      showBoardTool,
    }: {
      active: boolean;
      onOpenTool?: (kind: "board") => void;
      showBoardTool?: boolean;
    }) => {
      useEffect(() => {
        lifecycle.browserMounts += 1;
        return () => {
          lifecycle.browserUnmounts += 1;
        };
      }, []);
      return (
        <div data-active={active}>
          浏览器实例
          {showBoardTool ? (
            <button onClick={() => onOpenTool?.("board")}>看板工具</button>
          ) : null}
        </div>
      );
    },
  };
});
vi.mock("./chat-terminal-panel", async () => {
  const { useEffect } = await import("react");
  return {
    ChatTerminalPanel: () => {
      useEffect(() => {
        lifecycle.terminalMounts += 1;
        return () => {
          lifecycle.terminalUnmounts += 1;
        };
      }, []);
      return <div>终端实例</div>;
    },
  };
});
vi.mock("./workspace-file-viewer", () => ({
  WorkspaceFileViewer: ({ path }: { path: string }) => (
    <div>文件内容 {path}</div>
  ),
}));

function Harness({
  variant = "partner",
  isRunning = false,
  scopeKey: requestedScopeKey,
}: {
  variant?: "partner" | "team";
  isRunning?: boolean;
  scopeKey?: string;
}) {
  const scopeKey = requestedScopeKey ?? `${variant}:surface`;
  const workspace = useSceneWorkspace(scopeKey);
  const workbench = useWorkbench(scopeKey);
  const [panel, setPanel] = useState<ChatRightPanel | undefined>("workspace");
  const adapter: ChatWorkspaceAdapter = {
    identity: {
      chatId: `${variant}-chat`,
      key: conversationKeyOf(scopeKey, `${variant}-chat`),
      scopeKey,
      terminalWorkingDirectory: "/workspace",
      workspaceDirectory: "/workspace",
    },
    snapshot: {
      isRunning,
      pendingApprovalCount: 0,
      pendingElicitationCount: 0,
      streamState: "closed",
    },
    thread: {
      runtime: {} as AssistantRuntime,
      wakerName: variant === "team" ? "团队负责人" : "普通伙伴",
      emptyState: null,
      composerSettings: null,
      renderComposerContextStrip: undefined,
      sendFailure: { kind: "none" },
      hasEarlierMessages: false,
      isHistoryLoading: true,
      isLoadingEarlier: false,
      onLoadEarlier: async () => {},
    },
  };
  return (
    <>
      <button
        onClick={() => {
          const existing = workbench.snapshot.tabs.find(
            (tab) => tab.kind === "browser" && !tab.url,
          );
          if (existing) {
            workbench.dispatch({
              type: "update-browser-title",
              tabId: existing.id,
              title: "浏览器",
            });
            workbench.dispatch({ type: "focus", tabId: existing.id });
            return;
          }
          workbench.dispatch({
            type: "open-browser",
            title: "浏览器",
            url: "",
            rootPath: "/workspace",
            newTab: true,
          });
        }}
      >
        打开浏览器
      </button>
      <button
        onClick={() =>
          workbench.dispatch({
            type: "open-terminal",
            title: "终端",
            rootPath: "/workspace",
          })
        }
      >
        打开终端
      </button>
      <button
        onClick={() =>
          workbench.dispatch({
            type: "open-file",
            title: "a.txt",
            path: "a.txt",
            rootPath: "/workspace",
          })
        }
      >
        打开文件
      </button>
      <button
        onClick={() => {
          const tabId = workbench.snapshot.tabs.find(
            (tab) => tab.kind === "file",
          )?.id;
          if (!tabId) return;
          const key = `${scopeKey}:${tabId}`;
          workbenchDocumentBuffers.begin(key, "before", "v1");
          workbenchDocumentBuffers.change(key, "unsaved");
        }}
      >
        标记文件草稿
      </button>
      <button onClick={() => setPanel(undefined)}>收起右栏</button>
      <button onClick={() => setPanel("workspace")}>显示工作台</button>
      <ChatWorkspaceSurface
        activeRightPanel={panel}
        adapter={adapter}
        onAddCodeReference={() => {}}
        onDismissRightPanel={() => setPanel(undefined)}
        onOpenRightPanel={() => setPanel("workspace")}
        taskBoard={
          variant === "team" ? <section>详细任务内容</section> : undefined
        }
        workspace={workspace}
      />
    </>
  );
}

function renderHarness(props: Parameters<typeof Harness>[0] = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceProvider>
        <WorkbenchProvider>
          <Harness {...props} />
        </WorkbenchProvider>
      </WorkspaceProvider>
    </QueryClientProvider>,
  );
}

describe("ChatWorkspaceSurface right workbench", () => {
  beforeEach(() => {
    localStorage.clear();
    Object.assign(lifecycle, {
      browserMounts: 0,
      browserUnmounts: 0,
      terminalMounts: 0,
      terminalUnmounts: 0,
    });
  });

  it.each([320, 1000, 1400])(
    "工作台设为 %spx 后，缩窗保护聊天、放大保持原宽度",
    (preferredWidth) => {
      const resizeCallbacks = new Map<Element, () => void>();
      vi.stubGlobal(
        "ResizeObserver",
        class {
          constructor(private readonly callback: () => void) {}
          observe(element: Element) {
            resizeCallbacks.set(element, this.callback);
          }
          disconnect() {}
        },
      );
      const view = render(
        <SceneChromePortalProvider>
          <SceneChromeHost />
          <QueryClientProvider client={new QueryClient()}>
            <WorkspaceProvider>
              <WorkbenchProvider>
                <Harness />
              </WorkbenchProvider>
            </WorkspaceProvider>
          </QueryClientProvider>
        </SceneChromePortalProvider>,
      );
      try {
        const layout = screen.getByTestId("chat-page-layout");
        let availableWidth = 2000;
        Object.defineProperty(layout, "clientWidth", {
          configurable: true,
          get: () => availableWidth,
        });
        act(() => resizeCallbacks.get(layout)?.());
        const divider = screen.getByRole("separator", {
          name: "调整聊天与工作区的宽度",
        });
        fireEvent.mouseDown(divider, { clientX: 1680 });
        fireEvent.mouseUp(document, { clientX: 2000 - preferredWidth });
        expect(layout).toHaveStyle(
          `--chat-right-panel-width: ${preferredWidth}px`,
        );

        availableWidth = 3000;
        act(() => resizeCallbacks.get(layout)?.());
        expect(layout).toHaveStyle(
          `--chat-right-panel-width: ${preferredWidth}px`,
        );

        availableWidth = 1040;
        act(() => resizeCallbacks.get(layout)?.());
        const panelWidth = Number.parseFloat(
          layout.style.getPropertyValue("--chat-right-panel-width"),
        );
        expect(panelWidth).toBeLessThan(availableWidth * 0.8);
        expect(panelWidth).toBeCloseTo(
          Math.min(preferredWidth, availableWidth * 0.79),
        );
        expect(layout).not.toHaveAttribute("data-chat-full-view");
        const chrome = document.querySelector(
          "[data-scene-chrome-accessory-wrapper]",
        );
        expect(chrome).toHaveStyle({ width: `${panelWidth}px` });
        expect(lifecycle.browserMounts).toBe(1);
        expect(lifecycle.browserUnmounts).toBe(0);

        availableWidth = 2000;
        act(() => resizeCallbacks.get(layout)?.());
        expect(layout).toHaveStyle(
          `--chat-right-panel-width: ${preferredWidth}px`,
        );
      } finally {
        view.unmount();
        vi.unstubAllGlobals();
      }
    },
  );

  it("摘要位于聊天顶栏右端，打开或交换工作台后仍在聊天标签之后", async () => {
    const user = userEvent.setup();
    renderHarness();
    const summary = screen.getByRole("button", { name: "查看摘要" });
    const strip = screen
      .getByRole("tab", { name: "对话" })
      .closest("[data-scene-tab-strip]");
    expect(strip).toContainElement(summary);
    await user.click(screen.getByRole("button", { name: "收起右栏" }));
    expect(strip).toContainElement(summary);
    await user.click(screen.getByRole("button", { name: "显示工作台" }));
    await user.click(screen.getByRole("button", { name: "交换左右窗格" }));
    expect(strip).toContainElement(summary);
    const tabs = screen.getByRole("tab", { name: "对话" });
    expect(
      summary.compareDocumentPosition(tabs) & Node.DOCUMENT_POSITION_PRECEDING,
    ).toBeTruthy();
  });

  it("主应用顶栏承载当前聊天摘要，交换 pane 后摘要跟随聊天且完整视图隐藏", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <SceneChromePortalProvider>
          <SceneChromeHost />
          <SceneChromePortal>
            <SceneTabStrip
              activeTabId="chat:A"
              ariaLabel="应用场景"
              tabs={[{ id: "chat:A", label: "当前聊天" }]}
              onClose={() => {}}
              onSelect={() => {}}
            />
          </SceneChromePortal>
          <AppSceneWorkspaceContextProvider
            value={{
              dispatch: () => undefined,
              routeSceneId: "chat:A",
              snapshot: {
                activeTabId: "chat:A",
                tabs: [
                  {
                    id: "chat:A",
                    kind: "route",
                    label: "当前聊天",
                    lastUsed: 0,
                  },
                ],
              },
              syncWorkspace: () => undefined,
            }}
          >
            <WorkspaceProvider>
              <WorkbenchProvider>
                <Harness />
              </WorkbenchProvider>
            </WorkspaceProvider>
          </AppSceneWorkspaceContextProvider>
        </SceneChromePortalProvider>
      </QueryClientProvider>,
    );
    const summary = screen.getByRole("button", { name: "查看摘要" });
    const chatTab = screen.getByRole("tab", { name: "当前聊天" });
    const strip = chatTab.closest("[data-scene-tab-strip]");
    expect(strip).toContainElement(summary);
    expect(screen.getByTestId("chat-page-layout")).not.toContainElement(
      summary,
    );
    const accessory = strip?.querySelector("[data-scene-chrome-accessory]");
    if (!accessory) throw new Error("工作台顶栏未挂载");
    await user.click(screen.getByRole("button", { name: "交换左右窗格" }));
    expect(
      accessory.compareDocumentPosition(summary) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      summary.compareDocumentPosition(chatTab) &
        Node.DOCUMENT_POSITION_PRECEDING,
    ).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "完整视图" }));
    expect(summary).not.toBeVisible();
    await user.click(screen.getByRole("button", { name: "退出完整视图" }));
    expect(screen.getByRole("button", { name: "查看摘要" })).toBeVisible();
  });

  it("工作台保持打开时切换到空 Chat 会创建新标签", async () => {
    const view = renderHarness({ scopeKey: "chat-one" });
    expect(await screen.findByRole("tab", { name: "新标签页" })).toBeVisible();
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceProvider>
          <WorkbenchProvider>
            <Harness scopeKey="chat-two" />
          </WorkbenchProvider>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("tab", { name: "新标签页" })).toBeVisible();
    const saved = JSON.parse(
      localStorage.getItem("ally.right-workbench.v2.chat-two") ?? "null",
    ) as { state: { tabs: Array<{ kind: string }> } } | null;
    expect(saved?.state.tabs).toHaveLength(1);
  });

  it.each(["partner", "team"] as const)(
    "在 %s 聊天旁打开文件，聊天仍在主区",
    async (variant) => {
      const user = userEvent.setup();
      renderHarness({ variant, isRunning: true });
      await user.click(screen.getByRole("button", { name: "打开文件" }));
      expect(await screen.findByText("文件内容 a.txt")).toBeVisible();
      expect(screen.getByTestId("contract-chat-thread")).toBeVisible();
      expect(
        screen.getByText(variant === "team" ? "团队负责人" : "普通伙伴"),
      ).toBeVisible();
      expect(
        document.querySelector("[data-chat-activity-status]"),
      ).toBeVisible();
      expect(screen.getByRole("tab", { name: "a.txt" })).toHaveAttribute(
        "aria-selected",
        "true",
      );
    },
  );

  it("切换和收起工作台时保留浏览器、终端实例", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开浏览器" }));
    await user.click(screen.getByRole("button", { name: "打开终端" }));
    expect(await screen.findByText("终端实例")).toBeInTheDocument();
    expect(lifecycle.browserMounts).toBe(1);
    expect(lifecycle.terminalMounts).toBe(1);
    await user.click(screen.getByRole("button", { name: "收起右栏" }));
    expect(lifecycle.browserUnmounts).toBe(0);
    expect(lifecycle.terminalUnmounts).toBe(0);
    await user.click(screen.getByRole("button", { name: "显示工作台" }));
    await user.click(screen.getByRole("tab", { name: "浏览器" }));
    expect(screen.getByText("浏览器实例")).toHaveAttribute(
      "data-active",
      "true",
    );
    expect(lifecycle.browserMounts).toBe(1);
    expect(lifecycle.terminalMounts).toBe(1);
  });

  it("终端关闭晚于切换 Chat 时仍清理原范围标签", async () => {
    let resolveClose: ((closed: boolean) => void) | undefined;
    const close = vi.spyOn(desktop, "closeTerminal").mockImplementation(
      () =>
        new Promise<boolean>((resolve) => {
          resolveClose = resolve;
        }),
    );
    try {
      const user = userEvent.setup();
      const view = renderHarness();
      await user.click(screen.getByRole("button", { name: "打开终端" }));
      await user.click(screen.getByRole("button", { name: "关闭“终端”" }));
      expect(close).toHaveBeenCalledTimes(1);
      view.rerender(
        <QueryClientProvider client={new QueryClient()}>
          <WorkspaceProvider>
            <WorkbenchProvider>
              <Harness variant="team" />
            </WorkbenchProvider>
          </WorkspaceProvider>
        </QueryClientProvider>,
      );
      await act(async () => resolveClose?.(true));
      const saved = JSON.parse(
        localStorage.getItem("ally.right-workbench.v2.partner:surface") ??
          "null",
      ) as { state: { tabs: Array<{ kind: string }> } } | null;
      expect(saved?.state.tabs.some((tab) => tab.kind === "terminal")).toBe(
        false,
      );
    } finally {
      close.mockRestore();
    }
  });

  it("键盘左右键在工作台标签间移动焦点", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开浏览器" }));
    await user.click(screen.getByRole("button", { name: "打开终端" }));
    const terminal = screen.getByRole("tab", { name: "终端" });
    terminal.focus();
    await user.keyboard("{ArrowLeft}");
    const browser = screen.getByRole("tab", { name: "浏览器" });
    expect(browser).toHaveFocus();
    expect(browser).toHaveAttribute("aria-selected", "true");
  });

  it("拖动标签只改变顺序，不重建浏览器或终端", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开浏览器" }));
    await user.click(screen.getByRole("button", { name: "打开终端" }));
    const browser = screen.getByRole("tab", { name: "浏览器" });
    const terminal = screen.getByRole("tab", { name: "终端" });
    const dataTransfer = {
      effectAllowed: "none",
      setData: vi.fn(),
      getData: vi.fn(() => ""),
    };
    fireEvent.dragStart(terminal.parentElement as HTMLElement, {
      dataTransfer,
    });
    fireEvent.drop(browser.parentElement as HTMLElement, { dataTransfer });
    expect(
      Array.from(
        document.querySelectorAll<HTMLElement>(
          '[data-right-workbench] [role="tab"]',
        ),
      ).map((tab) => tab.textContent),
    ).toEqual(["终端", "浏览器"]);
    expect(lifecycle.browserMounts).toBe(1);
    expect(lifecycle.terminalMounts).toBe(1);
  });

  it("全视图保持同一标签实例并可返回侧栏", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开浏览器" }));
    await user.click(screen.getByRole("button", { name: "完整视图" }));
    expect(screen.getByTestId("chat-page-layout")).toHaveStyle(
      "--chat-right-panel-width: 0px",
    );
    expect(screen.getByRole("region", { name: "工作台" })).not.toHaveClass(
      "border-l",
    );
    expect(
      screen.queryByRole("separator", { name: "调整聊天与工作区的宽度" }),
    ).not.toBeInTheDocument();
    expect(lifecycle.browserMounts).toBe(1);
    await user.click(screen.getByRole("button", { name: "退出完整视图" }));
    expect(lifecycle.browserMounts).toBe(1);
    expect(lifecycle.browserUnmounts).toBe(0);
  });

  it("右栏拖动超过旧上限后，在占用 80% 时进入完整视图", () => {
    renderHarness();
    const layout = screen.getByTestId("chat-page-layout");
    Object.defineProperty(layout, "clientWidth", {
      configurable: true,
      value: 1500,
    });
    const divider = screen.getByRole("separator", {
      name: "调整聊天与工作区的宽度",
    });
    fireEvent.mouseDown(divider, { clientX: 1180 });
    fireEvent.mouseUp(document, { clientX: 500 });
    expect(layout).toHaveStyle("--chat-right-panel-width: 1000px");
    expect(layout).not.toHaveAttribute("data-chat-full-view");

    fireEvent.mouseDown(divider, { clientX: 500 });
    fireEvent.mouseUp(document, { clientX: 250 });
    expect(layout).toHaveAttribute("data-chat-full-view", "true");
    expect(layout).toHaveStyle("--chat-right-panel-width: 0px");
  });

  it("关闭唯一标签时收起右栏，再展开显示新标签页", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "关闭“新标签页”" }));
    expect(screen.getByTestId("chat-right-panel-host")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "打开文件" }));
    await user.click(screen.getByRole("button", { name: "显示工作台" }));
    await user.click(screen.getByRole("button", { name: "关闭“a.txt”" }));
    expect(screen.getByTestId("chat-right-panel-host")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "显示工作台" }));
    expect(screen.getByRole("tab", { name: "新标签页" })).toBeVisible();
    expect(screen.queryByText("打开工作台内容")).toBeNull();
  });

  it("拖动右栏边界时取消列宽过渡并同步更新工作台宽度", () => {
    renderHarness();
    const layout = screen.getByTestId("chat-page-layout");
    Object.defineProperty(layout, "clientWidth", { value: 1280 });
    fireEvent.mouseDown(
      screen.getByRole("separator", { name: "调整聊天与工作区的宽度" }),
      { clientX: 800 },
    );
    expect(screen.getByTestId("chat-right-panel-host")).toHaveClass(
      "transition-none",
    );
    fireEvent.mouseUp(document, { clientX: 650 });
    expect(layout).toHaveStyle("--chat-right-panel-width: 470px");
    expect(screen.getByTestId("chat-right-panel-host")).not.toHaveClass(
      "transition-none",
    );
  });

  it("聊天与工作台可左右交换，左侧工作台向右拖动会变宽", async () => {
    const user = userEvent.setup();
    renderHarness();
    const layout = screen.getByTestId("chat-page-layout");
    const host = screen.getByTestId("chat-right-panel-host");
    const browserMounts = lifecycle.browserMounts;
    await user.click(screen.getByRole("button", { name: "交换左右窗格" }));
    expect(layout).toHaveAttribute("data-workbench-side", "left");
    expect(layout).toHaveClass("lg:flex-row-reverse");
    expect(host).toHaveClass("lg:w-[var(--chat-right-panel-width)]");
    expect(layout.querySelector("main")).toHaveClass("flex-1");
    expect(host.parentElement).toBe(layout);
    expect(lifecycle.browserMounts).toBe(browserMounts);
    expect(lifecycle.browserUnmounts).toBe(0);
    Object.defineProperty(layout, "clientWidth", { value: 1280 });
    const divider = screen.getByRole("separator", {
      name: "调整聊天与工作区的宽度",
    });
    fireEvent.mouseDown(divider, { clientX: 320 });
    fireEvent.mouseUp(document, { clientX: 470 });
    expect(layout).toHaveStyle("--chat-right-panel-width: 470px");
    await user.click(screen.getByRole("button", { name: "交换左右窗格" }));
    expect(layout).toHaveAttribute("data-workbench-side", "right");
    expect(layout).not.toHaveClass("lg:flex-row-reverse");
  });

  it("分屏与全视图切换时浏览器和终端同时可见且不会重建", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开浏览器" }));
    await user.click(screen.getByRole("button", { name: "打开终端" }));
    await screen.findByText("终端实例");
    const content = screen.getByTestId("workbench-content");
    const targetPane = screen.getByText("终端实例")
      .parentElement as HTMLElement;
    vi.spyOn(targetPane, "getBoundingClientRect").mockReturnValue({
      left: 0,
      top: 0,
      right: 400,
      bottom: 400,
      width: 400,
      height: 400,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    const dataTransfer = {
      effectAllowed: "none",
      dropEffect: "none",
      setData: vi.fn(),
      getData: vi.fn(() => ""),
    };
    fireEvent.dragStart(
      screen.getByRole("tab", { name: "浏览器" }).parentElement as HTMLElement,
      { dataTransfer },
    );
    const dragOver = createEvent.dragOver(targetPane, { dataTransfer });
    Object.defineProperties(dragOver, {
      clientX: { value: 10 },
      clientY: { value: 200 },
    });
    fireEvent(targetPane, dragOver);
    expect(
      content.querySelector('[data-workbench-drop-zone="left"]'),
    ).not.toBeNull();
    const drop = createEvent.drop(targetPane, { dataTransfer });
    Object.defineProperties(drop, {
      clientX: { value: 10 },
      clientY: { value: 200 },
    });
    fireEvent(targetPane, drop);
    expect(screen.getByText("浏览器实例")).toHaveAttribute(
      "data-active",
      "true",
    );
    expect(screen.getByText("浏览器实例").parentElement).toHaveAttribute(
      "aria-hidden",
      "false",
    );
    expect(screen.getByText("终端实例").parentElement).toHaveAttribute(
      "aria-hidden",
      "false",
    );
    expect(
      screen.getByRole("separator", { name: "调整分屏比例" }),
    ).toHaveAttribute("aria-orientation", "vertical");
    const chrome = document.querySelector("[data-workbench-chrome]");
    expect(chrome).toContainElement(
      screen.getByRole("tab", { name: "浏览器" }),
    );
    expect(chrome).toContainElement(screen.getByRole("tab", { name: "终端" }));
    expect(content.querySelector("[data-workbench-pane-tabs]")).toBeNull();
    await user.click(screen.getByRole("button", { name: "完整视图" }));
    expect(
      screen.getByRole("separator", { name: "调整分屏比例" }),
    ).toHaveAttribute("aria-orientation", "vertical");
    expect(lifecycle.browserMounts).toBe(1);
    expect(lifecycle.browserUnmounts).toBe(0);
    expect(lifecycle.terminalMounts).toBe(1);
    expect(lifecycle.terminalUnmounts).toBe(0);
    const browserHeader = screen
      .getByRole("tab", { name: "浏览器" })
      .closest("[data-workbench-pane-header]") as HTMLElement;
    const terminalHeader = screen
      .getByRole("tab", { name: "终端" })
      .closest("[data-workbench-pane-header]") as HTMLElement;
    expect(browserHeader).not.toBe(terminalHeader);
    await user.click(
      within(terminalHeader).getByRole("button", {
        name: "添加工作台标签",
      }),
    );
    expect(
      within(terminalHeader).getByRole("tab", { name: "新标签页" }),
    ).toBeInTheDocument();
    expect(
      within(browserHeader).queryByRole("tab", { name: "新标签页" }),
    ).toBeNull();
    const browserLeft = browserHeader.style.left;
    const browserMountsBeforeSwap = lifecycle.browserMounts;
    await user.click(
      screen.getByRole("button", { name: "交换工作台左右窗格" }),
    );
    expect(browserHeader.style.left).not.toBe(browserLeft);
    expect(lifecycle.browserMounts).toBe(browserMountsBeforeSwap);
    expect(lifecycle.browserUnmounts).toBe(0);
    expect(lifecycle.terminalMounts).toBe(1);
    expect(lifecycle.terminalUnmounts).toBe(0);
  });

  it("上下分屏时下方 pane 的标签栏和新增按钮随内容下移", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开浏览器" }));
    await user.click(screen.getByRole("button", { name: "打开终端" }));
    const content = screen.getByTestId("workbench-content");
    const targetPane = (await screen.findByText("终端实例"))
      .parentElement as HTMLElement;
    vi.spyOn(targetPane, "getBoundingClientRect").mockReturnValue({
      left: 0,
      top: 0,
      right: 400,
      bottom: 400,
      width: 400,
      height: 400,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    const dataTransfer = {
      effectAllowed: "none",
      dropEffect: "none",
      setData: vi.fn(),
      getData: vi.fn(() => ""),
    };
    fireEvent.dragStart(
      screen.getByRole("tab", { name: "浏览器" }).parentElement as HTMLElement,
      { dataTransfer },
    );
    const dragOver = createEvent.dragOver(targetPane, { dataTransfer });
    Object.defineProperties(dragOver, {
      clientX: { value: 200 },
      clientY: { value: 390 },
    });
    fireEvent(targetPane, dragOver);
    const drop = createEvent.drop(targetPane, { dataTransfer });
    Object.defineProperties(drop, {
      clientX: { value: 200 },
      clientY: { value: 390 },
    });
    fireEvent(targetPane, drop);

    const chrome = document.querySelector("[data-workbench-chrome]");
    expect(chrome).toContainElement(screen.getByRole("tab", { name: "终端" }));
    expect(chrome).not.toContainElement(
      screen.getByRole("tab", { name: "浏览器" }),
    );
    const browserHeader = screen
      .getByRole("tab", { name: "浏览器" })
      .closest("[data-workbench-pane-header]") as HTMLElement;
    expect(content).toContainElement(browserHeader);
    expect(browserHeader).toHaveAttribute("data-workbench-local-header");
    expect(browserHeader).toHaveStyle({ top: "50%" });
    const addButtons = screen.getAllByRole("button", {
      name: "添加工作台标签",
    });
    expect(addButtons).toHaveLength(2);
    expect(
      within(browserHeader).getByRole("button", { name: "添加工作台标签" }),
    ).toBeInTheDocument();
    expect(content.querySelectorAll("[data-workbench-pane-tabs]")).toHaveLength(
      1,
    );
    expect(screen.getByText("浏览器实例").parentElement).toHaveStyle({
      top: "calc(50% + var(--shell-titlebar-height))",
    });
    expect(
      screen.getByRole("separator", { name: "调整分屏比例" }),
    ).toHaveAttribute("aria-orientation", "horizontal");
    await user.click(
      within(browserHeader).getByRole("button", {
        name: "添加工作台标签",
      }),
    );
    expect(
      within(browserHeader).getByRole("tab", { name: "新标签页" }),
    ).toBeInTheDocument();
    expect(chrome).not.toContainElement(
      within(browserHeader).getByRole("tab", { name: "新标签页" }),
    );
  });

  it("聚焦标签可用方向键拆分，两个 pane 均保留顶栏加号", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开终端" }));
    const terminal = screen.getByRole("tab", { name: "终端" });
    terminal.focus();
    await user.keyboard("{Alt>}{Shift>}{ArrowRight}{/Shift}{/Alt}");
    expect(
      screen.getByRole("separator", { name: "调整分屏比例" }),
    ).toHaveAttribute("aria-orientation", "vertical");
    const chrome = document.querySelector("[data-workbench-chrome]");
    const addButtons = screen.getAllByRole("button", {
      name: "添加工作台标签",
    });
    expect(addButtons).toHaveLength(2);
    for (const button of addButtons) expect(chrome).toContainElement(button);
  });

  it("关闭未保存文件可以取消，确认后才丢弃草稿", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByRole("button", { name: "打开文件" }));
    await screen.findByText("文件内容 a.txt");
    await user.click(screen.getByRole("button", { name: "标记文件草稿" }));
    await user.click(screen.getByRole("button", { name: "关闭“a.txt”" }));
    expect(screen.getByText("文件有未保存的修改")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "继续编辑" }));
    expect(screen.getByRole("tab", { name: "a.txt" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "关闭“a.txt”" }));
    await user.click(screen.getByRole("button", { name: "丢弃并关闭" }));
    expect(screen.queryByRole("tab", { name: "a.txt" })).toBeNull();
  });

  it("团队看板作为右侧工作台标签打开并复用", async () => {
    const user = userEvent.setup();
    renderHarness({ variant: "team" });
    await user.click(screen.getByRole("button", { name: "看板工具" }));
    await waitFor(() => expect(screen.getByText("详细任务内容")).toBeVisible());
    expect(screen.getAllByRole("tab", { name: "看板" })).toHaveLength(1);
    await user.click(screen.getByRole("tab", { name: "新标签页" }));
    await user.click(screen.getByRole("button", { name: "看板工具" }));
    expect(screen.getAllByRole("tab", { name: "看板" })).toHaveLength(1);
  });

  it("普通对话不显示看板工具", async () => {
    const user = userEvent.setup();
    renderHarness({ variant: "partner" });
    expect(screen.queryByRole("button", { name: "看板工具" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "收起右栏" }));
    expect(screen.queryByText("详细任务内容")).toBeNull();
    expect(screen.getByTestId("chat-right-panel-host")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
  });

  it("切到其他 Scene 后原生浏览器视图停止展示但保持实例", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <AppSceneWorkspaceContextProvider
          value={{
            dispatch: () => undefined,
            routeSceneId: "chat:A",
            snapshot: {
              activeTabId: "chat:B",
              tabs: [
                { id: "chat:A", kind: "route", label: "A", lastUsed: 0 },
                { id: "chat:B", kind: "route", label: "B", lastUsed: 0 },
              ],
            },
            syncWorkspace: () => undefined,
          }}
        >
          <WorkspaceProvider>
            <WorkbenchProvider>
              <Harness />
            </WorkbenchProvider>
          </WorkspaceProvider>
        </AppSceneWorkspaceContextProvider>
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "打开浏览器" }));
    expect(screen.getByText("浏览器实例")).toHaveAttribute(
      "data-active",
      "false",
    );
    expect(lifecycle.browserMounts).toBe(1);
    expect(lifecycle.browserUnmounts).toBe(0);
  });
});
