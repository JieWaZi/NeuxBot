import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Link, MemoryRouter } from "react-router-dom";

import { PageHeader } from "./page-header";
import { PageAction } from "./resource-page";

describe("详情页标题与操作", () => {
  it("链接操作通过 asChild 保留唯一子元素并正确导航", () => {
    render(
      <MemoryRouter>
        <PageAction asChild>
          <Link to="/chat" aria-label="发起对话">
            <span aria-hidden>图标</span>
            <span>发起对话</span>
          </Link>
        </PageAction>
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "发起对话" })).toHaveAttribute(
      "href",
      "/chat",
    );
  });
  it("asChild 追加动作文案时仍保留链接身份与点击回调", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    render(
      <MemoryRouter>
        <PageAction asChild intent="primary" label="发起对话">
          <Link to="/chat" onClick={onClick}>
            <span aria-hidden>图标</span>
          </Link>
        </PageAction>
      </MemoryRouter>,
    );
    const link = screen.getByRole("link", { name: "发起对话" });
    expect(link).toHaveAttribute("href", "/chat");
    expect(link).toHaveTextContent("发起对话");
    await user.click(link);
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByRole("button", { name: "发起对话" }),
    ).not.toBeInTheDocument();
  });
  it("未提供返回入口时保留普通标题，包括伙伴详情", () => {
    render(
      <PageHeader
        layout="detail"
        title="伙伴详情"
        visual={<span>形象</span>}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "伙伴详情", level: 1 }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "返回列表" }),
    ).not.toBeInTheDocument();
  });

  it("返回入口可通过键盘到达并调用页面提供的列表导航", async () => {
    const user = userEvent.setup();
    const onBack = vi.fn();
    render(
      <PageHeader
        layout="detail"
        title="项目详情"
        backAction={{ label: "返回项目列表", onClick: onBack }}
        actions={
          <PageAction intent="edit" label="编辑项目">
            <span aria-hidden>图标</span>
          </PageAction>
        }
      />,
    );
    await user.tab();
    expect(screen.getByRole("button", { name: "返回项目列表" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(onBack).toHaveBeenCalledTimes(1);
    await user.tab();
    expect(screen.getByRole("button", { name: "编辑项目" })).toHaveFocus();
  });

  it("删除只显示图标，保留可访问名称与禁用保护", async () => {
    const user = userEvent.setup();
    const onDelete = vi.fn();
    render(
      <PageHeader
        title="操作"
        actions={
          <PageAction
            intent="delete"
            label="删除项目"
            disabled
            onClick={onDelete}
          >
            <span aria-hidden>图标</span>
          </PageAction>
        }
      />,
    );
    const remove = screen.getByRole("button", { name: "删除项目" });
    expect(remove).not.toHaveTextContent("删除项目");
    expect(remove).toBeDisabled();
    await user.click(remove);
    expect(onDelete).not.toHaveBeenCalled();
  });

  it("编辑只显示图标，运行、更新与撤销授权保留文案", () => {
    render(
      <PageHeader
        title="操作"
        actions={
          <>
            <PageAction intent="edit" label="编辑展示名称">
              <span aria-hidden>图标</span>
            </PageAction>
            <PageAction intent="revoke" label="撤销授权">
              <span aria-hidden>图标</span>
            </PageAction>
            <PageAction intent="secondary" label="更新">
              <span aria-hidden>图标</span>
            </PageAction>
            <PageAction intent="primary" label="立即运行">
              <span aria-hidden>图标</span>
            </PageAction>
          </>
        }
      />,
    );
    const edit = screen.getByRole("button", { name: "编辑展示名称" });
    expect(edit).not.toHaveTextContent("编辑展示名称");
    expect(edit).toHaveAttribute("data-variant", "ghost");
    for (const label of ["立即运行", "更新", "撤销授权"]) {
      expect(screen.getByRole("button", { name: label })).toHaveTextContent(
        label,
      );
    }
  });
});
