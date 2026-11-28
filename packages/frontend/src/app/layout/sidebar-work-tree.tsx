import { ChevronDown } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";
import { useWorkTicketActivity } from "@/modules/chat/api/work-ticket-activity";
import { cn } from "@/shared/lib/cn";
import { mainConversationHref } from "@/shared/lib/main-conversation-navigation";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";

export function SidebarWorkTree({
  kind,
  id,
  search = "",
  searchScopeKeys,
  sourceName,
}: {
  kind: "waker" | "team";
  id: string;
  search?: string;
  searchScopeKeys?: ReadonlySet<string>;
  sourceName?: (key: string) => string | undefined;
}) {
  const { t } = useTranslation("common");
  const { pathname, search: routeSearch } = useLocation();
  const { data = [] } = useWorkTicketActivity(kind, id);
  const [opened, setOpened] = useState(false);
  const needle = search.trim().toLocaleLowerCase();
  const rows = data.filter((row) => {
    if (!needle) return true;
    const sourceKey = `${row.sourceSubjectType}:${row.sourceSubjectId}`;
    return (
      (!row.received || !searchScopeKeys?.has(sourceKey)) &&
      Boolean(
        row.title?.toLocaleLowerCase().includes(needle) ||
        sourceName?.(sourceKey)?.toLocaleLowerCase().includes(needle),
      )
    );
  });
  if (!rows.length) return null;
  const present = new Map(rows.map((row) => [row.ticketId, row]));
  const rootRows = rows.filter(
    (row) => !row.parentTicketId || !present.has(row.parentTicketId),
  );
  const ordered: { row: (typeof rows)[number]; level: number }[] = [];
  const visited = new Set<string>();
  const append = (row: (typeof rows)[number], level: number) => {
    if (!row.ticketId || visited.has(row.ticketId)) return;
    visited.add(row.ticketId);
    ordered.push({ row, level });
    for (const child of rows) {
      if (child.parentTicketId === row.ticketId) append(child, level + 1);
    }
  };
  for (const row of rootRows) append(row, 0);
  for (const row of rows) append(row, 0);
  return (
    <div className="min-w-0 pr-1 pl-4">
      {!needle ? (
        <button
          type="button"
          aria-expanded={opened}
          onClick={() => setOpened(!opened)}
          className="focus-visible:ring-ring text-content-muted type-caption hover:bg-surface-subtle flex w-full items-center gap-1 rounded-sm px-2 py-1 text-left focus-visible:ring-2"
        >
          <ChevronDown
            aria-hidden="true"
            strokeWidth={1.5}
            className={cn(
              "size-icon-sm duration-quick ease-ui transition-transform motion-reduce:transition-none",
              !opened && "-rotate-90",
            )}
          />
          {t("partnerNavigation.sidebarWork")}
        </button>
      ) : null}
      {opened || needle ? (
        <ul className="space-y-0.5">
          {ordered.map(({ row, level }) => {
            const owner = row.workSubjectType;
            const source = row.sourceSubjectType;
            const href =
              row.workChatId &&
              row.workSubjectId &&
              (owner === "waker" || owner === "team")
                ? mainConversationHref({
                    kind: owner,
                    id: row.workSubjectId,
                    workChatId: row.workChatId,
                  })
                : !row.received &&
                    row.sourceMessageId &&
                    row.sourceSubjectId &&
                    (source === "waker" || source === "team")
                  ? mainConversationHref({
                      kind: source,
                      id: row.sourceSubjectId,
                      messageId: row.sourceMessageId,
                    })
                  : undefined;
            const rowClass =
              "type-caption focus-visible:ring-ring min-h-row-compact grid min-w-0 grid-cols-[var(--spacing-icon-sm)_minmax(0,1fr)_auto] items-center gap-1.5 rounded-sm px-2 focus-visible:ring-2";
            const label = row.title || t("chatNavigation.unnamedChat");
            const state = row.state ?? "pending";
            const content = (
              <>
                <RuntimeCliIcon
                  runtimeId={row.runtimeId}
                  size={14}
                  className="size-icon-sm"
                />
                <span className="truncate" title={label}>
                  {label}
                  {row.received ? (
                    <span className="text-content-muted ml-1">
                      {t("partnerNavigation.sidebarReceived", {
                        name:
                          sourceName?.(`${source}:${row.sourceSubjectId}`) ||
                          t("partnerNavigation.sidebarUnknownSource"),
                      })}
                    </span>
                  ) : null}
                </span>
                <span className="text-content-muted whitespace-nowrap">
                  {t(`partnerNavigation.sidebarWorkStates.${state}`, {
                    defaultValue: state,
                  })}
                </span>
              </>
            );
            return (
              <li
                key={row.ticketId}
                style={{ paddingInlineStart: Math.min(level, 4) * 8 }}
              >
                {href ? (
                  <Link
                    to={href}
                    aria-current={
                      href === pathname + routeSearch ? "page" : undefined
                    }
                    className={cn(
                      rowClass,
                      "hover:bg-surface-subtle",
                      href === pathname + routeSearch && "bg-surface-panel",
                    )}
                  >
                    {content}
                  </Link>
                ) : (
                  <span className={rowClass}>{content}</span>
                )}
              </li>
            );
          })}
        </ul>
      ) : null}
    </div>
  );
}
