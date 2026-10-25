import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { ConnectorCard } from "./connector-card";

const connector = {
  activationPolicy: "per_execution" as const,
  affectedWakerCount: 1,
  compatibility: [
    { runtimeId: "codex", supported: true },
    { runtimeId: "claude", supported: true },
    { runtimeId: "kimi", supported: true },
  ],
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
  it("内容区与 Skill 高度一致，标签固定同行，底栏保持独立", () => {
    const { container } = render(
      <MemoryRouter>
        <ConnectorCard
          connector={connector}
          wakerId="waker-1"
          onEnabledChange={vi.fn()}
        />
      </MemoryRouter>,
    );

    const article = container.querySelector("article");
    const content = article?.querySelector(".h-connector-card-content");
    const footer = article?.querySelector("footer");

    expect(content).toBeInTheDocument();
    expect(footer).toBeInTheDocument();
    expect(screen.getByText("STDIO")).toBeVisible();
    expect(screen.getByText("STDIO").parentElement).toBe(
      screen.getByText("全局").parentElement,
    );
    expect(footer).toHaveClass("h-row-navigation", "shrink-0");
    expect(
      within(footer as HTMLElement).queryByText("STDIO"),
    ).not.toBeInTheDocument();
    expect(
      within(footer as HTMLElement).getByLabelText("Codex · 支持"),
    ).toBeVisible();
    expect(
      within(footer as HTMLElement).getByLabelText("Claude · 支持"),
    ).toBeVisible();
    expect(
      within(footer as HTMLElement).getByLabelText("Kimi · 支持"),
    ).toBeVisible();
    expect(screen.queryByText("已启用")).not.toBeInTheDocument();
    expect(screen.queryByText(/挂载/)).not.toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "停用 Chrome" })).toBeVisible();
  });

  it.each(["工作流", "名称很长的自定义连接器"])(
    "%s 的 Streamable HTTP 简写为 HTTP，始终与名称同行",
    (name) => {
      render(
        <MemoryRouter>
          <ConnectorCard
            connector={{ ...connector, name, transport: "streamable_http" }}
            wakerId="waker-1"
            onEnabledChange={vi.fn()}
          />
        </MemoryRouter>,
      );

      expect(screen.getByText("HTTP")).toBeVisible();
      expect(screen.queryByText("Streamable HTTP")).not.toBeInTheDocument();
      expect(screen.getByText("HTTP").parentElement).toBe(
        screen.getByRole("heading", { name }).parentElement,
      );
    },
  );

  it("协作连接器只通过开关展示启用状态", () => {
    render(
      <MemoryRouter>
        <ConnectorCard
          connector={{
            ...connector,
            activationPolicy: "team_context",
            id: "collaboration",
            name: "团队协作",
            scope: "built_in",
          }}
          wakerId="waker-1"
          onEnabledChange={vi.fn()}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("内置")).toBeVisible();
    expect(screen.queryByText(/挂载/)).not.toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "停用 团队协作" })).toBeChecked();
  });

  it("shows at most five runtime icons and collapses the remainder", () => {
    const connectorWithMoreRuntimes = {
      ...connector,
      compatibility: [
        { runtimeId: "codex", supported: true },
        { runtimeId: "claude", supported: true },
        { runtimeId: "gemini", supported: true },
        { runtimeId: "cursor", supported: true },
        { runtimeId: "copilot", supported: false },
        { runtimeId: "pi", supported: true },
      ],
    };
    const { container } = render(
      <MemoryRouter>
        <ConnectorCard
          connector={connectorWithMoreRuntimes}
          wakerId="waker-1"
          onEnabledChange={vi.fn()}
        />
      </MemoryRouter>,
    );

    const footer = container.querySelector("footer") as HTMLElement;

    expect(within(footer).getByLabelText("还有 1 个智能体")).toBeVisible();
    expect(footer.querySelectorAll("span[title]")).toHaveLength(5);
    expect(within(footer).getByTitle("copilot · 当前不支持")).toHaveClass(
      "opacity-35",
    );
    expect(within(footer).queryByTitle("pi · 支持")).not.toBeInTheDocument();
  });

  it("工作流连接器显示系统启用状态且没有手动开关", () => {
    const { container } = render(
      <MemoryRouter>
        <ConnectorCard
          connector={{
            ...connector,
            activationPolicy: "workflow_context",
            id: "workflow",
            name: "内置工作流",
            scope: "built_in",
          }}
          wakerId="waker-1"
          onEnabledChange={vi.fn()}
        />
      </MemoryRouter>,
    );

    expect(within(container).getByText("由系统按场景启用")).toBeVisible();
    expect(within(container).queryByRole("switch")).not.toBeInTheDocument();
  });
});
