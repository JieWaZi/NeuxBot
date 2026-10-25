import { cn } from "@/shared/lib/cn";
import { companionAvatar } from "@/shared/companion/appearance";
import { CompanionAvatar } from "@/shared/companion/companion-avatar";
import type { CompanionControl } from "@/shared/companion/companion-avatar";
import type { CompanionActivity } from "@/shared/companion/activity";
import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { useCompanionDock } from "@/shared/ui/companion-interaction";

type AvatarProps = {
  name: string;
  src?: string;
  shape?: "circle" | "square";
  size?: "xs" | "sm" | "md" | "lg" | "xl" | "hero" | "docked";
  className?: string;
  activity?: CompanionActivity;
  interactive?: boolean;
  /** 静态头像保留身体风格，关闭状态、待机动画和独立互动。 */
  animated?: boolean;
  preview?: boolean;
  summon?: boolean;
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
  interactive = true,
  animated = true,
  preview = false,
  summon = true,
  onDoubleInteract,
}: AvatarProps) {
  const { t } = useTranslation("common");
  const dock = useCompanionDock();
  const canSummon = Boolean(summon && dock);
  const control = useRef<CompanionControl>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const clicks = useRef({ count: 0, lastAt: 0 });
  const avatar = companionAvatar(src, name);
  const image = (
    <CompanionAvatar
      ref={control}
      avatar={avatar}
      activity={activity}
      animated={animated}
      preview={preview}
    />
  );
  const frameClass = cn(
    "relative inline-flex shrink-0 align-middle",
    sizeClass[size],
    className,
  );
  if (!interactive || !animated)
    return (
      <span className={frameClass} aria-hidden="true">
        {image}
      </span>
    );
  const interact = () => {
    control.current?.interact();
    const now = Date.now();
    clicks.current.count =
      now - clicks.current.lastAt <= 650 ? clicks.current.count + 1 : 1;
    clicks.current.lastAt = now;
    if (clicks.current.count === 2 && trigger.current)
      onDoubleInteract?.(trigger.current);
    if (clicks.current.count >= 3) {
      clicks.current.count = 0;
      if (canSummon && trigger.current)
        dock?.show(avatar, name, trigger.current);
    }
  };
  return (
    <>
      <span className={frameClass}>
        <button
          ref={trigger}
          type="button"
          className="focus-visible:ring-ring before:min-h-control-md before:min-w-control-md relative block size-full cursor-pointer rounded-sm outline-none before:absolute before:top-1/2 before:left-1/2 before:-translate-x-1/2 before:-translate-y-1/2 focus-visible:ring-2"
          aria-label={t("companion.interact", { name })}
          aria-expanded={
            canSummon ? dock?.selected?.avatar === avatar : undefined
          }
          title={t(
            onDoubleInteract
              ? "companion.showcaseHint"
              : canSummon
                ? "companion.hint"
                : "companion.tapHint",
          )}
          onPointerDown={(event) => event.stopPropagation()}
          onDoubleClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
          }}
          onKeyDown={(event) => {
            event.stopPropagation();
            if (event.key === "ArrowUp") {
              event.preventDefault();
              if (canSummon && trigger.current)
                dock?.show(avatar, name, trigger.current);
            }
          }}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            interact();
          }}
        >
          {image}
        </button>
      </span>
    </>
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
