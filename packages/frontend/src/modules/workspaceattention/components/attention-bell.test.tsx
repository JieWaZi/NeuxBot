import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { AttentionBell } from "./attention-bell";

it.each([
  [false, false],
  [false, true],
  [true, false],
  [true, true],
] as const)(
  "通知状态决定小点，键盘打开看板时不附带筛选 (%s, %s)",
  async (hasAction, hasUnread) => {
    const user = userEvent.setup();
    const open = vi.fn();
    const { container, rerender } = render(
      <TooltipProvider>
        <AttentionBell
          hasAction={hasAction}
          hasUnread={hasUnread}
          synced
          onOpen={open}
        />
      </TooltipProvider>,
    );
    const button = screen.getByRole("button", { name: /任务看板|task board/i });
    expect(button.textContent).toBe("");
    expect(Boolean(container.querySelector("[data-attention-dot]"))).toBe(
      hasAction || hasUnread,
    );
    await user.tab();
    expect(button).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(open).toHaveBeenCalledExactlyOnceWith();
    rerender(
      <TooltipProvider>
        <AttentionBell
          hasAction={hasAction}
          hasUnread={hasUnread}
          synced={false}
          onOpen={open}
        />
      </TooltipProvider>,
    );
    expect(button).toHaveAccessibleName(/未同步|not synced/i);
    expect(Boolean(container.querySelector("[data-attention-dot]"))).toBe(
      hasAction || hasUnread,
    );
  },
);
