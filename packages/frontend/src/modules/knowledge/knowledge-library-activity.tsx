import { useState } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { ChevronDown, ListFilter, RefreshCw } from "lucide-react";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type { ListLibraryActivityResponse } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import { Badge } from "@/shared/ui/badge";
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/shared/ui/table";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from "@/shared/ui/dropdown-menu";
import { IconAction, SectionTitle } from "./knowledge-controls";
import { jobsOptions, knowledgeKeys } from "./knowledge-queries";
import { KnowledgeCurrentActivity } from "./knowledge-current-activity";
import { KnowledgeActivityTaskResult } from "./knowledge-activity-task-result";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import type { Library } from "./knowledge-model";
import { useCopy } from "./use-extended-copy";

const actions = [
  ["kb.created", "activityKBCreated"],
  ["kb.updated", "activityKBUpdated"],
  ["kb.deleted", "activityKBDeleted"],
  ["knowledge.created", "activitySourceCreated"],
  ["knowledge.updated", "activitySourceUpdated"],
  ["knowledge.deleted", "activitySourceDeleted"],
  ["knowledge.reparse_started", "activityOrganizeStarted"],
  ["knowledge.parse_canceled", "activityOrganizeCancelled"],
  ["wiki.content_changed", "activityWikiChanged"],
  ["wiki.maintenance", "activityWikiMaintenance"],
] as const;
const outcomes = [
  ["accepted", "activityAccepted"],
  ["success", "activitySuccess"],
  ["failed", "activityFailed"],
  ["partial", "activityPartial"],
  ["canceled", "activityCancelled"],
  ["denied", "activityDenied"],
] as const;
type Entry = NonNullable<ListLibraryActivityResponse["data"]>[number];

function details(entry: Entry): Record<string, unknown> {
  const value: unknown = entry.details;
  if (typeof value === "string") {
    try {
      const decoded: unknown = JSON.parse(value);
      return decoded && typeof decoded === "object" && !Array.isArray(decoded)
        ? (decoded as Record<string, unknown>)
        : {};
    } catch {
      return {};
    }
  }
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

/** 历史沿用服务端过滤与游标；当前任务单独读取既有快照，不合成审计记录。 */
export function KnowledgeLibraryActivity({ library }: { library: Library }) {
  const t = useCopy();
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const [action, setAction] = useState("");
  const [outcome, setOutcome] = useState("");
  const key = knowledgeKeys.activity(library.ref, action, outcome);
  const local = knowledgeAdapter(library.ref).localConfiguration;
  const jobs = useQuery({
    ...jobsOptions(library.ref, { localErrorSurface: true }),
    enabled: local && active,
  });
  const query = useInfiniteQuery({
    queryKey: key,
    initialPageParam: 0,
    queryFn: async ({ pageParam, signal }) =>
      (
        await knowledgeAdapter(library.ref).listLibraryActivity(
          withLocalErrorSurface({
            path: { libraryId: library.id },
            query: {
              afterId: pageParam || undefined,
              limit: 10,
              action: action || undefined,
              outcome: outcome || undefined,
            },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
    getNextPageParam: (last) => last.nextCursor || undefined,
    staleTime: 0,
    enabled: active,
  });
  const rows = query.data?.pages.flatMap((page) => page.data ?? []) ?? [];
  const actionLabel = (value: string) => {
    const found = actions.find(([key]) => key === value);
    return found ? t(found[1]) : value;
  };
  const outcomeLabel = (value: string) => {
    const found = outcomes.find(([key]) => key === value);
    return found ? t(found[1]) : value;
  };
  const filter = (
    label: string,
    value: string,
    options: ReadonlyArray<readonly [string, string]>,
    change: (value: string) => void,
  ) => (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" size="icon-sm" variant="ghost" aria-label={label}>
          <ListFilter
            className={
              value ? "text-content-primary" : "text-content-secondary"
            }
          />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuRadioGroup value={value} onValueChange={change}>
          <DropdownMenuRadioItem value="">{t("all")}</DropdownMenuRadioItem>
          {options.map(([id, name]) => (
            <DropdownMenuRadioItem key={id} value={id}>
              {name}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
  return (
    <section className="grid min-w-0 content-start gap-4">
      <div className="flex items-center gap-2">
        <SectionTitle title={t("activitySection")} />
        <IconAction
          icon={RefreshCw}
          label={t("refreshActivity")}
          disabled={query.isFetching || jobs.isFetching}
          onClick={() => {
            void client.resetQueries({ queryKey: key, exact: true });
            if (local) void jobs.refetch();
          }}
        />
      </div>
      <p className="type-caption text-content-secondary">{t("activityHelp")}</p>
      {local && (
        <KnowledgeCurrentActivity
          job={jobs.data?.activeJob}
          pending={jobs.isPending}
          error={Boolean(jobs.error)}
        />
      )}
      {query.error && (
        <p role="alert" className="type-caption text-destructive">
          {t("activityLoadFailed")}
        </p>
      )}
      <Table aria-label={t("activitySection")} className="min-w-xl table-fixed">
        <colgroup>
          <col className="w-28" />
          <col className="w-36" />
          <col />
          <col className="w-28" />
          <col className="w-24" />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHead>{t("activityTime")}</TableHead>
            <TableHead>
              <div className="flex items-center gap-1">
                {t("activityAction")}
                {filter(
                  t("filterActivityAction"),
                  action,
                  actions.map(([id]) => [id, actionLabel(id)]),
                  setAction,
                )}
              </div>
            </TableHead>
            <TableHead>{t("activityObject")}</TableHead>
            <TableHead>{t("activityActor")}</TableHead>
            <TableHead>
              <div className="flex items-center gap-1">
                {t("activityOutcome")}
                {filter(
                  t("filterActivityOutcome"),
                  outcome,
                  outcomes.map(([id]) => [id, outcomeLabel(id)]),
                  setOutcome,
                )}
              </div>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((entry) => {
            const meta = details(entry);
            const maintenance = entry.targetType === "wiki_maintenance_task";
            const wiki =
              entry.targetType === "wiki" || entry.targetType === "wiki_page";
            const titles = [
              ...new Set(
                [
                  meta.title,
                  ...(Array.isArray(meta.titles) ? meta.titles : []),
                  meta.name,
                ]
                  .filter(
                    (value): value is string =>
                      typeof value === "string" && Boolean(value.trim()),
                  )
                  .map((value) => value.trim()),
              ),
            ].slice(0, 3);
            const changed =
              typeof meta.count === "number" &&
              Number.isSafeInteger(meta.count) &&
              meta.count >= 0
                ? meta.count
                : undefined;
            const title = maintenance
              ? t("activityWikiMaintenanceTask")
              : titles.length > 0
                ? wiki && changed !== undefined && changed > titles.length
                  ? t("activityPageTitles", {
                      titles: titles.join("、"),
                      count: changed,
                    })
                  : titles.join("、")
                : entry.targetType === "knowledge_base"
                  ? library.name
                  : entry.targetType === "knowledge"
                    ? t("activityDocument")
                    : entry.targetType === "knowledge_task"
                      ? t("activityTask")
                      : entry.targetType === "wiki"
                        ? t("activityWiki")
                        : entry.targetType === "wiki_page"
                          ? t("activityWikiPage")
                          : t("activityTargetUnknown");
            const counts = (
              maintenance ? ["changed_pages"] : ["count", "total"]
            ).flatMap((key) => {
              const count = meta[key];
              return typeof count === "number" &&
                Number.isSafeInteger(count) &&
                count >= 0
                ? [
                    t(
                      key === "changed_pages"
                        ? "activityChangedPages"
                        : wiki && key === "count"
                          ? "activityChangedPages"
                          : entry.targetType === "knowledge_task" &&
                              key === "total"
                            ? "activityDocumentScope"
                            : key === "count"
                              ? "activityCount"
                              : "activityTotal",
                      {
                        count,
                      },
                    ),
                  ]
                : [];
            });
            if (maintenance && meta.scope === "existing_wiki") {
              counts.unshift(t("activityExistingWikiScope"));
            }
            const date = entry.createdAt
              ? new Date(entry.createdAt)
              : undefined;
            const actor = entry.actorUserId?.startsWith("local:")
              ? t("activityLocalUser")
              : entry.actorUserId ||
                (meta.trigger === "system"
                  ? t("activitySystem")
                  : t("activityActorUnknown"));
            return (
              <TableRow key={entry.id}>
                <TableCell className="type-caption text-content-secondary">
                  {date && !Number.isNaN(date.getTime()) ? (
                    <>
                      <span className="block">{date.toLocaleDateString()}</span>
                      <span className="block tabular-nums">
                        {date.toLocaleTimeString()}
                      </span>
                    </>
                  ) : (
                    "—"
                  )}
                </TableCell>
                <TableCell>{actionLabel(entry.action ?? "")}</TableCell>
                <TableCell className="break-words">
                  <span>{title}</span>
                  {counts.length > 0 && (
                    <span className="type-caption text-content-secondary mt-1 block">
                      {counts.join(" · ")}
                    </span>
                  )}
                  {(maintenance || entry.targetType === "knowledge_task") &&
                    entry.targetId &&
                    ["success", "failed", "partial", "canceled"].includes(
                      entry.outcome ?? "",
                    ) &&
                    typeof meta.attempt === "number" &&
                    Number.isSafeInteger(meta.attempt) &&
                    meta.attempt > 0 && (
                      <KnowledgeActivityTaskResult
                        resource={library.ref}
                        jobId={entry.targetId}
                        attempt={meta.attempt}
                      />
                    )}
                </TableCell>
                <TableCell className="break-words">{actor}</TableCell>
                <TableCell>
                  <Badge
                    variant={
                      entry.outcome === "success"
                        ? "success"
                        : entry.outcome === "failed" ||
                            entry.outcome === "denied"
                          ? "destructive"
                          : entry.outcome === "partial"
                            ? "warning"
                            : "secondary"
                    }
                  >
                    {outcomeLabel(entry.outcome ?? "")}
                  </Badge>
                </TableCell>
              </TableRow>
            );
          })}
          {!rows.length && !query.error && (
            <TableRow>
              <TableCell
                colSpan={5}
                className="text-content-secondary py-8 text-center"
              >
                {t(query.isPending ? "activityLoading" : "activityEmpty")}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      {query.hasNextPage && (
        <Button
          type="button"
          variant="text"
          className="justify-self-center"
          disabled={query.isFetchingNextPage || query.isFetchNextPageError}
          onClick={() => void query.fetchNextPage()}
        >
          <ChevronDown aria-hidden="true" />
          {t(query.isFetchingNextPage ? "activityLoading" : "activityLoadMore")}
        </Button>
      )}
    </section>
  );
}
