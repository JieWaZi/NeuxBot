import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { LibraryList } from "./knowledge-library";
import { emptyLibrary, type Library } from "./knowledge-model";

const library = {
  ...emptyLibrary("library-1", "团队知识", "general"),
  pageTotal: 7,
};

function setup(
  onDelete = vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
) {
  const open = vi.fn();
  render(
    <TooltipProvider>
      <LibraryList
        libraries={[library]}
        open={open}
        create={vi.fn()}
        onDelete={onDelete}
      />
    </TooltipProvider>,
  );
  return { open, onDelete };
}

describe("knowledge library cards", () => {
  it("keeps title tags and Wiki total while opening only from the card body", async () => {
    const user = userEvent.setup();
    const { open } = setup();
    const card = screen.getByRole("article");
    const title = within(card).getByRole("heading", { name: "团队知识" });
    expect(title.parentElement).not.toHaveTextContent("通用");
    expect(title.parentElement).toHaveTextContent("本地");
    expect(within(card).queryByText("在线")).not.toBeInTheDocument();
    expect(within(card).queryByText("离线")).not.toBeInTheDocument();
    expect(within(card).getByText("7 篇 Wiki")).toBeInTheDocument();
    await user.click(within(card).getByRole("button", { name: "团队知识" }));
    expect(open).toHaveBeenCalledWith(library);
  });

  it("requires confirmation before permanently deleting a library and its originals", async () => {
    const user = userEvent.setup();
    const { open, onDelete } = setup();
    await user.click(
      screen.getByRole("button", { name: "团队知识的更多操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "永久删除知识库" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("原始资料");
    expect(dialog).toHaveTextContent("无法恢复");
    expect(onDelete).not.toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
    await user.click(
      within(dialog).getByRole("button", { name: "永久删除知识库" }),
    );
    expect(onDelete).toHaveBeenCalledWith(library, expect.any(String));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("keeps the confirmation open when deletion fails", async () => {
    const user = userEvent.setup();
    setup(vi.fn<() => Promise<void>>().mockRejectedValue(new Error("offline")));
    await user.click(
      screen.getByRole("button", { name: "团队知识的更多操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "永久删除知识库" }));
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "永久删除知识库",
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "永久删除知识库" }),
      ).toBeEnabled(),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(
      screen.getByRole("dialog", { name: "永久删除知识库" }),
    ).toBeInTheDocument();
  });

  it("omits the official type badge and disables unsupported deletion", async () => {
    const user = userEvent.setup();
    render(
      <TooltipProvider>
        <LibraryList
          libraries={[{ ...library, kind: "official", storage: "remote" }]}
          open={vi.fn()}
          create={vi.fn()}
          onDelete={vi.fn()}
          canDelete={() => false}
        />
      </TooltipProvider>,
    );
    expect(screen.queryByText("官方 Wiki")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "团队知识的更多操作" }),
    );
    expect(
      screen.getByRole("menuitem", { name: "永久删除知识库" }),
    ).toHaveAttribute("data-disabled");
  });
  it("远端离线卡片不可进入但仍可移除本地挂载", async () => {
    const user = userEvent.setup();
    const remote = {
      ...library,
      storage: "remote" as const,
      offline: true,
      mountId: "mount-1",
      mountRevision: 2,
    };
    const open = vi.fn();
    render(
      <TooltipProvider>
        <LibraryList
          libraries={[remote]}
          open={open}
          create={vi.fn()}
          onDelete={vi.fn()}
        />
      </TooltipProvider>,
    );
    expect(screen.getByText("离线")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "团队知识" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "团队知识" }));
    expect(open).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "团队知识的更多操作" }),
    );
    expect(
      screen.getByRole("menuitem", { name: "移除远端入口" }),
    ).not.toHaveAttribute("data-disabled");
  });

  it("未挂载的远端目录项不提供删除", async () => {
    const user = userEvent.setup();
    render(
      <TooltipProvider>
        <LibraryList
          libraries={[{ ...library, storage: "remote" }]}
          open={vi.fn()}
          create={vi.fn()}
          onDelete={vi.fn()}
        />
      </TooltipProvider>,
    );
    await user.click(
      screen.getByRole("button", { name: "团队知识的更多操作" }),
    );
    expect(
      screen.getByRole("menuitem", { name: "永久删除知识库" }),
    ).toHaveAttribute("data-disabled");
  });

  it("retains cleanup confirmation after the row disappears and reuses its request on retry", async () => {
    const user = userEvent.setup();
    const onDelete = vi
      .fn<(library: Library, requestId: string) => Promise<void>>()
      .mockRejectedValueOnce(new Error("cleanup pending"))
      .mockResolvedValue(undefined);
    const props = {
      open: vi.fn(),
      create: vi.fn(),
      onDelete,
    };
    const view = render(
      <TooltipProvider>
        <LibraryList {...props} libraries={[library]} />
      </TooltipProvider>,
    );
    await user.click(
      screen.getByRole("button", { name: "团队知识的更多操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "永久删除知识库" }));
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "永久删除知识库",
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "永久删除知识库" }),
      ).toBeEnabled(),
    );
    view.rerender(
      <TooltipProvider>
        <LibraryList {...props} libraries={[]} />
      </TooltipProvider>,
    );
    expect(screen.getByRole("dialog")).toHaveTextContent("原始资料");
    const requestId = onDelete.mock.calls[0]?.[1];
    expect(requestId).toEqual(expect.any(String));
    expect(
      screen.getByRole("dialog", { name: "永久删除知识库" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "永久删除知识库" }));
    expect(onDelete).toHaveBeenLastCalledWith(library, requestId);
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });
  it("identifies a remote Wiki count as the latest observed snapshot", async () => {
    const user = userEvent.setup();
    render(
      <TooltipProvider delayDuration={0}>
        <LibraryList
          libraries={[
            {
              ...library,
              storage: "remote",
              pageTotalObservedAt: 1792411200000,
            },
          ]}
          open={vi.fn()}
          create={vi.fn()}
        />
      </TooltipProvider>,
    );
    expect(screen.getByText("在线")).toBeInTheDocument();
    await user.hover(screen.getByText("7 篇 Wiki"));
    expect(await screen.findByRole("tooltip")).toHaveTextContent("最近读取");
    expect(screen.getByRole("tooltip")).toHaveTextContent("2026");
  });
});

it("加载时保留操作和搜索，并在卡片区域显示骨架", () => {
  render(
    <TooltipProvider>
      <LibraryList
        libraries={[]}
        state="loading"
        open={vi.fn()}
        create={vi.fn()}
      />
    </TooltipProvider>,
  );
  expect(screen.getByRole("button", { name: "新建知识库" })).toBeEnabled();
  expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
  expect(screen.queryByText("没有找到知识库")).not.toBeInTheDocument();
});
