import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { RolePresetFormDialog } from "./role-preset-form-dialog";
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
  createRolePresetMutation: () => ({ mutationFn: save }),
  updateRolePresetMutation: () => ({ mutationFn: save }),
  listRolePresetsQueryKey: () => ["role-presets"],
  listRolePresetDocumentsOptions: () => ({
    queryKey: ["role-preset-documents"],
    queryFn: async () => [],
  }),
}));
vi.mock("./role-preset-picker", () => ({ RolePresetPicker: () => null }));
vi.mock("@/shared/ui/avatar", () => ({ Avatar: () => null }));

const savedAvatar = "bloub:plush:nuage:curieux:38bdf8";
function openForm(avatar = savedAvatar) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  function Form() {
    const [open, setOpen] = useState(true);
    return (
      <QueryClientProvider client={client}>
        <button onClick={() => setOpen(true)}>重新编辑</button>
        <WakerFormDialog
          open={open}
          onOpenChange={setOpen}
          waker={{
            id: "alice",
            name: "Alice",
            roleName: "设计师",
            avatar,
          }}
        />
      </QueryClientProvider>
    );
  }
  return render(<Form />);
}

beforeEach(() => save.mockClear());

describe("头像编辑保存", () => {
  it.each([savedAvatar, "companion:0123456789abcdef0123456789abcdef"])(
    "只改名字时保留已保存头像 %s",
    async (avatar) => {
      const view = openForm(avatar);
      const user = userEvent.setup();
      expect(
        view.container.ownerDocument.querySelector('input[type="file"]'),
      ).toBeNull();
      const name = screen.getByPlaceholderText("例如：小艾");
      await user.clear(name);
      await user.type(name, "Alicia");
      await user.click(screen.getByRole("button", { name: "保存修改" }));
      await waitFor(() => expect(save).toHaveBeenCalledOnce());
      expect(save.mock.calls[0]?.[0]).toMatchObject({
        body: { name: "Alicia", avatar },
      });
    },
  );

  it("修改选项后保存完整配置", async () => {
    openForm();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    await user.click(screen.getByRole("button", { name: "粒子" }));
    await user.click(screen.getByRole("button", { name: "三角形" }));
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(save).toHaveBeenCalledOnce());
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      body: { avatar: "bloub:particles:triangle:curieux:38bdf8" },
    });
  });

  it("取消表单不保存头像，重新编辑恢复已保存配置", async () => {
    openForm();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    await user.click(screen.getByRole("button", { name: "粒子" }));
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(save).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "重新编辑" }));
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    expect(screen.getByRole("button", { name: "毛绒" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "云朵" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(save).toHaveBeenCalledOnce());
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      body: { avatar: savedAvatar },
    });
  });

  it("角色预设保存选择后的完整头像配置", async () => {
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={client}>
        <RolePresetFormDialog
          open
          onOpenChange={vi.fn()}
          onSaved={vi.fn()}
          preset={{
            id: "designer",
            source: "user",
            roleName: "设计师",
            description: "把需求转化为清晰的界面。",
            defaultAvatar: savedAvatar,
          }}
        />
      </QueryClientProvider>,
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑默认头像" }));
    await user.click(screen.getByRole("button", { name: "粒子" }));
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "保存角色预设" }));
    await waitFor(() => expect(save).toHaveBeenCalledOnce());
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      body: { defaultAvatar: "bloub:particles:nuage:curieux:38bdf8" },
    });
  });
});
