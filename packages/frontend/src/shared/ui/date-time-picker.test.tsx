import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { TimePicker } from "./date-time-picker";

beforeEach(() => {
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
    window.setTimeout(() => callback(performance.now()), 16),
  );
  vi.stubGlobal("cancelAnimationFrame", (id: number) =>
    window.clearTimeout(id),
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function TimeField() {
  const [value, setValue] = useState("09:00");
  return <TimePicker aria-label="运行时间" value={value} onChange={setValue} />;
}

it.each([0, 20])(
  "连续输入（间隔 %i ms）小时和分钟后，完成操作保留完整选值",
  async (delay) => {
    const user = userEvent.setup({ delay });
    render(<TimeField />);
    await user.click(screen.getByRole("button", { name: "运行时间" }));
    screen.getByRole("spinbutton", { name: "小时" }).focus();
    await user.keyboard("13");
    await waitFor(() =>
      expect(screen.getByRole("spinbutton", { name: "小时" })).toHaveAttribute(
        "aria-valuenow",
        "13",
      ),
    );
    screen.getByRole("spinbutton", { name: "分钟" }).focus();
    await user.keyboard("45");
    await user.click(screen.getByRole("button", { name: "完成" }));
    expect(screen.getByRole("button", { name: "运行时间" })).toHaveTextContent(
      "13:45",
    );
  },
);

it("点击滚轮后快速输入并关闭，旧动画不会覆盖已确认的时间", async () => {
  const frames = new Map<number, FrameRequestCallback>();
  let sequence = 0;
  vi.spyOn(performance, "now").mockReturnValue(0);
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    frames.set(++sequence, callback);
    return sequence;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  const user = userEvent.setup();
  render(<TimeField />);
  await user.click(screen.getByRole("button", { name: "运行时间" }));
  await user.click(screen.getByRole("spinbutton", { name: "小时" }));
  await user.keyboard("13");
  screen.getByRole("spinbutton", { name: "分钟" }).focus();
  await user.keyboard("45");
  await user.click(screen.getByRole("button", { name: "完成" }));
  act(() => {
    for (let time = 16; time <= 1000 && frames.size; time += 16) {
      const pending = [...frames.values()];
      frames.clear();
      pending.forEach((callback) => callback(time));
    }
  });
  expect(screen.getByRole("button", { name: "运行时间" })).toHaveTextContent(
    "13:45",
  );
});
