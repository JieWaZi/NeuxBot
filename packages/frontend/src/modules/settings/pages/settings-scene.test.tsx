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

vi.mock("../components/mcp-integration-section", () => ({
  MCPIntegrationSection: ({ onOpenAPI }: { onOpenAPI?: () => void }) => (
    <>
      <h1>MCP 接入内容</h1>
      <button onClick={onOpenAPI}>前往 API 集成管理</button>
    </>
  ),
}));

describe("SettingsScene", () => {
  it("懒加载独立 MCP 分类并转到已有 API 集成", async () => {
    const user = userEvent.setup();
    const onSelectSection = vi.fn();
    render(
      <>
        <SettingsContextSidebar
          activeSection="integration-mcp"
          onSelect={onSelectSection}
        />
        <SettingsScene
          section="integration-mcp"
          onClose={() => undefined}
          onSelectSection={onSelectSection}
        />
      </>,
    );
    expect(screen.getByRole("navigation", { name: "设置菜单" })).toBeVisible();
    expect(screen.getByRole("button", { name: "MCP 集成" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("button", { name: "API 集成" })).toBeVisible();
    expect(
      await screen.findByRole("heading", { name: "MCP 接入内容" }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "前往 API 集成管理" }));
    expect(onSelectSection).toHaveBeenCalledWith("integration-api");
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
