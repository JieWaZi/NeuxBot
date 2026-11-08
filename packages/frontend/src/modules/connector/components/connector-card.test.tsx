import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { ConnectorCard } from "./connector-card";

const connector = {
  activationPolicy: "per_execution" as const,
  affectedWakerCount: 1,
  compatibility: [{ runtimeId: "codex", supported: true }],
  description: "检查实时 Chrome 页面。",
  enabled: true,
  id: "chrome",
  name: "Chrome",
  scope: "public" as const,
  testStatus: "succeeded" as const,
  toolCount: 29,
  transport: "stdio" as const,
};

describe("ConnectorCard", () => {
  it("使用两行目录条目，详情链接与伙伴开关独立命中", () => {
    const toggle = vi.fn();
    const { container } = render(
      <MemoryRouter initialEntries={["/wakers/w1/connectors?search=Chrome"]}>
        <ConnectorCard
          connector={connector}
          wakerId="w1"
          onEnabledChange={toggle}
        />
      </MemoryRouter>,
    );
    expect(container.querySelector("article")).toHaveAttribute(
      "data-appearance",
      "row",
    );
    expect(
      screen.getByRole("link", { name: "查看连接器 Chrome" }),
    ).toHaveAttribute("href", "/wakers/w1/connectors/chrome");
    expect(screen.getByText(connector.description)).toHaveClass("truncate");
    expect(
      screen.queryByText(/STDIO|全局|个工具|支持/),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("switch", { name: "停用 Chrome" }));
    expect(toggle).toHaveBeenCalledWith(false);
  });
  it("库中安装操作与详情链接独立", () => {
    const install = vi.fn();
    render(
      <MemoryRouter>
        <ConnectorCard
          connector={connector}
          wakerId=""
          onEnabledChange={vi.fn()}
          onInstall={install}
        />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(install).toHaveBeenCalledOnce();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });
  it("系统工作流连接器保留系统状态，不提供手动开关", () => {
    render(
      <MemoryRouter>
        <ConnectorCard
          connector={{
            ...connector,
            scope: "built_in",
            activationPolicy: "workflow_context",
          }}
          wakerId="w1"
          onEnabledChange={vi.fn()}
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("由系统按场景启用")).toBeVisible();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });
});
