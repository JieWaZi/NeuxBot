import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { MemoryWorkspaceDialogContent } from "./memory-workspace-dialog-content";
import { Dialog } from "@/shared/ui/dialog";

describe("MemoryWorkspaceDialogContent", () => {
  it("keeps memory workspaces on the shared dialog size and layout", () => {
    render(
      <Dialog open>
        <MemoryWorkspaceDialogContent title="记忆窗口">
          <div>窗口内容</div>
        </MemoryWorkspaceDialogContent>
      </Dialog>,
    );

    expect(screen.getByRole("dialog")).toHaveClass(
      "max-w-[720px]",
      "h-[min(800px,calc(100dvh-48px))]",
    );
  });
});
