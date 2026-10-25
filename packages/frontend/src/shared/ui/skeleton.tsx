import { cn } from "@/shared/lib/cn";

export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "bg-muted animate-pulse rounded-md motion-reduce:animate-none",
        className,
      )}
      aria-hidden="true"
    />
  );
}
