import { ChevronRight } from "lucide-react";
import { Button } from "./button";

export function ResourceSourceHeading({
  title,
  count,
  onSelect,
}: {
  title: string;
  count: number;
  onSelect?: () => void;
}) {
  return (
    <h3 className="type-body text-content-caption flex items-center gap-2 font-medium">
      {onSelect ? (
        <Button variant="ghost" size="sm" onClick={onSelect}>
          {title} {count}
          <ChevronRight />
        </Button>
      ) : (
        <>
          {title} <span>{count}</span>
        </>
      )}
    </h3>
  );
}
