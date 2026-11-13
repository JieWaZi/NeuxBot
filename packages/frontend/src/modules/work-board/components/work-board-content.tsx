import {
  ArrowRight,
  CheckCheck,
  ShieldCheck,
  CircleCheck,
  CircleMinus,
  CirclePause,
  LoaderCircle,
} from "lucide-react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardAttention as Attention } from "@/shared/api/generated/types.gen";
import { EmptyState } from "@/shared/ui/empty-state";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
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
  status,
  action,
}: {
  avatar: ReactNode;
  title: string;
  context?: string;
  at?: number;
  href: string;
  chatId?: string;
  status?: ReactNode;
  action: boolean;
}) {
  const { t } = useTranslation("workBoard");
  return (
    <div className="bg-surface-raised min-h-row-rich flex min-w-0 flex-wrap items-center gap-3 rounded-md px-4 py-2">
      {avatar}
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="type-label min-w-0 font-medium break-words">
            <ChatReadingLink
              chatId={chatId}
              className="hover:text-link"
              to={href}
            >
              {title}
            </ChatReadingLink>
          </p>
          {status}
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
      status={
        entry.lastStatus === "failed" ? (
          <Badge variant="destructive">{t("failed")}</Badge>
        ) : null
      }
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
  expanded: boolean,
  active: boolean,
  isBrowsingDeep: () => boolean,
) {
  const client = useQueryClient();
  const read = useCallback(
    () => refreshWorkBoardFocus(client, mode, isBrowsingDeep),
    [client, mode, isBrowsingDeep],
  );
  useQueryEvents({
    topic: "work-board",
    resource: workspaceAttentionResource,
    queryKeys: expanded ? [workBoardFocusOptions(mode).queryKey] : [],
    eventTypes: ["work-board.changed"],
    authority: true,
    active,
    read,
  });
  return useCallback(
    (signal?: AbortSignal) =>
      refreshQueryFromEvent(
        client,
        workBoardFocusOptions(mode).queryKey,
        read,
        0,
        signal,
      ),
    [client, mode, read],
  );
}

export function WorkBoardOverview({
  summary,
  pending,
  active,
  error,
  onRetry,
}: {
  summary?: Summary;
  pending: boolean;
  active: boolean;
  error?: string;
  onRetry: () => void;
}) {
  const { t } = useTranslation("workBoard");
  const navigationState: unknown = useLocation().state;
  const openToken =
    navigationState &&
    typeof navigationState === "object" &&
    "workBoardOpenToken" in navigationState &&
    typeof navigationState.workBoardOpenToken === "string"
      ? navigationState.workBoardOpenToken
      : undefined;
  return (
    <section aria-label={t("currentWork")} className="min-w-0">
      <h2 className="type-title mb-3 font-semibold">{t("currentWork")}</h2>
      {error || summary?.complete === false ? (
        <div
          role="status"
          className="text-content-secondary type-ui mb-3 flex items-center gap-2"
        >
          {error ?? t("attentionIncomplete")}
          <Button size="sm" variant="ghost" onClick={onRetry}>
            {t("retry")}
          </Button>
        </div>
      ) : null}
      <div className="grid min-w-0 gap-3 @[48rem]/resource-page:grid-cols-2">
        <WorkBoardFocusPanel
          focus="attention"
          summary={summary}
          pending={pending}
          active={active}
          openToken={openToken}
        />
        <WorkBoardFocusPanel
          focus="results"
          summary={summary}
          pending={pending}
          active={active}
          openToken={openToken}
        />
      </div>
    </section>
  );
}

function WorkBoardFocusPanel({
  focus,
  summary,
  pending,
  active,
  openToken,
}: {
  focus: WorkBoardFocus;
  summary?: Summary;
  pending: boolean;
  active: boolean;
  openToken?: string;
}) {
  const { t } = useTranslation("workBoard");
  const sceneActive = useRouteSceneActive();
  const isAction = focus === "attention";
  const focusMode = isAction ? "action" : "awaiting_view";
  const count = isAction
    ? summary?.pendingRequestCount
    : summary?.awaitingViewWorkCount;
  const label = t(isAction ? "needsAction" : "results");
  const client = useQueryClient();
  const focusOptions = workBoardFocusOptions(focusMode);
  const focusQuery = useInfiniteQuery({ ...focusOptions, enabled: false });
  const { dataUpdateCount = 0, errorUpdateCount = 0 } =
    client.getQueryState(focusOptions.queryKey) ?? {};
  const [expansion, setExpansion] = useState<{
    openToken?: string;
    dataUpdateCount: number;
    errorUpdateCount: number;
    deep: boolean;
  }>();
  const inScene = sceneActive && expansion && expansion.openToken === openToken;
  const canExpand = (count ?? 0) > focusVisibleRows;
  const deep = Boolean(inScene && expansion?.deep);
  const paged = Boolean(
    inScene && (canExpand || deep || summary?.complete !== true),
  );
  if (expansion && !paged) setExpansion(undefined);
  const listRef = useRef<HTMLUListElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  const viewportRef = useRef<HTMLDivElement>(null);
  const isBrowsingDeep = useCallback(() => {
    const viewport = viewportRef.current;
    const boundary = listRef.current?.children.item(workBoardFocusHeadLimit);
    return Boolean(
      viewport &&
      viewport.scrollTop > 0 &&
      boundary &&
      boundary.getBoundingClientRect().top <=
        (headerRef.current?.getBoundingClientRect().bottom ??
          viewport.getBoundingClientRect().top),
    );
  }, []);
  const refreshFocus = useWorkBoardFocusEvents(
    focusMode,
    paged,
    active && paged && !deep,
    isBrowsingDeep,
  );
  // 首次展开沿用摘要预览，直到当前展开取得新的权威页面。
  const pages =
    paged && expansion && dataUpdateCount > expansion.dataUpdateCount
      ? focusQuery.data?.pages
      : undefined;
  const focusError =
    paged && expansion && errorUpdateCount > expansion.errorUpdateCount
      ? focusQuery.error
      : null;
  const waitingForFocus = paged && !pages && !focusError;
  useEffect(() => {
    if (!active || !waitingForFocus) return;
    const controller = new AbortController();
    void refreshFocus(controller.signal).catch(() => {});
    return () => controller.abort();
  }, [active, refreshFocus, waitingForFocus]);
  const focusIncomplete =
    focusError instanceof WorkBoardFocusIncomplete ||
    pages?.some((page) => page.complete === false);
  const actionRows = isAction
    ? (pages?.flatMap((page) => page.attention ?? []) ??
      summary?.actionPreview ??
      [])
    : [];
  const resultRows = isAction
    ? []
    : (pages?.flatMap((page) => page.awaitingView ?? []) ??
      summary?.awaitingViewPreview ??
      []);
  const rowCount = actionRows.length + resultRows.length;
  const [focusMaxHeight, setFocusMaxHeight] = useState<number>();

  useLayoutEffect(() => {
    if (viewportRef.current) viewportRef.current.scrollTop = 0;
  }, [openToken, sceneActive]);
  useLayoutEffect(() => {
    const rows = [...(listRef.current?.children ?? [])].slice(
      0,
      focusVisibleRows,
    );
    const last = rows[focusVisibleRows - 1];
    const viewport = viewportRef.current;
    if (!last || !viewport) {
      setFocusMaxHeight(undefined);
      return;
    }
    const measure = () => {
      const padding =
        Number.parseFloat(getComputedStyle(viewport).paddingBottom) || 0;
      const height =
        last.getBoundingClientRect().bottom -
        viewport.getBoundingClientRect().top +
        viewport.scrollTop +
        padding;
      setFocusMaxHeight(height > 0 ? Math.ceil(height) : undefined);
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    if (headerRef.current) observer.observe(headerRef.current);
    if (listRef.current) observer.observe(listRef.current);
    for (const row of rows) observer.observe(row);
    return () => observer.disconnect();
  }, [rowCount, summary, focusError, focusQuery.dataUpdatedAt]);

  const loading = pending || waitingForFocus;
  return (
    <section className="min-w-0">
      <div
        ref={viewportRef}
        role="region"
        aria-label={label}
        aria-busy={loading || (paged && focusQuery.isFetching)}
        tabIndex={canExpand || rowCount >= focusVisibleRows ? 0 : undefined}
        style={focusMaxHeight ? { maxHeight: focusMaxHeight } : undefined}
        onScroll={(event) => {
          const viewport = event.currentTarget;
          if (!active || !expansion || !pages) return;
          const nextDeep = isBrowsingDeep();
          if (nextDeep !== deep) setExpansion({ ...expansion, deep: nextDeep });
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
        className="focus-visible:ring-ring max-h-work-board-focus-list min-h-work-board-focus-min bg-card flex min-w-0 flex-col gap-2 overflow-y-auto overscroll-y-auto rounded-md px-3 pb-2 outline-none focus-visible:ring-2 focus-visible:ring-inset"
      >
        <div
          ref={headerRef}
          className="bg-card sticky top-0 z-10 flex min-w-0 shrink-0 flex-wrap items-center justify-between gap-2 pt-2 pb-1"
        >
          <h3 className="type-label flex min-w-0 flex-wrap items-center gap-2 font-semibold">
            {label}
            {typeof count === "number" ? (
              <span className="text-content-secondary type-ui tabular-nums">
                {count}
              </span>
            ) : pending ? (
              <Skeleton className="h-4 w-6" />
            ) : null}
          </h3>
          {canExpand && !paged ? (
            <Button
              size="sm"
              variant="ghost"
              onClick={(event) => {
                if (event.detail === 0)
                  viewportRef.current?.focus({ preventScroll: true });
                setExpansion({
                  openToken,
                  dataUpdateCount,
                  errorUpdateCount,
                  deep: false,
                });
              }}
            >
              {t("more")}
            </Button>
          ) : null}
        </div>
        {focusError || focusIncomplete ? (
          <div
            role="alert"
            className="bg-surface-raised text-destructive type-body flex items-center justify-between gap-2 rounded-md px-3 py-2"
          >
            {focusIncomplete
              ? t("attentionIncomplete")
              : localApiErrorMessage(focusError, t("loadFailed"))}
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
          <ul ref={listRef} className="grid min-w-0 shrink-0 gap-2">
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
        ) : loading ? (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("loadingRecords")}
            className="grid min-h-24 flex-1 content-center gap-3 px-3 py-2"
          >
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-4 w-1/2" />
          </div>
        ) : !focusIncomplete && summary?.complete && (!paged || count === 0) ? (
          <EmptyState
            className="min-h-24 flex-1"
            density="compact"
            icon={isAction ? ShieldCheck : CheckCheck}
            title={t(isAction ? "attentionEmpty" : "resultsEmpty")}
            description={t(
              isAction ? "attentionEmptyHint" : "resultsEmptyHint",
            )}
          />
        ) : null}
      </div>
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
