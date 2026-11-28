import { ArrowUpRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { mainConversationHref } from "@/shared/lib/main-conversation-navigation";

export type DelegationActivity = {
  ticketId?: string;
  parentTicketId?: string;
  sourceMessageId?: string;
  received?: boolean;
  title?: string;
  executorWakerId?: string;
  executorName?: string;
  runtimeId?: string;
  state?: string;
  workChatId?: string;
  workSubjectType?: string;
  workSubjectId?: string;
};

export function delegatedWorkForMessage(
  items: readonly DelegationActivity[],
  messageId: string | undefined,
) {
  if (!messageId) return [];
  return items.filter(
    (item) =>
      !item.received &&
      item.parentTicketId &&
      item.sourceMessageId === messageId,
  );
}

function stateLabel(state?: string) {
  switch (state) {
    case "active":
    case "running":
      return "delegationRunning";
    case "waiting":
      return "delegationWaiting";
    case "waiting-approval":
    case "blocked":
      return "delegationBlocked";
    case "completed":
    case "succeeded":
      return "delegationCompleted";
    case "failed":
      return "delegationFailed";
    case "stopped":
    case "canceled":
    case "cancelled":
      return "delegationStopped";
    default:
      return "delegationQueued";
  }
}

export function MainConversationDelegations({
  entries,
  executorNames,
}: {
  entries: readonly DelegationActivity[];
  executorNames?: ReadonlyMap<string, string>;
}) {
  const { t } = useTranslation("chat");
  if (!entries.length) return null;
  return (
    <section aria-label={t("delegatedWork")} className="min-w-0 space-y-1 px-6">
      {entries.map((item) => {
        const executor =
          item.executorName || executorNames?.get(item.executorWakerId ?? "");
        const kind = item.workSubjectType;
        const href =
          item.workChatId &&
          item.workSubjectId &&
          (kind === "waker" || kind === "team")
            ? mainConversationHref({
                kind,
                id: item.workSubjectId,
                workChatId: item.workChatId,
              })
            : undefined;
        const className =
          "flex min-w-0 items-center gap-2 rounded-md bg-muted/40 px-3 py-2 text-sm";
        const content = (
          <>
            {item.runtimeId && (
              <RuntimeCliIcon runtimeId={item.runtimeId} size={16} />
            )}
            <span className="min-w-0 flex-1 truncate">
              {item.title || t("workSummary.untitled")}
            </span>
            {executor && (
              <span className="text-muted-foreground max-w-24 truncate text-xs">
                {executor}
              </span>
            )}
            <span
              className="text-muted-foreground shrink-0 text-xs"
              aria-live="polite"
            >
              {t(stateLabel(item.state))}
            </span>
            {href && <ArrowUpRight aria-hidden className="size-3.5 shrink-0" />}
          </>
        );
        return href ? (
          <Link key={item.ticketId} className={className} to={href}>
            {content}
          </Link>
        ) : (
          <div key={item.ticketId} className={className}>
            {content}
          </div>
        );
      })}
    </section>
  );
}
