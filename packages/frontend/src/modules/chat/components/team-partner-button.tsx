import type { ComponentProps, CSSProperties } from "react";

import {
  companionAppearance,
  companionAvatar,
} from "@/shared/companion/appearance";
import { CompanionAvatar } from "@/shared/companion/companion-avatar";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";

/** 委派与收件提示共用伙伴入口的色调、头像交互和焦点反馈。 */
export function TeamPartnerButton({
  name,
  avatar,
  className,
  ...props
}: Omit<
  ComponentProps<typeof Button>,
  "size" | "variant" | "children" | "style"
> & { name: string; avatar?: string }) {
  const resolvedAvatar = companionAvatar(avatar, name);
  const appearance = companionAppearance(resolvedAvatar);
  return (
    <Button
      {...props}
      type="button"
      variant="ghost"
      size="sm"
      className={cn(
        "max-w-full min-w-0 hover:bg-(--companion-hover-surface) hover:text-(--companion-hover-content) focus-visible:bg-(--companion-hover-surface) focus-visible:text-(--companion-hover-content)",
        className,
      )}
      style={
        {
          "--companion-hover-surface": `color-mix(in srgb, ${appearance.color} 16%, transparent)`,
          "--companion-hover-content": `color-mix(in srgb, ${appearance.color} 45%, var(--foreground))`,
        } as CSSProperties
      }
      data-avatar-hover
    >
      <span className="size-avatar-xs inline-flex shrink-0" aria-hidden="true">
        <CompanionAvatar avatar={resolvedAvatar} interactive />
      </span>
      <span className="truncate">{name}</span>
    </Button>
  );
}
