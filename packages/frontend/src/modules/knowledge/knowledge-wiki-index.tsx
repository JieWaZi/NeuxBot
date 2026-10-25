import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { useState, type ComponentProps, type ReactNode } from "react";
import type {
  GithubComJieWaZiNeuxBotServerApisKnowledgeWikiIndexView as IndexView,
  GithubComJieWaZiNeuxBotServerApisKnowledgeWikiIndexGroup as IndexGroup,
} from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { StaticMarkdown } from "@/shared/ui/static-markdown";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import type { ResourceRef } from "./knowledge-transfer";
import { readWikiIndex, wikiIndexOptions } from "./knowledge-wiki-queries";
import { useWikiCopy } from "./use-wiki-copy";
import {
  remarkWikiIndexBreaks,
  normalizeWikiIndexIntro,
  stripDuplicateWikiTitle,
} from "./knowledge-wiki-markdown";

/** 复用上游结构化索引：导言只渲染一次，分组目录来自全库轻量分页。 */
export function KnowledgeWikiStructuredIndex({
  resource,
  active,
  onLink,
  remarkPlugins,
  actions,
}: {
  resource: ResourceRef;
  active: boolean;
  onLink: (slug: string) => void;
  remarkPlugins: ComponentProps<typeof StaticMarkdown>["remarkPlugins"];
  actions?: ReactNode;
}) {
  const query = useQuery({ ...wikiIndexOptions(resource), enabled: active });
  if (query.isPending) return <KnowledgeWikiContentLoading />;
  if (query.error && !query.data)
    return (
      <KnowledgeReadFeedback
        state={{
          pending: false,
          error: query.error,
          retry: () => void query.refetch(),
        }}
      />
    );
  if (!query.data) return null;
  return (
    <KnowledgeWikiIndexContent
      resource={resource}
      data={query.data}
      refreshedAt={query.dataUpdatedAt}
      onLink={onLink}
      remarkPlugins={remarkPlugins}
      actions={actions}
      feedback={
        query.error ? (
          <KnowledgeReadFeedback
            hasData
            state={{
              pending: false,
              error: query.error,
              retry: () => {
                void query.refetch();
              },
            }}
          />
        ) : undefined
      }
    />
  );
}

/** 本机索引与远端已读目录共享标题、导言和分组展示。 */
export function KnowledgeWikiIndexContent({
  resource,
  data,
  refreshedAt,
  onLink,
  remarkPlugins,
  actions,
  feedback,
  complete = false,
}: {
  resource: ResourceRef;
  data: IndexView;
  refreshedAt: number;
  onLink: (slug: string) => void;
  remarkPlugins: ComponentProps<typeof StaticMarkdown>["remarkPlugins"];
  actions?: ReactNode;
  feedback?: ReactNode;
  complete?: boolean;
}) {
  const t = useWikiCopy();
  const indexRemarkPlugins = [...(remarkPlugins ?? []), remarkWikiIndexBreaks];
  const groups = (data.groups ?? []).filter((group) => (group.total ?? 0) > 0);
  return (
    <div className="space-y-6">
      {feedback}
      <header className="flex min-w-0 items-center gap-3">
        <h1 className="type-conversation-title min-w-0 flex-1 truncate font-semibold">
          {t("indexTitle")}
        </h1>
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      </header>
      <StaticMarkdown
        source={stripDuplicateWikiTitle(
          normalizeWikiIndexIntro(data.intro ?? ""),
          t("indexTitle"),
        )}
        remarkPlugins={indexRemarkPlugins}
        onLink={onLink}
      />
      {groups.map((group) => (
        <KnowledgeWikiIndexGroup
          key={`${group.type}:${refreshedAt}`}
          resource={resource}
          group={group}
          complete={complete}
          refreshedAt={refreshedAt}
          onLink={onLink}
          remarkPlugins={indexRemarkPlugins}
        />
      ))}
      {!groups.length && !data.intro && (
        <EmptyState title={t("empty")} description={t("emptyHelp")} />
      )}
    </div>
  );
}

function KnowledgeWikiIndexGroup({
  resource,
  group,
  complete,
  refreshedAt,
  onLink,
  remarkPlugins,
}: {
  resource: ResourceRef;
  group: IndexGroup;
  complete: boolean;
  refreshedAt: number;
  onLink: (slug: string) => void;
  remarkPlugins: ComponentProps<typeof StaticMarkdown>["remarkPlugins"];
}) {
  const t = useWikiCopy();
  // 新首屏读取后重置该组追加页，避免把更新前后的目录窗口拼成一份假快照。
  const [visibleCount, setVisibleCount] = useState(50);
  const options = wikiIndexOptions(resource, group.type);
  const more = useInfiniteQuery({
    queryKey: [...options.queryKey, "more", refreshedAt],
    initialPageParam: group.nextCursor ?? "",
    queryFn: ({ signal, pageParam }) =>
      readWikiIndex(resource, group.type, pageParam, signal),
    getNextPageParam: (last) => last.groups?.[0]?.nextCursor || undefined,
    enabled: false,
  });
  const rows = [
    ...(group.items ?? []),
    ...(more.data?.pages.flatMap((page) => page.groups?.[0]?.items ?? []) ??
      []),
  ];
  const visibleRows = complete ? rows.slice(0, visibleCount) : rows;
  const next = complete
    ? visibleCount < rows.length
    : more.data
      ? more.hasNextPage
      : Boolean(group.nextCursor);
  const label =
    group.type === "summary"
      ? t("summaryType")
      : group.type === "entity"
        ? t("entity")
        : group.type === "concept"
          ? t("concept")
          : group.type === "synthesis"
            ? t("synthesis")
            : group.type === "comparison"
              ? t("comparison")
              : group.type === "topic"
                ? t("topic")
                : t("page");
  return (
    <section className="space-y-3" aria-label={label}>
      <StaticMarkdown
        source={`## ${label} (${group.total ?? 0})\n\n${appendIndexDirectoryLines(visibleRows)}`}
        remarkPlugins={remarkPlugins}
        onLink={onLink}
      />
      {more.error && (
        <KnowledgeReadFeedback
          hasData
          state={{
            pending: false,
            error: more.error,
            retry: () => {
              void more.fetchNextPage();
            },
          }}
        />
      )}
      {next && (
        <div className="flex justify-center">
          <Button
            type="button"
            variant="text"
            disabled={more.isFetching || more.isError}
            onClick={() => {
              if (complete) setVisibleCount((count) => count + 50);
              else void more.fetchNextPage();
            }}
          >
            <ChevronDown aria-hidden="true" />
            {t("loadMore")}
          </Button>
        </div>
      )}
    </section>
  );
}

/** 迁自WeKnora固定8776 WikiBrowser.vue appendIndexDirectoryLines，仅DTO字段改为camelCase。 */
function appendIndexDirectoryLines(
  items: NonNullable<IndexGroup["items"]>,
): string {
  let out = "";
  const emittedDirs = new Set<string>();
  for (const entry of items) {
    const path = (Array.isArray(entry.categoryPath) ? entry.categoryPath : [])
      .map((part) => String(part || "").trim())
      .filter(Boolean);
    for (let i = 0; i < path.length; i++) {
      const parts = path.slice(0, i + 1);
      const key = parts.join("/");
      if (emittedDirs.has(key)) continue;
      emittedDirs.add(key);
      out += `${"  ".repeat(i)}**${path[i]}**\n`;
    }
    const display = entry.title || entry.slug;
    const indent = "  ".repeat(path.length);
    if (entry.summary) {
      out += `${indent}[[${entry.slug}|${display}]] — ${entry.summary}\n`;
    } else {
      out += `${indent}[[${entry.slug}|${display}]]\n`;
    }
  }
  return out;
}
