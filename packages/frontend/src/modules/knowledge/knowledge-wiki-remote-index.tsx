import { useQuery } from "@tanstack/react-query";
import type { ComponentProps, ReactNode } from "react";
import type { GithubComJieWaZiNeuxBotServerApisKnowledgeWikiIndexGroup as IndexGroup } from "@/shared/api/generated/types.gen";
import type { StaticMarkdown } from "@/shared/ui/static-markdown";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import type { ResourceRef } from "./knowledge-transfer";
import { KnowledgeWikiIndexContent } from "./knowledge-wiki-index";
import { wikiPageOptions, wikiPagesOptions } from "./knowledge-wiki-queries";

/** 公开远端协议未提供结构化索引；仅用同库完整分页目录和索引正文组装阅读视图。 */
export function KnowledgeWikiRemoteIndex({
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
  const pages = useQuery({
    ...wikiPagesOptions(resource),
    enabled: active,
    refetchOnMount: false,
  });
  const index = pages.data?.find((entry) => entry.pageType === "index");
  const page = useQuery({
    ...wikiPageOptions(resource, index?.id ?? "", index?.slug),
    enabled: active && Boolean(index?.id),
    refetchOnMount: false,
  });
  const error = pages.error || page.error;
  const pending = pages.isPending || (Boolean(index?.id) && page.isPending);
  const hasData = Boolean(pages.data && (!index?.id || page.data));
  const feedback = (
    <KnowledgeReadFeedback
      hasData={hasData}
      state={{
        pending,
        error,
        retry: () => {
          if (pages.error) void pages.refetch();
          if (page.error) void page.refetch();
        },
      }}
      loading={<KnowledgeWikiContentLoading />}
    />
  );
  if (!hasData) return feedback;
  const groups = new Map<string, IndexGroup>();
  const groupOrder = [
    "summary",
    "entity",
    "concept",
    "synthesis",
    "comparison",
  ];
  for (const entry of pages.data ?? []) {
    if (entry.pageType === "index" || !entry.slug) continue;
    const kind = entry.pageType || "concept";
    let group = groups.get(kind);
    if (!group) {
      group = { type: kind, total: 0, items: [] };
      groups.set(kind, group);
    }
    group.items?.push({
      slug: entry.slug,
      title: entry.title,
      summary: entry.summary,
      categoryPath: entry.categoryPath,
      sortOrder: entry.sortOrder,
    });
    group.total = (group.total ?? 0) + 1;
  }
  // 目录展示只输出一次目录标签，先聚拢路径再应用同目录业务顺序，避免交错页归属错误。
  for (const group of groups.values())
    group.items?.sort((left, right) => {
      const a = left.categoryPath ?? [],
        b = right.categoryPath ?? [];
      for (let i = 0; i < Math.min(a.length, b.length); i++) {
        const difference = (a[i] ?? "").localeCompare(b[i] ?? "");
        if (difference) return difference;
      }
      return (
        a.length - b.length || (left.sortOrder ?? 0) - (right.sortOrder ?? 0)
      );
    });
  return (
    <KnowledgeWikiIndexContent
      resource={resource}
      data={{
        intro: page.data?.content?.trim()
          ? page.data.content
          : (page.data?.summary ?? ""),
        version: page.data?.version,
        groups: [...groups.values()].sort((left, right) => {
          const rank = (kind: string | undefined) => {
            const position = groupOrder.indexOf(kind ?? "");
            return position < 0 ? groupOrder.length : position;
          };
          return (
            rank(left.type) - rank(right.type) ||
            (left.type ?? "").localeCompare(right.type ?? "")
          );
        }),
      }}
      refreshedAt={Math.max(pages.dataUpdatedAt, page.dataUpdatedAt)}
      complete
      actions={actions}
      onLink={onLink}
      remarkPlugins={remarkPlugins}
      feedback={feedback}
    />
  );
}
