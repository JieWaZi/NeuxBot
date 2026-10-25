import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type * as ReactQuery from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { ChatTaskListPage } from "./chat-task-list-page";

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof ReactQuery>();
  return {
    ...actual,
    useQuery: () => ({
      data: {
        pageNum: 1,
        pageSize: 50,
        resources: [
          {
            activeExecutionId: "",
            id: "chat-1",
            projectName: "NeuxBot",
            title: "整理对话任务页",
            updatedAt: 1_785_892_500_000,
            wakerId: "waker-1",
            workspaceDirectory: "/tmp/ally",
          },
          {
            activeExecutionId: "execution-1",
            id: "chat-2",
            projectName: "",
            title: "修复日期筛选",
            updatedAt: 1_785_888_900_000,
            wakerId: "waker-1",
            workspaceDirectory: "/tmp/share",
          },
        ],
        totalSize: 2,
      },
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(async () => undefined),
    }),
  };
});

vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}));

describe("ChatTaskListPage", () => {
  it("支持名称查询、日期范围和表格批量操作", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/wakers/waker-1/chats"]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/chats"
              element={
                <>
                  <ChatTaskListPage />
                  <LocationSearch />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(
      screen.getByRole("button", { name: "选择最近活动时间" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("开始日期")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("结束日期")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "清除" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("查询名称")).not.toBeInTheDocument();
    const table = screen.getByRole("table", { name: "对话任务列表" });
    expect(table).not.toHaveClass("h-full");
    expect(table.parentElement).toHaveClass("overflow-auto");
    expect(container.querySelectorAll("[data-table-empty-row]")).toHaveLength(
      0,
    );
    expect(screen.getAllByRole("checkbox")).toHaveLength(3);

    await user.click(
      screen.getByRole("checkbox", { name: "选择对话“整理对话任务页”" }),
    );
    await user.click(
      screen.getByRole("checkbox", { name: "选择对话“修复日期筛选”" }),
    );
    expect(screen.getByText("已选择 2 项")).toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: "选择本页全部对话" }),
    ).toBeChecked();
    await user.click(screen.getByRole("button", { name: "删除所选" }));
    expect(
      screen.getByRole("heading", { name: "删除所选对话" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/所选 2 个对话/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "取消" }));

    await user.type(
      screen.getByRole("searchbox", { name: "查询名称" }),
      "修复",
    );
    expect(screen.getByTestId("location-search")).toHaveTextContent(
      "?query=%E4%BF%AE%E5%A4%8D",
    );

    await user.click(screen.getByRole("button", { name: "每页条数" }));
    await user.click(screen.getByRole("menuitemradio", { name: "30 条/页" }));
    expect(container.querySelectorAll("[data-table-empty-row]")).toHaveLength(
      0,
    );
    expect(screen.getByTestId("location-search")).toHaveTextContent(
      "?query=%E4%BF%AE%E5%A4%8D&pageSize=30",
    );

    await user.click(screen.getByRole("button", { name: "选择最近活动时间" }));
    expect(screen.getByRole("grid")).toBeInTheDocument();
  });
});

function LocationSearch() {
  const location = useLocation();
  return <output data-testid="location-search">{location.search}</output>;
}
