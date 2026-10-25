import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import "@/shared/i18n";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelMemberDetail as ChannelMember } from "@/shared/api/generated/types.gen";

import {
  FRAME_SIZE_PX,
  TeamAvatarStack,
  teamAvatarStackLayoutsForTest,
} from "./team-avatar-stack";

function members(count: number): ChannelMember[] {
  return Array.from({ length: count }, (_, index) => ({
    waker: {
      id: `waker-${index + 1}`,
      name: `伙伴 ${index + 1}`,
    },
  }));
}

function miniAvatarFrames(container: HTMLElement) {
  return [...container.querySelectorAll(".size-avatar-sm > span[style]")].map(
    (node) => ({
      height: Number((node as HTMLElement).style.height.replace("px", "")),
      left: Number((node as HTMLElement).style.left.replace("px", "")),
      top: Number((node as HTMLElement).style.top.replace("px", "")),
      width: Number((node as HTMLElement).style.width.replace("px", "")),
    }),
  );
}

describe("TeamAvatarStack", () => {
  it("静态选项传递到单人及多人团队的所有成员头像", () => {
    for (const count of [1, 2, 3, 5]) {
      const view = render(
        <TeamAvatarStack members={members(count)} animated={false} />,
      );
      const avatars = view.container.querySelectorAll(
        "[data-companion-animated]",
      );
      expect(avatars).toHaveLength(Math.min(count, 3));
      for (const avatar of avatars)
        expect(avatar).toHaveAttribute("data-companion-animated", "false");
      view.unmount();
    }
  });

  it("单人与多人团队都使用同一尺寸的方框容器", () => {
    const single = render(<TeamAvatarStack members={members(1)} />);
    const pair = render(<TeamAvatarStack members={members(2)} />);

    expect(single.container.querySelector(".size-avatar-sm")).toHaveClass(
      "inline-flex",
      "rounded-sm",
      "size-avatar-sm",
    );
    expect(pair.container.querySelector(".size-avatar-sm")).toHaveClass(
      "inline-flex",
      "rounded-sm",
      "size-avatar-sm",
    );
  });

  it("双人团队使用内联尺寸约束头像，并在对角分布", () => {
    const { container } = render(<TeamAvatarStack members={members(2)} />);

    expect(miniAvatarFrames(container)).toEqual([
      {
        height: teamAvatarStackLayoutsForTest.pair[0]?.sizePx,
        left: teamAvatarStackLayoutsForTest.pair[0]?.x,
        top: teamAvatarStackLayoutsForTest.pair[0]?.y,
        width: teamAvatarStackLayoutsForTest.pair[0]?.sizePx,
      },
      {
        height: teamAvatarStackLayoutsForTest.pair[1]?.sizePx,
        left: teamAvatarStackLayoutsForTest.pair[1]?.x,
        top: teamAvatarStackLayoutsForTest.pair[1]?.y,
        width: teamAvatarStackLayoutsForTest.pair[1]?.sizePx,
      },
    ]);
    expect(teamAvatarStackLayoutsForTest.pair[0]?.sizePx).toBeGreaterThan(
      teamAvatarStackLayoutsForTest.pair[1]?.sizePx ?? 0,
    );
  });

  it("3 位成员比 2 位成员更小且分布在三个角", () => {
    const pair = render(<TeamAvatarStack members={members(2)} />);
    const triangle = render(<TeamAvatarStack members={members(3)} />);

    const pairFrames = miniAvatarFrames(pair.container);
    const triangleFrames = miniAvatarFrames(triangle.container);

    expect(Math.max(...pairFrames.map((frame) => frame.width))).toBeGreaterThan(
      Math.max(...triangleFrames.map((frame) => frame.width)),
    );
    expect(
      new Set(triangleFrames.map((frame) => `${frame.left},${frame.top}`)).size,
    ).toBe(3);
  });

  it("4 位及以上成员使用更小四角布局并显示 +N", () => {
    const { container } = render(<TeamAvatarStack members={members(5)} />);

    const overflowBadge = screen.getByText("+2");
    const frames = miniAvatarFrames(container);

    expect(overflowBadge).toBeVisible();
    expect(overflowBadge).toHaveClass(
      "rounded-sm",
      "font-semibold",
      "text-primary",
    );
    expect(overflowBadge).not.toHaveClass("bg-primary/12", "bg-muted");
    expect(
      frames
        .slice(0, 3)
        .every(
          (frame) =>
            frame.width === teamAvatarStackLayoutsForTest.grid[0]?.sizePx,
        ),
    ).toBe(true);
    expect(frames.at(-1)?.width).toBe(
      teamAvatarStackLayoutsForTest.overflowBadge.sizePx,
    );
    expect(frames.at(-1)?.width ?? 0).toBeGreaterThan(
      teamAvatarStackLayoutsForTest.grid[0]?.sizePx ?? 0,
    );
    expect(
      teamAvatarStackLayoutsForTest.grid[0]?.sizePx ?? FRAME_SIZE_PX,
    ).toBeLessThan(
      teamAvatarStackLayoutsForTest.triangle[0]?.sizePx ?? FRAME_SIZE_PX,
    );
  });
});
