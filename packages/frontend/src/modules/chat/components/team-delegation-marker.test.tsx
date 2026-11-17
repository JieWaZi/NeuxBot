import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, onTestFinished, vi } from "vitest";

import "@/shared/i18n";
import { TeamDelegationMarker } from "./team-delegation-marker";

describe("TeamDelegationMarker", () => {
  it("多个伙伴共用一条提示且不显示时间，各自保留会话入口", () => {
    const onOpen = vi.fn();
    const { container } = render(
      <TeamDelegationMarker
        direction="outgoing"
        partners={["A", "B", "C", "D"].map((id) => ({
          id,
          name: id,
          available: true,
          onOpen: () => onOpen(id),
        }))}
      />,
    );
    expect(screen.getAllByText("委派给")).toHaveLength(1);
    expect(container.querySelectorAll("time")).toHaveLength(0);
    for (const id of ["A", "B", "C", "D"]) {
      fireEvent.click(screen.getByRole("button", { name: `打开${id}的会话` }));
    }
    expect(onOpen.mock.calls).toEqual([["A"], ["B"], ["C"], ["D"]]);
  });
  it("悬停伙伴入口切换表情，移开恢复，并保持同一个可点击入口", () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
    onTestFinished(() => random.mockRestore());
    const onOpen = vi.fn();
    render(
      <TeamDelegationMarker
        direction="incoming"
        partners={[
          {
            id: "lumi",
            name: "lumi",
            avatar: "bloub:plush:cercle:neutre:ef69bc",
            available: true,
            onOpen,
          },
        ]}
      />,
    );
    const button = screen.getByRole("button", { name: "打开lumi的会话" });
    const avatar = button.querySelector("svg[data-companion-avatar]");
    expect(avatar).toHaveAttribute("data-expression", "neutre");
    fireEvent.pointerEnter(button);
    expect(avatar?.getAttribute("data-expression")).not.toBe("neutre");
    fireEvent.pointerLeave(button);
    expect(avatar).toHaveAttribute("data-expression", "neutre");
    fireEvent.click(button);
    expect(onOpen).toHaveBeenCalledOnce();
    expect(button.querySelector("button")).toBeNull();
  });
});
