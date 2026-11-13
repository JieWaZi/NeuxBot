import { useWorkBoardDateBoundary } from "../model/use-work-board-date-boundary";
import { useQuery } from "@tanstack/react-query";
import { FolderSearch, ListTodo } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useSearchParams } from "react-router-dom";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { useVisibleScene } from "@/shared/api/chat-reading";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { workspaceAttentionResource } from "@/shared/api/reading-queries";
import {
  workBoardKeys,
  workBoardListOptions,
  workBoardSummaryOptions,
  type WorkBoardMode,
} from "../api/queries";
import { MarkViewedDialog } from "../components/mark-viewed-dialog";
import {
  localDateRangeBounds,
  normalizeDateInput,
} from "@/shared/lib/local-date";
import { Button } from "@/shared/ui/button";
import { DateRangePicker } from "@/shared/ui/date-range-picker";
import { EmptyState } from "@/shared/ui/empty-state";
import type { PaginationProps } from "@/shared/ui/pagination";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import {
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import { Skeleton } from "@/shared/ui/skeleton";
import { ExecutorFilter } from "../components/executor-filter";
import {
  WorkBoardOverview,
  WorkBoardTable,
} from "../components/work-board-content";

import { WorkBoardIntro } from "../components/work-board-intro";

const requestStates: readonly string[] = ["approval", "input", "review"];

export function WorkBoardPage() {
  const { t } = useTranslation("workBoard");
  const { state: navigationState } = useLocation();
  const [params, setParams] = useSearchParams();
  const active = useVisibleScene();
  const mode: WorkBoardMode =
    params.get("mode") === "action"
      ? "action"
      : params.get("mode") === "awaiting_view"
        ? "awaiting_view"
        : "all";
  const executorId = params.get("executorId") ?? "";
  const executorKind = params.get("executorKind") ?? "";
  const source = params.get("source") ?? "";
  const text = params.get("query") ?? "";
  const [search, setSearch] = useState(text);
  const pageCursors = useRef(new Map<number, string>());
  // 旧交付视图的游标只适用于当时的结果范围。
  const requestedPage =
    params.get("view") === "deliveries" ? 1 : Number(params.get("page") ?? 1);
  const page =
    Number.isSafeInteger(requestedPage) && requestedPage > 0
      ? requestedPage
      : 1;
  const cursor = page > 1 ? (params.get("cursor") ?? "") : "";
  const requestedSize = Number(params.get("pageSize") ?? 10);
  const pageSize = [10, 20, 50].includes(requestedSize) ? requestedSize : 10;
  const from = normalizeDateInput(params.get("from"));
  const to = normalizeDateInput(params.get("to"));
  const dates = localDateRangeBounds(from, to);
  const states = (params.get("states") ?? "").split(",").filter(Boolean);
  const pendingStates = states.filter((value) => requestStates.includes(value));
  const selectedStates =
    mode === "action"
      ? pendingStates.length
        ? pendingStates
        : requestStates
      : mode === "awaiting_view"
        ? ["awaiting_view"]
        : states;
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(text), 250);
    return () => window.clearTimeout(timer);
  }, [text]);
  const filters = {
    executorId: executorId || undefined,
    source: source || undefined,
    query: search,
  };
  const listOptions = workBoardListOptions({
    ...filters,
    mode,
    limit: pageSize,
    cursor: cursor || undefined,
    page: cursor ? undefined : page,
    ...(mode === "all"
      ? {
          from: dates.start,
          to: dates.end,
          states: states.join(",") || undefined,
        }
      : mode === "action"
        ? { states: pendingStates.join(",") || undefined }
        : {}),
  });
  const list = useQuery({
    ...listOptions,
    enabled: active && (mode !== "all" || dates.valid),
  });
  useEffect(() => {
    const total = list.data?.totalSize;
    if (
      !active ||
      !list.isSuccess ||
      list.data?.complete === false ||
      list.isFetching ||
      typeof total !== "number" ||
      (mode === "all" && !dates.valid)
    )
      return;
    const lastPage = Math.max(1, Math.ceil(total / pageSize));
    if (page <= lastPage) return;
    // 处理或阅读使末页消失后，实际请求与页码一起回到有效页。
    pageCursors.current.clear();
    setParams(
      (old) => {
        const next = new URLSearchParams(old);
        next.delete("cursor");
        if (lastPage > 1) next.set("page", String(lastPage));
        else next.delete("page");
        return next;
      },
      { replace: true, state: navigationState },
    );
  }, [
    active,
    dates.valid,
    list.data?.complete,
    list.data?.totalSize,
    list.isFetching,
    list.isSuccess,
    mode,
    navigationState,
    page,
    pageSize,
    setParams,
  ]);
  const summary = useQuery({ ...workBoardSummaryOptions, enabled: active });
  useWorkBoardDateBoundary(active, [
    workBoardKeys.summary,
    listOptions.queryKey,
  ]);
  useQueryEvents({
    topic: "work-board",
    resource: workspaceAttentionResource,
    queryKeys: [workBoardKeys.list, workBoardKeys.summary],
    eventTypes: ["work-board.changed"],
    authority: true,
    active,
  });
  const change = (values: Record<string, string>) => {
    pageCursors.current.clear();
    setParams(
      (old) => {
        const next = new URLSearchParams(old);
        next.delete("view");
        next.delete("cursor");
        next.delete("page");
        for (const [key, value] of Object.entries(values)) {
          if (value) next.set(key, value);
          else next.delete(key);
        }
        return next;
      },
      { replace: true, state: navigationState },
    );
  };
  const move = (target: number) => {
    if (target === page) return;
    pageCursors.current.set(page, cursor);
    const anchor =
      target === page + 1
        ? list.data?.nextCursor
        : pageCursors.current.get(target);
    setParams(
      (old) => {
        const next = new URLSearchParams(old);
        next.delete("view");
        if (target > 1) next.set("page", String(target));
        else next.delete("page");
        if (target > 1 && anchor) next.set("cursor", anchor);
        else next.delete("cursor");
        return next;
      },
      { replace: true, state: navigationState },
    );
  };
  const clearFilters = () => {
    setSearch("");
    change({
      mode: "all",
      executorId: "",
      executorName: "",
      executorKind: "",
      source: "",
      query: "",
      states: "",
      from: "",
      to: "",
    });
  };
  const filtered = Boolean(
    source ||
    executorId ||
    text ||
    (mode === "all" && (states.length || from || to)),
  );
  const canClearFilters =
    mode !== "all" || filtered || Boolean(states.length || from || to);
  const count =
    mode === "action"
      ? (list.data?.attention?.length ?? 0)
      : mode === "awaiting_view"
        ? (list.data?.awaitingView?.length ?? 0)
        : (list.data?.resources?.length ?? 0);
  const invalidDates = mode === "all" && !dates.valid;
  const incomplete = list.data?.complete === false;

  const pagination: PaginationProps | undefined =
    typeof list.data?.totalSize === "number" &&
    !invalidDates &&
    !(incomplete && mode === "awaiting_view")
      ? {
          labels: {
            navigation: t("pagination"),
            next: t("next"),
            previous: t("previous"),
            page: (page) => t("page", { page }),
            pageSize: (size) => t("pageSize", { size }),
            pageSizeSelection: t("pageSizeSelection"),
            total: (count) => t("totalCount", { count }),
          },
          page,
          pageSize,
          pageSizeOptions: [10, 20, 50],
          onPageSizeChange: (size) => change({ pageSize: String(size) }),
          totalItems: list.data?.totalSize ?? 0,
          pending: list.isFetching,
          onPageChange: move,
        }
      : undefined;
  const empty = invalidDates ? (
    <EmptyState
      icon={FolderSearch}
      title={t("invalidRange")}
      description={t("invalidRangeHint")}
      actions={
        <Button
          variant="secondary"
          onClick={() => change({ from: "", to: "" })}
        >
          {t("clear")}
        </Button>
      }
    />
  ) : list.isPending && !list.data ? (
    <div className="grid gap-3" role="status" aria-label={t("loadingRecords")}>
      <Skeleton className="h-20 w-full rounded-xl" />
      <Skeleton className="h-20 w-full rounded-xl" />
      <Skeleton className="h-20 w-full rounded-xl" />
    </div>
  ) : list.isError && !list.data ? (
    <EmptyState
      title={t("loadFailed")}
      description={localApiErrorMessage(list.error, t("loadFailed"))}
      actions={
        <Button variant="secondary" onClick={() => void list.refetch()}>
          {t("retry")}
        </Button>
      }
    />
  ) : !count && !incomplete ? (
    <EmptyState
      icon={filtered ? FolderSearch : ListTodo}
      title={t(
        filtered
          ? "noMatches"
          : mode === "all"
            ? "empty"
            : mode === "action"
              ? "attentionEmpty"
              : "resultsEmpty",
      )}
      description={t(
        filtered
          ? "noMatchesHint"
          : mode === "all"
            ? "emptyHint"
            : mode === "action"
              ? "attentionEmptyHint"
              : "resultsEmptyHint",
      )}
    />
  ) : undefined;

  return (
    <ResourcePageSurface>
      <WorkBoardIntro active={active} />
      <WorkBoardOverview
        summary={summary.data}
        pending={summary.isPending}
        active={active}
        error={
          summary.isError
            ? localApiErrorMessage(summary.error, t("loadFailed"))
            : undefined
        }
        onRetry={() => void summary.refetch()}
      />
      <section className="mt-8 min-w-0 scroll-mt-4" aria-label={t("records")}>
        <ResourcePageToolbar
          filters={
            <>
              <ResourceFilterSelect
                ariaLabel={t("source")}
                value={source || "all"}
                onValueChange={(value) =>
                  change({ source: value === "all" ? "" : value })
                }
                options={[
                  { value: "all", label: t("allSources") },
                  ...["chat", "team", "workflow", "automatic", "im"].map(
                    (value) => ({ value, label: t(value) }),
                  ),
                ]}
              />
              <div className="w-field-filter min-w-0">
                <SearchableMultiSelect
                  value={selectedStates}
                  onValueChange={(values) => {
                    const added = values.find(
                      (value) => !selectedStates.includes(value),
                    );
                    if (added === "awaiting_view") {
                      change({ mode: "awaiting_view", states: "" });
                    } else if (
                      (added && requestStates.includes(added)) ||
                      (mode === "action" && !added)
                    ) {
                      const pending = values.filter((value) =>
                        requestStates.includes(value),
                      );
                      change({
                        mode: pending.length ? "action" : "all",
                        states: pending.join(","),
                      });
                    } else {
                      change({
                        mode: "all",
                        states: values
                          .filter(
                            (value) =>
                              !requestStates.includes(value) &&
                              value !== "awaiting_view",
                          )
                          .join(","),
                      });
                    }
                  }}
                  triggerLabel={
                    selectedStates.length
                      ? t("selectedStates", { count: selectedStates.length })
                      : t("taskState")
                  }
                  searchLabel={t("searchState")}
                  emptyLabel={t("noStates")}
                  clearSearchLabel={t("clearSearch")}
                  clearSelectionLabel={t("allStates")}
                  options={[
                    ...[
                      "running",
                      "waiting",
                      "completed",
                      "failed",
                      "canceled",
                      "idle",
                    ].map((value) => ({ value, label: t(value) })),
                    { value: "approval", label: t("pendingApproval") },
                    { value: "input", label: t("pendingInput") },
                    { value: "review", label: t("pendingReview") },
                    { value: "awaiting_view", label: t("results") },
                  ]}
                />
              </div>
              <ExecutorFilter
                value={executorId}
                label={params.get("executorName") ?? ""}
                kind={executorKind}
                onChange={(id, name, kind) =>
                  change({
                    executorId: id,
                    executorName: name,
                    executorKind: kind,
                  })
                }
              />
              {mode === "all" && (
                <DateRangePicker
                  from={from}
                  to={to}
                  onChange={(start, end) => change({ from: start, to: end })}
                  labels={{
                    clear: t("clear"),
                    empty: t("recent"),
                    title: t("range"),
                    help: t("rangeHelp"),
                    trigger: t("chooseRange"),
                  }}
                />
              )}
              <ResourcePageSearchField
                aria-label={t("search")}
                placeholder={t("search")}
                value={text}
                onChange={(event) => change({ query: event.target.value })}
              />
              <MarkViewedDialog
                active={active}
                visible={(summary.data?.awaitingViewWorkCount ?? 0) > 0}
                filters={{
                  ...filters,
                  query: text,
                  states:
                    mode === "all" ? states.join(",") || undefined : undefined,
                }}
              />
            </>
          }
        >
          <div className="flex items-center gap-2">
            <h2 className="type-title font-semibold">{t("allTasks")}</h2>
            {canClearFilters ? (
              <Button variant="ghost" size="sm" onClick={clearFilters}>
                {t("clearFilters")}
              </Button>
            ) : null}
          </div>
        </ResourcePageToolbar>
        <div className="min-h-64 min-w-0 pt-3" aria-busy={list.isFetching}>
          {incomplete && !list.isError ? (
            <div
              role="status"
              className="text-content-secondary type-ui mb-3 flex items-center gap-2"
            >
              {t("attentionIncomplete")}
              <Button
                size="sm"
                variant="ghost"
                onClick={() => void list.refetch()}
              >
                {t("retry")}
              </Button>
            </div>
          ) : null}
          {list.isError && list.data ? (
            <div
              role="alert"
              className="text-destructive type-ui mb-3 flex flex-wrap items-center justify-between gap-2"
            >
              {localApiErrorMessage(list.error, t("loadFailed"))}
              <Button
                size="sm"
                variant="ghost"
                onClick={() => void list.refetch()}
              >
                {t("retry")}
              </Button>
            </div>
          ) : null}
          <WorkBoardTable
            mode={mode}
            entries={
              mode === "awaiting_view"
                ? list.data?.awaitingView
                : list.data?.resources
            }
            attention={list.data?.attention}
            pagination={pagination}
            empty={empty}
          />
        </div>
      </section>
    </ResourcePageSurface>
  );
}
