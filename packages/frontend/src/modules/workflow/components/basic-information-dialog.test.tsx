import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { BasicInformationDialog } from "./basic-information-dialog";
import { ApiError } from "@/shared/api/errors";
import "@/shared/i18n";

const { update, save } = vi.hoisted(() => ({ update: vi.fn(), save: vi.fn() }));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  updateWorkflowCategoriesMutation: () => ({ mutationFn: update }),
  getWorkflowQueryKey: () => ["workflow"],
  listWorkflowsQueryKey: () => ["workflows"],
  listResourceCategoriesQueryKey: () => ["categories"],
  listResourceCategoriesOptions: () => ({
    queryKey: ["categories"],
    queryFn: async () => [],
  }),
}));
vi.mock("../model/use-workflow-configuration", () => ({
  useWorkflowConfiguration: () => ({ save, pending: false, error: null }),
}));
beforeEach(() => {
  update.mockReset().mockResolvedValue({});
  save.mockReset().mockResolvedValue(true);
});

it("只保存分类时复用共享标签字段，不提交源码或创建版本", async () => {
  const close = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <BasicInformationDialog
        id="flow"
        revisionId="revision"
        title="流程"
        description="说明"
        categories={[]}
        open
        onOpenChange={close}
      />
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  await user.type(
    screen.getByRole("combobox", { name: "分类" }),
    "设计{Enter}",
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { id: "flow" },
        body: { categories: ["设计"] },
      }),
      expect.anything(),
    ),
  );
  expect(save).not.toHaveBeenCalled();
  expect(close).toHaveBeenCalledWith(false);
});

it("分类保存失败保留草稿和错误，重试不修改源码", async () => {
  update.mockRejectedValueOnce(new ApiError("分类保存失败", "DatabaseError"));
  const close = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <BasicInformationDialog
        id="flow"
        revisionId="revision"
        title="流程"
        description="说明"
        open
        onOpenChange={close}
      />
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  await user.type(
    screen.getByRole("combobox", { name: "分类" }),
    "研究{Enter}",
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("分类保存失败");
  expect(close).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "移除分类“研究”" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(close).toHaveBeenCalledWith(false));
  expect(save).not.toHaveBeenCalled();
});
