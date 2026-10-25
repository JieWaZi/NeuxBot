import type { ReactNode } from "react";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { Skeleton } from "@/shared/ui/skeleton";
import { KnowledgeLibraryNavigation } from "./knowledge-library-navigation";
import { useCopy } from "./use-copy";

/** 代码和数据加载共用设置布局，只有右侧数据区域进入等待状态。 */
export function KnowledgeLibrarySettingsLoading({
  close,
  children,
}: {
  close: () => void;
  children?: ReactNode;
}) {
  const t = useCopy();
  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <KnowledgeLibraryNavigation mode="edit" disabled />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        {children ?? (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("loadingContent")}
            className="scrollbar-hidden flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-6 pb-5"
          >
            <Skeleton className="h-6 w-24 shrink-0 motion-reduce:animate-none" />
            {["identifier", "name", "purpose"].map((field) => (
              <div key={field} className="grid gap-2">
                <Skeleton className="h-4 w-16 motion-reduce:animate-none" />
                <Skeleton
                  className={
                    field === "purpose"
                      ? "h-24 w-full motion-reduce:animate-none"
                      : "h-control-md w-full motion-reduce:animate-none"
                  }
                />
              </div>
            ))}
            <Skeleton className="h-6 w-24 shrink-0 motion-reduce:animate-none" />
            <div className="flex gap-3">
              <Skeleton className="h-24 flex-1 motion-reduce:animate-none" />
              <Skeleton className="h-24 flex-1 motion-reduce:animate-none" />
            </div>
            <Skeleton className="h-6 w-24 shrink-0 motion-reduce:animate-none" />
            <Skeleton className="h-control-md w-40 shrink-0 motion-reduce:animate-none" />
          </div>
        )}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={close}>
            {t("cancel")}
          </Button>
          <Button type="button" disabled>
            {t("saveAndClose")}
          </Button>
        </DialogFooter>
      </div>
    </div>
  );
}
