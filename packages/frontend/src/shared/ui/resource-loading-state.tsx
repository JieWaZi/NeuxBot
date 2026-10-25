import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Card, CardBody, CardFooter } from "./card";
import { ResourceCardGrid } from "./resource-page";
import { Skeleton } from "./skeleton";

export type ResourceLoadingLayout = "cards" | "detail" | "content";

/** 首次读取按内容布局占位；页头、筛选与页签由调用方保持可用。 */
export function ResourceLoadingState({
  layout = "content",
  className,
  label,
}: {
  layout?: ResourceLoadingLayout;
  className?: string;
  label?: string;
}) {
  const { t } = useTranslation("common");
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={label ?? t("loading")}
      className={cn("min-w-0", className)}
    >
      {layout === "cards" ? (
        <ResourceCardGrid>
          {Array.from({ length: 9 }, (_, index) => (
            <Card key={index} variant="resource" aria-hidden="true">
              <CardBody className="space-y-4 p-4">
                <div className="flex items-center gap-3">
                  <Skeleton className="size-10 shrink-0" />
                  <div className="min-w-0 flex-1 space-y-2">
                    <Skeleton className="h-5 w-2/3" />
                    <Skeleton className="h-4 w-1/3" />
                  </div>
                </div>
                <Skeleton className="h-5 w-full" />
              </CardBody>
              <CardFooter className="justify-between px-4 py-2">
                <Skeleton className="h-4 w-1/3" />
                <Skeleton className="size-control-sm" />
              </CardFooter>
            </Card>
          ))}
        </ResourceCardGrid>
      ) : layout === "detail" ? (
        <div className="grid min-h-96 gap-8" aria-hidden="true">
          {Array.from({ length: 3 }, (_, index) => (
            <div key={index} className="space-y-4">
              <Skeleton className="h-6 w-1/4" />
              <Skeleton className="h-5 w-full" />
              <Skeleton className="h-5 w-5/6" />
              <Skeleton className="h-5 w-2/3" />
            </div>
          ))}
        </div>
      ) : (
        <div className="min-h-64 space-y-4" aria-hidden="true">
          <Skeleton className="h-6 w-1/3" />
          <Skeleton className="h-5 w-full" />
          <Skeleton className="h-5 w-5/6" />
          <Skeleton className="h-5 w-full" />
          <Skeleton className="h-5 w-2/3" />
        </div>
      )}
    </div>
  );
}
