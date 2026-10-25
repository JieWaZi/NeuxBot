// 直接加载使用的图形，避免开发预构建把整套图标与 sourcemap 带入首屏。
import ClaudeCode from "@lobehub/icons/es/ClaudeCode/components/Color";
import Codex from "@lobehub/icons/es/Codex/components/Color";
import Cursor from "@lobehub/icons/es/Cursor/components/Mono";
import GeminiCLI from "@lobehub/icons/es/GeminiCLI/components/Color";
import Grok from "@lobehub/icons/es/Grok/components/Mono";
import Kimi from "@lobehub/icons/es/Kimi/components/Mono";
import OpenCode from "@lobehub/icons/es/OpenCode/components/Mono";
import { Bot } from "lucide-react";

import qoderIcon from "@/shared/assets/brands/qoder.svg";
import workbuddyIcon from "@/shared/assets/brands/workbuddy.svg";
import { cn } from "@/shared/lib/cn";

type RuntimeCliIconProps = {
  runtimeId?: string;
  className?: string;
  size?: number;
};

export function RuntimeCliIcon({
  runtimeId,
  className,
  size = 16,
}: RuntimeCliIconProps) {
  const normalizedRuntimeId = runtimeId?.trim().toLowerCase();

  if (
    normalizedRuntimeId === "qoder" ||
    normalizedRuntimeId === "qoder-cn" ||
    normalizedRuntimeId === "workbuddy"
  ) {
    return (
      <img
        aria-hidden
        alt=""
        src={normalizedRuntimeId === "workbuddy" ? workbuddyIcon : qoderIcon}
        className={cn("shrink-0 object-contain", className)}
        width={size}
        height={size}
      />
    );
  }

  if (normalizedRuntimeId === "claude") {
    return (
      <ClaudeCode
        aria-hidden
        className={cn("shrink-0", className)}
        size={size}
      />
    );
  }

  if (normalizedRuntimeId === "codex") {
    return (
      <Codex aria-hidden className={cn("shrink-0", className)} size={size} />
    );
  }

  if (normalizedRuntimeId === "kimi") {
    return (
      <Kimi aria-hidden className={cn("shrink-0", className)} size={size} />
    );
  }
  if (normalizedRuntimeId === "cursor") {
    return (
      <Cursor aria-hidden className={cn("shrink-0", className)} size={size} />
    );
  }
  if (normalizedRuntimeId === "gemini") {
    return (
      <GeminiCLI
        aria-hidden
        className={cn("shrink-0", className)}
        size={size}
      />
    );
  }
  if (normalizedRuntimeId === "opencode") {
    return (
      <OpenCode aria-hidden className={cn("shrink-0", className)} size={size} />
    );
  }
  if (normalizedRuntimeId === "grok") {
    return (
      <Grok aria-hidden className={cn("shrink-0", className)} size={size} />
    );
  }
  if (normalizedRuntimeId === "pi") {
    return (
      // Pi Coding Agent 官方标识，来源：https://pi.dev/favicon.svg。
      <svg
        aria-hidden
        className={cn("shrink-0", className)}
        width={size}
        height={size}
        viewBox="0 0 800 800"
        fill="currentColor"
      >
        <path
          fillRule="evenodd"
          d="M165.29 165.29H517.36V400H400V517.36H282.65V634.72H165.29ZM282.65 282.65V400H400V282.65Z"
        />
        <path d="M517.36 400H634.72V634.72H517.36Z" />
      </svg>
    );
  }

  return (
    <Bot
      aria-hidden
      className={cn("size-icon-md shrink-0", className)}
      strokeWidth={1.5}
    />
  );
}
