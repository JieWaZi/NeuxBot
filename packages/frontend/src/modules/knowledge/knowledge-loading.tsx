import type { ReactNode } from "react";
import { Skeleton } from "@/shared/ui/skeleton";
import { Card, CardFooter } from "@/shared/ui/card";
import { ResourceCardGrid } from "@/shared/ui/resource-page";
import { useCopy } from "./use-copy";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";

/** 库列表骨架与当前资源卡片对应。 */
export function KnowledgeLoading() {
  return (
    <LoadingRegion>
      <ResourceCardGrid className="mt-6" aria-hidden="true">
        {[0, 1, 2].map((id) => (
          <Card key={id} variant="resource">
            <div className="flex min-h-32 items-start gap-4 p-5">
              <Skeleton className="size-icon-lg shrink-0 motion-reduce:animate-none" />
              <div className="min-w-0 flex-1 space-y-3">
                <Skeleton className="h-5 w-2/3 motion-reduce:animate-none" />
                <Skeleton className="h-3 w-full motion-reduce:animate-none" />
                <Skeleton className="h-3 w-4/5 motion-reduce:animate-none" />
              </div>
            </div>
            <CardFooter>
              <Skeleton className="h-3 w-1/3 motion-reduce:animate-none" />
            </CardFooter>
          </Card>
        ))}
      </ResourceCardGrid>
    </LoadingRegion>
  );
}

/** 加载占位只表达所属内容区域，不阻塞页面操作。 */
function LoadingRegion({ children }: { children: ReactNode }) {
  const t = useCopy();
  const active = useRouteSceneActive();
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={t("loadingContent")}
      className={active ? undefined : "[&_*]:animate-none"}
    >
      {children}
    </div>
  );
}

export function KnowledgeDocumentsLoading() {
  return (
    <LoadingRegion>
      <div aria-hidden="true" className="space-y-3">
        {[0, 1, 2, 3, 4, 5].map((row) => (
          <div key={row} className="flex min-w-0 items-center gap-2 px-3 py-2">
            <Skeleton className="size-icon-sm shrink-0 motion-reduce:animate-none" />
            <Skeleton className="h-4 w-2/3 motion-reduce:animate-none" />
          </div>
        ))}
      </div>
    </LoadingRegion>
  );
}

export function KnowledgeWikiDirectoryLoading() {
  return (
    <LoadingRegion>
      <div className="space-y-4" aria-hidden="true">
        <Skeleton className="h-control-sm w-full motion-reduce:animate-none" />
        <Skeleton className="h-4 w-16 motion-reduce:animate-none" />
        <div className="space-y-3">
          {[0, 1, 2, 3, 4, 5].map((row) => (
            <div key={row} className="flex items-center gap-2 px-3 py-1">
              <Skeleton className="size-icon-sm shrink-0 motion-reduce:animate-none" />
              <Skeleton className="h-4 w-2/3 motion-reduce:animate-none" />
            </div>
          ))}
        </div>
      </div>
    </LoadingRegion>
  );
}

export function KnowledgeWikiContentLoading() {
  return (
    <LoadingRegion>
      <div className="space-y-6" aria-hidden="true">
        <div className="space-y-3">
          <Skeleton className="h-6 w-2/5 motion-reduce:animate-none" />
          <div className="flex gap-2">
            <Skeleton className="h-5 w-12 rounded-full motion-reduce:animate-none" />
            <Skeleton className="h-5 w-16 rounded-full motion-reduce:animate-none" />
            <Skeleton className="h-4 w-36 motion-reduce:animate-none" />
          </div>
        </div>
        {[0, 1, 2].map((section) => (
          <div key={section} className="space-y-3">
            <Skeleton className="h-5 w-1/4 motion-reduce:animate-none" />
            <Skeleton className="h-4 w-full motion-reduce:animate-none" />
            <Skeleton className="h-4 w-11/12 motion-reduce:animate-none" />
            <Skeleton className="h-4 w-3/4 motion-reduce:animate-none" />
          </div>
        ))}
      </div>
    </LoadingRegion>
  );
}

/** 代码分包与数据请求使用相同的 Wiki 内容占位布局。 */
export function KnowledgeWikiLoading() {
  return (
    <div className="@container/wiki flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="grid min-h-0 flex-1 grid-rows-[minmax(12rem,35%)_minmax(0,1fr)] @[42rem]/wiki:grid-cols-[minmax(14rem,18rem)_minmax(0,1fr)] @[42rem]/wiki:grid-rows-[minmax(0,1fr)]">
        <div className="border-border-subtle min-h-0 overflow-hidden border-b pb-3 @[42rem]/wiki:border-r @[42rem]/wiki:border-b-0 @[42rem]/wiki:pr-4 @[42rem]/wiki:pb-0">
          <KnowledgeWikiDirectoryLoading />
        </div>
        <div className="min-h-0 overflow-hidden pt-4 @[42rem]/wiki:pt-0 @[42rem]/wiki:pl-6">
          <KnowledgeWikiContentLoading />
        </div>
      </div>
    </div>
  );
}

/** 图谱首次读取保留节点和连线的空间结构。 */
export function KnowledgeGraphLoading() {
  return (
    <LoadingRegion>
      <div className="relative h-80 overflow-hidden" aria-hidden="true">
        <Skeleton className="absolute top-1/4 left-1/4 size-8 rounded-full motion-reduce:animate-none" />
        <Skeleton className="absolute top-1/2 left-1/2 size-10 rounded-full motion-reduce:animate-none" />
        <Skeleton className="absolute top-1/3 left-3/4 size-6 rounded-full motion-reduce:animate-none" />
        <Skeleton className="absolute top-1/2 left-1/4 h-px w-1/2 motion-reduce:animate-none" />
        <Skeleton className="absolute top-3/4 left-1/3 size-6 rounded-full motion-reduce:animate-none" />
      </div>
    </LoadingRegion>
  );
}
