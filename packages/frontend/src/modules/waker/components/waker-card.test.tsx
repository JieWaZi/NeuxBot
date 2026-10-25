import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { PERSONAL_ASSISTANT_WAKER_ID } from "../model/builtin-waker";
import { WakerCard } from "./waker-card";

vi.mock("./waker-form-dialog", () => ({ WakerFormDialog: () => null }));
vi.mock("./waker-delete-dialog", () => ({ WakerDeleteDialog: () => null }));

const waker = {
  id: "waker-1",
  name: "伙伴",
  roleName: "助理",
  avatar: "companion:v1:0123456789abcdef0123456789abcdef",
};

describe("WakerCard", () => {
  it("默认伙伴不提供删除入口", async () => {
    render(
      <MemoryRouter>
        <WakerCard waker={{ ...waker, isDefault: true }} />
      </MemoryRouter>,
    );

    await userEvent.click(screen.getByRole("button", { name: "更多操作" }));
    expect(screen.queryByText("删除伙伴")).not.toBeInTheDocument();
  });

  it("内置伙伴 ID 即使缺少 isDefault 也不提供删除入口", async () => {
    render(
      <MemoryRouter>
        <WakerCard
          waker={{
            ...waker,
            id: PERSONAL_ASSISTANT_WAKER_ID,
            name: "NeuxBot",
            roleName: "私人助理",
          }}
        />
      </MemoryRouter>,
    );

    await userEvent.click(screen.getByRole("button", { name: "更多操作" }));
    expect(screen.queryByText("删除伙伴")).not.toBeInTheDocument();
  });

  it("普通伙伴仍可删除", async () => {
    render(
      <MemoryRouter>
        <WakerCard waker={waker} />
      </MemoryRouter>,
    );

    await userEvent.click(screen.getByRole("button", { name: "更多操作" }));
    expect(screen.getByText("删除伙伴")).toBeVisible();
  });

  it("悬停覆盖整块内容区，头像不再套内层卡片", () => {
    const { container } = render(
      <MemoryRouter>
        <WakerCard waker={waker} />
      </MemoryRouter>,
    );

    const hoverRegion = container.querySelector("article > div");
    expect(hoverRegion).toHaveAttribute("data-slot", "card-body");
    expect(hoverRegion).toHaveClass(
      "group-hover/resource-card:bg-surface-subtle",
    );
    expect(container.querySelector("article")).toHaveClass(
      "group/resource-card",
    );
    expect(hoverRegion?.querySelector(".size-avatar-lg")).not.toBeNull();
    expect(
      screen.getByRole("link", { name: "查看 伙伴 的详情" }),
    ).not.toHaveClass("hover:bg-surface-subtle");
  });
});
