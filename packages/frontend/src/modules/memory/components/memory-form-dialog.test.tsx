import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { MemoryFormDialog } from "./memory-form-dialog";
import "@/shared/i18n";

function renderDialog(
  props: Partial<Parameters<typeof MemoryFormDialog>[0]> = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryFormDialog
        open
        onOpenChange={vi.fn()}
        projectId=""
        scope="partner"
        wakerId="waker-1"
        {...props}
      />
    </QueryClientProvider>,
  );
}

describe("MemoryFormDialog", () => {
  it("rejects empty manual memories before calling the API", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByRole("button", { name: "保存" }));

    expect(screen.getByRole("alert")).toHaveTextContent("请输入记忆内容。");
  });

  it("starts editing from the current memory content", () => {
    renderDialog({
      memory: { content: "用户偏好深色模式", id: "memory-1" },
    });

    expect(screen.getByLabelText("记忆内容")).toHaveValue("用户偏好深色模式");
    expect(screen.getByText("编辑记忆")).toBeVisible();
  });
});
