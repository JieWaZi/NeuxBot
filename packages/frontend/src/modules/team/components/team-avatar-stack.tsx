import { type CSSProperties, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelMemberDetail as ChannelMember } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { Avatar } from "@/shared/ui/avatar";
import {
  wakerCompanionActivity,
  type CompanionActivity,
} from "@/shared/companion/activity";

const DEFAULT_MAX_VISIBLE = 3;
const FRAME_SIZE_PX = 32;

const frameSizeClass = {
  sm: "size-avatar-sm",
  xs: "size-avatar-xs",
} as const;

type TeamAvatarStackSize = keyof typeof frameSizeClass;

type TeamAvatarPerson = {
  activity: CompanionActivity;
  avatar?: string;
  id: string;
  name: string;
};

type MiniAvatarSlot = {
  sizePx: number;
  x: number;
  y: number;
  zIndex: number;
};

function placeMiniAvatar(
  sizePx: number,
  x: number,
  y: number,
  zIndex: number,
): MiniAvatarSlot {
  return { sizePx, x, y, zIndex };
}

function cornerSlot(
  frameSizePx: number,
  sizePx: number,
  corner: "top-left" | "top-right" | "bottom-left" | "bottom-right",
  zIndex: number,
): MiniAvatarSlot {
  const offset = frameSizePx - sizePx;
  switch (corner) {
    case "top-left":
      return placeMiniAvatar(sizePx, 0, 0, zIndex);
    case "top-right":
      return placeMiniAvatar(sizePx, offset, 0, zIndex);
    case "bottom-left":
      return placeMiniAvatar(sizePx, 0, offset, zIndex);
    case "bottom-right":
      return placeMiniAvatar(sizePx, offset, offset, zIndex);
  }
}

function scalePx(frameSizePx: number, value: number) {
  return Math.round((value * frameSizePx) / FRAME_SIZE_PX);
}

function createStackLayouts(frameSizePx: number) {
  return {
    pair: [
      placeMiniAvatar(scalePx(frameSizePx, 25), 0, 0, 10),
      placeMiniAvatar(
        scalePx(frameSizePx, 19),
        frameSizePx - scalePx(frameSizePx, 19),
        frameSizePx - scalePx(frameSizePx, 19),
        20,
      ),
    ],
    triangle: [
      placeMiniAvatar(
        scalePx(frameSizePx, 16),
        (frameSizePx - scalePx(frameSizePx, 16)) / 2,
        0,
        10,
      ),
      cornerSlot(frameSizePx, scalePx(frameSizePx, 16), "bottom-left", 20),
      cornerSlot(frameSizePx, scalePx(frameSizePx, 16), "bottom-right", 30),
    ],
    grid: [
      cornerSlot(frameSizePx, scalePx(frameSizePx, 14), "top-left", 10),
      cornerSlot(frameSizePx, scalePx(frameSizePx, 14), "top-right", 20),
      cornerSlot(frameSizePx, scalePx(frameSizePx, 14), "bottom-left", 30),
    ],
    overflowBadge: placeMiniAvatar(
      scalePx(frameSizePx, 20),
      frameSizePx - scalePx(frameSizePx, 20),
      frameSizePx - scalePx(frameSizePx, 20),
      50,
    ),
  } as const;
}

const stackLayouts = createStackLayouts(FRAME_SIZE_PX);

function miniAvatarFrameStyle(slot: MiniAvatarSlot): CSSProperties {
  return {
    height: slot.sizePx,
    left: slot.x,
    position: "absolute",
    top: slot.y,
    width: slot.sizePx,
    zIndex: slot.zIndex,
  };
}

function projectTeamAvatarMembers(
  members: ChannelMember[],
): TeamAvatarPerson[] {
  return members.flatMap((member) =>
    member.waker?.id
      ? [
          {
            avatar: member.waker.avatar,
            activity: wakerCompanionActivity(member.waker.activityState),
            id: member.waker.id,
            name: member.waker.name ?? "",
          },
        ]
      : [],
  );
}

function TeamAvatarFrame({
  ariaLabel,
  children,
  className,
}: {
  ariaLabel?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <span
      aria-label={ariaLabel}
      className={cn(
        "relative inline-flex shrink-0 rounded-sm align-middle",
        className,
      )}
      role={ariaLabel ? "img" : undefined}
    >
      {children}
    </span>
  );
}

function MiniTeamAvatar({
  member,
  nameFallback,
  slot,
  animated,
}: {
  member: TeamAvatarPerson;
  nameFallback: string;
  slot: MiniAvatarSlot;
  animated: boolean;
}) {
  return (
    <span
      aria-hidden="true"
      className="flex"
      style={miniAvatarFrameStyle(slot)}
    >
      <Avatar
        interactive={false}
        animated={animated}
        className="size-full min-h-0 min-w-0 rounded-full border-0"
        name={member.name || nameFallback}
        shape="circle"
        size="xs"
        src={member.avatar}
        activity={member.activity}
      />
    </span>
  );
}

function TeamAvatarStackContent({
  ariaLabel,
  className,
  fallbackName,
  frameClassName,
  layouts,
  maxVisible,
  members,
  singleAvatarSize,
  animated,
}: {
  ariaLabel: string;
  className?: string;
  fallbackName: string;
  frameClassName: string;
  layouts: ReturnType<typeof createStackLayouts>;
  maxVisible: number;
  members: TeamAvatarPerson[];
  singleAvatarSize: "sm" | "xs";
  animated: boolean;
}) {
  const overflowCount = Math.max(0, members.length - maxVisible);
  const visibleMembers = members.slice(0, maxVisible);

  if (members.length === 1) {
    const member = members[0];
    if (!member) return null;
    return (
      <TeamAvatarFrame
        ariaLabel={ariaLabel}
        className={cn(frameClassName, className)}
      >
        <Avatar
          interactive={false}
          animated={animated}
          className="size-full min-h-0 min-w-0 rounded-none"
          name={member.name || fallbackName}
          shape="square"
          size={singleAvatarSize}
          src={member.avatar}
          activity={member.activity}
        />
      </TeamAvatarFrame>
    );
  }

  return (
    <TeamAvatarFrame
      ariaLabel={ariaLabel}
      className={cn(frameClassName, className)}
    >
      {members.length === 2 ? (
        visibleMembers.map((member, index) => {
          const slot = layouts.pair[index];
          if (!slot) return null;
          return (
            <MiniTeamAvatar
              key={member.id}
              member={member}
              nameFallback={fallbackName}
              slot={slot}
              animated={animated}
            />
          );
        })
      ) : overflowCount > 0 ? (
        <>
          {visibleMembers.slice(0, 3).map((member, index) => {
            const slot = layouts.grid[index];
            if (!slot) return null;
            return (
              <MiniTeamAvatar
                key={member.id}
                member={member}
                nameFallback={fallbackName}
                slot={slot}
                animated={animated}
              />
            );
          })}
          <span
            className="text-primary type-caption grid place-items-center rounded-sm font-semibold"
            style={miniAvatarFrameStyle(layouts.overflowBadge)}
          >
            +{overflowCount}
          </span>
        </>
      ) : (
        visibleMembers.map((member, index) => {
          const slot = layouts.triangle[index];
          if (!slot) return null;
          return (
            <MiniTeamAvatar
              key={member.id}
              member={member}
              nameFallback={fallbackName}
              slot={slot}
              animated={animated}
            />
          );
        })
      )}
    </TeamAvatarFrame>
  );
}

const frameSizePxByToken = {
  sm: FRAME_SIZE_PX,
  xs: 24,
} as const;

export function TeamAvatarStack({
  className,
  maxVisible = DEFAULT_MAX_VISIBLE,
  members,
  size = "sm",
  animated = true,
}: {
  className?: string;
  maxVisible?: number;
  members: ChannelMember[];
  size?: TeamAvatarStackSize;
  animated?: boolean;
}) {
  const { t } = useTranslation("team");
  const resolvedMembers = projectTeamAvatarMembers(members);
  const fallbackName = t("member");
  const frameSizePx = frameSizePxByToken[size];

  if (!resolvedMembers.length) return null;

  return (
    <TeamAvatarStackContent
      ariaLabel={t("membersAria")}
      className={className}
      fallbackName={fallbackName}
      frameClassName={frameSizeClass[size]}
      layouts={createStackLayouts(frameSizePx)}
      maxVisible={maxVisible}
      members={resolvedMembers}
      singleAvatarSize={size}
      animated={animated}
    />
  );
}

export { FRAME_SIZE_PX, stackLayouts as teamAvatarStackLayoutsForTest };
