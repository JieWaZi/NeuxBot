import { Sparkles } from "lucide-react";

import { cn } from "@/shared/lib/cn";

type Props = {
  className?: string;
};

export function SkillIcon({ className }: Props) {
  return (
    <span
      className={cn(
        "bg-muted text-muted-foreground grid size-10 shrink-0 place-items-center rounded-md",
        className,
      )}
      aria-hidden="true"
    >
      <Sparkles className="size-icon-md" strokeWidth={1.5} />
    </span>
  );
}
