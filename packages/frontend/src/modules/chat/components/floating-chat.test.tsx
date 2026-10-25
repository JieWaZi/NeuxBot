import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import {
  FloatingChatLauncherProvider,
  useFloatingChatLauncher,
} from "@/shared/workspace/floating-chat-launcher";
import { FloatingChat, type FloatingChatSnapshot } from "./floating-chat";

function TestLauncher() {
  const entry = useFloatingChatLauncher();
  return entry ? (
    <button
      type="button"
      data-floating-chat-shell-launcher
      onClick={entry.onActivate}
    >
      {entry.ariaLabel}
    </button>
  ) : null;
}

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <FloatingChatLauncherProvider>
      <TestLauncher />
      {children}
    </FloatingChatLauncherProvider>
  );
}

const idle: FloatingChatSnapshot = {
  isRunning: false,
  pendingApprovalCount: 0,
  pendingElicitationCount: 0,
  streamState: "open",
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function mockFloatingViewport() {
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 1000);
  const geometry = { width: 400, height: 720 };
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
    function (this: HTMLElement) {
      if (!this.hasAttribute("data-floating-chat-panel")) return new DOMRect();
      return new DOMRect(
        Number.parseFloat(this.style.left) || 500,
        Number.parseFloat(this.style.top) || 120,
        geometry.width,
        geometry.height,
      );
    },
  );
  return geometry;
}

function dragFloatingPanel(panel: HTMLElement) {
  const header = panel.querySelector("header");
  if (!header) throw new Error("浮窗缺少拖动标题栏");
  fireEvent(
    header,
    new MouseEvent("pointerdown", {
      button: 0,
      clientX: 500,
      clientY: 120,
      bubbles: true,
    }),
  );
  fireEvent(
    window,
    new MouseEvent("pointermove", { clientX: 700, clientY: 180 }),
  );
  fireEvent(window, new MouseEvent("pointerup"));
}

describe("FloatingChat", () => {
  it("新的整理请求展开同一浮窗，收起后仍保留未发送内容", async () => {
    const user = userEvent.setup();
    const view = (request: number) => (
      <FloatingChat
        fullView
        scopeKey="knowledge:research"
        openRequest={request}
        snapshot={idle}
        title="知识库维护"
      >
        <input aria-label="维护草稿" defaultValue="保留这段要求" />
      </FloatingChat>
    );
    const { rerender } = render(view(1), { wrapper: Wrapper });
    const input = screen.getByRole("textbox", { name: "维护草稿" });
    await user.click(screen.getByRole("button", { name: "收起对话窗口" }));
    expect(screen.queryByRole("region", { name: "悬浮对话" })).toBeNull();
    rerender(view(2));
    expect(screen.getByRole("textbox", { name: "维护草稿" })).toBe(input);
    expect(input).toHaveValue("保留这段要求");
  });
  it("拖动后缩小窗口会重新约束位置，收起期间缩小再打开也保持可见", async () => {
    const user = userEvent.setup();
    const geometry = mockFloatingViewport();
    render(
      <FloatingChat fullView scopeKey="scope-1" snapshot={idle} title="会话">
        <button type="button">聊天输入</button>
      </FloatingChat>,
      { wrapper: Wrapper },
    );
    await user.click(screen.getByRole("button", { name: "打开对话" }));
    const panel = screen.getByRole("region", { name: "悬浮对话" });
    const input = screen.getByRole("button", { name: "聊天输入" });
    dragFloatingPanel(panel);
    expect(panel).toHaveStyle({ left: "700px", top: "180px" });

    geometry.height = 504;
    vi.stubGlobal("innerWidth", 640);
    vi.stubGlobal("innerHeight", 600);
    fireEvent(window, new Event("resize"));
    expect(panel).toHaveStyle({ left: "232px", top: "88px" });
    expect(screen.getByRole("button", { name: "聊天输入" })).toBe(input);

    await user.click(screen.getByRole("button", { name: "收起对话窗口" }));
    geometry.width = 288;
    geometry.height = 304;
    vi.stubGlobal("innerWidth", 320);
    vi.stubGlobal("innerHeight", 400);
    fireEvent(window, new Event("resize"));
    await user.click(screen.getByRole("button", { name: "打开对话" }));
    expect(screen.getByRole("region", { name: "悬浮对话" })).toHaveStyle({
      left: "24px",
      top: "88px",
    });
    expect(screen.getByRole("button", { name: "聊天输入" })).toBe(input);
  });

  it("浮窗自身尺寸变化会校正拖动坐标，默认贴边位置保持由布局决定", async () => {
    const user = userEvent.setup();
    const geometry = mockFloatingViewport();
    const observers = new Map<Element, () => void>();
    const disconnect = vi.fn();
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(private readonly callback: () => void) {}
        observe(element: Element) {
          observers.set(element, this.callback);
        }
        disconnect = disconnect;
      },
    );
    render(
      <FloatingChat fullView scopeKey="scope-1" snapshot={idle} title="会话">
        <div>聊天</div>
      </FloatingChat>,
      { wrapper: Wrapper },
    );
    await user.click(screen.getByRole("button", { name: "打开对话" }));
    const panel = screen.getByRole("region", { name: "悬浮对话" });
    const callback = observers.get(panel);
    expect(callback).toBeDefined();
    act(() => callback?.());
    expect(panel.style.left).toBe("");
    expect(panel.style.right).toBe("");
    dragFloatingPanel(panel);
    geometry.height = 900;
    act(() => callback?.());
    expect(panel).toHaveStyle({ left: "700px", top: "92px" });

    await user.click(screen.getByRole("button", { name: "收起对话窗口" }));
    expect(disconnect).toHaveBeenCalledOnce();
  });

  it("在主区与浮窗间移动唯一聊天子树，收起后不可聚焦", async () => {
    const user = userEvent.setup();
    const mounted = vi.fn();
    const unmounted = vi.fn();
    function Thread() {
      useEffect(() => {
        mounted();
        return unmounted;
      }, []);
      return <button type="button">聊天输入</button>;
    }
    const { container, rerender } = render(
      <FloatingChat
        chatId="chat-1"
        fullView={false}
        scopeKey="scope-1"
        snapshot={idle}
        title="会话"
      >
        <Thread />
      </FloatingChat>,
      { wrapper: Wrapper },
    );

    expect(screen.getByRole("button", { name: "聊天输入" })).toBeVisible();
    rerender(
      <FloatingChat
        chatId="chat-1"
        fullView
        scopeKey="scope-1"
        snapshot={idle}
        title="会话"
      >
        <Thread />
      </FloatingChat>,
    );
    const normalHost = container.querySelector(
      "[data-floating-chat-normal-host]",
    );
    expect(normalHost).toHaveAttribute("inert");
    expect(normalHost).toHaveAttribute("aria-hidden", "true");
    expect(
      screen.queryByRole("button", { name: "聊天输入" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "打开对话" }));
    expect(screen.getByRole("region", { name: "悬浮对话" })).toBeVisible();
    expect(
      document.querySelectorAll("[data-floating-chat-shell-launcher]"),
    ).toHaveLength(1);
    expect(document.querySelector("[data-floating-chat-launcher]")).toBeNull();
    expect(screen.getByRole("button", { name: "聊天输入" })).toBeVisible();
    expect(mounted).toHaveBeenCalledTimes(1);
    expect(unmounted).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "收起对话窗口" }));
    expect(
      screen.queryByRole("region", { name: "悬浮对话" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "聊天输入" }),
    ).not.toBeInTheDocument();
    expect(mounted).toHaveBeenCalledTimes(1);
    expect(unmounted).not.toHaveBeenCalled();
  });

  it("只显示当前聊天的真实进度，优先提示待确认交互", () => {
    const { rerender } = render(
      <FloatingChat fullView scopeKey="draft" snapshot={idle} title="草稿">
        <div>聊天</div>
      </FloatingChat>,
      { wrapper: Wrapper },
    );
    expect(
      screen.getByRole("button", { name: "打开对话" }),
    ).toBeInTheDocument();

    rerender(
      <FloatingChat
        chatId="chat-1"
        fullView
        scopeKey="scope-1"
        snapshot={{ ...idle, isRunning: true, pendingApprovalCount: 1 }}
        title="会话"
      >
        <div>聊天</div>
      </FloatingChat>,
    );
    expect(
      screen.getByRole("button", { name: "等待审批，打开对话" }),
    ).toBeInTheDocument();

    rerender(
      <FloatingChat
        chatId="chat-1"
        fullView
        scopeKey="scope-1"
        snapshot={{
          ...idle,
          isRunning: true,
          pendingApprovalCount: 1,
          pendingElicitationCount: 1,
        }}
        title="会话"
      >
        <div>聊天</div>
      </FloatingChat>,
    );
    expect(
      screen.getByRole("button", { name: "等待输入，打开对话" }),
    ).toBeInTheDocument();
  });

  it("Esc 收起悬浮聊天，切换聊天时也恢复入口", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <FloatingChat
        chatId="chat-1"
        fullView
        scopeKey="scope-1"
        snapshot={idle}
        title="会话一"
      >
        <div>聊天</div>
      </FloatingChat>,
      { wrapper: Wrapper },
    );
    await user.click(screen.getByRole("button", { name: "打开对话" }));
    expect(
      screen.getByRole("region", { name: "悬浮对话" }),
    ).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(
      screen.queryByRole("region", { name: "悬浮对话" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "打开对话" }));
    rerender(
      <FloatingChat
        chatId="chat-2"
        fullView
        scopeKey="scope-2"
        snapshot={idle}
        title="会话二"
      >
        <div>聊天</div>
      </FloatingChat>,
    );
    expect(
      screen.queryByRole("region", { name: "悬浮对话" }),
    ).not.toBeInTheDocument();
  });
});
