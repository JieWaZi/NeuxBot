import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { ChatComposerWorkspaceStrip } from "./chat-composer-workspace-strip";

it("展示目录名", () => {
  render(
    <TooltipProvider>
      <ChatComposerWorkspaceStrip
        workspaceLabel="acp-go"
        workspaceDirectory="/tmp/acp-go"
      />
    </TooltipProvider>,
  );

  expect(screen.getByText("acp-go")).toBeInTheDocument();
});

it("无工作区信息时不渲染", () => {
  const { container } = render(<ChatComposerWorkspaceStrip />);
  expect(container).toBeEmptyDOMElement();
});
