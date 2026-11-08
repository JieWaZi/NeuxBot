import { Sparkles } from "lucide-react";

import { cn } from "@/shared/lib/cn";

type Props = {
  className?: string;
};

export function SkillIcon({ className }: Props) {
  return (
    <span
      className={cn(
        "bg-resource-surface text-resource-muted size-resource-icon grid shrink-0 place-items-center rounded-lg",
        className,
      )}
      aria-hidden="true"
    >
      <Sparkles className="size-resource-logo" strokeWidth={1.5} />
    </span>
  );
}
