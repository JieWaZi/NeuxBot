import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RolePresetCard } from "./role-preset-card";

const preset = {
  id: "preset-1",
  roleName: "产品设计师",
  defaultAvatar: "preset:designer",
  description: "负责产品体验设计",
  source: "user" as const,
  expertise: [],
  workStyle: [],
  workMethods: [],
};

describe("RolePresetCard", () => {
  it("selects when the card body is clicked", () => {
    const onSelect = vi.fn();
    render(
      <RolePresetCard
        preset={preset}
        selected={false}
        onSelect={onSelect}
        onView={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("radio"));

    expect(onSelect).toHaveBeenCalledOnce();
  });

  it("opens details without changing the selection", () => {
    const onSelect = vi.fn();
    const onView = vi.fn();
    render(
      <RolePresetCard
        preset={preset}
        selected={false}
        onSelect={onSelect}
        onView={onView}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "查看详情" }));

    expect(onView).toHaveBeenCalledOnce();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("opens the user action menu without changing the selection", () => {
    const onSelect = vi.fn();
    render(
      <RolePresetCard
        preset={preset}
        selected={false}
        onSelect={onSelect}
        onView={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "角色预设更多操作" }));

    expect(onSelect).not.toHaveBeenCalled();
  });

  it("does not expose edit actions for a system preset", () => {
    render(
      <RolePresetCard
        preset={{ ...preset, source: "system" }}
        selected={false}
        onSelect={vi.fn()}
        onView={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
      />,
    );

    expect(
      screen.queryByRole("button", { name: "角色预设更多操作" }),
    ).not.toBeInTheDocument();
  });
});
