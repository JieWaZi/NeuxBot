import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatActivityDateRangePicker } from "./chat-activity-date-range-picker";

describe("ChatActivityDateRangePicker", () => {
  it("使用两次日期点击确认一个范围", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<ChatActivityDateRangePicker from="" to="" onChange={onChange} />);

    await user.click(screen.getByRole("button", { name: "选择最近活动时间" }));
    const dayButtons = within(screen.getByRole("grid")).getAllByRole("button");
    expect(dayButtons.length).toBeGreaterThan(12);
    const startDate = dayButtons[10];
    const endDate = dayButtons[12];
    if (!startDate || !endDate) throw new Error("日历缺少可选择的日期");

    await user.click(startDate);
    expect(onChange).not.toHaveBeenCalled();
    await user.click(endDate);

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange.mock.calls[0]).toEqual([
      expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
    ]);
    expect(screen.queryByRole("grid")).not.toBeInTheDocument();
  });

  it("在日期选择弹层内清除已有范围", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ChatActivityDateRangePicker
        from="2026-08-01"
        to="2026-08-05"
        onChange={onChange}
      />,
    );

    expect(
      screen.queryByRole("button", { name: "清除" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "选择最近活动时间" }));
    await user.click(screen.getByRole("button", { name: "清除" }));

    expect(onChange).toHaveBeenCalledWith("", "");
    expect(screen.queryByRole("grid")).not.toBeInTheDocument();
  });
});
