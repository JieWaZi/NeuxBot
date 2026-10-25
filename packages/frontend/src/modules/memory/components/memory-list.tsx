import { Pencil, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { MemoryResource } from "../model/types";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";

type Props = {
  deleting?: boolean;
  items: MemoryResource[];
  onDelete: (memory: MemoryResource) => void;
  onEdit: (memory: MemoryResource) => void;
  scopeName: (memory: MemoryResource) => string;
};

export function MemoryList({
  deleting = false,
  items,
  onDelete,
  onEdit,
  scopeName,
}: Props) {
  const { t } = useTranslation("memory");

  return (
    <ol>
      {items.map((memory, index) => {
        const { description, title } = splitMemoryContent(memory.content);
        const first = index === 0;
        const last = index === items.length - 1;
        return (
          <li
            className="relative grid grid-cols-[20px_minmax(6.5rem,auto)_minmax(0,1fr)_auto] items-start gap-x-4 pb-6 last:pb-0"
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
            <time className="flex items-baseline gap-x-2">
              <span className="type-label font-medium">
                {formatMemoryDate(memory.updatedAt ?? memory.createdAt)}
              </span>
              <span className="text-muted-foreground type-caption">
                {formatMemoryWeekday(memory.updatedAt ?? memory.createdAt)}
              </span>
            </time>
            <article className="min-w-0">
              <h3 className="type-label font-medium">{title}</h3>
              {description ? (
                <p className="text-muted-foreground type-body mt-1 whitespace-pre-wrap">
                  {description}
                </p>
              ) : null}
              <p className="text-muted-foreground type-caption mt-2 flex flex-wrap items-center gap-x-3 gap-y-1">
                <span>
                  {t("list.scope")} {scopeName(memory)}
                </span>
                <span>
                  {t("list.source")}{" "}
                  {memory.sourceRunId
                    ? t("list.createdByMcp")
                    : t("list.createdManually")}
                </span>
              </p>
            </article>
            <div className="flex shrink-0 items-center gap-1">
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
  return new Intl.DateTimeFormat("zh-CN", { weekday: "short" }).format(
    new Date(value),
  );
}
