import {
  MessageCircleQuestionMark,
  ShieldCheck,
  FileCheck2,
  CircleAlert,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";

export type ConversationMark =
  | { kind: "action"; request?: "input" | "approval" | "review" }
  | { kind: "working" }
  | { kind: "failed" }
  | { kind: "unread"; count: number };

export function ConversationAttentionMark({
  mark,
  className,
  unreadDot,
  variant = "inline",
}: {
  mark?: ConversationMark;
  className?: string;
  unreadDot?: boolean;
  variant?: "inline" | "avatar";
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
  const waitingForAnswer = mark.kind === "action" && mark.request === "input";
  if (mark.kind === "unread" && unreadDot)
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
