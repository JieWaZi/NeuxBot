import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { ChatComposerWorkspaceStrip } from "./chat-composer-workspace-strip";

it("展示目录名与 Git 分支", () => {
  render(
    <TooltipProvider>
      <ChatComposerWorkspaceStrip
        workspaceLabel="acp-go"
        workspaceDirectory="/tmp/acp-go"
        branchControl={<span data-testid="branch-control">main</span>}
      />
    </TooltipProvider>,
  );

  expect(screen.getByText("acp-go")).toBeInTheDocument();
  expect(screen.getByTestId("branch-control")).toHaveTextContent("main");
});

it("无工作区信息时不渲染", () => {
  const { container } = render(<ChatComposerWorkspaceStrip />);
  expect(container).toBeEmptyDOMElement();
});
