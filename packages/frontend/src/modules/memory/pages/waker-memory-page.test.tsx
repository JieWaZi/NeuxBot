import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { WakerMemoryPage } from "./waker-memory-page";
import "@/shared/i18n";

const { listMemoriesOptions } = vi.hoisted(() => ({
  listMemoriesOptions: vi.fn(
    (input: {
      query: { pageNum: number; pageSize: number; scope: string };
    }) => ({
      queryFn: async () => ({
        pageNum: input.query.pageNum,
        pageSize: input.query.pageSize,
        resources: [
          {
            content: "记忆内容",
            id: "memory-1",
            scope: "partner",
            scopeId: "waker-1",
            updatedAt: 1_700_000_000_000,
          },
        ],
        totalSize: 40,
      }),
      queryKey: ["memories", input.query.pageSize],
    }),
  ),
}));

vi.mock("../api/memory-queries", () => ({
  deleteMemoryMutation: () => ({ mutationFn: vi.fn() }),
  invalidateMemoryQueries: vi.fn(),
  listMemoriesOptions,
}));

vi.mock("@/modules/project", () => ({
  listProjectsOptions: () => ({
    queryFn: async () => ({ resources: [], totalSize: 0 }),
    queryKey: ["projects"],
  }),
}));

vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("../components/memory-form-dialog", () => ({
  MemoryFormDialog: () => null,
}));

vi.mock("../components/memory-settings-dialog", () => ({
  MemorySettingsDialog: () => null,
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/wakers/waker-1/memory"]}>
        <Routes>
          <Route path="/wakers/:wakerId/memory" element={<WakerMemoryPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("WakerMemoryPage", () => {
  it("queries the combined current-memory view", async () => {
    renderPage();

    expect(await screen.findByText("记忆内容")).toBeVisible();
    await waitFor(() => {
      expect(listMemoriesOptions).toHaveBeenCalledWith(
        expect.objectContaining({
          query: expect.objectContaining({
            pageNum: 1,
            pageSize: 20,
            scope: "all",
          }),
        }),
      );
    });
  });

  it("loads another twenty memories in the page", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(
      await screen.findByRole("button", { name: "展开更多 20 条" }),
    );
    await waitFor(() => {
      expect(listMemoriesOptions).toHaveBeenCalledWith(
        expect.objectContaining({
          query: expect.objectContaining({ pageSize: 40, scope: "all" }),
        }),
      );
    });
  });

  it("uses the standard delete confirmation structure", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole("button", { name: "遗忘记忆" }));

    const dialog = screen.getByRole("dialog", { name: "遗忘记忆" });
    expect(within(dialog).getByText("确定遗忘这条记忆吗？")).toBeVisible();
    expect(
      within(dialog).getByText(
        "当前记忆会被删除，但遗忘事件及原内容仍保留在记忆时间线中。",
      ),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "确认遗忘" }),
    ).toBeVisible();
  });
});
