import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { WakerListPage } from "./waker-list-page";

vi.mock("../api/waker-queries", () => ({
  listWakersOptions: () => ({
    queryKey: ["wakers"],
    queryFn: async () => ({ resources: [], totalSize: 0 }),
  }),
}));

vi.mock("../components/waker-form-dialog", () => ({
  WakerFormDialog: () => null,
}));

function renderPage(initialEntry = "/wakers") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route
            path="/wakers"
            element={
              <WakerListPage
                renderTeamCollection={() => <div>团队列表</div>}
                renderTeamDialog={() => null}
              />
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("WakerListPage", () => {
  it("使用资源页工具栏布局并支持搜索伙伴", async () => {
    const user = userEvent.setup();
    renderPage();

    expect(
      screen.queryByRole("radiogroup", { name: "伙伴管理范围" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("searchbox", { name: "搜索伙伴" }).parentElement,
    ).toHaveClass(
      "basis-field-search-min",
      "min-w-field-search-min",
      "max-w-field-search",
      "grow-[4]",
    );
    expect(screen.getByText("全部伙伴")).toBeVisible();
    expect(screen.getByText("0 位伙伴")).toBeVisible();

    await user.type(
      screen.getByRole("searchbox", { name: "搜索伙伴" }),
      "阿斯顿",
    );
    expect(screen.getByRole("searchbox", { name: "搜索伙伴" })).toHaveValue(
      "阿斯顿",
    );
  });

  it("仍可通过路由参数展示团队管理内容", () => {
    renderPage("/wakers?tab=teams");

    expect(screen.getByText("团队列表")).toBeInTheDocument();
    expect(
      screen.queryByRole("searchbox", { name: "搜索伙伴" }),
    ).not.toBeInTheDocument();
  });

  it("不再渲染页面级伙伴管理侧栏", () => {
    renderPage();

    expect(
      screen.queryByRole("navigation", { name: "伙伴管理目录" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("main")).toHaveClass("bg-surface-scene");
    expect(screen.getByRole("main")).not.toHaveClass("shadow-panel");
  });
});
