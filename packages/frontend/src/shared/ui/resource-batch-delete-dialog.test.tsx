import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import "@/shared/i18n";

import { ResourceBatchDeleteDialog } from "./resource-batch-delete-dialog";

describe("ResourceBatchDeleteDialog", () => {
  it("keeps only failed items for retry and reports successful deletions", async () => {
    const user = userEvent.setup();
    const onDelete = vi
      .fn<(id: string) => Promise<void>>()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce(undefined);
    const onDeleted = vi.fn(async () => undefined);
    const onOpenChange = vi.fn();
    render(
      <ResourceBatchDeleteDialog
        items={[
          { id: "a", name: "项目 A" },
          { id: "b", name: "项目 B" },
        ]}
        title="删除项目"
        description="确认范围"
        warning="保留本地文件"
        onDelete={onDelete}
        onDeleted={onDeleted}
        onOpenChange={onOpenChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "确认删除" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("1 项删除失败");
    expect(onDeleted).toHaveBeenCalledWith(["a"]);
    expect(
      within(screen.getByRole("list", { name: "待删除条目" })).queryByText(
        "项目 A",
      ),
    ).not.toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "重试失败项" }));
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onDelete.mock.calls.map(([id]) => id)).toEqual(["a", "b", "b"]);
    expect(onDeleted).toHaveBeenLastCalledWith(["b"]);
  });

  it("blocks dismissal and duplicate submission while deletion is pending", async () => {
    const user = userEvent.setup();
    let finish = () => {};
    const onDelete = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const onOpenChange = vi.fn();
    render(
      <ResourceBatchDeleteDialog
        items={[{ id: "a", name: "A" }]}
        title="删除项目"
        description="确认范围"
        warning="保留本地文件"
        onDelete={onDelete}
        onDeleted={async () => undefined}
        onOpenChange={onOpenChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "确认删除" }));
    expect(screen.getByRole("button", { name: "正在删除…" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(onOpenChange).not.toHaveBeenCalled();
    finish();
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });
});
