import { knowledgeAdapter } from "@/shared/api/knowledge";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { defaultRemarkPlugins } from "streamdown";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { Card, CardContent } from "@/shared/ui/card";
import { DetailDockScrollArea } from "@/shared/ui/detail-dock-layout";
import { StaticMarkdown } from "@/shared/ui/static-markdown";
import { IconAction } from "./knowledge-controls";
import type { KnowledgeGraphNode } from "./knowledge-graph-model";
import { graphPageOptions } from "./knowledge-graph-queries";
import { KnowledgeWikiStructuredIndex } from "./knowledge-wiki-index";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import type { ResourceRef } from "./knowledge-transfer";
import {
  remarkWikiLinks,
  stripDuplicateWikiTitle,
  normalizeWikiIndexIntro,
} from "./knowledge-wiki-markdown";
import { graphPageType, useGraphCopy } from "./use-graph-copy";

const plugins = [...Object.values(defaultRemarkPlugins), remarkWikiLinks];

/** 单击只读取当前节点正文；展开与叠加分别保持上游两种图谱操作的含义。 */
export function KnowledgeGraphReader({
  resource,
  node,
  center,
  visibleNeighbors,
  close,
  select,
  expand,
  bloom,
  busy,
  visible,
}: {
  resource: ResourceRef;
  node: KnowledgeGraphNode;
  center: string;
  visibleNeighbors: number;
  close: () => void;
  select: (slug: string) => void;
  expand: () => void;
  bloom: () => void;
  busy: boolean;
  visible: boolean;
}) {
  const t = useGraphCopy();
  const active = useRouteSceneActive();
  const page = useQuery({
    ...graphPageOptions(resource, node.slug, node.pageId),
    enabled: active && visible,
  });
  const isIndex =
    (page.data?.pageType || node.page_type) === "index" ||
    node.slug === "index";
  const title = isIndex ? t("index") : page.data?.title || node.title;
  const total = node.link_count;
  const hidden = Math.max(0, total - visibleNeighbors);
  const atCenter = center === node.slug;
  const fullyExplored = !total || !hidden || atCenter;
  function follow(href: string) {
    if (href.startsWith("#")) return;
    try {
      const slug = decodeURIComponent(href.split("#")[0] ?? "")
        .replace(/^\.\//, "")
        .replace(/^\//, "")
        .replace(/\.md$/, "");
      if (slug) select(slug);
    } catch {
      /* 无效地址不改变当前阅读身份。 */
    }
  }
  return (
    <Card
      variant="floating"
      className="h-full"
      role="region"
      aria-label={t("selected")}
    >
      <CardContent className="flex min-h-0 flex-1 flex-col">
        <div className="border-border-subtle flex items-start justify-between gap-3 border-b pb-4">
          <h3 className="type-title min-w-0 wrap-anywhere">{title}</h3>
          <IconAction icon={X} label={t("close")} onClick={close} />
        </div>
        <DetailDockScrollArea className="mt-4 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">
              {t(graphPageType(page.data?.pageType || node.page_type))}
            </Badge>
            {page.data?.version !== undefined && (
              <span className="type-caption text-content-muted">
                v{page.data.version}
              </span>
            )}
            <div className="ml-auto flex flex-wrap justify-end gap-2">
              {center && !atCenter && (
                <Button
                  variant="accent"
                  size="sm"
                  onClick={bloom}
                  disabled={busy || fullyExplored}
                >
                  {t("bloom")}
                </Button>
              )}
              {!atCenter && (
                <Button
                  variant="accent"
                  size="sm"
                  onClick={expand}
                  disabled={busy}
                >
                  {t("expand")}
                </Button>
              )}
            </div>
          </div>
          <p className="type-caption text-content-muted mt-3 mb-5">
            {total < 0
              ? t("loadingLinks")
              : !total
                ? t("isolated")
                : !hidden
                  ? t("neighborsAll", { total })
                  : t(atCenter ? "neighborsFiltered" : "neighborsHidden", {
                      visible: visibleNeighbors,
                      total,
                      hidden,
                    })}
          </p>
          <KnowledgeReadFeedback
            hasData={Boolean(page.data)}
            state={{
              pending: page.isPending,
              error: page.error,
              retry: () => void page.refetch(),
              refreshing: page.isFetching && !page.isPending,
            }}
            loading={<KnowledgeWikiContentLoading />}
          />
          {page.data && (
            <article aria-label={title}>
              {knowledgeAdapter(resource).localConfiguration && isIndex ? (
                <KnowledgeWikiStructuredIndex
                  resource={resource}
                  active={active && visible}
                  onLink={follow}
                  remarkPlugins={plugins}
                />
              ) : (
                <StaticMarkdown
                  source={
                    isIndex
                      ? normalizeWikiIndexIntro(page.data.content ?? "")
                      : stripDuplicateWikiTitle(
                          page.data.content ?? "",
                          page.data.title || node.title,
                        ) || t("noContent")
                  }
                  onLink={follow}
                  remarkPlugins={plugins}
                />
              )}
            </article>
          )}
        </DetailDockScrollArea>
      </CardContent>
    </Card>
  );
}
