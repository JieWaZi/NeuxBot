import type { CSSProperties } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelMemberDetail as ChannelMember } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { companionAvatar } from "@/shared/companion/appearance";
import { CompanionAvatar } from "@/shared/companion/companion-avatar";

const frames = {
  xs: { className: "size-avatar-xs", size: "var(--spacing-avatar-xs)" },
  sm: { className: "size-avatar-sm", size: "var(--spacing-avatar-sm)" },
  md: { className: "size-avatar-md", size: "var(--spacing-avatar-md)" },
  navigation: {
    className: "size-avatar-navigation",
    size: "var(--spacing-avatar-navigation)",
  },
} as const;

// 轮廓只沿透明身体绘制，用所在行的底色遮住后方头像，不给整个方框加边。
const contour = [
  "drop-shadow(var(--team-avatar-edge) 0 0 var(--team-avatar-outline,var(--canvas)))",
  "drop-shadow(calc(var(--team-avatar-edge) * -1) 0 0 var(--team-avatar-outline,var(--canvas)))",
  "drop-shadow(0 var(--team-avatar-edge) 0 var(--team-avatar-outline,var(--canvas)))",
  "drop-shadow(0 calc(var(--team-avatar-edge) * -1) 0 var(--team-avatar-outline,var(--canvas)))",
].join(" ");

/** 等大成员按两列聚合；导航槽与绘图区域分开，给身体轮廓保留透明边缘。 */
export function TeamAvatarStack({
  className,
  maxVisible = 4,
  members,
  size = "sm",
  animated = true,
}: {
  className?: string;
  maxVisible?: number;
  members: ChannelMember[];
  size?: keyof typeof frames;
  animated?: boolean;
}) {
  const { t } = useTranslation("team");
  const people = members.flatMap((member) =>
    member.waker?.id ? [member.waker] : [],
  );
  if (!people.length) return null;
  const capacity = Math.max(1, Math.min(4, Math.floor(maxVisible)));
  const overflowing = people.length > capacity;
  const availableFaces = overflowing ? capacity - 1 : capacity;
  // 宽数量与极小头像把数量放在底行，保留字号和真实成员数量。
  const visibleFaces =
    overflowing && (size === "xs" || people.length - availableFaces >= 10)
      ? Math.min(2, availableFaces)
      : availableFaces;
  const visible = people.slice(0, visibleFaces);
  const hidden = people.length - visible.length;
  const cells = visible.length + (hidden > 0 ? 1 : 0);
  const frame = frames[size];
  // 导航头像槽与行高一致，在槽内给重叠的身体和轮廓留出绘图余量。
  const drawingSize =
    "calc(" + frame.size + (size === "navigation" ? " - 1rem)" : " - 0.5rem)");
  const cellClass = (index: number) =>
    cn(
      "relative inline-flex size-(--team-avatar-face) shrink-0",
      cells > 1 &&
        "[&>span>svg]:absolute [&>span>svg]:top-1/2 [&>span>svg]:left-1/2 [&>span>svg]:size-(--team-avatar-graphic-size) [&>span>svg]:-translate-1/2 [&>span>svg]:scale-115 [&>span>svg]:[filter:var(--team-avatar-contour)]",
      cells === 1 && "col-span-2 row-span-2",
      cells === 2 && "row-span-2",
      cells === 3 && index === 2 && "col-span-2",
    );
  return (
    <span
      role="img"
      data-avatar-hover
      aria-label={t("membersAria")}
      className={cn(
        "relative isolate inline-grid shrink-0 grid-cols-[repeat(2,var(--team-avatar-pitch))] grid-rows-[repeat(2,var(--team-avatar-pitch))] place-content-center place-items-center align-middle",
        frame.className,
        className,
      )}
      style={
        {
          "--team-avatar-pitch": "calc(" + drawingSize + " * 0.38)",
          "--team-avatar-face":
            cells === 1 ? frame.size : "calc(" + drawingSize + " * 0.54)",
          "--team-avatar-graphic-size": "128%",
          "--team-avatar-count-size":
            "max(var(--spacing-icon-md), calc(" + drawingSize + " * 0.46))",
          "--team-avatar-edge": size === "navigation" ? "3.5px" : "2px",
          "--team-avatar-contour": contour,
        } as CSSProperties
      }
    >
      {visible.map((person, index) => (
        <span
          key={person.id}
          aria-hidden="true"
          className={cellClass(index)}
          style={{ zIndex: index + 1 }}
        >
          <CompanionAvatar
            interactive={animated}
            animated={animated}
            avatar={companionAvatar(person.avatar, person.name || t("member"))}
          />
        </span>
      ))}
      {hidden > 0 ? (
        <span
          aria-hidden="true"
          style={{
            zIndex: visible.length + 1,
            filter: size === "xs" ? undefined : "var(--team-avatar-contour)",
          }}
          className={cn(
            cellClass(visible.length),
            "bg-accent-surface text-accent-content type-caption h-(--team-avatar-count-size) w-max min-w-(--team-avatar-count-size) items-center justify-center rounded-full px-0.5 font-semibold whitespace-nowrap tabular-nums",
            size === "xs" && "px-0",
          )}
        >
          +{hidden}
        </span>
      ) : null}
    </span>
  );
}
