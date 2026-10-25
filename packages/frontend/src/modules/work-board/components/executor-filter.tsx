import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { listWorkBoardExecutorsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";
import { NavigationItem } from "@/shared/ui/navigation-panel";
import { cn } from "@/shared/lib/cn";
import { WorkExecutorAvatar } from "./work-executor-avatar";

export function ExecutorFilter({
  value,
  label,
  kind,
  onChange,
}: {
  value: string;
  label: string;
  kind: string;
  onChange: (id: string, name: string, kind: string) => void;
}) {
  const { t } = useTranslation("workBoard");
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [search, setSearch] = useState("");
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(text), 250);
    return () => window.clearTimeout(timer);
  }, [text]);
  const query = useQuery({
    ...listWorkBoardExecutorsOptions(
      withLocalErrorSurface({ query: { query: search } }),
    ),
    enabled: open,
  });
  const select = (id: string, name: string, nextKind = "") => {
    onChange(id, name, nextKind);
    setOpen(false);
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <FieldControlTrigger
          size="sm"
          width="filter"
          aria-label={value ? label || t("executor") : t("allExecutors")}
          title={value ? label || t("executor") : undefined}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            {value ? (
              <WorkExecutorAvatar
                kind={kind}
                name={label}
                executorId={value}
                size="xs"
              />
            ) : null}
            <span className="min-w-0 truncate">
              {value ? label || t("executor") : t("allExecutors")}
            </span>
          </span>
        </FieldControlTrigger>
      </PopoverTrigger>
      <PopoverContent align="end">
        <SearchField
          size="sm"
          autoFocus
          aria-label={t("executorSearch")}
          placeholder={t("executorSearch")}
          value={text}
          onChange={(event) => setText(event.target.value)}
        />
        <p className="text-content-muted type-caption py-2">
          {t("executorHint")}
        </p>
        <div className="max-h-64 overflow-y-auto">
          <NavigationItem
            label={t("allExecutors")}
            active={!value}
            onClick={() => select("", "", "")}
          />
          {query.isError ? (
            <Button variant="ghost" onClick={() => void query.refetch()}>
              {t("retry")}
            </Button>
          ) : query.isPending ? (
            <p role="status" className="type-caption p-2">
              {t("refresh")}
            </p>
          ) : query.data?.resources?.length ? (
            query.data.resources.map((executor) => (
              <button
                key={`${executor.kind}:${executor.id}`}
                type="button"
                aria-current={value === executor.id ? "page" : undefined}
                aria-label={executor.name ?? ""}
                className={cn(
                  "focus-visible:ring-ring flex w-full items-center rounded-md px-2 py-1.5 outline-none focus-visible:ring-2",
                  value === executor.id
                    ? "bg-selection-surface"
                    : "hover:bg-action-quiet-hover",
                )}
                onClick={() =>
                  select(
                    executor.id ?? "",
                    executor.name ?? "",
                    executor.kind ?? "waker",
                  )
                }
              >
                <WorkExecutorAvatar
                  kind={executor.kind}
                  name={executor.name ?? ""}
                  executorId={executor.id}
                  size="sm"
                />
                <span className="type-ui ml-2 min-w-0 truncate">
                  {executor.name || t("unknownExecutor")}
                </span>
              </button>
            ))
          ) : (
            <p className="text-content-muted type-caption p-2">
              {t("noExecutors")}
            </p>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
