import { render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { SettingsContextSidebar } from "../components/settings-context-sidebar";
import { SettingsScene } from "./settings-scene";
import type { SettingsSection } from "../settings-navigation";
import { useNativeOverlayVisible } from "@/shared/ui/native-overlay";
import "@/shared/i18n";

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ mode: "system", setMode: vi.fn() }),
}));

vi.mock("../components/runtime-environments-section", () => ({
  RuntimeEnvironmentsSection: ({
    onOpenAccounts,
  }: {
    onOpenAccounts?: () => void;
  }) => (
    <>
      <h1>执行环境内容</h1>
      <button onClick={onOpenAccounts}>前往账号设置</button>
    </>
  ),
}));

vi.mock("../components/api-integration-section", () => ({
  APIIntegrationSection: () => <h1>API 与 MCP 集成内容</h1>,
}));

describe("SettingsScene", () => {
  it("在发布集成导航选择 IM 并显示应用装配的内容", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <>
        <SettingsContextSidebar
          activeSection="integration-im"
          onSelect={onSelect}
        />
        <SettingsScene
          section="integration-im"
          onClose={() => undefined}
          imIntegrationContent={<h1>IM 平台与连接列表</h1>}
        />
      </>,
    );
    expect(screen.getByText("发布集成")).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "IM 平台与连接列表" }),
    ).toBeVisible();
    const item = screen.getByRole("button", { name: "IM 集成" });
    expect(item).toHaveAttribute("aria-current", "page");
    await user.click(item);
    expect(onSelect).toHaveBeenCalledWith("integration-im");
  });
  it("通过 API 分类懒加载集成内容，不再显示独立 MCP 导航", async () => {
    const onSelectSection = vi.fn();
    render(
      <>
        <SettingsContextSidebar
          activeSection="integration-api"
          onSelect={onSelectSection}
        />
        <SettingsScene
          section="integration-api"
          onClose={() => undefined}
          onSelectSection={onSelectSection}
        />
      </>,
    );
    expect(screen.getByRole("navigation", { name: "设置菜单" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "MCP 集成" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "API 集成" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("button", { name: "API 集成" })).toBeVisible();
    expect(
      await screen.findByRole("heading", { name: "API 与 MCP 集成内容" }),
    ).toBeVisible();
  });
  it("从智能体诊断入口切换到 AI 账号设置", async () => {
    const user = userEvent.setup();
    const onSelectSection = vi.fn();
    render(
      <SettingsScene
        section="runtime-environments"
        onClose={() => undefined}
        onSelectSection={onSelectSection}
      />,
    );
    await user.click(
      await screen.findByRole("button", { name: "前往账号设置" }),
    );
    expect(onSelectSection).toHaveBeenCalledWith("ai-accounts");
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

  it("通过局部状态恢复分类并在关闭后返回此前 Scene", async () => {
    const user = userEvent.setup();
    function Harness() {
      const [open, setOpen] = useState(true);
      const [section, setSection] = useState<SettingsSection>(
        "runtime-environments",
      );
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
    expect(runtimeNavItem).toHaveClass("min-h-row-default", "rounded-md");
    expect(runtimeNavItem.querySelector("span.type-body")).toHaveClass(
      "font-medium",
    );
    expect(runtimeNavItem).toHaveClass("bg-selection-surface");
    expect(screen.getByRole("button", { name: "通用" })).not.toHaveClass(
      "bg-selection-surface",
    );
    expect(
      await screen.findByRole("heading", { name: "执行环境内容" }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "关闭 设置" }));

    expect(screen.getByText("伙伴页面")).toBeInTheDocument();
  });
});
