import {
  render as renderView,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { NotificationPreferences } from "@neuxbot/platform-contracts/services";
import { useState, type ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { SettingsContextSidebar } from "../components/settings-context-sidebar";
import { SettingsScene } from "./settings-scene";
import type { SettingsSection } from "../settings-navigation";
import { useNativeOverlayVisible } from "@/shared/ui/native-overlay";
import "@/shared/i18n";

function render(content: ReactNode) {
  const client = new QueryClient();
  return renderView(
    <QueryClientProvider client={client}>
      <MemoryRouter>{content}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const host = vi.hoisted(() => ({
  get_notification_preferences: vi.fn(),
  set_notification_preferences: vi.fn(),
  send_test_notification: vi.fn(),
  open_notification_settings: vi.fn(),
}));

vi.mock(import("@/shared/platform/provider"), async (importOriginal) => {
  const original = await importOriginal();
  return {
    ...original,
    platform: () => {
      const platform = original.platform();
      return { ...platform, host: { ...platform.host, ...host } };
    },
  };
});

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ mode: "system", setMode: vi.fn() }),
}));

vi.mock("../components/runtime-environments-section", () => ({
  RuntimeEnvironmentsSection: ({
    onOpenRuntime,
    onBack,
    runtimeId,
  }: {
    onOpenRuntime?: (id: string) => void;
    onBack?: () => void;
    runtimeId?: string;
  }) => (
    <>
      <h1>{runtimeId ? `智能体 ${runtimeId}` : "执行环境内容"}</h1>
      <button onClick={() => onOpenRuntime?.("codex")}>打开 Codex</button>
      <button onClick={onBack}>返回智能体</button>
    </>
  ),
}));

vi.mock("../components/integrations-section", () => ({
  IntegrationsSection: ({ imContent }: { imContent?: ReactNode }) => (
    <>
      {imContent}
      <h1>集成内容</h1>
    </>
  ),
}));

describe("SettingsScene", () => {
  it("从独立通知栏更改提醒范围和本机设置，并可发送测试", async () => {
    let preferences: NotificationPreferences = {
      mode: "all",
      enabled: true,
      preview: false,
    };
    host.get_notification_preferences.mockImplementation(
      async () => preferences,
    );
    host.set_notification_preferences.mockImplementation(async (next) => {
      preferences = next;
      return preferences;
    });
    host.send_test_notification.mockRejectedValueOnce(new Error("denied"));
    const user = userEvent.setup();
    function Harness() {
      const [section, setSection] = useState<SettingsSection>("general");
      return (
        <>
          <SettingsContextSidebar
            activeSection={section}
            onSelect={setSection}
          />
          <SettingsScene section={section} onClose={() => undefined} />
        </>
      );
    }
    render(<Harness />);
    expect(screen.queryByRole("switch", { name: "显示消息预览" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "通知" }));
    const needsMe = await screen.findByRole("radio", { name: "需要我时" });
    await user.click(needsMe);
    await waitFor(() => expect(needsMe).toBeChecked());
    const preview = screen.getByRole("switch", { name: "显示消息预览" });
    await user.click(preview);
    await waitFor(() => expect(preview).toBeChecked());
    const desktop = screen.getByRole("switch", { name: "桌面通知" });
    await user.click(desktop);
    await waitFor(() => expect(desktop).not.toBeChecked());
    expect(preview).toBeDisabled();
    expect(preferences).toEqual({
      mode: "needs-me",
      enabled: false,
      preview: true,
    });
    await user.click(screen.getByRole("button", { name: "打开系统设置" }));
    expect(host.open_notification_settings).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "发送测试" }));
    expect(host.send_test_notification).toHaveBeenCalledOnce();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "未能发送测试通知",
    );
  });

  it("集成是唯一入口并显示应用装配的 IM 内容", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <>
        <SettingsContextSidebar
          activeSection="integrations"
          onSelect={onSelect}
        />
        <SettingsScene
          section="integrations"
          onClose={() => undefined}
          imIntegrationContent={<h1>IM 平台与连接列表</h1>}
        />
      </>,
    );
    expect(screen.getByRole("button", { name: "集成" })).toBeVisible();
    expect(
      screen.queryByRole("heading", { name: "设置" }),
    ).not.toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "IM 平台与连接列表" }),
    ).toBeVisible();
    const item = screen.getByRole("button", { name: "集成" });
    expect(item).toHaveAttribute("aria-current", "page");
    await user.click(item);
    expect(onSelect).toHaveBeenCalledWith("integrations");
  });
  it("通过集成分类懒加载内容，不显示独立 MCP 导航", async () => {
    const onSelectSection = vi.fn();
    render(
      <>
        <SettingsContextSidebar
          activeSection="integrations"
          onSelect={onSelectSection}
        />
        <SettingsScene
          section="integrations"
          onClose={() => undefined}
          onSelectSection={onSelectSection}
        />
      </>,
    );
    expect(screen.getByRole("navigation", { name: "设置菜单" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "MCP 集成" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "集成" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("button", { name: "集成" })).toBeVisible();
    expect(
      await screen.findByRole("heading", { name: "集成内容" }),
    ).toBeVisible();
  });
  it("在同一设置分类打开智能体详情并返回", async () => {
    const user = userEvent.setup();
    const onOpenRuntime = vi.fn();
    const onRuntimeBack = vi.fn();
    const view = render(
      <SettingsScene
        section="agents"
        onClose={() => undefined}
        onOpenRuntime={onOpenRuntime}
      />,
    );
    await user.click(await screen.findByRole("button", { name: "打开 Codex" }));
    expect(onOpenRuntime).toHaveBeenCalledWith("codex");
    view.rerender(
      <SettingsScene
        section="agents"
        runtimeId="codex"
        onClose={() => undefined}
        onRuntimeBack={onRuntimeBack}
      />,
    );
    expect(
      await screen.findByRole("heading", { name: "智能体 codex" }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "返回智能体" }));
    expect(onRuntimeBack).toHaveBeenCalledOnce();
  });

  it("打开时注册原生浮层以隐藏分屏浏览器", async () => {
    const visibility = renderHook(() => useNativeOverlayVisible());
    expect(visibility.result.current).toBe(false);

    const { unmount } = render(
      <SettingsScene section="general" onClose={() => undefined} />,
    );

    await waitFor(() => expect(visibility.result.current).toBe(true));
    unmount();
    await waitFor(() => expect(visibility.result.current).toBe(false));
  });

  it("智能体分类选中，不再显示顶级账号入口", async () => {
    function Harness() {
      const [open, setOpen] = useState(true);
      const [section, setSection] = useState<SettingsSection>("agents");
      return open ? (
        <div className="grid grid-cols-[240px_1fr]">
          <SettingsContextSidebar
            activeSection={section}
            onSelect={setSection}
          />
          <SettingsScene section={section} onClose={() => setOpen(false)} />
        </div>
      ) : (
        <p>伙伴页面</p>
      );
    }
    render(<Harness />);

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(
      screen.getByRole("navigation", { name: "设置菜单" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "智能体" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    const runtimeNavItem = screen.getByRole("button", { name: "智能体" });
    expect(runtimeNavItem).toHaveClass(
      "min-h-control-hit-target",
      "rounded-md",
    );
    expect(runtimeNavItem.querySelector("span.type-resource-body")).toHaveClass(
      "font-medium",
    );
    expect(runtimeNavItem).toHaveClass("bg-resource-hover");
    expect(screen.getByRole("button", { name: "通用" })).not.toHaveClass(
      "bg-resource-hover",
    );
    expect(
      await screen.findByRole("heading", { name: "执行环境内容" }),
    ).toBeInTheDocument();

    expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "账号管理" }),
    ).not.toBeInTheDocument();
  });
});

it("设置搜索在本地筛选分类并可以清除", async () => {
  const user = userEvent.setup();
  render(<SettingsContextSidebar activeSection="general" onSelect={vi.fn()} />);
  await user.type(
    screen.getByRole("searchbox", { name: "搜索设置" }),
    "智能体",
  );
  expect(screen.getByRole("button", { name: "智能体" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "集成" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "清除设置搜索" }));
  expect(screen.getByRole("button", { name: "集成" })).toBeVisible();
  await user.type(
    screen.getByRole("searchbox", { name: "搜索设置" }),
    "不存在",
  );
  expect(screen.getByRole("status")).toHaveTextContent("没有匹配的设置");
});
