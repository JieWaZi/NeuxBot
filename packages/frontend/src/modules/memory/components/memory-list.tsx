import { localizedDateTimeFormat } from "@/shared/i18n/format";
import { Pencil, Pin, PinOff, Trash2 } from "lucide-react";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { useTranslation } from "react-i18next";

import type { MemoryResource } from "../model/types";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";

type Props = {
  deleting?: boolean;
  items: MemoryResource[];
  onDelete: (memory: MemoryResource) => void;
  onEdit: (memory: MemoryResource) => void;
  changingId?: string;
  pending?: boolean;
  error?: unknown;
  onToggleCore: (memory: MemoryResource) => void;
};

export function MemoryList({
  deleting = false,
  items,
  onDelete,
  onEdit,
  changingId,
  pending,
  error,
  onToggleCore,
}: Props) {
  const { t } = useTranslation("memory");

  return (
    <ol className="@container/memory-list">
      {items.map((memory, index) => {
        const { description, title } = splitMemoryContent(memory.content);
        const first = index === 0;
        const last = index === items.length - 1;
        const coreLabel = t(memory.core ? "setRegular" : "setCore");
        const CoreIcon = memory.core ? PinOff : Pin;
        return (
          <li
            className="relative grid grid-cols-[20px_minmax(0,1fr)_auto] items-start gap-x-2 pb-6 last:pb-0 @min-[36rem]/memory-list:grid-cols-[20px_minmax(6.5rem,auto)_minmax(0,1fr)_auto] @min-[36rem]/memory-list:gap-x-4"
            key={memory.id}
          >
            {items.length > 1 ? (
              <span
                aria-hidden
                className={cn(
                  "bg-border pointer-events-none absolute left-2.5 w-px -translate-x-1/2",
                  first ? "top-2.5" : "top-0",
                  last ? "h-2.5" : "bottom-0",
                )}
              />
            ) : null}
            <div className="relative z-10 flex justify-center pt-1.5">
              <span className="bg-success size-2 rounded-full" />
            </div>
            <time className="flex flex-wrap items-baseline gap-x-2">
              <span className="type-label font-medium">
                {formatMemoryDate(memory.updatedAt ?? memory.createdAt)}
              </span>
              <span className="text-muted-foreground type-caption">
                {formatMemoryWeekday(memory.updatedAt ?? memory.createdAt)}
              </span>
            </time>
            <article className="col-start-2 col-end-4 row-start-2 min-w-0 @min-[36rem]/memory-list:col-start-3 @min-[36rem]/memory-list:row-start-1">
              <h3 className="type-label font-medium break-words">{title}</h3>
              {description ? (
                <p className="text-muted-foreground type-body mt-1 break-words whitespace-pre-wrap">
                  {description}
                </p>
              ) : null}
              <p className="text-muted-foreground type-caption mt-2">
                {t("list.source")}{" "}
                {memory.source === "review"
                  ? t("list.createdByReview")
                  : memory.source === "mcp"
                    ? t("list.createdByMcp")
                    : t("list.createdManually")}
              </p>
              {changingId === memory.id && error ? (
                <p role="alert" className="text-destructive type-body mt-2">
                  {localApiErrorMessage(error, t("coreChangeFailed"))}
                </p>
              ) : null}
            </article>
            <div className="col-start-3 row-start-1 flex shrink-0 items-center gap-1 @min-[36rem]/memory-list:col-start-4">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    aria-label={coreLabel}
                    aria-disabled={pending || !memory.id}
                    data-memory-core-action={memory.id}
                    size="icon"
                    variant="ghost"
                    onClick={() => onToggleCore(memory)}
                  >
                    <CoreIcon />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{coreLabel}</TooltipContent>
              </Tooltip>
              <Button
                aria-label={t("editAction")}
                size="icon-sm"
                variant="ghost"
                onClick={() => onEdit(memory)}
              >
                <Pencil />
              </Button>
              <Button
                aria-label={t("forgetAction")}
                disabled={deleting}
                size="icon-sm"
                variant="ghost"
                onClick={() => onDelete(memory)}
              >
                <Trash2 />
              </Button>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

export function MemoryListEmpty({ text }: { text: string }) {
  return <EmptyState className="min-h-32" density="compact" title={text} />;
}

export function MemoryListError({
  onRetry,
  text,
}: {
  onRetry: () => void;
  text: string;
}) {
  const { t } = useTranslation("memory");
  return (
    <EmptyState
      className="min-h-32"
      density="compact"
      title={text}
      actions={
        <Button size="sm" variant="secondary" onClick={onRetry}>
          {t("retry")}
        </Button>
      }
    />
  );
}

export function splitMemoryContent(content?: string) {
  const trimmed = content?.trim() ?? "";
  if (!trimmed) {
    return { description: "", title: "—" };
  }
  const linebreak = trimmed.search(/\r?\n/);
  if (linebreak >= 0) {
    return {
      description: trimmed.slice(linebreak).trim(),
      title: trimmed.slice(0, linebreak).trim() || "—",
    };
  }
  return { description: "", title: trimmed };
}

function formatMemoryDate(value?: number) {
  if (!value) return "—";
  const date = new Date(value);
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${month}/${day}`;
}

function formatMemoryWeekday(value?: number) {
  if (!value) return "";
  return localizedDateTimeFormat({ weekday: "short" }).format(new Date(value));
}
