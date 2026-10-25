import { Card, CardFooter } from "@/shared/ui/card";
import { Skeleton } from "@/shared/ui/skeleton";
import { ResourceCardGrid } from "@/shared/ui/resource-page";

export function TeamListSkeleton() {
  return (
    <ResourceCardGrid>
      {Array.from({ length: 3 }, (_, index) => (
        <Card key={index} variant="resource">
          <div className="grid min-h-32 grid-cols-[40px_1fr] gap-4 p-5">
            <Skeleton className="size-avatar-md rounded-sm" />
            <div className="space-y-2">
              <Skeleton className="h-3.5 w-28" />
              <Skeleton className="h-2.5 w-full" />
              <Skeleton className="h-2.5 w-3/4" />
            </div>
          </div>
          <CardFooter className="grid grid-cols-[1fr_44px]">
            <Skeleton className="m-auto h-2.5 w-24" />
            <Skeleton className="m-auto size-4 rounded-full" />
          </CardFooter>
        </Card>
      ))}
    </ResourceCardGrid>
  );
}
