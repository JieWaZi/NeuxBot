import { FileText, Folder } from "lucide-react";
import { cn } from "@/shared/lib/cn";
import type { SkillResource } from "../model/types";

export function SkillResourceRow({
  depth,
  onSelect,
  resource,
  selected,
}: {
  depth: number;
  onSelect: (path: string) => void;
  resource: SkillResource;
  selected: string | null;
}) {
  const Icon = resource.type === "directory" ? Folder : FileText;
  return (
    <div>
      <button
        type="button"
        className={cn(
          "type-body min-h-row-default flex w-full items-center gap-2 rounded-sm px-2 text-left",
          selected === resource.path
            ? "bg-muted text-foreground"
            : "hover:bg-muted/60",
        )}
        style={{ paddingLeft: `${8 + depth * 16}px` }}
        onClick={() => onSelect(resource.path)}
      >
        <Icon
          className="text-muted-foreground size-icon-sm"
          strokeWidth={1.5}
        />
        <span className="min-w-0 truncate">{resource.name}</span>
      </button>
      {resource.children?.map((child) => (
        <SkillResourceRow
          key={child.path}
          resource={child}
          depth={depth + 1}
          selected={selected}
          onSelect={onSelect}
        />
      ))}
    </div>
  );
}
