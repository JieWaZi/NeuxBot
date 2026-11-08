import type { ReactNode } from "react";
import { cn } from "@/shared/lib/cn";

export type StatusPillTone =
  "connected" | "attention" | "connecting" | "idle" | "unavailable";

const tones: Record<StatusPillTone, string> = {
  connected:
    "border-transparent bg-status-success-surface text-status-success-content",
  attention:
    "border-transparent bg-status-warning-surface text-status-warning-content",
  connecting: "border-transparent bg-resource-surface text-resource-secondary",
  idle: "border-transparent bg-resource-surface text-resource-secondary",
  unavailable: "border-resource-border text-resource-muted",
};

/** 集成状态统一使用语义浅底和状态点；程序可用与认证成功分别由领域决定。 */
export function StatusPill({
  tone,
  children,
}: {
  tone: StatusPillTone;
  children: ReactNode;
}) {
  return (
    <span
      data-status={tone}
      className={cn(
        "type-ui inline-flex w-fit shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 font-medium whitespace-nowrap",
        tones[tone],
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          tone === "unavailable" ? "border border-current" : "bg-current",
        )}
      />
      {children}
    </span>
  );
}
