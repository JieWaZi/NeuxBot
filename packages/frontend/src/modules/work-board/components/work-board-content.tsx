import {
  ArrowRight,
  ListTodo,
  CheckCheck,
  CircleCheck,
  CircleMinus,
  CirclePause,
  FileCheck2,
  LoaderCircle,
  MessageCircle,
  ShieldCheck,
  ShieldQuestion,
  type LucideIcon,
} from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import type {
  GithubComJieWaZiNeuxBotServerApisWorkBoardWorkBoardSummary as Summary,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardEntry as Entry,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardAttention as Attention,
} from "@/shared/api/generated/types.gen";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { OverviewCard } from "@/shared/ui/overview-card";
import { EmptyState } from "@/shared/ui/empty-state";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Skeleton } from "@/shared/ui/skeleton";
import { cn } from "@/shared/lib/cn";
import {
  notificationSnapshotOptions,
  notificationListOptions,
  NotificationRow,
} from "@/modules/notification";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { useVisibleScene } from "@/shared/api/work-result-reading";
import { WorkExecutorAvatar } from "./work-executor-avatar";
import { workAttentionHref, workEntryHref } from "../model/navigation";
import { PaginatedTable } from "@/shared/ui/paginated-table";
import type { PaginationProps } from "@/shared/ui/pagination";
import {
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/shared/ui/table";

export type WorkBoardMode = "all" | "attention" | "results";
export type WorkBoardMetric = "all" | "running" | "attention" | "ended";

const focusVisibleRows = 5;

function ResultTime({ at, label }: { at?: number; label?: string }) {
  const { i18n } = useTranslation("workBoard");
  if (!at) return null;
  const absolute = new Intl.DateTimeFormat(i18n.language, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(at);
  return (
    <time dateTime={new Date(at).toISOString()} className="whitespace-nowrap">
      {label ? label + " · " : ""}
      {absolute}
    </time>
  );
}

function WorkState({ entry }: { entry: Entry }) {
  const { t } = useTranslation("workBoard");
  if (entry.activity === "running")
    return (
      <Badge variant="secondary">
        <LoaderCircle className="animate-spin motion-reduce:animate-none" />
        {t("running")}
      </Badge>
    );
  if (entry.activity === "waiting")
    return (
      <Badge variant="warning">
        <CirclePause />
        {t("waiting")}
      </Badge>
    );
  if (entry.lastStatus === "failed")
    return <Badge variant="destructive">{t("failed")}</Badge>;
  if (entry.lastStatus === "canceled" || entry.lastStatus === "cancelled")
    return (
      <Badge variant="secondary">
        <CircleMinus />
        {t("canceled")}
      </Badge>
    );
  if (entry.lastStatus === "completed" || entry.lastStatus === "succeeded")
    return (
      <Badge variant="success">
        <CircleCheck />
        {t("completed")}
      </Badge>
    );
  return <Badge variant="secondary">{t("idle")}</Badge>;
}

/** 一行围绕工作组织信息，状态和待办独立表达，原结果仍在原详情阅读。 */
export function WorkEntryRow({
  entry,
  result = false,
  compact = false,
}: {
  entry: Entry;
  result?: boolean;
  compact?: boolean;
}) {
  const { t } = useTranslation("workBoard");
  const unread =
    (entry.result?.latestVersion ?? 0) > (entry.result?.viewedVersion ?? 0);
  return (
    <div
      className={cn(
        "flex min-w-0 flex-wrap items-center gap-3",
        compact
          ? "bg-surface-scene min-h-row-rich rounded-md px-3 py-2"
          : "min-h-row-rich p-4",
      )}
    >
      <WorkExecutorAvatar
        name={entry.executorName || t("unknownExecutor")}
        ownerKind={entry.ownerKind}
        executorId={entry.executorId || entry.wakerId}
      />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <Link
            className="type-label focus-visible:ring-ring hover:text-link min-w-0 rounded-sm font-medium break-words outline-none focus-visible:ring-2"
            to={workEntryHref(entry, result)}
          >
            {entry.title || t("untitled")}
          </Link>
          {!compact ? (
            <WorkState entry={entry} />
          ) : entry.lastStatus === "failed" ? (
            <Badge variant="destructive">{t("failed")}</Badge>
          ) : null}
          {unread && !result && !compact ? (
            <Badge variant="success">{t("unread")}</Badge>
          ) : null}
        </div>
        <div className="text-content-secondary type-ui mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>{entry.executorName || t("unknownExecutor")}</span>
          <span>{t(entry.source ?? "chat")}</span>
          <ResultTime
            at={result ? entry.result?.availableAt : entry.updatedAt}
            label={compact ? undefined : t(result ? "resultTime" : "time")}
          />
        </div>
        {!compact &&
        ((entry.approvalCount ?? 0) > 0 ||
          (entry.inputCount ?? 0) > 0 ||
          (entry.reviewCount ?? 0) > 0) ? (
          <div className="type-ui text-warning mt-2 flex flex-wrap gap-x-3 gap-y-1">
            {(entry.approvalCount ?? 0) > 0 ? (
              <span>{t("approvalCount", { count: entry.approvalCount })}</span>
            ) : null}
            {(entry.inputCount ?? 0) > 0 ? (
              <span>{t("inputCount", { count: entry.inputCount })}</span>
            ) : null}
            {(entry.reviewCount ?? 0) > 0 ? (
              <span>{t("reviewCount", { count: entry.reviewCount })}</span>
            ) : null}
          </div>
        ) : null}
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-2">
        {!result && unread && !compact ? (
          <Button asChild size="sm" variant="surface">
            <Link to={workEntryHref(entry, true)}>
              <CheckCheck />
              {t("view")}
            </Link>
          </Button>
        ) : null}
        <Button asChild size="sm" variant="ghost">
          <Link to={workEntryHref(entry, result)}>
            {t(result ? "view" : "open")}
            <ArrowRight />
          </Link>
        </Button>
      </div>
    </div>
  );
}

export function WorkAttentionRow({
  entry,
  compact = false,
}: {
  entry: Attention;
  compact?: boolean;
}) {
  const { t } = useTranslation("workBoard");
  const Icon =
    entry.kind === "review"
      ? FileCheck2
      : entry.kind === "input"
        ? MessageCircle
        : ShieldQuestion;
  return (
    <div
      className={cn(
        "flex min-w-0 flex-wrap items-center gap-3",
        compact
          ? "bg-surface-scene min-h-row-rich rounded-md px-3 py-2"
          : "min-h-row-rich p-4",
      )}
    >
      <span
        className="bg-warning/10 text-warning size-avatar-sm grid shrink-0 place-items-center rounded-lg"
        aria-hidden="true"
      >
        <Icon className="size-icon-md" strokeWidth={1.5} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="type-label font-medium break-words">
            {entry.title || entry.workTitle || t("untitled")}
          </p>
          <Badge variant="warning">{t(entry.kind ?? "input")}</Badge>
        </div>
        <div className="text-content-secondary type-ui mt-1 flex flex-wrap gap-x-3 gap-y-1">
          <span>{entry.workTitle}</span>
          <span>{entry.actorName}</span>
          {!compact ? <span>{t(entry.source ?? "chat")}</span> : null}
          <ResultTime
            at={entry.createdAt}
            label={compact ? undefined : t("waitingTime")}
          />
        </div>
      </div>
      <Button asChild size="sm" variant={compact ? "subtle" : "surface"}>
        <Link to={workAttentionHref(entry)}>
          {t("handle")}
          <ArrowRight />
        </Link>
      </Button>
    </div>
  );
}

/** 精确事项数量作为筛选入口；不从当前页推算全历史总数。 */
export function WorkBoardOverview({
  summary,
  pending,
  error,
  onRetry,
  selected,
  onSelect,
  onClearResults,
  clearingResults,
  clearError,
}: {
  summary?: Summary;
  pending: boolean;
  error?: string;
  onRetry: () => void;
  selected?: WorkBoardMetric;
  onSelect: (metric: WorkBoardMetric) => void;
  onClearResults: () => void;
  clearingResults: boolean;
  clearError?: string;
}) {
  const { t } = useTranslation("workBoard");
  const [focus, setFocus] = useState<"attention" | "results">("attention");
  const active = useVisibleScene();
  const listRef = useRef<HTMLUListElement>(null);
  const viewportRef = useRef<HTMLElement>(null);
  const [focusMaxHeight, setFocusMaxHeight] = useState<number>();
  const notices = useQuery(notificationSnapshotOptions());
  const counts = {
    attention: notices.data?.actionCount,
    results: notices.data
      ? notices.data.unreadResultCount + notices.data.unreadFailureCount
      : undefined,
  };
  const paged = (counts[focus] ?? 0) > focusVisibleRows;
  const focusQuery = useInfiniteQuery({
    ...notificationListOptions(focus === "attention" ? "action" : "unread"),
    enabled: active && paged,
  });
  const noticeEntries =
    paged && focusQuery.data
      ? focusQuery.data.pages.flatMap((page) => page.resources)
      : focus === "attention"
        ? (notices.data?.actionPreview ?? [])
        : (notices.data?.unreadPreview ?? []);
  useLayoutEffect(() => {
    if (viewportRef.current) viewportRef.current.scrollTop = 0;
  }, [focus]);
  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const rows = [...list.children].slice(0, focusVisibleRows);
    const measure = () => {
      const first = rows[0];
      const last = rows[focusVisibleRows - 1];
      if (!first || !last) {
        setFocusMaxHeight(undefined);
        return;
      }
      const styles = getComputedStyle(list);
      const height =
        last.getBoundingClientRect().bottom -
        first.getBoundingClientRect().top +
        (Number.parseFloat(styles.paddingTop) || 0) +
        (Number.parseFloat(styles.paddingBottom) || 0);
      setFocusMaxHeight(height > 0 ? height : undefined);
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    for (const row of rows) observer.observe(row);
    return () => observer.disconnect();
  }, [focus, summary, focusQuery.dataUpdatedAt, noticeEntries.length]);
  const cards: {
    metric: WorkBoardMetric;
    Icon: LucideIcon;
    value?: number;
    description: string;
  }[] = [
    {
      metric: "all",
      Icon: ListTodo,
      value: summary?.workCount,
      description: t("totalHint"),
    },
    {
      metric: "running",
      Icon: LoaderCircle,
      value: summary?.runningCount,
      description: t("runningHint"),
    },
    {
      metric: "attention",
      Icon: ShieldQuestion,
      value: summary?.actionWorkCount,
      description: t("attentionHint"),
    },
    {
      metric: "ended",
      Icon: CircleCheck,
      value: summary?.endedCount,
      description: t("endedHint"),
    },
  ];
  return (
    <section aria-label={t("overview")} className="min-w-0">
      <div className="grid grid-cols-1 gap-3 @[24rem]/resource-page:grid-cols-2 @[48rem]/resource-page:grid-cols-4">
        {cards.map((item) => (
          <OverviewCard
            key={item.metric}
            description={item.description}
            label={t(
              item.metric === "all"
                ? "totalWork"
                : item.metric === "running"
                  ? "runningWork"
                  : item.metric === "ended"
                    ? "endedWork"
                    : "attention",
            )}
            selected={selected === item.metric}
            onClick={() => {
              if (selected === item.metric) return;
              if (item.metric === "attention") setFocus("attention");
              onSelect(item.metric);
            }}
            icon={
              <item.Icon
                className={
                  item.metric === "attention" && (counts.attention ?? 0) > 0
                    ? "text-warning"
                    : undefined
                }
                strokeWidth={1.5}
                aria-hidden="true"
              />
            }
            value={
              typeof item.value !== "number" && pending ? (
                <Skeleton className="h-control-md w-12" />
              ) : typeof item.value !== "number" ? (
                <span className="type-ui text-destructive">
                  {t("loadFailed")}
                </span>
              ) : (
                item.value
              )
            }
          />
        ))}
      </div>
      <section className="mt-6 min-w-0" aria-label={t("focus")}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h2 className="type-title font-semibold">{t("focus")}</h2>
          <div className="flex items-center gap-2">
            {(counts.results ?? 0) > 0 ? (
              <Button
                size="sm"
                variant="ghost"
                disabled={clearingResults}
                aria-busy={clearingResults}
                onClick={onClearResults}
              >
                {t(clearingResults ? "clearingResults" : "clearResults")}
              </Button>
            ) : null}
            <SegmentedControl
              size="sm"
              ariaLabel={t("focus")}
              value={focus}
              onValueChange={setFocus}
              options={(["attention", "results"] as const).map((value) => ({
                value,
                label:
                  counts[value] === undefined
                    ? t(value)
                    : t(value) + " " + counts[value],
              }))}
            />
          </div>
        </div>
        {clearError ? (
          <p role="alert" className="text-destructive type-body mb-3">
            {clearError}
          </p>
        ) : null}
        {/* 少量待办也保留关注区的默认高度，条目自身保持紧凑。 */}
        <section
          ref={viewportRef}
          aria-label={t(focus)}
          aria-busy={pending || (paged && focusQuery.isFetching)}
          tabIndex={0}
          style={focusMaxHeight ? { maxHeight: focusMaxHeight } : undefined}
          onScroll={(event) => {
            const viewport = event.currentTarget;
            if (
              focusQuery.hasNextPage &&
              !focusQuery.isFetching &&
              !focusQuery.isFetchNextPageError &&
              viewport.scrollHeight -
                viewport.scrollTop -
                viewport.clientHeight <=
                viewport.clientHeight
            ) {
              void focusQuery.fetchNextPage();
            }
          }}
          className="scrollbar-hidden bg-muted/60 focus-visible:ring-ring max-h-work-board-focus-list grid min-h-44 min-w-0 content-start gap-2 overflow-y-auto overscroll-contain rounded-md outline-none focus-visible:ring-2"
        >
          {error ? (
            <div
              role="alert"
              className="text-destructive type-ui flex flex-wrap items-center justify-between gap-2 px-3 py-2"
            >
              {error}
              <Button size="sm" variant="surface" onClick={onRetry}>
                {t("retry")}
              </Button>
            </div>
          ) : null}
          {paged && focusQuery.isError ? (
            <div
              role="alert"
              className="text-destructive type-body flex items-center justify-between gap-2 px-3 py-2"
            >
              {localApiErrorMessage(focusQuery.error, t("loadFailed"))}
              <Button
                size="sm"
                variant="surface"
                onClick={() => {
                  if (focusQuery.isFetchNextPageError)
                    void focusQuery.fetchNextPage();
                  else void focusQuery.refetch();
                }}
              >
                {t("retry")}
              </Button>
            </div>
          ) : null}
          {notices.isPending && !notices.data ? (
            <div
              role="status"
              aria-label={t("loadingRecords")}
              className="grid min-h-44 content-center gap-3 px-3 py-2"
            >
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ) : noticeEntries.length ? (
            <ul ref={listRef} className="grid min-w-0 gap-2 p-3">
              {noticeEntries.map((notice) => (
                <li key={notice.id}>
                  <NotificationRow
                    notice={notice}
                    executorAvatar={
                      <WorkExecutorAvatar
                        name={notice.executor.name || t("unknownExecutor")}
                        kind={notice.executor.kind}
                        executorId={notice.executor.id}
                      />
                    }
                  />
                </li>
              ))}
            </ul>
          ) : notices.isError && !notices.data ? (
            <div
              role="alert"
              className="text-destructive type-body flex items-center justify-between gap-2 px-3 py-2"
            >
              {localApiErrorMessage(notices.error, t("loadFailed"))}
              <Button
                size="sm"
                variant="surface"
                onClick={() => void notices.refetch()}
              >
                {t("retry")}
              </Button>
            </div>
          ) : (
            <EmptyState
              density="compact"
              icon={focus === "attention" ? ShieldCheck : CheckCheck}
              title={t(
                focus === "attention" ? "attentionEmpty" : "resultsEmpty",
              )}
              description={t(
                focus === "attention"
                  ? "attentionEmptyHint"
                  : "resultsEmptyHint",
              )}
            />
          )}
          {paged && focusQuery.isFetching ? (
            <p
              role="status"
              className="text-content-secondary type-ui px-3 py-2"
            >
              {t("loadingRecords")}
            </p>
          ) : null}
        </section>
      </section>
    </section>
  );
}

/** 工作记录复用共享表格与分页，空状态仍保留列标题和当前筛选上下文。 */
export function WorkBoardTable({
  mode,
  entries,
  attention,
  pagination,
  empty,
}: {
  mode: WorkBoardMode;
  entries?: Entry[];
  attention?: Attention[];
  pagination?: PaginationProps;
  empty?: ReactNode;
}) {
  const { t } = useTranslation("workBoard");
  const isAttention = mode === "attention";
  return (
    <PaginatedTable
      aria-label={t("records")}
      className="min-w-[880px]"
      pagination={pagination}
    >
      <TableHeader>
        <TableRow>
          {[
            "titleColumn",
            "executor",
            "source",
            isAttention ? "requestKind" : "state",
            mode === "results"
              ? "resultTime"
              : isAttention
                ? "waitingTime"
                : "time",
            "operation",
          ].map((key) => (
            <TableHead key={key}>{t(key)}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {empty ? (
          <TableRow>
            <TableCell colSpan={6}>{empty}</TableCell>
          </TableRow>
        ) : isAttention ? (
          attention?.map((entry) => (
            <TableRow key={entry.id} className="hover:bg-muted/50">
              <TableCell className="max-w-64">
                <Link
                  className="hover:text-link font-medium"
                  to={workAttentionHref(entry)}
                >
                  {entry.title || t("untitled")}
                </Link>
                <p className="type-caption text-content-secondary mt-1">
                  {entry.workTitle}
                </p>
              </TableCell>
              <TableCell>
                <WorkExecutorAvatar
                  name={entry.actorName || t("unknownExecutor")}
                  kind={
                    entry.wakerId
                      ? "waker"
                      : entry.ownerKind === "workflow_run"
                        ? "workflow"
                        : "team"
                  }
                  executorId={entry.wakerId || entry.executorId}
                  size="xs"
                />
              </TableCell>
              <TableCell className="whitespace-nowrap">
                {t(entry.source ?? "chat")}
              </TableCell>
              <TableCell>
                <Badge variant="warning">{t(entry.kind ?? "input")}</Badge>
              </TableCell>
              <TableCell>
                <ResultTime at={entry.createdAt} />
              </TableCell>
              <TableCell>
                <Button asChild variant="ghost" size="sm">
                  <Link to={workAttentionHref(entry)}>{t("handle")}</Link>
                </Button>
              </TableCell>
            </TableRow>
          ))
        ) : (
          entries?.map((entry) => {
            const unread =
              (entry.result?.latestVersion ?? 0) >
              (entry.result?.viewedVersion ?? 0);
            return (
              <TableRow
                key={entry.ownerKind + ":" + entry.ownerId}
                className="hover:bg-muted/50"
              >
                <TableCell className="max-w-64">
                  <Link
                    className="hover:text-link font-medium"
                    to={workEntryHref(entry, mode === "results")}
                  >
                    {entry.title || t("untitled")}
                  </Link>
                  {unread ? (
                    <p className="type-caption text-success mt-1">
                      {t("unread")}
                    </p>
                  ) : null}
                </TableCell>
                <TableCell>
                  <span className="inline-flex items-center gap-2 whitespace-nowrap">
                    <WorkExecutorAvatar
                      name={entry.executorName || t("unknownExecutor")}
                      ownerKind={entry.ownerKind}
                      executorId={entry.executorId || entry.wakerId}
                      size="xs"
                    />
                  </span>
                </TableCell>
                <TableCell className="whitespace-nowrap">
                  {t(entry.source ?? "chat")}
                </TableCell>
                <TableCell>
                  <WorkState entry={entry} />
                </TableCell>
                <TableCell>
                  <ResultTime
                    at={
                      mode === "results"
                        ? entry.result?.availableAt
                        : entry.updatedAt
                    }
                  />
                </TableCell>
                <TableCell>
                  <Button asChild variant="ghost" size="sm">
                    <Link
                      to={workEntryHref(entry, mode === "results" || unread)}
                    >
                      {t(mode === "results" || unread ? "view" : "open")}
                    </Link>
                  </Button>
                </TableCell>
              </TableRow>
            );
          })
        )}
      </TableBody>
    </PaginatedTable>
  );
}
