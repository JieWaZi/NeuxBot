import {
  useId,
  type Dispatch,
  type SetStateAction,
  type ReactNode,
} from "react";
import { Trans, useTranslation } from "react-i18next";

import { TeamPartnerButton } from "./team-partner-button";
import { cn } from "@/shared/lib/cn";
import type { ChatMessageAuthor } from "./chat-thread";
import type { ChatMessageView } from "./chat-message-nodes";

export type TeamReportGroupRow = {
  kind: "team-reports";
  key: string;
  members: Array<{
    id: string;
    author: ChatMessageAuthor;
    views: ChatMessageView[];
  }>;
};

/** 只归组已加载的非直达报告；用户输入与负责人回复划定边界，不修改权威消息。 */
export function groupTeamReports(
  views: readonly ChatMessageView[],
  authors?: Readonly<Record<string, ChatMessageAuthor>>,
): Array<ChatMessageView | TeamReportGroupRow> {
  const rows: Array<ChatMessageView | TeamReportGroupRow> = [];
  let pending: TeamReportGroupRow | undefined;
  const flush = () => {
    if (pending) rows.push(pending);
    pending = undefined;
  };
  for (const view of views) {
    const author = authors?.[view.message.id ?? ""];
    if (author?.kind === "member-report") {
      pending ??= {
        kind: "team-reports",
        key: `reports:${view.key}`,
        members: [],
      };
      const id = author.wakerId || author.sourceChatId || view.key;
      const member = pending.members.find((entry) => entry.id === id);
      if (member) member.views.push(view);
      else pending.members.push({ id, author, views: [view] });
      continue;
    }
    if (!view.message.question && !author?.displayAsParticipant) flush();
    rows.push(view);
  }
  flush();
  return rows;
}

/** 每个伙伴独立控制本组消息；展开内容仍由原聊天消息渲染器负责。 */
export function TeamReportGroup({
  group,
  renderMessage,
  expanded,
  setExpanded,
}: {
  group: TeamReportGroupRow;
  renderMessage: (view: ChatMessageView) => ReactNode;
  expanded: ReadonlySet<string>;
  setExpanded: Dispatch<SetStateAction<ReadonlySet<string>>>;
}) {
  const { t } = useTranslation("chat");
  const panelId = useId();
  return (
    <div className="min-w-0">
      <div className="text-content-secondary type-ui my-3 flex min-w-0 flex-wrap items-center justify-center gap-1 px-6">
        <Trans
          t={t}
          i18nKey="team.receivedMessages"
          components={{
            partners: (
              <TeamReportPartners
                members={group.members}
                expanded={expanded}
                setExpanded={setExpanded}
                panelId={panelId}
              />
            ),
          }}
        />
      </div>
      {group.members.map((member, index) => (
        <div
          key={member.id}
          id={`${panelId}-${index}`}
          hidden={!member.views.some((view) => expanded.has(view.key))}
        >
          {member.views.some((view) => expanded.has(view.key))
            ? member.views.map(renderMessage)
            : null}
        </div>
      ))}
    </div>
  );
}

function TeamReportPartners({
  members,
  expanded,
  setExpanded,
  panelId,
}: {
  members: TeamReportGroupRow["members"];
  expanded: ReadonlySet<string>;
  setExpanded: Dispatch<SetStateAction<ReadonlySet<string>>>;
  panelId: string;
}) {
  const { t } = useTranslation("chat");
  return (
    <span className="inline-flex max-w-full min-w-0 flex-wrap items-center justify-center gap-1">
      {members.map((member, index) => {
        const open = member.views.some((view) => expanded.has(view.key));
        return (
          <TeamPartnerButton
            key={member.id}
            name={member.author.name}
            avatar={member.author.avatar}
            className={cn("max-w-full min-w-0", open && "bg-surface-panel")}
            aria-label={t(
              open ? "team.collapseMessages" : "team.expandMessages",
              { name: member.author.name },
            )}
            aria-expanded={open}
            aria-controls={`${panelId}-${index}`}
            onClick={() =>
              setExpanded((current) => {
                const next = new Set(current);
                for (const view of member.views) {
                  if (open) next.delete(view.key);
                  else next.add(view.key);
                }
                return next;
              })
            }
          />
        );
      })}
    </span>
  );
}
