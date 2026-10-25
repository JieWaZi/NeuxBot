import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { QueryState } from "./query-state";

describe("详情读取状态", () => {
  it("失败提供重试而不显示空数据，恢复时移除错误", async () => {
    const retry = vi.fn();
    const view = render(
      <QueryState
        error={new Error("读取失败")}
        emptyLabel="暂无记录"
        onRetry={retry}
      />,
    );
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.queryByText("暂无记录")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(retry).toHaveBeenCalledOnce();
    view.rerender(<QueryState pending emptyLabel="暂无记录" onRetry={retry} />);
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    view.rerender(<QueryState emptyLabel="暂无记录" onRetry={retry} />);
    expect(screen.getByText("暂无记录")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
