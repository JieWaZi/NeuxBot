import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import type { Waker } from "../model/types";
import { WakerFormDialog } from "./waker-form-dialog";

const { save } = vi.hoisted(() => ({
  save: vi.fn<(request: unknown) => Promise<unknown>>(async () => ({})),
}));
vi.mock("../api/waker-queries", () => ({
  updateWakerMutation: () => ({ mutationFn: save }),
  listWakersQueryKey: () => ["wakers"],
  getWakerQueryKey: () => ["waker"],
}));
vi.mock("@/shared/ui/avatar", () => ({ Avatar: () => null }));

const savedAvatar = "bloub:plush:nuage:curieux:38bdf8";
function openForm(
  avatar = savedAvatar,
  roleName = "设计师",
  profile: Pick<Waker, "expertise" | "workStyle" | "workMethods"> = {},
) {
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
            roleName,
            avatar,
            ...profile,
          }}
        />
      </QueryClientProvider>
    );
  }
  return render(<Form />);
}

beforeEach(() => save.mockClear());

it("角色名称为空时也能补填角色和简介", async () => {
  openForm(savedAvatar, "");
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("角色名称"), "文档评审员");
  await user.type(screen.getByLabelText("简介"), "复核草稿与来源。");
  await user.click(screen.getByRole("button", { name: "保存修改" }));
  await waitFor(() => expect(save).toHaveBeenCalledOnce());
  expect(save.mock.calls[0]?.[0]).toMatchObject({
    body: { roleName: "文档评审员", bio: "复核草稿与来源。" },
  });
  expect(save.mock.calls[0]?.[0]).not.toHaveProperty("body.manual");
});

it("基本编辑使用紧凑弹窗，只提交姓名、角色、简介和头像", async () => {
  openForm(savedAvatar, "设计师", {
    expertise: [{ title: "方案设计", description: "整理方案与交付边界" }],
    workStyle: ["小步迭代"],
    workMethods: [{ name: "交付", steps: ["确认目标", "实现方案"] }],
  });
  const user = userEvent.setup();
  expect(screen.getAllByRole("textbox")).toHaveLength(3);
  const dialog = screen.getByRole("dialog", { name: "编辑伙伴" });
  expect(dialog).toHaveClass("max-w-[520px]");
  expect(dialog).not.toHaveClass("h-[min(800px,calc(100dvh-48px))]");
  expect(
    screen.queryByRole("button", { name: "默认模型" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("概要")).not.toBeInTheDocument();
  expect(screen.queryByText("擅长领域")).not.toBeInTheDocument();
  expect(screen.queryByText("工作风格")).not.toBeInTheDocument();
  expect(screen.queryByText("工作方式")).not.toBeInTheDocument();
  await user.type(
    screen.getByRole("textbox", { name: "简介" }),
    "说明当前职责。",
  );
  await user.click(screen.getByRole("button", { name: "保存修改" }));
  await waitFor(() => expect(save).toHaveBeenCalledOnce());
  expect(save.mock.calls[0]?.[0]).toHaveProperty("path.wakerId", "alice");
  expect(save.mock.calls[0]?.[0]).toHaveProperty("body", {
    name: "Alice",
    roleName: "设计师",
    bio: "说明当前职责。",
    avatar: savedAvatar,
  });
});

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

  it.each([
    { label: "粒子", material: "particles" },
    { label: "软胶", material: "balloon" },
  ])("修改为 $label 后保存完整配置", async ({ label, material }) => {
    openForm();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    await user.click(screen.getByRole("button", { name: label }));
    await user.click(screen.getByRole("button", { name: "三角形" }));
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(save).toHaveBeenCalledOnce());
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      body: { avatar: `bloub:${material}:triangle:timide:38bdf8` },
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
});
