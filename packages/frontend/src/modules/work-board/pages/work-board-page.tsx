import { useWorkBoardDateBoundary } from "../model/use-work-board-date-boundary";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderSearch, ListTodo } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";
import {
  getWorkBoardSummaryOptions,
  listWorkBoardOptions,
  listWorkBoardQueryKey,
  listWorkBoardInfiniteQueryKey,
  getWorkBoardSummaryQueryKey,
  clearNotificationUnreadMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { useVisibleScene } from "@/shared/api/work-result-reading";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  getNotificationSnapshotQueryKey,
  listNotificationsQueryKey,
} from "@/modules/notification";
import {
  localDateRangeBounds,
  normalizeDateInput,
} from "@/shared/lib/local-date";
import { Button } from "@/shared/ui/button";
import { DateRangePicker } from "@/shared/ui/date-range-picker";
import { EmptyState } from "@/shared/ui/empty-state";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import {
  ResourcePageHeader,
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
  type WorkBoardMetric,
  type WorkBoardMode,
} from "../components/work-board-content";

const requestStates: readonly string[] = ["approval", "input", "review"];

export function WorkBoardPage() {
  const { t } = useTranslation("workBoard");
  const client = useQueryClient();
  const clearResults = useMutation({
    ...withLocalErrorMutation(clearNotificationUnreadMutation()),
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: listWorkBoardQueryKey() }),
        client.invalidateQueries({ queryKey: getWorkBoardSummaryQueryKey() }),
        client.invalidateQueries({
          queryKey: getNotificationSnapshotQueryKey(),
        }),
        client.invalidateQueries({ queryKey: listNotificationsQueryKey() }),
      ]);
    },
  });
  const [params, setParams] = useSearchParams();
  const active = useVisibleScene();
  const mode: WorkBoardMode =
    params.get("mode") === "attention"
      ? "attention"
      : params.get("mode") === "results"
        ? "results"
        : "all";
  const executorId = params.get("executorId") ?? "";
  const executorKind = params.get("executorKind") ?? "";
  const source = params.get("source") ?? "";
  const text = params.get("query") ?? "";
  const [search, setSearch] = useState(text);
  const pageCursors = useRef(new Map<number, string>());
  const records = useRef<HTMLElement>(null);
  const requestedPage = Number(params.get("page") ?? 1);
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
    mode === "attention"
      ? pendingStates.length
        ? pendingStates
        : requestStates
      : mode === "results"
        ? ["results"]
        : states;
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(text), 250);
    return () => window.clearTimeout(timer);
  }, [text]);
  const listOptions = listWorkBoardOptions(
    withLocalErrorSurface({
      query: {
        mode,
        executorId: executorId || undefined,
        source: source || undefined,
        query: search,
        cursor: cursor || undefined,
        page: cursor ? undefined : page,
        limit: pageSize,
        ...(mode === "all"
          ? {
              from: dates.start,
              to: dates.end,
              states: states.join(",") || undefined,
            }
          : mode === "attention"
            ? { states: pendingStates.join(",") || undefined }
            : {}),
      },
    }),
  );
  const list = useQuery({
    ...listOptions,
    queryFn: async (context) => {
      if (typeof listOptions.queryFn !== "function")
        throw new Error(t("loadFailed"));
      const data = await listOptions.queryFn(context);
      if (
        !data ||
        typeof data.totalSize !== "number" ||
        !Number.isSafeInteger(data.totalSize) ||
        data.totalSize < 0
      )
        throw new Error(t("loadFailed"));
      return data;
    },
    enabled: active && (mode !== "all" || dates.valid),
  });
  useEffect(() => {
    const total = list.data?.totalSize;
    if (
      !active ||
      !list.isSuccess ||
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
      { replace: true },
    );
  }, [
    active,
    dates.valid,
    list.data?.totalSize,
    list.isFetching,
    list.isSuccess,
    mode,
    page,
    pageSize,
    setParams,
  ]);
  // 概览与关注使用全局数据，工作记录筛选只传给列表请求。
  const summaryOptions = getWorkBoardSummaryOptions(withLocalErrorSurface({}));
  const summary = useQuery({
    ...summaryOptions,
    queryFn: async (context) => {
      if (typeof summaryOptions.queryFn !== "function")
        throw new Error(t("loadFailed"));
      const data = await summaryOptions.queryFn(context);
      if (
        !data ||
        (
          [
            "workCount",
            "runningCount",
            "endedCount",
            "actionWorkCount",
            "attentionCount",
            "resultCount",
          ] as const
        ).some(
          (key) =>
            typeof data[key] !== "number" ||
            !Number.isSafeInteger(data[key]) ||
            (data[key] ?? -1) < 0,
        )
      )
        throw new Error(t("loadFailed"));
      return data;
    },
    enabled: active,
  });
  useWorkBoardDateBoundary(active, [
    summaryOptions.queryKey,
    listOptions.queryKey,
  ]);
  useQueryEvents({
    topic: "work-board",
    resource: { kind: "global" },
    queryKeys: [
      listWorkBoardQueryKey(),
      listWorkBoardInfiniteQueryKey(),
      getWorkBoardSummaryQueryKey(),
    ],
    active,
  });
  useEffect(() => {
    if (active && summary.isSuccess && summary.dataUpdatedAt) {
      void client.invalidateQueries({
        queryKey: listWorkBoardInfiniteQueryKey(),
      });
    }
  }, [active, client, summary.isSuccess, summary.dataUpdatedAt]);
  const change = (values: Record<string, string>) => {
    pageCursors.current.clear();
    setParams(
      (old) => {
        const next = new URLSearchParams(old);
        next.delete("cursor");
        next.delete("page");
        for (const [key, value] of Object.entries(values)) {
          if (value) next.set(key, value);
          else next.delete(key);
        }
        return next;
      },
      { replace: true },
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
        if (target > 1) next.set("page", String(target));
        else next.delete("page");
        if (target > 1 && anchor) next.set("cursor", anchor);
        else next.delete("cursor");
        return next;
      },
      { replace: true },
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
    mode === "attention"
      ? (list.data?.attention?.length ?? 0)
      : (list.data?.resources?.length ?? 0);
  const invalidDates = mode === "all" && !dates.valid;

  return (
    <ResourcePageSurface>
      <ResourcePageHeader
        title={t("title")}
        description={
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span>{t("description")}</span>
            <span className="type-caption whitespace-nowrap">
              {t("recent")}
            </span>
          </div>
        }
        actions={null}
      />
      <WorkBoardOverview
        summary={summary.data}
        pending={summary.isPending}
        error={
          summary.isError
            ? localApiErrorMessage(summary.error, t("loadFailed"))
            : undefined
        }
        onRetry={() => void summary.refetch()}
        selected={
          mode === "attention"
            ? "attention"
            : mode !== "all"
              ? undefined
              : states.length === 1 && states[0] === "running"
                ? "running"
                : states.length === 3 &&
                    ["completed", "failed", "canceled"].every((state) =>
                      states.includes(state),
                    )
                  ? "ended"
                  : !states.length && !text
                    ? "all"
                    : undefined
        }
        onSelect={(metric: WorkBoardMetric) => {
          setSearch("");
          change({
            mode: metric === "attention" ? "attention" : "all",
            states:
              metric === "running"
                ? "running"
                : metric === "ended"
                  ? "completed,failed,canceled"
                  : "",
            query: "",
          });
          records.current?.scrollIntoView?.({
            block: "start",
            behavior: window.matchMedia("(prefers-reduced-motion: reduce)")
              .matches
              ? "auto"
              : "smooth",
          });
        }}
        onClearResults={() => clearResults.mutate({})}
        clearingResults={clearResults.isPending}
        clearError={
          clearResults.isError
            ? localApiErrorMessage(clearResults.error, t("clearResultsFailed"))
            : undefined
        }
      />
      <section
        ref={records}
        className="mt-8 min-w-0 scroll-mt-4"
        aria-label={t("records")}
      >
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
                    if (added === "results") {
                      change({ mode: "results", states: "" });
                    } else if (
                      (added && requestStates.includes(added)) ||
                      (mode === "attention" && !added)
                    ) {
                      const pending = values.filter((value) =>
                        requestStates.includes(value),
                      );
                      change({
                        mode: pending.length ? "attention" : "all",
                        states: pending.join(","),
                      });
                    } else {
                      change({
                        mode: "all",
                        states: values
                          .filter(
                            (value) =>
                              !requestStates.includes(value) &&
                              value !== "results",
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
                    { value: "results", label: t("results") },
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
            </>
          }
        >
          <div className="flex items-center gap-2">
            <h2 className="type-title font-semibold">{t("records")}</h2>
            {canClearFilters ? (
              <Button variant="ghost" size="sm" onClick={clearFilters}>
                {t("clearFilters")}
              </Button>
            ) : null}
          </div>
        </ResourcePageToolbar>
        <div className="min-h-64 min-w-0 pt-6" aria-busy={list.isFetching}>
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
            entries={list.data?.resources}
            attention={list.data?.attention}
            pagination={
              typeof list.data?.totalSize === "number" && !invalidDates
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
                    onPageSizeChange: (size) =>
                      change({ pageSize: String(size) }),
                    totalItems: list.data?.totalSize ?? 0,
                    pending: list.isFetching,
                    onPageChange: move,
                  }
                : undefined
            }
            empty={
              invalidDates ? (
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
                <div
                  className="grid gap-3"
                  role="status"
                  aria-label={t("loadingRecords")}
                >
                  <Skeleton className="h-20 w-full rounded-xl" />
                  <Skeleton className="h-20 w-full rounded-xl" />
                  <Skeleton className="h-20 w-full rounded-xl" />
                </div>
              ) : list.isError && !list.data ? (
                <EmptyState
                  title={t("loadFailed")}
                  description={localApiErrorMessage(
                    list.error,
                    t("loadFailed"),
                  )}
                  actions={
                    <Button
                      variant="secondary"
                      onClick={() => void list.refetch()}
                    >
                      {t("retry")}
                    </Button>
                  }
                />
              ) : !count ? (
                <EmptyState
                  icon={filtered ? FolderSearch : ListTodo}
                  title={t(
                    filtered
                      ? "noMatches"
                      : mode === "all"
                        ? "empty"
                        : mode === "attention"
                          ? "attentionEmpty"
                          : "resultsEmpty",
                  )}
                  description={t(
                    filtered
                      ? "noMatchesHint"
                      : mode === "all"
                        ? "emptyHint"
                        : mode === "attention"
                          ? "attentionEmptyHint"
                          : "resultsEmptyHint",
                  )}
                />
              ) : undefined
            }
          />
        </div>
      </section>
    </ResourcePageSurface>
  );
}
