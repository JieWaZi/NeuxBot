import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, onTestFinished, vi } from "vitest";

import "@/shared/i18n";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelMemberDetail as ChannelMember } from "@/shared/api/generated/types.gen";
import { TeamAvatarStack } from "./team-avatar-stack";

function members(count: number): ChannelMember[] {
  return Array.from({ length: count }, (_, index) => ({
    waker: { id: "waker-" + (index + 1), name: "伙伴 " + (index + 1) },
  }));
}

describe("TeamAvatarStack", () => {
  it("悬停整个团队头像时成员切换表情，移开恢复且不产生嵌套按钮", () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
    onTestFinished(() => random.mockRestore());
    const { container } = render(<TeamAvatarStack members={members(2)} />);
    const group = screen.getByRole("img");
    const avatars = [
      ...container.querySelectorAll("svg[data-companion-avatar]"),
    ];
    const resting = avatars.map((avatar) =>
      avatar.getAttribute("data-expression"),
    );
    fireEvent.pointerEnter(group);
    for (const [index, avatar] of avatars.entries()) {
      expect(avatar.getAttribute("data-expression")).not.toBe(resting[index]);
    }
    fireEvent.pointerLeave(group);
    expect(
      avatars.map((avatar) => avatar.getAttribute("data-expression")),
    ).toEqual(resting);
    expect(group.querySelector("button")).toBeNull();
  });
  it.each([1, 2, 3, 4])("%s 位成员全部展示头像，不提前替换成数量", (count) => {
    const { container } = render(
      <TeamAvatarStack members={members(count)} animated={false} />,
    );
    expect(
      container.querySelectorAll("svg[data-companion-avatar]"),
    ).toHaveLength(count);
    expect(screen.queryByText(/^\+/)).toBeNull();
  });
  it("五位成员显示前三位和剩余数量，保持四个单元", () => {
    const { container } = render(
      <TeamAvatarStack members={members(5)} animated={false} />,
    );
    expect(
      container.querySelectorAll("svg[data-companion-avatar]"),
    ).toHaveLength(3);
    expect(screen.getByText("+2")).toBeVisible();
  });

  it("双位数量为底行留位，保留两位真实成员并计入其余成员", () => {
    const { container } = render(
      <TeamAvatarStack members={members(13)} animated={false} />,
    );
    expect(
      container.querySelectorAll("svg[data-companion-avatar]"),
    ).toHaveLength(2);
    expect(screen.getByText("+11")).toBeVisible();
  });

  it("极小头像为可读数量留出底行，不把文字挤进第四个微小单元", () => {
    const { container } = render(
      <TeamAvatarStack members={members(5)} size="xs" animated={false} />,
    );
    expect(
      container.querySelectorAll("svg[data-companion-avatar]"),
    ).toHaveLength(2);
    expect(screen.getByText("+3")).toBeVisible();
  });
  it("静态场景的单人与多人头像不启动播放循环", () => {
    const raf = vi.spyOn(window, "requestAnimationFrame");
    try {
      for (const count of [1, 2, 3, 4, 5]) {
        const view = render(
          <TeamAvatarStack members={members(count)} animated={false} />,
        );
        expect(raf).not.toHaveBeenCalled();
        view.unmount();
      }
    } finally {
      raf.mockRestore();
    }
  });
});
