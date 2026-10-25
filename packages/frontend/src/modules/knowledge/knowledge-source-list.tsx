import { FileTreeRow } from "@/shared/ui/file-tree-row";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import { useEffect, useRef } from "react";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import type { Source } from "./knowledge-model";
import { useDocumentCopy } from "./use-document-copy";

/** 原件目录只展示文件图标与名称，分页仍读取服务端真实结果。 */
export function KnowledgeSourceList({
  rows,
  selectedId,
  onOpen,
  hasMore = false,
  pending = false,
  failed = false,
  onLoadMore,
}: {
  rows: Source[];
  selectedId?: string;
  onOpen: (source: Source) => void;
  hasMore?: boolean;
  pending?: boolean;
  failed?: boolean;
  onLoadMore: () => void;
}) {
  const t = useDocumentCopy();
  const active = useRouteSceneActive();
  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (
      !active ||
      !hasMore ||
      pending ||
      failed ||
      typeof IntersectionObserver === "undefined" ||
      !sentinelRef.current
    )
      return;
    let requested = false;
    let disposed = false;
    const observer = new IntersectionObserver(
      (entries) => {
        if (
          !disposed &&
          !requested &&
          entries.some((entry) => entry.isIntersecting)
        ) {
          requested = true;
          onLoadMore();
        }
      },
      { root: scrollRef.current, rootMargin: "0px 0px 120px 0px" },
    );
    observer.observe(sentinelRef.current);
    return () => {
      disposed = true;
      observer.disconnect();
    };
  }, [active, hasMore, pending, failed, onLoadMore]);
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3">
      <div
        ref={scrollRef}
        onScroll={(event) => {
          const pane = event.currentTarget;
          if (
            active &&
            hasMore &&
            !pending &&
            !failed &&
            pane.scrollHeight - pane.scrollTop - pane.clientHeight < 120
          )
            onLoadMore();
        }}
        role="tree"
        aria-label={t("sourceDirectory")}
        className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto"
      >
        {rows.map((source) => (
          <FileTreeRow
            key={source.id}
            className="w-full"
            selected={selectedId === source.id}
            aria-label={source.name}
            title={source.name}
            onClick={() => onOpen(source)}
            icon={
              <WorkspaceFileTypeIcon isDirectory={false} name={source.name} />
            }
          >
            <span className="min-w-0 truncate">{source.name}</span>
          </FileTreeRow>
        ))}
        {hasMore && (
          <div ref={sentinelRef} role="none">
            {pending ? (
              <div role="status" aria-busy="true">
                <Skeleton className="h-4 w-2/3" />
              </div>
            ) : (
              <Button
                variant="ghost"
                className={failed ? undefined : "sr-only focus:not-sr-only"}
                disabled={failed}
                onClick={onLoadMore}
              >
                {t("loadMore")}
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
