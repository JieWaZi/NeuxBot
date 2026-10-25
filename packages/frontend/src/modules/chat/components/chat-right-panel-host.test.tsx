import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { ChatRightPanelHost } from "./chat-right-panel-host";

describe("ChatRightPanelHost", () => {
  it("keeps the workspace mounted when collapsed", () => {
    const { rerender } = render(
      <ChatRightPanelHost
        activePanel="workspace"
        workspacePanel={<div data-testid="workspace-panel" />}
      />,
    );
    const host = screen.getByTestId("chat-right-panel-host");
    expect(screen.getByTestId("workspace-panel")).toBeVisible();

    rerender(
      <ChatRightPanelHost
        workspacePanel={<div data-testid="workspace-panel" />}
      />,
    );

    expect(host).toBeInTheDocument();
    expect(host).toHaveAttribute("aria-hidden", "true");
    expect(screen.getByTestId("workspace-panel")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-panel").parentElement).toHaveClass(
      "invisible",
    );
  });

  it("renders the retained workbench and dismisses the right panel", () => {
    const onDismiss = vi.fn();
    render(
      <ChatRightPanelHost
        activePanel="workspace"
        onDismiss={onDismiss}
        workspacePanel={<div data-testid="workspace-panel" />}
      />,
    );

    expect(screen.getByTestId("workspace-panel")).toBeVisible();
    const host = screen.getByTestId("chat-right-panel-host");
    expect(host).toHaveClass("max-lg:absolute", "max-lg:right-0");
    fireEvent.click(screen.getByRole("button", { name: "关闭工作台" }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});
