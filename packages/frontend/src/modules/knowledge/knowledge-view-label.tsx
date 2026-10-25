import { ResourcePageLibraryLabel } from "@/shared/ui/resource-page";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { cn } from "@/shared/lib/cn";
import { Skeleton } from "@/shared/ui/skeleton";
import { useCopy } from "./use-copy";

/** 文档与 Wiki 内容区域保留同层级标题，计数未读到时不伪造零值。 */
export function KnowledgeViewLabel({
  view,
  count,
}: {
  view: "documents" | "wiki";
  count?: number;
}) {
  const t = useCopy();
  const active = useRouteSceneActive();
  return (
    <div className="flex items-center gap-3">
      <ResourcePageLibraryLabel
        installed={t(view === "documents" ? "documents" : "wikiTitle")}
        count={
          count === undefined
            ? ""
            : t(view === "documents" ? "documentCount" : "pageCount", { count })
        }
      />
      {count === undefined && (
        <Skeleton
          className={cn(
            "h-3 w-16 motion-reduce:animate-none",
            !active && "animate-none",
          )}
        />
      )}
    </div>
  );
}
