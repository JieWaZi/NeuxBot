import { lazy, Suspense, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  KnowledgeWikiContentLoading,
  KnowledgeWikiDirectoryLoading,
} from "./knowledge-loading";
import { cn } from "@/shared/lib/cn";
import { useTheme } from "@/shared/theme";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { knowledgeKeys } from "./knowledge-queries";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import { WikiTime } from "./knowledge-wiki-time";
import type { ResourceRef } from "./knowledge-transfer";
import {
  wikiRevisionOptions,
  type WikiPage,
  type WikiRevision,
} from "./knowledge-wiki-queries";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";
import { KnowledgeWikiRestore } from "./knowledge-wiki-restore";
import { useWikiCopy } from "./use-wiki-copy";

const TextDiffView = lazy(() =>
  import("@/shared/ui/text-diff-view").then((module) => ({
    default: module.TextDiffView,
  })),
);
type ViewMode = "incremental" | "cumulative" | "raw";

export function KnowledgeWikiHistory({
  resource,
  page,
  writable,
  structureVersion,
  commands,
  commandKey,
  onClose,
  onRestored,
}: {
  resource: ResourceRef;
  page: WikiPage;
  writable: boolean;
  structureVersion: number;
  commands: KnowledgeWikiCommands;
  commandKey: string;
  onClose: () => void;
  onRestored: () => void;
}) {
  const t = useWikiCopy();
  const active = useRouteSceneActive();
  const adapter = knowledgeAdapter(resource);
  const [version, setVersion] = useState<number>();
  const [mode, setMode] = useState<ViewMode>("incremental");
  const [restoreVersion, setRestoreVersion] = useState(
    () => commands.get(commandKey)?.revisionVersion,
  );
  const revisions = useInfiniteQuery({
    queryKey: [
      ...knowledgeKeys.resource(resource),
      "wiki",
      "revisions",
      page.id,
      page.slug ?? "",
    ],
    initialPageParam: 0,
    enabled: active && Boolean(page.id),
    queryFn: async ({ pageParam, signal }) => {
      const result = (
        await adapter.listRevisions(
          withLocalErrorSurface({
            path: {
              libraryId: resource.knowledgeBaseId,
              connectionId: resource.connectionId,
              pageId: page.id ?? "",
            },
            query: { offset: pageParam, limit: 50, slug: page.slug },
            signal,
            throwOnError: true,
          }),
        )
      ).data;
      if (!result.items?.length && pageParam < (result.total ?? 0))
        throw new Error(t("incomplete"));
      return result;
    },
    getNextPageParam: (last, all) => {
      const count = all.reduce(
        (total, entry) => total + (entry.items?.length ?? 0),
        0,
      );
      return count < (last.total ?? count) ? count : undefined;
    },
  });
  const rows =
    revisions.data?.pages.flatMap((entry) => entry.items ?? []) ?? [];
  const selectedVersion = version ?? page.version ?? rows[0]?.version ?? 0;
  const current = selectedVersion === page.version;
  // 上游当前页不在历史列表；本地或重叠分页返回同版时按版本去重。
  const seen = new Set<number | undefined>([page.version]);
  const history = rows.filter((row) => {
    if (!row.version || seen.has(row.version)) return false;
    seen.add(row.version);
    return true;
  });
  const canRestore =
    writable &&
    page.pageType !== "index" &&
    !current &&
    selectedVersion > 0 &&
    selectedVersion < (page.version ?? 0);
  if (restoreVersion !== undefined)
    return (
      <KnowledgeWikiRestore
        resource={resource}
        page={page}
        revisionVersion={restoreVersion}
        structureVersion={structureVersion}
        commands={commands}
        commandKey={commandKey}
        onClose={() => setRestoreVersion(undefined)}
        onRestored={onRestored}
      />
    );
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        title={t("history")}
        description={page.title}
        size="xl"
        layout="form"
      >
        <div className="@container/history flex min-h-0 flex-1 flex-col px-6 pb-5">
          {revisions.error && (
            <KnowledgeReadFeedback
              hasData={Boolean(revisions.data)}
              state={{
                pending: false,
                error: revisions.error,
                retry: () => {
                  if (revisions.isFetchNextPageError)
                    void revisions.fetchNextPage();
                  else void revisions.refetch();
                },
              }}
            />
          )}
          <div className="grid min-h-0 flex-1 grid-rows-[minmax(8rem,30%)_minmax(0,1fr)] gap-5 @[36rem]/history:grid-cols-[12rem_minmax(0,1fr)] @[36rem]/history:grid-rows-[minmax(0,1fr)]">
            <div
              className="scrollbar-hidden min-h-0 space-y-2 overflow-y-auto"
              aria-label={t("history")}
            >
              {page.version && (
                <button
                  type="button"
                  aria-pressed={current}
                  onClick={() => {
                    setVersion(page.version);
                    setMode("incremental");
                  }}
                  className={cn(
                    "focus-visible:ring-ring hover:bg-muted min-h-control-md flex w-full flex-col items-start gap-1 rounded-md px-3 py-3 text-left outline-none focus-visible:ring-2",
                    current && "bg-selection-surface",
                  )}
                >
                  <span>
                    {t("version", { version: page.version })} ·{" "}
                    {t("currentVersion")}
                  </span>
                  <WikiTime value={page.updatedAt} />
                </button>
              )}
              {revisions.isPending ? (
                <KnowledgeWikiDirectoryLoading />
              ) : (
                history.map((entry) => (
                  <button
                    type="button"
                    key={entry.version}
                    aria-pressed={selectedVersion === entry.version}
                    onClick={() => setVersion(entry.version)}
                    className={cn(
                      "focus-visible:ring-ring hover:bg-muted min-h-control-md flex w-full flex-col items-start gap-1 rounded-md px-3 py-3 text-left outline-none focus-visible:ring-2",
                      selectedVersion === entry.version &&
                        "bg-selection-surface",
                    )}
                  >
                    <span>{t("version", { version: entry.version ?? 0 })}</span>
                    <span className="type-caption text-content-secondary">
                      {t(
                        entry.editSource === "user"
                          ? "userEdit"
                          : entry.editSource === "pipeline"
                            ? "pipelineEdit"
                            : entry.editSource === "revert"
                              ? "restoreEdit"
                              : entry.editSource === "agent"
                                ? "agentEdit"
                                : "unknownEdit",
                      )}
                    </span>
                    <WikiTime value={entry.editedAt} />
                  </button>
                ))
              )}
              {revisions.hasNextPage && (
                <Button
                  variant="secondary"
                  className="w-full"
                  disabled={
                    revisions.isFetchingNextPage ||
                    revisions.isFetchNextPageError
                  }
                  onClick={() => {
                    void revisions.fetchNextPage();
                  }}
                >
                  {t("loadMore")}
                </Button>
              )}
              {!revisions.isPending && !revisions.error && !history.length && (
                <p className="type-body text-content-secondary">
                  {t("historyEmpty")}
                </p>
              )}
            </div>
            <KnowledgeWikiRevision
              resource={resource}
              page={page}
              version={selectedVersion}
              rows={rows}
              mode={current && mode === "cumulative" ? "incremental" : mode}
              onMode={setMode}
              canRestore={canRestore}
              onRestore={() => setRestoreVersion(selectedVersion)}
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function KnowledgeWikiRevision({
  resource,
  page,
  version,
  rows,
  mode,
  onMode,
  canRestore,
  onRestore,
}: {
  resource: ResourceRef;
  page: WikiPage;
  version: number;
  rows: WikiRevision[];
  mode: ViewMode;
  onMode: (mode: ViewMode) => void;
  canRestore: boolean;
  onRestore: () => void;
}) {
  const t = useWikiCopy();
  const active = useRouteSceneActive();
  const { resolvedTheme } = useTheme();
  const local = knowledgeAdapter(resource).revisionsIncludeContent;
  const current = version === page.version;
  const selected = useQuery({
    ...wikiRevisionOptions(
      resource,
      page,
      version,
      local ? rows.find((row) => row.version === version) : undefined,
    ),
    enabled: active && version > 0 && !current,
  });
  const previousVersion = version - 1;
  const previous = useQuery({
    ...wikiRevisionOptions(
      resource,
      page,
      previousVersion,
      local ? rows.find((row) => row.version === previousVersion) : undefined,
    ),
    enabled: active && mode === "incremental" && previousVersion > 0,
  });
  const snapshot = current ? page : selected.data;
  const before =
    mode === "cumulative" ? snapshot : previousVersion > 0 ? previous.data : {};
  const after = mode === "cumulative" ? page : snapshot;
  const missingPrevious =
    mode === "incremental" && previousVersion > 0 && previous.data === null;
  const error =
    selected.error ?? (mode === "incremental" ? previous.error : null);
  const pending =
    (!current && selected.isPending) ||
    (mode === "incremental" && previousVersion > 0 && previous.isPending);
  const fields = ["title", "summary", "content"] as const;
  const changed = fields.filter(
    (field) => (before?.[field] ?? "") !== (after?.[field] ?? ""),
  );
  const fromVersion = mode === "cumulative" ? version : previousVersion;
  const toVersion = mode === "cumulative" ? (page.version ?? 0) : version;
  return (
    <div className="flex min-h-0 min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SegmentedControl
          ariaLabel={t("revisionView")}
          value={mode}
          onValueChange={onMode}
          options={[
            { value: "incremental", label: t("incremental") },
            ...(!current
              ? [{ value: "cumulative" as const, label: t("cumulative") }]
              : []),
            { value: "raw", label: t("raw") },
          ]}
        />
        {canRestore && snapshot && !selected.error && (
          <Button variant="secondary" onClick={onRestore}>
            {t("restoreVersion")}
          </Button>
        )}
      </div>
      <div className="scrollbar-hidden min-h-0 flex-1 overflow-auto">
        {error ? (
          <KnowledgeReadFeedback
            hasData={Boolean(snapshot)}
            state={{
              pending: false,
              error,
              retry: () => {
                if (selected.error) void selected.refetch();
                if (mode === "incremental" && previous.error)
                  void previous.refetch();
              },
            }}
          />
        ) : pending ? (
          <KnowledgeWikiContentLoading />
        ) : !snapshot ? (
          <p className="type-body text-content-secondary">
            {t("snapshotMissing")}
          </p>
        ) : mode === "raw" ? (
          <pre
            aria-label={t("rawContent")}
            className="type-body font-mono break-words whitespace-pre-wrap"
          >
            {[snapshot.title, snapshot.summary, snapshot.content]
              .filter(Boolean)
              .join("\n\n") || t("noContent")}
          </pre>
        ) : (
          <div className="space-y-4">
            <p className="type-body text-content-secondary">
              {missingPrevious
                ? t("previousMissing", { version: previousVersion })
                : fromVersion < 1
                  ? t("initialCreation")
                  : t("versionRange", { from: fromVersion, to: toVersion })}
            </p>
            {!changed.length ? (
              <p className="type-body text-content-secondary">
                {t("noDifference")}
              </p>
            ) : (
              <Suspense fallback={<KnowledgeWikiContentLoading />}>
                {changed.map((field) => (
                  <section
                    key={field}
                    aria-label={t(field)}
                    className="min-w-0 space-y-2"
                  >
                    <h3 className="type-body font-medium">{t(field)}</h3>
                    <TextDiffView
                      beforeContent={before?.[field] ?? ""}
                      afterContent={after?.[field] ?? ""}
                      beforeLabel={
                        missingPrevious || fromVersion < 1
                          ? t("emptyContent")
                          : t("version", { version: fromVersion })
                      }
                      afterLabel={t("version", { version: toVersion })}
                      language={field === "content" ? "markdown" : "text"}
                      theme={resolvedTheme}
                    />
                  </section>
                ))}
              </Suspense>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
