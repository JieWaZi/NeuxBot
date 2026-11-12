import {
  ArrowRight,
  ListTodo,
  CheckCheck,
  CircleCheck,
  CircleMinus,
  CirclePause,
  LoaderCircle,
  ShieldCheck,
  ShieldQuestion,
  type LucideIcon,
} from "lucide-react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardAttention as Attention } from "@/shared/api/generated/types.gen";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { OverviewCard } from "@/shared/ui/overview-card";
import { EmptyState } from "@/shared/ui/empty-state";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  refreshWorkBoardFocus,
  WorkBoardFocusIncomplete,
  workBoardFocusHeadLimit,
  workBoardFocusOptions,
  type WorkBoardMode,
  type WorkBoardSummary as Summary,
  type WorkBoardEntry as Entry,
} from "../api/queries";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { ChatReadingLink } from "@/shared/api/chat-reading";
import {
  refreshQueryFromEvent,
  useQueryEvents,
} from "@/shared/api/use-query-events";
import { workspaceAttentionResource } from "@/shared/api/reading-queries";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
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

export type WorkBoardMetric = "all" | "running" | "action" | "ended";

function ResultTime({ at, compact }: { at?: number; compact?: boolean }) {
  const { i18n } = useTranslation("workBoard");
  if (!at) return null;
  const absolute = new Intl.DateTimeFormat(
    i18n.language,
    compact
      ? { month: "numeric", day: "numeric", hour: "numeric", minute: "2-digit" }
      : { dateStyle: "medium", timeStyle: "short" },
  ).format(at);
  return (
    <time dateTime={new Date(at).toISOString()} className="whitespace-nowrap">
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
      <Badge variant="success" className="bg-success/6">
        <CircleCheck />
        {t("completed")}
      </Badge>
    );
  return <Badge variant="secondary">{t("idle")}</Badge>;
}

function WorkFocusRow({
  avatar,
  title,
  context,
  at,
  href,
  chatId,
  failed,
  action,
}: {
  avatar: ReactNode;
  title: string;
  context?: string;
  at?: number;
  href: string;
  chatId?: string;
  failed?: boolean;
  action: boolean;
}) {
  const { t } = useTranslation("workBoard");
  return (
    <div className="bg-surface-raised min-h-row-rich flex min-w-0 flex-wrap items-center gap-3 rounded-md px-4 py-2">
      {avatar}
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="type-label font-medium break-words">
            <ChatReadingLink
              chatId={chatId}
              className="hover:text-link"
              to={href}
            >
              {title}
            </ChatReadingLink>
          </p>
          {failed ? <Badge variant="destructive">{t("failed")}</Badge> : null}
        </div>
        {context || at ? (
          <div className="text-content-secondary type-ui mt-1 flex flex-wrap gap-x-3 gap-y-1">
            {context ? <span>{context}</span> : null}
            <ResultTime at={at} compact />
          </div>
        ) : null}
      </div>
      <Button
        asChild
        size="sm"
        variant={action ? "ghost" : "subtle"}
        className={
          action
            ? undefined
            : "bg-(--action-neutral-surface) hover:bg-(--action-neutral-hover) active:bg-(--action-neutral-pressed)"
        }
      >
        <ChatReadingLink chatId={chatId} to={href}>
          {t(action ? "handle" : "view")}
          <ArrowRight />
        </ChatReadingLink>
      </Button>
    </div>
  );
}

function WorkEntryRow({ entry }: { entry: Entry }) {
  const { t } = useTranslation("workBoard");
  const title = entry.title || t("untitled");
  return (
    <WorkFocusRow
      avatar={
        <WorkExecutorAvatar
          name={entry.executorName ?? ""}
          kind={
            entry.ownerKind === "automatic_run"
              ? entry.workflowId
                ? "workflow"
                : entry.channelId
                  ? "team"
                  : entry.wakerId
                    ? "waker"
                    : undefined
              : undefined
          }
          ownerKind={entry.ownerKind}
          executorId={entry.executorId || entry.wakerId}
        />
      }
      title={title}
      context={entry.executorName !== title ? entry.executorName : undefined}
      at={entry.updatedAt}
      href={workEntryHref(entry, true)}
      chatId={entry.chatId}
      failed={entry.lastStatus === "failed"}
      action={false}
    />
  );
}

function WorkAttentionRow({ entry }: { entry: Attention }) {
  const { t } = useTranslation("workBoard");
  const title = entry.title || entry.workTitle || t("untitled");
  const context = [entry.workTitle, entry.actorName].find(
    (name) => name && name !== title,
  );
  return (
    <WorkFocusRow
      avatar={
        <WorkExecutorAvatar
          name={entry.actorName ?? ""}
          kind={
            entry.wakerId ? "waker" : entry.workflowId ? "workflow" : undefined
          }
          ownerKind={entry.ownerKind}
          executorId={
            entry.wakerId ||
            entry.executorId ||
            entry.workflowId ||
            entry.channelId
          }
        />
      }
      title={title}
      context={context}
      at={entry.createdAt}
      href={workAttentionHref(entry)}
      action
    />
  );
}

const focusVisibleRows = 5;
type WorkBoardFocus = "attention" | "results";

function useWorkBoardFocusEvents(
  mode: Exclude<WorkBoardMode, "all">,
  enabled: boolean,
  paused: boolean,
  isBrowsingDeep: () => boolean,
) {
  const client = useQueryClient();
  const read = useCallback(
    () => refreshWorkBoardFocus(client, mode, isBrowsingDeep),
    [client, mode, isBrowsingDeep],
  );
  const { queryKey } = workBoardFocusOptions(mode);
  useQueryEvents({
    topic: "work-board",
    resource: workspaceAttentionResource,
    queryKeys: [queryKey],
    eventTypes: ["work-board.changed"],
    authority: true,
    enabled,
    active: enabled && !paused,
    read,
  });
  return () => refreshQueryFromEvent(client, queryKey, read);
}

/** 精确事项数量作为筛选入口；不从当前页推算全历史总数。 */
export function WorkBoardOverview({
  summary,
  pending,
  active,
  mode,
  error,
  onRetry,
  selected,
  onSelect,
  markViewed,
}: {
  summary?: Summary;
  pending: boolean;
  active: boolean;
  mode: WorkBoardMode;
  error?: string;
  onRetry: () => void;
  selected?: WorkBoardMetric;
  onSelect: (metric: WorkBoardMetric) => void;
  markViewed: ReactNode;
}) {
  const { t } = useTranslation("workBoard");
  const sceneActive = useRouteSceneActive();
  const navigationState: unknown = useLocation().state;
  const openToken =
    navigationState &&
    typeof navigationState === "object" &&
    "workBoardOpenToken" in navigationState &&
    typeof navigationState.workBoardOpenToken === "string"
      ? navigationState.workBoardOpenToken
      : undefined;
  const counts = {
    attention: summary?.pendingRequestCount,
    results: summary?.awaitingViewWorkCount,
  };
  const defaultFocus: WorkBoardFocus =
    mode === "awaiting_view" ||
    (mode === "all" && counts.attention === 0 && (counts.results ?? 0) > 0)
      ? "results"
      : "attention";
  const [selection, setSelection] = useState<{
    mode: WorkBoardMode;
    focus: WorkBoardFocus;
    openToken?: string;
    explicit: boolean;
    deep: boolean;
  }>();
  // 默认页签随摘要对账，显式选择和正在浏览的深页保留当前分组。
  if (
    selection &&
    (!sceneActive ||
      selection.mode !== mode ||
      (openToken !== undefined && selection.openToken !== openToken))
  ) {
    setSelection(undefined);
  }
  const focus = selection?.focus ?? defaultFocus;
  const focusMode = focus === "attention" ? "action" : "awaiting_view";
  const deep = Boolean(selection?.deep);
  const paged = deep || (counts[focus] ?? 0) > focusVisibleRows;
  const listRef = useRef<HTMLUListElement>(null);
  const viewportRef = useRef<HTMLElement>(null);
  const isBrowsingDeep = useCallback(() => {
    const viewport = viewportRef.current;
    const boundary = listRef.current?.children.item(workBoardFocusHeadLimit);
    return Boolean(
      viewport &&
      viewport.scrollTop > 0 &&
      boundary &&
      boundary.getBoundingClientRect().top <=
        viewport.getBoundingClientRect().top,
    );
  }, []);
  const chooseFocus = (value: WorkBoardFocus) =>
    setSelection({
      mode,
      focus: value,
      openToken,
      explicit: true,
      deep: value === focus && deep,
    });
  const refreshFocus = useWorkBoardFocusEvents(
    focusMode,
    active && paged,
    deep,
    isBrowsingDeep,
  );
  const focusQuery = useInfiniteQuery({
    ...workBoardFocusOptions(focusMode),
    enabled: active && paged,
  });
  const pages = paged ? focusQuery.data?.pages : undefined;
  const focusIncomplete =
    focusQuery.error instanceof WorkBoardFocusIncomplete ||
    pages?.some((page) => page.complete === false);
  const actionRows =
    focus === "attention"
      ? (pages?.flatMap((page) => page.attention ?? []) ??
        summary?.actionPreview ??
        [])
      : [];
  const resultRows =
    focus === "results"
      ? (pages?.flatMap((page) => page.awaitingView ?? []) ??
        summary?.awaitingViewPreview ??
        [])
      : [];
  const rowCount = actionRows.length + resultRows.length;
  const [focusMaxHeight, setFocusMaxHeight] = useState<number>();

  useLayoutEffect(() => {
    if (viewportRef.current) viewportRef.current.scrollTop = 0;
  }, [focus, sceneActive]);
  useLayoutEffect(() => {
    if (openToken && viewportRef.current) viewportRef.current.scrollTop = 0;
  }, [openToken]);
  useLayoutEffect(() => {
    const list = listRef.current;
    const rows = [...(list?.children ?? [])].slice(0, focusVisibleRows);
    const first = rows[0];
    const last = rows[focusVisibleRows - 1];
    if (!list || !first || !last) {
      setFocusMaxHeight(undefined);
      return;
    }
    const measure = () => {
      // 实际行高和行间距一起计入五条可见记录的高度。
      const height =
        last.getBoundingClientRect().bottom - first.getBoundingClientRect().top;
      setFocusMaxHeight(height > 0 ? height : undefined);
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    for (const row of rows) observer.observe(row);
    return () => observer.disconnect();
  }, [focus, summary, focusQuery.dataUpdatedAt, rowCount]);

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
      metric: "action",
      Icon: ShieldQuestion,
      value: summary?.waitingWorkCount,
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
            className="bg-surface-raised"
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
              if (item.metric === "action" && selected === item.metric)
                chooseFocus("attention");
              if (selected !== item.metric) onSelect(item.metric);
            }}
            icon={
              <item.Icon
                className={
                  item.metric === "action" &&
                  (summary?.waitingWorkCount ?? 0) > 0
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
      {error || summary?.complete === false ? (
        <div
          role="status"
          className="text-content-secondary type-ui mt-4 flex items-center gap-2"
        >
          {error ?? t("attentionIncomplete")}
          <Button size="sm" variant="ghost" onClick={onRetry}>
            {t("retry")}
          </Button>
        </div>
      ) : null}
      <section className="mt-6 min-w-0" aria-label={t("focus")}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h2 className="type-title font-semibold">{t("focus")}</h2>
          <div className="flex flex-wrap items-center gap-2">
            {markViewed}
            <SegmentedControl
              size="sm"
              ariaLabel={t("focus")}
              value={focus}
              onValueChange={chooseFocus}
              options={(["attention", "results"] as const).map((value) => ({
                value,
                label:
                  counts[value] === undefined
                    ? t(value)
                    : `${t(value)} ${counts[value]}`,
              }))}
            />
          </div>
        </div>
        <section
          ref={viewportRef}
          aria-label={t(focus)}
          aria-busy={pending || (paged && focusQuery.isFetching)}
          tabIndex={0}
          style={focusMaxHeight ? { maxHeight: focusMaxHeight } : undefined}
          onScroll={(event) => {
            const viewport = event.currentTarget;
            const nextDeep = isBrowsingDeep();
            if (nextDeep !== deep)
              setSelection(
                nextDeep || selection?.explicit
                  ? {
                      mode,
                      focus,
                      openToken,
                      explicit: Boolean(selection?.explicit),
                      deep: nextDeep,
                    }
                  : undefined,
              );
            if (
              focusQuery.hasNextPage &&
              !focusQuery.isFetching &&
              !focusQuery.isError &&
              !focusIncomplete &&
              viewport.scrollHeight -
                viewport.scrollTop -
                viewport.clientHeight <=
                viewport.clientHeight
            )
              void focusQuery.fetchNextPage({ cancelRefetch: false });
          }}
          className="focus-visible:ring-ring max-h-work-board-focus-list grid min-w-0 content-start gap-2 overflow-y-auto overscroll-y-auto rounded-md outline-none focus-visible:ring-2 focus-visible:ring-inset"
        >
          {paged && (focusQuery.isError || focusIncomplete) ? (
            <div
              role="alert"
              className="bg-surface-raised text-destructive type-body flex items-center justify-between gap-2 rounded-md px-3 py-2"
            >
              {focusIncomplete
                ? t("attentionIncomplete")
                : localApiErrorMessage(focusQuery.error, t("loadFailed"))}
              <Button
                size="sm"
                variant="surface"
                disabled={focusQuery.isFetching}
                onClick={() => {
                  if (focusQuery.isFetchNextPageError)
                    void focusQuery.fetchNextPage({ cancelRefetch: false });
                  else void refreshFocus().catch(() => {});
                }}
              >
                {t("retry")}
              </Button>
            </div>
          ) : null}
          {rowCount ? (
            <ul ref={listRef} className="grid min-w-0 gap-2">
              {actionRows.map((entry) => (
                <li key={entry.id}>
                  <WorkAttentionRow entry={entry} />
                </li>
              ))}
              {resultRows.map((entry) => (
                <li key={`${entry.ownerKind}:${entry.ownerId}`}>
                  <WorkEntryRow entry={entry} />
                </li>
              ))}
            </ul>
          ) : pending || (paged && focusQuery.isPending) ? (
            <div
              role="status"
              aria-label={t("loadingRecords")}
              className="bg-surface-raised grid min-h-44 content-center gap-3 rounded-md px-3 py-2"
            >
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ) : !focusIncomplete &&
            summary?.complete &&
            (!paged || counts[focus] === 0) ? (
            <EmptyState
              className="bg-surface-raised rounded-md"
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
  const isAttention = mode === "action";
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
            mode === "awaiting_view"
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
                  name={entry.actorName ?? ""}
                  kind={entry.wakerId ? "waker" : undefined}
                  ownerKind={entry.ownerKind}
                  executorId={
                    entry.wakerId ||
                    entry.executorId ||
                    entry.workflowId ||
                    entry.channelId
                  }
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
            const unread = Boolean(entry.unreadMessageCount);
            return (
              <TableRow
                key={entry.ownerKind + ":" + entry.ownerId}
                className="hover:bg-muted/50"
              >
                <TableCell className="max-w-64">
                  <ChatReadingLink
                    chatId={entry.chatId}
                    className="hover:text-link font-medium"
                    to={workEntryHref(entry, mode === "awaiting_view")}
                  >
                    {entry.title || t("untitled")}
                  </ChatReadingLink>
                  {unread ? (
                    <p className="type-caption text-success mt-1">
                      {t("unread")}
                    </p>
                  ) : null}
                </TableCell>
                <TableCell>
                  <span className="inline-flex items-center gap-2 whitespace-nowrap">
                    <WorkExecutorAvatar
                      name={entry.executorName ?? ""}
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
                  <ResultTime at={entry.updatedAt} />
                </TableCell>
                <TableCell>
                  <Button asChild variant="ghost" size="sm">
                    <ChatReadingLink
                      chatId={entry.chatId}
                      to={workEntryHref(
                        entry,
                        mode === "awaiting_view" || unread,
                      )}
                    >
                      {t(mode === "awaiting_view" || unread ? "view" : "open")}
                    </ChatReadingLink>
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
