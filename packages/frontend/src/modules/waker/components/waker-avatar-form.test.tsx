import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { nextCompanionAvatar } from "@/shared/companion/appearance";
import { WakerFormDialog } from "./waker-form-dialog";

const { save } = vi.hoisted(() => ({
  save: vi.fn<(request: unknown) => Promise<unknown>>(async () => ({})),
}));
vi.mock("../api/waker-queries", () => ({
  createWakerMutation: () => ({ mutationFn: save }),
  updateWakerMutation: () => ({ mutationFn: save }),
  listWakersQueryKey: () => ["wakers"],
  getWakerQueryKey: () => ["waker"],
  listWakerDocumentsQueryKey: () => ["documents"],
}));
vi.mock("./role-preset-picker", () => ({ RolePresetPicker: () => null }));

const savedAvatar = "companion:v1:0123456789abcdef0123456789abcdef";
function openForm() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <WakerFormDialog
        open
        onOpenChange={vi.fn()}
        waker={{
          id: "alice",
          name: "Alice",
          roleName: "设计师",
          avatar: savedAvatar,
        }}
      />
    </QueryClientProvider>,
  );
}

describe("形象编辑保存", () => {
  it("编辑名字保存原形象，下一款后保存顺序切换的形象，没有图片上传入口", async () => {
    save.mockClear();
    const view = openForm();
    expect(
      view.container.ownerDocument.querySelector(
        'input[accept="image/png,image/jpeg"]',
      ),
    ).toBeNull();
    fireEvent.change(screen.getByPlaceholderText("例如：小艾"), {
      target: { value: "Alicia" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      body: { name: "Alicia", avatar: savedAvatar },
    });
    view.unmount();
    save.mockClear();
    openForm();
    fireEvent.click(screen.getByRole("button", { name: "下一款" }));
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      body: { avatar: nextCompanionAvatar(savedAvatar) },
    });
    expect(save.mock.calls[0]?.[0]).not.toMatchObject({
      body: { avatar: savedAvatar },
    });
  });
});
