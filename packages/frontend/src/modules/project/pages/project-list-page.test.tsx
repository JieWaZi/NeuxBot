import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, expect, it, vi } from "vitest";
import "@/shared/i18n";

import { ProjectListPage } from "./project-list-page";
const { remove } = vi.hoisted(() => ({
  remove: vi.fn<
    (options: { path: Record<string, string> }) => Promise<{ success: boolean }>
  >(async () => ({ success: true })),
}));
vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("../components/project-form-dialog", () => ({
  ProjectFormDialog: () => null,
}));
vi.mock("../api/project-queries", () => ({
  listProjectsOptions: ({ query }: { query: { search?: string } }) => ({
    queryKey: ["projects", query.search ?? ""],
    queryFn: async () => ({
      totalSize: 2,
      resources: [
        { id: "a", name: "项目 A", available: true },
        { id: "b", name: "项目 B", available: false },
      ].filter((item) => !query.search || item.name.includes(query.search)),
    }),
  }),
  listProjectsQueryKey: () => ["projects"],
  deleteProjectMutation: () => ({ mutationFn: remove }),
}));
beforeEach(() => remove.mockClear());
function renderPage() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <MemoryRouter initialEntries={["/wakers/owner/projects"]}>
        <Routes>
          <Route
            path="/wakers/:wakerId/projects"
            element={<ProjectListPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
it("selects individual projects without navigation and deletes all selected projects after confirmation", async () => {
  const user = userEvent.setup();
  renderPage();
  await user.click(
    await screen.findByRole("checkbox", { name: "选择项目 项目 A" }),
  );
  expect(screen.getByRole("heading", { name: "项目" })).toBeVisible();
  expect(screen.getByRole("checkbox", { name: "选择" })).toBePartiallyChecked();
  await user.click(screen.getByRole("checkbox", { name: "选择" }));
  expect(screen.getByText("已选择 2 项")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "删除所选" }));
  expect(remove).not.toHaveBeenCalled();
  const dialog = screen.getByRole("dialog", { name: "删除所选项目" });
  expect(within(dialog).getByText(/不会删除任何本地目录/)).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: "确认删除" }));
  await waitFor(() => expect(remove).toHaveBeenCalledTimes(2));
  expect(remove.mock.calls.map((call) => call[0])).toEqual([
    expect.objectContaining({ path: { projectId: "a", wakerId: "owner" } }),
    expect.objectContaining({ path: { projectId: "b", wakerId: "owner" } }),
  ]);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("button", { name: "删除所选" }),
  ).not.toBeInTheDocument();
});
it("clears selection when filtering and when clearing explicitly", async () => {
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole("checkbox", { name: "选择项目 项目 A" });
  await user.click(screen.getByRole("checkbox", { name: "选择" }));
  await user.click(screen.getByRole("button", { name: "清除选择" }));
  expect(screen.getByRole("checkbox", { name: "选择" })).not.toBeChecked();
  await user.click(screen.getByRole("checkbox", { name: "选择" }));
  await user.type(screen.getByRole("searchbox", { name: "搜索项目" }), "B");
  await waitFor(() =>
    expect(
      screen.queryByRole("checkbox", { name: "选择项目 项目 A" }),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("button", { name: "删除所选" }),
  ).not.toBeInTheDocument();
});
