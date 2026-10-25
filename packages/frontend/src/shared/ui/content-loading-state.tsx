import { cn } from "@/shared/lib/cn";
import { Spinner } from "./spinner";

/** 仅占用待加载的内容区，页面布局与操作区由调用方保持挂载。 */
export function ContentLoadingState({
  label,
  className,
}: {
  label: string;
  className?: string;
}) {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={label}
      className={cn("grid min-h-24 min-w-0 place-items-center", className)}
    >
      <Spinner
        className="size-icon-lg"
        aria-hidden="true"
        role="presentation"
      />
    </div>
  );
}
