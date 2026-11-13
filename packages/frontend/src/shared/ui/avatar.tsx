import type { StateId } from "@neuxbot/bloub/states";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import { companionAvatar } from "@/shared/companion/appearance";
import { CompanionAvatar } from "@/shared/companion/companion-avatar";
import type {
  CompanionActivity,
  CompanionAttention,
} from "@/shared/companion/activity";

type AvatarProps = {
  name: string;
  src?: string;
  shape?: "circle" | "square";
  size?: "xs" | "sm" | "md" | "lg" | "xl" | "hero" | "docked";
  className?: string;
  activity?: CompanionActivity;
  attention?: CompanionAttention;
  interactive?: boolean;
  /** 静态头像保留形状、材质和默认表情，不启动帧循环。 */
  animated?: boolean;
  preview?: boolean;
  animation?: StateId;
  onDoubleInteract?: (trigger: HTMLElement) => void;
};
const sizeClass = {
  xs: "size-avatar-xs",
  sm: "size-avatar-sm",
  md: "size-avatar-md",
  lg: "size-avatar-lg",
  xl: "size-avatar-xl",
  hero: "size-avatar-hero",
  docked: "size-avatar-docked",
} as const;

export function Avatar({
  className,
  name,
  size = "md",
  src,
  activity,
  attention,
  interactive = true,
  animated = true,
  preview = false,
  animation,
  onDoubleInteract,
}: AvatarProps) {
  const { t } = useTranslation("common");
  const canInteract = animated && (interactive || Boolean(onDoubleInteract));
  const image = (
    <CompanionAvatar
      avatar={companionAvatar(src, name)}
      activity={activity}
      attention={attention}
      animated={animated}
      interactive={canInteract}
      preview={preview}
      animation={animation}
    />
  );
  return (
    <span
      className={cn(
        "relative inline-flex shrink-0 align-middle",
        sizeClass[size],
        className,
      )}
      aria-hidden={canInteract ? undefined : true}
    >
      {canInteract ? (
        <button
          type="button"
          data-avatar-hover
          aria-label={t(
            onDoubleInteract ? "companion.showcase" : "companion.interact",
            { name },
          )}
          className="focus-visible:ring-ring before:min-h-control-md before:min-w-control-md relative block size-full cursor-pointer rounded-sm outline-none before:absolute before:top-1/2 before:left-1/2 before:-translate-x-1/2 before:-translate-y-1/2 focus-visible:ring-2"
          onPointerDown={(event) => event.stopPropagation()}
          onDoubleClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onDoubleInteract?.(event.currentTarget);
          }}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            if (event.detail === 0) onDoubleInteract?.(event.currentTarget);
          }}
        >
          {image}
        </button>
      ) : (
        image
      )}
    </span>
  );
}

/** 静态缩略图与动态头像共用同一渲染层，固定采样而不创建播放循环。 */
export function StaticAvatar({
  name,
  src,
  size = "md",
  className,
}: Pick<AvatarProps, "name" | "src" | "size" | "className">) {
  return (
    <Avatar
      name={name}
      src={src}
      size={size}
      className={className}
      animated={false}
    />
  );
}

export function RoleAvatar({
  preset,
  size = "md",
}: {
  preset: string;
  size?: "md" | "lg";
}) {
  return <Avatar name={preset} src={preset} size={size} />;
}
