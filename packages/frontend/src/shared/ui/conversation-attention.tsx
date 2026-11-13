import {
  MessageCircleQuestionMark,
  ShieldCheck,
  FileCheck2,
  CircleAlert,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { drawAttention } from "@neuxbot/bloub/attention-drawing";
import type { CompanionAttention } from "@/shared/companion/activity";
import { renderDrawing } from "@/shared/companion/render-drawing";
import { cn } from "@/shared/lib/cn";

export type ConversationMark =
  | { kind: "action"; request?: "input" | "approval" | "review" }
  | { kind: "working" }
  | { kind: "failed" }
  | { kind: "unread"; count: number };

export function companionAttention(
  mark?: ConversationMark,
): CompanionAttention | undefined {
  if (mark?.kind === "working" || mark?.kind === "unread") return mark;
  if (mark?.kind !== "action") return;
  if (mark.request === "input") return { kind: "waiting-answer" };
  if (mark.request === "approval") return { kind: "waiting-approval" };
}

export function ConversationAttentionMark({
  mark,
  className,
  variant = "inline",
}: {
  mark?: ConversationMark;
  className?: string;
  variant?: "inline" | "avatar" | "tab";
}) {
  const { t } = useTranslation("common");
  if (!mark) return null;
  const avatar = variant === "avatar";
  const avatarEdge = "motion-safe:animate-fade-in ring-surface-raised ring-2";
  const label =
    mark.kind === "action"
      ? t(
          mark.request
            ? `partnerNavigation.request.${mark.request}`
            : "partnerNavigation.needsAction",
        )
      : mark.kind === "working"
        ? t("partnerNavigation.working")
        : mark.kind === "failed"
          ? t("partnerNavigation.recentFailed")
          : t("partnerNavigation.unreadMessages", { count: mark.count });
  if (mark.kind === "unread" && variant === "tab")
    return (
      <span
        role="img"
        aria-label={label}
        title={label}
        data-conversation-attention={mark.kind}
        className={cn(
          "bg-accent-secondary size-1.5 shrink-0 rounded-full",
          className,
        )}
      />
    );
  const attention = variant === "tab" ? companionAttention(mark) : undefined;
  if (attention && attention.kind !== "unread") {
    const { kind } = attention;
    const drawing = drawAttention(
      { x: 0, y: 0, r: 1, attention: { kind, progress: 1, phase: 0 } },
      null,
      0,
      "currentColor",
    );
    return (
      <svg
        aria-label={label}
        role="img"
        data-conversation-attention={mark.kind}
        viewBox="-4.25 -6.25 9 9"
        className={cn(
          "size-icon-sm shrink-0",
          kind === "waiting-approval" && "text-status-warning-content",
          kind === "waiting-answer" && "text-accent-content",
          kind === "working" &&
            "text-accent-secondary motion-safe:[&_circle]:animate-avatar-attention-wave [&_circle:nth-child(2)]:[animation-delay:130ms] [&_circle:nth-child(3)]:[animation-delay:260ms]",
          className,
        )}
      >
        <title>{label}</title>
        {drawing && renderDrawing(drawing.mark, 0)}
      </svg>
    );
  }
  const waitingForAnswer = mark.kind === "action" && mark.request === "input";
  const Icon =
    mark.kind === "failed"
      ? X
      : mark.kind === "action"
        ? mark.request
          ? {
              input: MessageCircleQuestionMark,
              approval: ShieldCheck,
              review: FileCheck2,
            }[mark.request]
          : CircleAlert
        : undefined;
  return (
    <span
      key={
        avatar ? (mark.kind === "action" ? mark.request : mark.kind) : undefined
      }
      className={cn(
        "type-caption inline-flex shrink-0 items-center justify-center rounded-full font-medium tabular-nums",
        avatar
          ? cn(
              mark.kind === "working" ? "h-2.5 px-0.5" : "h-3.5 min-w-3.5 px-1",
              avatarEdge,
            )
          : "h-4 min-w-4 px-1",
        mark.kind === "action" &&
          (waitingForAnswer
            ? "bg-accent-surface-strong text-accent-content"
            : avatar
              ? "bg-status-warning-surface text-status-warning-content"
              : "bg-warning text-warning-foreground"),
        mark.kind === "working" &&
          (avatar
            ? "bg-surface-raised text-accent-secondary"
            : "bg-surface-elevated text-content-secondary"),
        mark.kind === "failed" && "bg-destructive text-destructive-foreground",
        mark.kind === "unread" &&
          "bg-accent-secondary-strong text-primary-foreground",
        className,
      )}
      data-conversation-attention={mark.kind}
      aria-label={label}
      role="img"
      title={label}
    >
      {mark.kind === "unread" ? (
        mark.count
      ) : Icon ? (
        <Icon
          className={avatar ? "size-2.5" : "size-icon-xs"}
          aria-hidden="true"
        />
      ) : (
        <span
          className={cn(
            "inline-flex [&>i]:rounded-full [&>i]:bg-current",
            avatar
              ? "motion-safe:[&>i]:animate-avatar-attention-wave gap-0.25 [&>i]:size-0.75"
              : "gap-0.5 [&>i]:size-1 motion-safe:[&>i]:animate-pulse",
          )}
          aria-hidden="true"
        >
          <i />
          <i className="[animation-delay:130ms]" />
          <i className="[animation-delay:260ms]" />
        </span>
      )}
    </span>
  );
}

export function conversationAttentionMark({
  requestKinds,
  waiting = false,
  running = false,
  failed = false,
  unread = 0,
}: {
  requestKinds?: readonly string[];
  waiting?: boolean;
  running?: boolean;
  failed?: boolean;
  unread?: number;
}): ConversationMark | undefined {
  const request = requestKinds?.includes("input")
    ? "input"
    : requestKinds?.includes("approval")
      ? "approval"
      : requestKinds?.includes("review")
        ? "review"
        : undefined;
  if (request || waiting) return { kind: "action", request };
  if (running) return { kind: "working" };
  if (failed) return { kind: "failed" };
  if (unread > 0) return { kind: "unread", count: unread };
}
